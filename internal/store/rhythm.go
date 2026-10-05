package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// 联系节奏（N5）。
//
// 「多久该联系一次」按亲密度分级：♥×5、♥×4 是家人与挚友，默认 7 天一联系；
// ♥×3、♥×2 常来往，30 天；♥×1 与未分级算浅交，90 天。三档是默认值，
// 每一级都能单独改——有人把岳父母记成 4 级、把发小记成 5 级，
// 节奏不该被档位绑死。
//
// 配置存在 settings 的一个 JSON 键里，不建表：它是一台机器上的偏好，
// 和自动备份的 Schedule 同一种东西。

const rhythmKey = "contact_rhythm"

// rhythmEnabledKey 是「要不要按节奏催」的总开关。它和 apprise_urls 同一种东西
// ——一台机器上的偏好，所以直接躺在 settings 表里，设置页读 GET /settings 就能拿到，
// 不另开端点。
const rhythmEnabledKey = "contact_rhythm_enabled"

// MaxRhythmDays 一格最多排到十年后，再大就是把提醒关掉了，不如直接填个更大的档。
const MaxRhythmDays = 3650

// RhythmTier 一个亲密度等级对应的「多久该联系」（天）。
type RhythmTier struct {
	Grade int `json:"grade"`
	Days  int `json:"days"`
}

// 0 = 未设置亲密度，与 1 级同样按浅交对待。
var defaultRhythm = map[int]int{5: 7, 4: 7, 3: 30, 2: 30, 1: 90, 0: 90}

// rhythmGrades 由亲到疏：前端照这个顺序排，不用再排一次。
var rhythmGrades = []int{5, 4, 3, 2, 1, 0}

// normalizeRhythm 把任意来路的配置收成 6 条、每级都有值。
// 坏 JSON、缺级、越界天数都回落默认，绝不让提醒引擎因为一条脏配置整个哑掉。
func normalizeRhythm(raw map[int]int) map[int]int {
	out := map[int]int{}
	for _, g := range rhythmGrades {
		d, ok := raw[g]
		if !ok || d <= 0 {
			d = defaultRhythm[g]
		}
		if d > MaxRhythmDays {
			d = MaxRhythmDays
		}
		out[g] = d
	}
	return out
}

// ContactRhythm 读当前节奏表，按 ♥×5 → 未分级 排列。没配过时返回默认值。
func (s *Store) ContactRhythm(ctx context.Context) ([]RhythmTier, error) {
	raw, err := s.SettingGet(ctx, rhythmKey)
	if err != nil {
		return nil, err
	}
	days := map[int]int{}
	if raw != "" {
		var tiers []RhythmTier
		if err := json.Unmarshal([]byte(raw), &tiers); err != nil {
			days = map[int]int{} // 读不懂就当没配过，下面回落默认
		}
		for _, t := range tiers {
			days[t.Grade] = t.Days
		}
	}
	return rhythmTiers(normalizeRhythm(days)), nil
}

// ContactRhythmSet 存节奏表。只认 0..5 六个等级，其余丢掉。
func (s *Store) ContactRhythmSet(ctx context.Context, tiers []RhythmTier) error {
	days := map[int]int{}
	for _, t := range tiers {
		if _, ok := defaultRhythm[t.Grade]; !ok {
			continue
		}
		days[t.Grade] = t.Days
	}
	blob, err := json.Marshal(rhythmTiers(normalizeRhythm(days)))
	if err != nil {
		return err
	}
	return s.SettingSet(ctx, rhythmKey, string(blob))
}

// ContactRhythmReset 恢复三档默认节奏。
func (s *Store) ContactRhythmReset(ctx context.Context) error {
	return s.ContactRhythmSet(ctx, nil)
}

// ContactRhythmEnabled 总开关。没配过算开着——老库升上来行为不变；
// 值读不懂也按开着处理，跟 normalizeRhythm 一个脾气：一条脏配置不该让整个提醒哑掉。
func (s *Store) ContactRhythmEnabled(ctx context.Context) (bool, error) {
	raw, err := s.SettingGet(ctx, rhythmEnabledKey)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "0", "false", "off", "no":
		return false, nil
	default:
		return true, nil
	}
}

func rhythmTiers(days map[int]int) []RhythmTier {
	out := make([]RhythmTier, 0, len(rhythmGrades))
	for _, g := range rhythmGrades {
		out = append(out, RhythmTier{Grade: g, Days: days[g]})
	}
	return out
}

// rhythmDays 取某一级的节奏。people.grade 在库里没有约束，脏值（7 级、负数）
// 按未分级那一档算——落到 0 就等于这个人一建档已经逾期，天天催。
func rhythmDays(days map[int]int, grade int) int {
	if grade < 0 || grade > 5 {
		grade = 0
	}
	if d, ok := days[grade]; ok && d > 0 {
		return d
	}
	return defaultRhythm[0]
}

// DriftPerson 渐远名单上的一行。
type DriftPerson struct {
	PersonID    string `json:"person_id"`
	Name        string `json:"name"`
	Grade       int    `json:"grade"`
	LastContact string `json:"last_contact,omitempty"` // 从没记过就是空
	Anchor      string `json:"anchor"`                 // 参与计算的那天：有接触用接触，否则用建档日
	AnchorFrom  string `json:"anchor_from"`            // contact | created
	Days        int    `json:"days"`                   // 这一级的节奏
	DueAt       string `json:"due_at"`                 // 下一次该联系的日子
	DaysSince   int    `json:"days_since"`             // 距锚点多少天
	OverdueDays int    `json:"overdue_days"`           // 已逾期几天，未逾期为 0
	Overdue     bool   `json:"overdue"`
}

type DriftFilter struct {
	OnlyOverdue bool
	Limit       int
	Offset      int
}

// driftContactCTE 最近一次接触的口径：往来、对话、金钱与「已联系过」打卡取最大日。
// 与 PersonStatsFor 的 contact 段一致，多出来的是 contact_checkins ——
// 用户点了「已联系过」却没记一笔，也该把下一次到期往后推。
const driftContactCTE = `WITH contact AS (
  SELECT person_id, MAX(d) AS d FROM (
    SELECT person_id, substr(occurred_at,1,10) AS d FROM live_transactions
    UNION ALL SELECT person_id, substr(said_at,1,10) FROM live_memos
    UNION ALL SELECT ep.person_id, substr(e.event_date,1,10) FROM event_participants ep JOIN live_events e ON e.id=ep.event_id
    UNION ALL SELECT person_id, day FROM contact_checkins
  ) WHERE d <> '' GROUP BY person_id)`

// DriftList 按各自的节奏算出「谁该联系了」。
//
// 从没记过任何往来的人用建档日起算，否则新导入的通讯录会在第一天就把名单刷屏。
// 归档的人不算：收进档案袋的关系不该再催。
func (s *Store) DriftList(ctx context.Context, f DriftFilter) ([]*DriftPerson, error) {
	rhythm, err := s.ContactRhythm(ctx)
	if err != nil {
		return nil, err
	}
	days := map[int]int{}
	for _, t := range rhythm {
		days[t.Grade] = t.Days
	}
	rows, err := s.DB.QueryContext(ctx, driftContactCTE+`
SELECT p.id,p.name,p.grade,COALESCE(contact.d,''),substr(p.created_at,1,10)
FROM live_people p LEFT JOIN contact ON contact.person_id=p.id
WHERE p.archived=0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type row struct {
		id, name, last, created string
		grade                   int
	}
	var found []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.name, &r.grade, &r.last, &r.created); err != nil {
			return nil, err
		}
		found = append(found, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	today := time.Now()
	list := []*DriftPerson{}
	for _, r := range found {
		anchor, from := r.last, "contact"
		if anchor == "" {
			anchor, from = r.created, "created"
		}
		base, ok := parseLocalDay(anchor)
		if !ok {
			continue // 日期读不懂就别催，宁可少提醒也不要催错
		}
		interval := rhythmDays(days, r.grade)
		due := base.AddDate(0, 0, interval)
		d := &DriftPerson{
			PersonID:    r.id,
			Name:        r.name,
			Grade:       r.grade,
			LastContact: r.last,
			Anchor:      anchor,
			AnchorFrom:  from,
			Days:        interval,
			DueAt:       due.Format(dateFormat),
			DaysSince:   int(daytrunc(today).Sub(daytrunc(base)).Hours() / 24),
		}
		diff := int(daytrunc(due).Sub(daytrunc(today)).Hours() / 24)
		if diff <= 0 {
			d.Overdue = true
			d.OverdueDays = -diff
		}
		if f.OnlyOverdue && !d.Overdue {
			continue
		}
		list = append(list, d)
	}
	// 逾期的排前面、逾期越久越前；没到期的按下一次到期日从近到远
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Overdue != list[j].Overdue {
			return list[i].Overdue
		}
		if list[i].Overdue {
			return list[i].OverdueDays > list[j].OverdueDays
		}
		return list[i].DueAt < list[j].DueAt
	})
	if f.Limit > 0 {
		if f.Offset >= len(list) {
			return []*DriftPerson{}, nil
		}
		end := f.Offset + f.Limit
		if end > len(list) {
			end = len(list)
		}
		list = list[f.Offset:end]
	}
	return list, nil
}

// ContactUpcoming 把已经逾期的人派生成待办。
//
// 与纪念日、承诺一样是实时派生、不落库：勾掉等于记一次「已联系过」打卡，
// 锚点前移，下一轮到期日自然推后，不需要额外的 dismiss 表。
// 还没到那天的人不进待办——提前催只会把待办页变成倒计时。
func (s *Store) ContactUpcoming(ctx context.Context) ([]*Reminder, error) {
	// 总开关关掉：待办和每日推送都不再出「该联系 X 了」。
	// 渐远名单不受影响——那是「谁很久没联系了」的查看工具，跟催不催是两回事。
	on, err := s.ContactRhythmEnabled(ctx)
	if err != nil {
		return nil, err
	}
	if !on {
		return []*Reminder{}, nil
	}
	list, err := s.DriftList(ctx, DriftFilter{OnlyOverdue: true})
	if err != nil {
		return nil, err
	}
	out := []*Reminder{}
	for _, d := range list {
		out = append(out, &Reminder{
			ID:         "contact:" + d.PersonID,
			PersonID:   d.PersonID,
			PersonName: d.Name,
			RefType:    "contact",
			RefID:      d.PersonID,
			Title:      "该联系 " + d.Name + " 了",
			DueAt:      d.DueAt + "T09:00:00",
			Status:     "pending",
		})
	}
	return out, nil
}

// ContactCheckin 记一次「联系过了」。同一天重复点是幂等的，
// 不然连点两次会把节奏往后挪两轮。
func (s *Store) ContactCheckin(ctx context.Context, personID string) error {
	var one int
	if err := s.DB.QueryRowContext(ctx, "SELECT 1 FROM live_people WHERE id=?", personID).Scan(&one); err != nil {
		if err == sql.ErrNoRows {
			return sql.ErrNoRows // 不存在或已进回收站：404，不写孤儿打卡
		}
		return err
	}
	_, err := s.DB.ExecContext(ctx,
		"INSERT OR IGNORE INTO contact_checkins(person_id,day,created_at) VALUES(?,?,?)",
		personID, todayLocal(), nowUTC())
	return err
}

func parseLocalDay(s string) (time.Time, bool) {
	if len(s) < 10 {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(dateFormat, s[:10], time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// daytrunc 抹掉时分秒，天数差不能带着当天几点的影响。
func daytrunc(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
