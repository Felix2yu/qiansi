package backup

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// 备份周期的三种取值。
const (
	FreqDaily  = "daily"  // 每天在指定时间点执行一次
	FreqWeekly = "weekly" // 每周指定星期几的指定时间点
	FreqCustom = "custom" // 每隔 N 分钟执行一次（忽略时间点）
)

// FrequencyLabel 供接口回显时给出中文名，前端直接展示。
var FrequencyLabel = map[string]string{
	FreqDaily:  "每日",
	FreqWeekly: "每周",
	FreqCustom: "自定义间隔",
}

// DefaultSchedule 是未配置时的兜底：每日 04:00、保留 7 份。
// 与旧行为一致 —— 迁移前 notify 调度器就是每天 04:00 归档一次、保留 7 份。
func DefaultSchedule() Schedule {
	return Schedule{
		Enabled:   true,
		Frequency: FreqDaily,
		At:        "04:00",
		Weekday:   0,
		EveryMin:  1440,
		Keep:      7,
	}
}

// Schedule 是自动备份配置。序列化后存进 settings 表的 backup_schedule 键（JSON），
// 整体读写，避免多个 key 半更新导致周期与时间点不一致。
type Schedule struct {
	Enabled   bool   `json:"enabled"`
	Frequency string `json:"frequency"`
	At        string `json:"at"`       // "HH:MM"，daily/weekly 生效
	Weekday   int    `json:"weekday"`  // 0=周日 … 6=周六，weekly 生效
	EveryMin  int    `json:"every_min"`// custom 生效
	Keep      int    `json:"keep"`     // 归档保留份数
}

// Normalize 修正非法输入：未知周期回落到每日，时间格式非法回落到 04:00，
// 间隔与保留份数给出上下界。API 入口与调度器启动都会走一遍，保证内存里的一定可用。
func (s Schedule) Normalize() Schedule {
	if s.Frequency != FreqDaily && s.Frequency != FreqWeekly && s.Frequency != FreqCustom {
		s.Frequency = FreqDaily
	}
	if _, err := time.Parse("15:04", s.At); err != nil {
		s.At = "04:00"
	}
	if s.Weekday < 0 || s.Weekday > 6 {
		s.Weekday = 0
	}
	if s.EveryMin < 1 {
		s.EveryMin = 1440
	}
	if s.EveryMin > 60*24*30 { // 上限 30 天，防止误填成"永不执行"
		s.EveryMin = 60 * 24 * 30
	}
	if s.Keep < 1 {
		s.Keep = 7
	}
	if s.Keep > 200 {
		s.Keep = 200
	}
	return s
}

// Clock 返回该配置在 daily/weekly 模式下当天的执行时刻，custom 模式返回 false。
func (s Schedule) clock() (time.Time, bool, error) {
	if s.Frequency == FreqCustom {
		return time.Time{}, false, nil
	}
	t, err := time.Parse("15:04", s.At)
	if err != nil {
		return time.Time{}, false, err
	}
	return t, true, nil
}

// NextRun 计算严格晚于 after 的下一次执行时间。
// after 传上次执行时间；从未执行过时传零值，此时以 now 为基准。
//
// 三种周期：
//   - daily：今天该时间点若还没到就用今天，否则顺延到明天
//   - weekly：向后找最近的目标星期（0-6），同一天只算一次
//   - custom：上次时间 + EveryMin 分钟；间隔明显短于 since 时（服务长时间未运行）
//     按步进补齐到第一个未来时刻，避免"重启后要等一整个间隔才补跑"
func (s Schedule) NextRun(now, after time.Time) time.Time {
	s = s.Normalize()
	if s.Frequency == FreqCustom {
		base := after
		if base.IsZero() {
			base = now
		}
		next := base.Add(time.Duration(s.EveryMin) * time.Minute)
		if !next.After(now) {
			missed := now.Sub(base)
			steps := missed / (time.Duration(s.EveryMin) * time.Minute)
			next = base.Add((steps + 1) * time.Duration(s.EveryMin) * time.Minute)
		}
		return next
	}
	clock, _, err := s.clock()
	if err != nil {
		clock, _ = time.Parse("15:04", "04:00")
	}
	ref := after
	if ref.IsZero() || !ref.After(now) {
		// 从未执行过，或上次执行时间已是过去（服务停机期间错过了周期）：
		// 以 now 为锚点找下一个候选，再叠加周维度。
		ref = now
	}
	base := time.Date(ref.Year(), ref.Month(), ref.Day(), clock.Hour(), clock.Minute(), 0, 0, ref.Location())
	if !base.After(ref) {
		base = base.AddDate(0, 0, 1)
	}
	if s.Frequency == FreqDaily {
		return base
	}
	// weekly：向后推进到目标星期（weekday 已归一到 0-6）
	for delta := 0; delta < 8; delta++ {
		cand := base.AddDate(0, 0, delta)
		if int(cand.Weekday()) == s.Weekday {
			return cand
		}
	}
	return base
}

// DueAfter 返回「紧随 after 之后的那次执行时刻」，不做任何跳过。
//
// 与 NextRun 的区别是本方法不推进到未来：它回答的是"按配置，after 之后本该什么时候跑"，
// 因此调用方可以直接与 now 比较判断是否到点。NextRun 回答的是"下次什么时候跑"，
// 会跳过已错过的周期（停机 5 小时、间隔 10 分钟时它返回 10 分钟后的那一刻，而不是立即执行）。
func (s Schedule) DueAfter(after time.Time) time.Time {
	s = s.Normalize()
	if s.Frequency == FreqCustom {
		return after.Add(time.Duration(s.EveryMin) * time.Minute)
	}
	clock, _, err := s.clock()
	if err != nil {
		clock, _ = time.Parse("15:04", "04:00")
	}
	base := time.Date(after.Year(), after.Month(), after.Day(), clock.Hour(), clock.Minute(), 0, 0, after.Location())
	if !base.After(after) {
		base = base.AddDate(0, 0, 1)
	}
	if s.Frequency == FreqDaily {
		return base
	}
	for delta := 0; delta < 8; delta++ {
		cand := base.AddDate(0, 0, delta)
		if int(cand.Weekday()) == s.Weekday {
			return cand
		}
	}
	return base
}

// DescribeNextRun 生成给界面看的中文说明。
func (s Schedule) DescribeNextRun(next time.Time) string {
	if s.Frequency == FreqCustom {
		return fmt.Sprintf("每 %s 执行一次", humanDuration(s.EveryMin))
	}
	name := "每日"
	if s.Frequency == FreqWeekly {
		name = "每周" + WeekdayName(s.Weekday)
	}
	return fmt.Sprintf("%s %s 执行", name, next.Format("15:04"))
}

// WeekdayName 星期中文化。
func WeekdayName(w int) string {
	names := []string{"日", "一", "二", "三", "四", "五", "六"}
	if w < 0 || w >= len(names) {
		return "日"
	}
	return names[w]
}

// humanDuration 把分钟数说成人话，用于"每 X 分钟/小时/天"这类描述。
func humanDuration(min int) string {
	switch {
	case min < 60:
		return fmt.Sprintf("%d 分钟", min)
	case min%1440 == 0:
		return fmt.Sprintf("%d 天", min/1440)
	case min%60 == 0:
		return fmt.Sprintf("%d 小时", min/60)
	default:
		return fmt.Sprintf("%d 小时 %d 分钟", min/60, min%60)
	}
}

// Describe 描述当前生效的周期文案（不含时间点），用于设置页回显。
func (s Schedule) Describe() string {
	s = s.Normalize()
	switch s.Frequency {
	case FreqWeekly:
		return fmt.Sprintf("每周%s %s", WeekdayName(s.Weekday), s.At)
	case FreqCustom:
		return "每 " + humanDuration(s.EveryMin) + "一次"
	default:
		return "每天 " + s.At
	}
}

// ParseSchedule 从 settings 表读出的 JSON 还原配置。
// 读失败或为空时返回默认值 —— 单机自托管场景下宁可退回"每天 04:00"，
// 也不能因为一条脏数据就彻底不备份。
func ParseSchedule(raw string) Schedule {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultSchedule()
	}
	var s Schedule
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return DefaultSchedule()
	}
	return s.Normalize()
}
