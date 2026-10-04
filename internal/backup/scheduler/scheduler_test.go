package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiansi/app/internal/config"
	"github.com/qiansi/app/internal/store"
	"github.com/qiansi/app/internal/testdb"
)

type fixture struct {
	r    *Runner
	st   *store.Store
	dir  string
	ctx  context.Context
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	db := testdb.New(t)
	cfg := &config.Config{
		DataDir: dir,
		DBPath:  filepath.Join(dir, "qiansi.db"),
		Uploads: filepath.Join(dir, "uploads"),
		Backups: filepath.Join(dir, "backups"),
	}
	// 生产路径由 config.Load() 建目录；测试直接构造 Config，需要自己补上这一步，
	// 否则归档会因 backups 不存在而失败（表现为"目录不存在"而非逻辑错误）。
	if err := os.MkdirAll(cfg.Backups, 0o755); err != nil {
		t.Fatal(err)
	}
	st := store.New(db)
	return &fixture{r: New(st, db, cfg), st: st, dir: dir, ctx: context.Background()}
}

func (f *fixture) save(t *testing.T, s Schedule) {
	t.Helper()
	if err := f.r.SaveSchedule(f.ctx, s); err != nil {
		t.Fatalf("SaveSchedule: %v", err)
	}
}

func (f *fixture) archives(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(f.r.Cfg.Backups)
	if err != nil {
		return nil
	}
	names := []string{}
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "qiansi-") {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestLoadScheduleDefaultsWhenUnset(t *testing.T) {
	f := newFixture(t)
	s := f.r.LoadSchedule(f.ctx)
	if !s.Enabled || s.Frequency != "daily" || s.At != "04:00" || s.Keep != 7 {
		t.Fatalf("未配置时应为默认（每日 04:00 / 保留 7），得到 %+v", s)
	}
}

func TestSaveSchedulePersistsAndNormalizes(t *testing.T) {
	f := newFixture(t)
	f.save(t, Schedule{Enabled: true, Frequency: "hourly", At: "99:99", Keep: 1000})
	// 重新构造 Runner 读库，确认真的落盘了（不是只改了内存）
	got := New(f.st, f.st.DB, f.r.Cfg).LoadSchedule(f.ctx)
	if got.Frequency != "daily" || got.At != "04:00" || got.Keep != 200 {
		t.Fatalf("应归一化后落盘，得到 %+v", got)
	}
}

func TestRunNowArchivesAndPrunes(t *testing.T) {
	f := newFixture(t)
	f.save(t, Schedule{Enabled: true, Frequency: "daily", At: "04:00", Keep: 2})
	// 预置 3 份旧档
	for i := 1; i <= 3; i++ {
		p := filepath.Join(f.r.Cfg.Backups, "qiansi-20200101-00000"+string(rune('0'+i))+".db")
		if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	path, err := f.r.RunNow(f.ctx, now)
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if path == "" {
		t.Fatal("RunNow 应返回落盘路径")
	}
	if names := f.archives(t); len(names) != 2 {
		t.Fatalf("keep=2 应裁剪到 2 份，得到 %v", names)
	}
	// 状态已记账
	if v, _ := f.st.SettingGet(f.ctx, keyLastRun); v == "" {
		t.Error("成功后应记录 backup_last_run")
	}
	if v, _ := f.st.SettingGet(f.ctx, keyLastPath); v != path {
		t.Errorf("backup_last_path = %q，期望 %q", v, path)
	}
	if v, _ := f.st.SettingGet(f.ctx, keyNextRun); v == "" {
		t.Error("成功后应排定下次执行时间")
	}
}

func TestRunNowRecordsFailure(t *testing.T) {
	f := newFixture(t)
	// 把 backups 目录换成普通文件 → 归档必然失败（MkdirAll 报 "not a directory"）
	if err := os.RemoveAll(f.r.Cfg.Backups); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.r.Cfg.Backups, []byte("blocked"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.save(t, Schedule{Enabled: true, Frequency: "daily", At: "04:00", Keep: 7})
	if _, err := f.r.RunNow(f.ctx, time.Now()); err == nil {
		t.Fatal("归档目录不可用时应返回错误")
	}
	msg, _ := f.st.SettingGet(f.ctx, keyLastFail)
	if msg == "" {
		t.Error("失败原因应落库供界面展示")
	}
	if p, _ := f.st.SettingGet(f.ctx, keyLastPath); p != "" {
		t.Errorf("失败后不应保留上次的成功路径，得到 %q", p)
	}
}

func TestTickRunsWhenDueAndSkipsWhenNot(t *testing.T) {
	f := newFixture(t)
	// 每 60 分钟一次，基准设为 2 小时前 → 早已到点
	base := time.Now().Add(-2 * time.Hour)
	if err := f.st.SettingSet(f.ctx, keyLastRun, base.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	f.save(t, Schedule{Enabled: true, Frequency: "custom", EveryMin: 60, Keep: 7})
	f.r.tick(f.ctx, time.Now())
	if len(f.archives(t)) != 1 {
		t.Fatalf("已到点应执行一次，得到 %v", f.archives(t))
	}
	// 立刻再 tick：next_run 已推到未来，不应重复执行
	f.r.tick(f.ctx, time.Now())
	if len(f.archives(t)) != 1 {
		t.Fatalf("未到点不应重复执行，得到 %v", f.archives(t))
	}
}

func TestTickSkipsWhenDisabled(t *testing.T) {
	f := newFixture(t)
	f.save(t, Schedule{Enabled: false, Frequency: "custom", EveryMin: 1, Keep: 7})
	f.r.tick(f.ctx, time.Now())
	if names := f.archives(t); len(names) != 0 {
		t.Fatalf("关闭时不应执行备份，得到 %v", names)
	}
}

func TestTickDoesNotBackfillMissedCycles(t *testing.T) {
	f := newFixture(t)
	// 每 10 分钟一次，但上次执行在 5 小时前 → 错过的 30 个周期不应密集补跑
	if err := f.st.SettingSet(f.ctx, keyLastRun, time.Now().Add(-5*time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	f.save(t, Schedule{Enabled: true, Frequency: "custom", EveryMin: 10, Keep: 50})
	f.r.tick(f.ctx, time.Now())
	if names := f.archives(t); len(names) != 1 {
		t.Fatalf("重启后至多补跑一次，得到 %v", names)
	}
}

func TestStatusReportsNextRunAndLastResult(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	f.save(t, Schedule{Enabled: true, Frequency: "daily", At: "04:00", Keep: 5})

	st := f.r.Status(f.ctx, now)
	if !st.Enabled || st.Keep != 5 {
		t.Fatalf("状态应回显配置，得到 %+v", st)
	}
	if st.NextRun == "" || st.NextRunIn == "" {
		t.Fatalf("启用时应给出下次执行时间与倒计时，得到 %+v", st)
	}
	if st.NextHint == "" {
		t.Error("应给出中文执行说明")
	}
	// 从未执行过 → last_run 为空
	if st.LastRun != "" {
		t.Errorf("从未执行时 last_run 应为空，得到 %q", st.LastRun)
	}
	// 关闭后不再排期
	f.save(t, Schedule{Enabled: false, Frequency: "daily", At: "04:00", Keep: 5})
	off := f.r.Status(f.ctx, now)
	if off.Enabled {
		t.Error("关闭后 enabled 应为 false")
	}
}

func TestSaveScheduleClearsStaleNextRun(t *testing.T) {
	f := newFixture(t)
	// 先跑一次，让 next_run 落库为 daily 的排期
	f.save(t, Schedule{Enabled: true, Frequency: "daily", At: "04:00", Keep: 7})
	if _, err := f.r.RunNow(f.ctx, time.Now()); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	before := f.r.Status(f.ctx, time.Now())
	if before.NextRun == "" {
		t.Fatal("执行后应有排期")
	}

	// 改成 weekly：下次执行时间必须按新配置重算，不能沿用 daily 的旧排期
	f.save(t, Schedule{Enabled: true, Frequency: "weekly", At: "03:30", Weekday: 3, Keep: 4})
	after := f.r.Status(f.ctx, time.Now())
	if after.Label != "每周三 03:30" {
		t.Fatalf("label 应按新配置，得到 %q", after.Label)
	}
	if after.NextRun == before.NextRun {
		t.Errorf("改配置后 next_run 应重算，仍是旧值 %s", after.NextRun)
	}
	// 且重算结果必须真的落在周三 03:30
	next, err := time.Parse(time.RFC3339, after.NextRun)
	if err != nil {
		t.Fatalf("next_run 解析: %v", err)
	}
	if next.Weekday() != time.Wednesday || next.Hour() != 3 || next.Minute() != 30 {
		t.Errorf("next_run 应为周三 03:30，得到 %s", next)
	}
	if after.NextHint != "每周三 03:30 执行" {
		t.Errorf("next_hint 应与新配置一致，得到 %q", after.NextHint)
	}
}

func TestStatusReflectsFailureMessage(t *testing.T) {
	f := newFixture(t)
	if err := f.st.SettingSet(f.ctx, keyLastFail, "磁盘满"); err != nil {
		t.Fatal(err)
	}
	f.save(t, Schedule{Enabled: true, Frequency: "daily", At: "04:00", Keep: 7 })
	st := f.r.Status(f.ctx, time.Now())
	if st.LastError != "磁盘满" {
		t.Errorf("状态应带出上次失败原因，得到 %q", st.LastError)
	}
}

func TestStatusToleratesCorruptState(t *testing.T) {
	f := newFixture(t)
	// 时间戳是脏数据：不应 panic，且仍能算出下次时间
	if err := f.st.SettingSet(f.ctx, keyLastRun, "not-a-time"); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SettingSet(f.ctx, keyNextRun, "also-bad"); err != nil {
		t.Fatal(err)
	}
	f.save(t, Schedule{Enabled: true, Frequency: "daily", At: "04:00", Keep: 7})
	st := f.r.Status(f.ctx, time.Now())
	if st.NextRun == "" {
		t.Error("脏状态时应回退为按 now 现算，而不是留空")
	}
	if st.LastRun != "" {
		t.Errorf("脏时间戳不应被当作有效 last_run，得到 %q", st.LastRun)
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		f.r.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ctx 取消后 Run 应退出")
	}
}
