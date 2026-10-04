// Package scheduler 是自动备份的定时执行器。
package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/qiansi/app/internal/backup"
	"github.com/qiansi/app/internal/config"
	"github.com/qiansi/app/internal/store"
)

// Schedule 是 backup.Schedule 的别名，方便调用方只 import 本包即可拼装配置。
type Schedule = backup.Schedule

// settings 键名。配置整体存一条 JSON，状态分散存以便排查。
const (
	keySchedule = "backup_schedule"   // Schedule 的 JSON
	keyLastRun  = "backup_last_run"   // RFC3339
	keyNextRun  = "backup_next_run"   // RFC3339，调度器算出的下次执行时刻
	keyLastFail = "backup_last_error" // 最近一次失败原因（成功时清空）
	keyLastPath = "backup_last_path"  // 最近一次归档落盘路径
)

// tickInterval 是轮询间隔。备份精度到分钟级，30 秒轮询足够，
// 且比"算准下次时间再 Sleep"更容易处理配置变更与系统休眠后的追补。
const tickInterval = 30 * time.Second

// Runner 执行自动备份。它是 API 层与 main 之间唯一的耦合点：
// API 只负责读写 Schedule 与查询状态，实际执行交给 Runner。
type Runner struct {
	Store *store.Store
	DB    *sql.DB
	Cfg   *config.Config

	mu sync.Mutex // 串行化 Run，避免手动触发与定时触发并发写同一目录
}

// New 构造 Runner。
func New(st *store.Store, db *sql.DB, cfg *config.Config) *Runner {
	return &Runner{Store: st, DB: db, Cfg: cfg}
}

// LoadSchedule 读取当前配置；无配置时返回默认（每日 04:00、保留 7 份）。
func (r *Runner) LoadSchedule(ctx context.Context) backup.Schedule {
	raw, err := r.Store.SettingGet(ctx, keySchedule)
	if err != nil {
		return backup.DefaultSchedule()
	}
	return backup.ParseSchedule(raw)
}

// SaveSchedule 持久化配置（整体覆盖一条 JSON）。
//
// 同时清掉已排期的下次执行时刻：周期或时间点变了之后，旧排期已失效，
// 若不清除，设置页会继续显示按旧配置算出的时间（乃至"今天 04:00"这种明显错值）。
// 清空后由下一次 Status/tick 按新配置现算。
func (r *Runner) SaveSchedule(ctx context.Context, s backup.Schedule) error {
	s = s.Normalize()
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := r.Store.SettingSet(ctx, keySchedule, string(b)); err != nil {
		return err
	}
	_ = r.Store.SettingSet(ctx, keyNextRun, "")
	return nil
}

// Status 是给设置页看的执行状态。
type Status struct {
	Schedule   backup.Schedule `json:"schedule"`
	Enabled    bool            `json:"enabled"`
	Label      string          `json:"label"`       // 周期中文描述
	NextRun    string          `json:"next_run"`    // RFC3339，空串表示不排期
	NextRunIn  string          `json:"next_run_in"` // 人类可读的倒计时
	NextHint   string          `json:"next_hint"`   // 例如"每天 04:00 执行"
	LastRun    string          `json:"last_run"`    // RFC3339，从未执行过则空串
	LastPath   string          `json:"last_path"`
	LastError  string          `json:"last_error"`
	Keep       int             `json:"keep"`
	BackupsDir string          `json:"backups_dir"`
}

// Status 汇总当前配置与执行状态。now 便于测试注入。
func (r *Runner) Status(ctx context.Context, now time.Time) Status {
	s := r.LoadSchedule(ctx)
	st := Status{
		Schedule:   s,
		Enabled:    s.Enabled,
		Label:      s.Describe(),
		Keep:       s.Keep,
		BackupsDir: r.Cfg.Backups,
		LastError:  r.get(ctx, keyLastFail),
		LastPath:   r.get(ctx, keyLastPath),
	}
	if last := r.get(ctx, keyLastRun); last != "" {
		if t, err := time.Parse(time.RFC3339, last); err == nil {
			st.LastRun = t.Format(time.RFC3339)
		}
	}
	// 下次执行时间：优先用调度器已排定的值；没有则以"上次执行"为基准现算。
	next := r.get(ctx, keyNextRun)
	nextTime := parseNext(next)
	if nextTime.IsZero() {
		if !s.Enabled {
			return st
		}
		if st.LastRun != "" {
			if lastT, err := time.Parse(time.RFC3339, st.LastRun); err == nil {
				nextTime = s.DueAfter(lastT)
			}
		}
		if nextTime.IsZero() {
			// 从未执行过：只展示未来排期，不假装已经跑过
			nextTime = s.DueAfter(now)
		}
	}
	if !s.Enabled && nextTime.IsZero() {
		return st
	}
	st.NextRun = nextTime.Format(time.RFC3339)
	st.NextHint = s.DescribeNextRun(nextTime)
	st.NextRunIn = humanizeDuration(now, nextTime)
	return st
}

func parseNext(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func humanizeDuration(now, next time.Time) string {
	if next.IsZero() {
		return ""
	}
	d := next.Sub(now)
	if d < 0 {
		return "即将执行"
	}
	switch {
	case d < time.Minute:
		return "不到 1 分钟"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟后", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) - h*60
		if m == 0 {
			return fmt.Sprintf("%d 小时后", h)
		}
		return fmt.Sprintf("%d 小时 %d 分后", h, m)
	default:
		days := int(d.Hours()) / 24
		return fmt.Sprintf("%d 天后", days)
	}
}

// RunNow 立即执行一次备份，无论是否到点。设置页的"立即执行"与测试都走这里。
// 保留份数按当前配置生效。
func (r *Runner) RunNow(ctx context.Context, now time.Time) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.LoadSchedule(ctx)
	return r.execute(ctx, s, now)
}

// execute 做一次归档 + 清理 + 记账。调用方需持有锁。
func (r *Runner) execute(ctx context.Context, s backup.Schedule, now time.Time) (string, error) {
	path, err := backup.Archive(ctx, r.DB, r.Cfg.DBPath, r.Cfg.Backups)
	if err != nil {
		msg := err.Error()
		log.Printf("[backup] 自动备份失败（%s）: %v", s.Describe(), err)
		// 失败原因落库，设置页直接展示；同时清空上次的成功路径，避免误导
		_ = r.Store.SettingSet(ctx, keyLastFail, msg)
		_ = r.Store.SettingSet(ctx, keyLastPath, "")
		// 失败后不推进 next_run：本轮已到点，保持原值会让倒计时停留在过去，
		// 下一次 tick 会立即重试（每次最多一次，符合"到点补一次"的预期）。
		return "", err
	}
	if err := backup.Prune(r.Cfg.Backups, s.Keep); err != nil {
		// 清理失败不影响本次备份成果，只记日志
		log.Printf("[backup] 清理旧归档失败: %v", err)
	}
	// 成功即清空错误信息，并按本次执行时刻排下次时间
	_ = r.Store.SettingSet(ctx, keyLastRun, now.Format(time.RFC3339))
	_ = r.Store.SettingSet(ctx, keyLastPath, path)
	_ = r.Store.SettingSet(ctx, keyLastFail, "")
	_ = r.Store.SettingSet(ctx, keyNextRun, s.DueAfter(now).Format(time.RFC3339))
	log.Printf("[backup] 自动备份完成: %s（保留最近 %d 份）", path, s.Keep)
	return path, nil
}

// Run 是常驻循环，直到 ctx 取消。
//
// 每轮逻辑：配置关闭则跳过；否则算出 next，若 now >= next 就执行一次。
// 停机期间错过的周期不会密集补跑 —— NextRun 直接给出下一个未来时刻，
// 因此重启后至多立即执行一次。
func (r *Runner) Run(ctx context.Context) {
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	r.tick(ctx, time.Now()) // 启动即评估一次
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.tick(ctx, time.Now())
		}
	}
}

// tick 是调度的判定核心。
//
// 到点判定不看已排期的 backup_next_run，而是每次用 DueAfter 从"上次执行"现算：
//   - last_run 存在 → due = DueAfter(last)，now >= due 即执行
//   - 从未执行过 → 以 now 为锚点排一次未来（不立刻补跑，避免每次重启都生成一份）
//
// 这样停机期间错过的周期会在启动后补一次，且只补一次（执行后 last_run 前进到 now）。
func (r *Runner) tick(ctx context.Context, now time.Time) {
	s := r.LoadSchedule(ctx)
	last := parseNext(r.get(ctx, keyLastRun))

	if !s.Enabled {
		// 关闭：清掉排期，避免下次开启时按一个过期的时刻立刻触发
		if r.get(ctx, keyNextRun) != "" {
			_ = r.Store.SettingSet(ctx, keyNextRun, "")
		}
		return
	}
	if last.IsZero() {
		// 首次启用：只排期，不补跑
		next := s.DueAfter(now)
		_ = r.Store.SettingSet(ctx, keyNextRun, next.Format(time.RFC3339))
		return
	}
	if now.Before(s.DueAfter(last)) {
		return
	}
	// 到点执行。execute 内部会记账并把 next_run 推到未来；
	// 失败时不推进 last_run，下一个 tick 会重试（同一次 tick 只试一次，不会打转）。
	_, _ = r.execute(ctx, s, now)
}

func (r *Runner) get(ctx context.Context, key string) string {
	v, err := r.Store.SettingGet(ctx, key)
	if err != nil {
		return ""
	}
	return v
}
