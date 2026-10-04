package backup

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestNormalizeFallsBackOnInvalidInput(t *testing.T) {
	got := Schedule{Frequency: "hourly", At: "25:99", Weekday: 9, EveryMin: 0, Keep: -3}.Normalize()
	if got.Frequency != FreqDaily {
		t.Errorf("未知频率应回落 daily，得到 %q", got.Frequency)
	}
	if got.At != "04:00" {
		t.Errorf("非法时间应回落 04:00，得到 %q", got.At)
	}
	if got.Weekday != 0 {
		t.Errorf("越界 weekday 应归零，得到 %d", got.Weekday)
	}
	if got.EveryMin != 1440 {
		t.Errorf("非正间隔应回落 1440，得到 %d", got.EveryMin)
	}
	if got.Keep != 7 {
		t.Errorf("非正保留数应回落 7，得到 %d", got.Keep)
	}
	// 上限夹紧
	big := Schedule{Frequency: FreqCustom, EveryMin: 60 * 24 * 365, Keep: 9999}.Normalize()
	if big.EveryMin > 60*24*30 {
		t.Errorf("超长间隔应夹到 30 天，得到 %d", big.EveryMin)
	}
	if big.Keep != 200 {
		t.Errorf("保留份数应夹到 200，得到 %d", big.Keep)
	}
}

func TestDefaultScheduleMatchesLegacyBehavior(t *testing.T) {
	// 旧实现是「每天 04:00、保留 7 份」，默认配置必须等价，不能因为重构改变行为
	d := DefaultSchedule()
	if !d.Enabled || d.Frequency != FreqDaily || d.At != "04:00" || d.Keep != 7 {
		t.Fatalf("默认配置应等价于旧的每日 04:00/保留 7 份，得到 %+v", d)
	}
}

func TestParseSchedule(t *testing.T) {
	if s := ParseSchedule(""); s.Frequency != FreqDaily || s.At != "04:00" {
		t.Errorf("空值应回落到默认，得到 %+v", s)
	}
	if s := ParseSchedule("{不是 JSON"); s.Frequency != FreqDaily {
		t.Errorf("脏数据应回落到默认而非报错，得到 %+v", s)
	}
	s := ParseSchedule(`{"enabled":true,"frequency":"weekly","at":"03:30","weekday":3,"keep":5}`)
	if s.Frequency != FreqWeekly || s.Weekday != 3 || s.At != "03:30" || s.Keep != 5 {
		t.Errorf("合法 JSON 应完整还原，得到 %+v", s)
	}
}

func TestNextRunDaily(t *testing.T) {
	now := time.Date(2026, 3, 10, 9, 0, 0, 0, time.Local) // 周二 09:00
	s := Schedule{Frequency: FreqDaily, At: "04:00", Enabled: true}

	// 今天 04:00 已过 → 顺延到明天同一时刻
	next := s.NextRun(now, time.Time{})
	if !next.Equal(time.Date(2026, 3, 11, 4, 0, 0, 0, time.Local)) {
		t.Errorf("已过今日时点应顺延明天，得到 %s", next)
	}
	// 今天 04:00 还没到 → 就是今天
	early := time.Date(2026, 3, 10, 2, 0, 0, 0, time.Local)
	if n := s.NextRun(early, time.Time{}); !n.Equal(time.Date(2026, 3, 10, 4, 0, 0, 0, time.Local)) {
		t.Errorf("未到时点应为今天，得到 %s", n)
	}
}

func TestNextRunWeekly(t *testing.T) {
	now := time.Date(2026, 3, 10, 9, 0, 0, 0, time.Local) // 周二
	// 目标周五 04:00
	s := Schedule{Frequency: FreqWeekly, At: "04:00", Weekday: 5, Enabled: true}
	want := time.Date(2026, 3, 13, 4, 0, 0, 0, time.Local)
	if n := s.NextRun(now, time.Time{}); !n.Equal(want) {
		t.Errorf("weekly 应推到下一个周五，得到 %s（期望 %s，weekday=%v）", n, want, n.Weekday())
	}
	// 目标星期就是今天但时点已过 → 推到下周同一天，而不是今天
	overdue := Schedule{Frequency: FreqWeekly, At: "04:00", Weekday: 2, Enabled: true}
	if n := overdue.NextRun(now, time.Time{}); n.Weekday() != time.Tuesday || n.Day() != 17 {
		t.Errorf("同一天时点已过应推到下周同一天，得到 %s", n)
	}
	// 目标星期就是今天且时点未到 → 今天
	beforeHour := time.Date(2026, 3, 10, 1, 0, 0, 0, time.Local) // 同为周二
	if n := overdue.NextRun(beforeHour, time.Time{}); n.Day() != 10 || n.Hour() != 4 {
		t.Errorf("同一天未到时点应为今天 04:00，得到 %s", n)
	}
}

func TestNextRunCustom(t *testing.T) {
	now := time.Date(2026, 3, 10, 9, 0, 0, 0, time.Local)
	s := Schedule{Frequency: FreqCustom, EveryMin: 30, Enabled: true}
	// 从未执行过 → now + 30min
	if n := s.NextRun(now, time.Time{}); !n.Equal(now.Add(30 * time.Minute)) {
		t.Errorf("custom 从未执行应为 now+30m，得到 %s", n)
	}
	// 有上次执行 → 上次 + 30min
	last := now.Add(-10 * time.Minute)
	if n := s.NextRun(now, last); !n.Equal(last.Add(30 * time.Minute)) {
		t.Errorf("custom 应以上次执行为基准，得到 %s", n)
	}
	// 长时间停机后重启：不密集补跑，直接跳到第一个未来时刻
	stale := now.Add(-5 * time.Hour)
	n := s.NextRun(now, stale)
	if !n.After(now) {
		t.Errorf("停机后应跳到未来时刻，得到 %s", n)
	}
	if got := n.Sub(stale) % (30 * time.Minute); got != 0 {
		t.Errorf("结果应落在间隔网格上，偏移 %s", got)
	}
}

func TestNextRunAlwaysStrictlyAfter(t *testing.T) {
	now := time.Date(2026, 3, 10, 9, 0, 0, 0, time.Local)
	for _, s := range []Schedule{
		{Frequency: FreqDaily, At: "04:00", Enabled: true},
		{Frequency: FreqWeekly, At: "04:00", Weekday: 2, Enabled: true},
		{Frequency: FreqCustom, EveryMin: 60, Enabled: true},
	} {
		// 上次执行 == now（边界：刚好在执行点上再次求值）
		if n := s.NextRun(now, now); !n.After(now) {
			t.Errorf("%s: 结果必须严格晚于基准，得到 %s", s.Frequency, n)
		}
	}
}

func TestDueAfterDoesNotSkipMissedCycles(t *testing.T) {
	now := time.Date(2026, 3, 10, 9, 0, 0, 0, time.Local) // 周二
	// custom：间隔 60 分钟、上次在 2 小时前 → 本该在 1 小时前就该跑
	// DueAfter 必须如实返回那个过期时刻（由调用方判定到点），
	// 而 NextRun 会跳过到未来 —— 二者语义不同，不可混用。
	c := Schedule{Frequency: FreqCustom, EveryMin: 60, Enabled: true}
	last := now.Add(-2 * time.Hour)
	due := c.DueAfter(last)
	if !due.Equal(last.Add(time.Hour)) {
		t.Errorf("DueAfter 应返回上次+1h，得到 %s", due)
	}
	if !due.Before(now) {
		t.Errorf("该时刻已过期，调用方应据此判定到点，得到 %s", due)
	}
	if n := c.NextRun(now, last); n.Before(now) {
		t.Errorf("NextRun 应跳到未来，得到 %s", n)
	}

	// daily：上次是昨天 04:00 → 本该今天 04:00（已过 5 小时）
	d := Schedule{Frequency: FreqDaily, At: "04:00", Enabled: true}
	dueDaily := d.DueAfter(time.Date(2026, 3, 9, 4, 0, 0, 0, time.Local))
	if !dueDaily.Equal(time.Date(2026, 3, 10, 4, 0, 0, 0, time.Local)) {
		t.Errorf("daily 应返回次日同一时刻，得到 %s", dueDaily)
	}

	// weekly：上次上周五 04:00 → 本该本周五 04:00（严格晚于入参，同一时刻不算"之后"）
	w := Schedule{Frequency: FreqWeekly, At: "04:00", Weekday: 5, Enabled: true}
	dueWeekly := w.DueAfter(time.Date(2026, 3, 6, 4, 0, 0, 0, time.Local)) // 周五 04:00
	if !dueWeekly.Equal(time.Date(2026, 3, 13, 4, 0, 0, 0, time.Local)) {
		t.Errorf("weekly 应推到下一个周五，得到 %s", dueWeekly)
	}
	if n := w.DueAfter(dueWeekly); !n.Equal(time.Date(2026, 3, 20, 4, 0, 0, 0, time.Local)) {
		t.Errorf("weekly 应逐周推进一周，得到 %s", n)
	}
}

func TestDescribe(t *testing.T) {
	cases := []struct {
		s    Schedule
		want string
	}{
		{Schedule{Frequency: FreqDaily, At: "04:00"}, "每天 04:00"},
		{Schedule{Frequency: FreqWeekly, At: "09:30", Weekday: 1}, "每周一 09:30"},
		{Schedule{Frequency: FreqCustom, EveryMin: 90}, "每 1 小时 30 分钟一次"},
		{Schedule{Frequency: FreqCustom, EveryMin: 1440}, "每 1 天一次"},
		{Schedule{Frequency: FreqCustom, EveryMin: 45}, "每 45 分钟一次"},
	}
	for _, c := range cases {
		if got := c.s.Describe(); got != c.want {
			t.Errorf("Describe(%+v) = %q，期望 %q", c.s, got, c.want)
		}
	}
	if WeekdayName(0) != "日" || WeekdayName(6) != "六" || WeekdayName(99) != "日" {
		t.Error("WeekdayName 越界或映射错误")
	}
}

// ===== 快照与清理 =====

func TestArchiveProducesValidSQLite(t *testing.T) {
	db, srcPath := newTestDB(t)
	dir := t.TempDir() + "/backups"
	ctx := context.Background()

	path, err := Archive(ctx, db, srcPath, dir)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	// 目录应被自动创建
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("backups 目录应被创建: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data[:16]) != "SQLite format 3\x00" {
		t.Errorf("归档不是合法 SQLite 头: %q", string(data[:16]))
	}
}

func TestArchiveSameSecondGetsSuffix(t *testing.T) {
	db, srcPath := newTestDB(t)
	dir := t.TempDir() + "/backups"
	ctx := context.Background()
	p1, err := Archive(ctx, db, srcPath, dir)
	if err != nil {
		t.Fatalf("Archive#1: %v", err)
	}
	// 同一秒内再归档：不能因目标已存在而失败
	p2, err := Archive(ctx, db, srcPath, dir)
	if err != nil {
		t.Fatalf("Archive#2（同秒）不应失败: %v", err)
	}
	if p1 == p2 {
		t.Errorf("同秒两次归档应生成不同文件: %s", p1)
	}
}

// 实测（modernc sqlite）：VACUUM INTO 对已存在的目标一律报错 ——
// 目标是合法库时报 "output file already exists"，是垃圾文件报 "file is not a database"。
// 因此 Snapshot 会降级到拷贝兜底（os.Create 覆盖目标），Archive 必须自己做同秒去重。
func TestSnapshotOverwritesExistingTargetViaFallback(t *testing.T) {
	db, srcPath := newTestDB(t)
	dest := t.TempDir() + "/existing.db"
	if err := os.WriteFile(dest, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 降级路径最终仍应产出一份合法快照（覆盖掉垃圾内容）
	if err := Snapshot(context.Background(), db, srcPath, dest); err != nil {
		t.Fatalf("目标已存在时 Snapshot 应经兜底成功: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data[:16]) != "SQLite format 3\x00" {
		t.Errorf("兜底后目标应为合法 SQLite，得到 %q", string(data[:16]))
	}
}

func TestPruneKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	write := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 5; i++ {
		write(namef(i))
	}
	write("before-restore-20230101-000000.db") // 恢复前保险，不该被清
	write("note.txt")

	if err := Prune(dir, 2); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	names, err := ArchiveNames(dir)
	if err != nil {
		t.Fatalf("ArchiveNames: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("应保留 2 份，得到 %v", names)
	}
	sort.Strings(names)
	if names[0] != namef(4) || names[1] != namef(5) {
		t.Errorf("应保留最新两份，得到 %v", names)
	}
	if _, err := os.Stat(filepath.Join(dir, "before-restore-20230101-000000.db")); err != nil {
		t.Errorf("恢复前保险文件不应被清理: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "note.txt")); err != nil {
		t.Errorf("非 .db 文件不应被清理: %v", err)
	}
	// keep<=0 → 7；数量不足时不动
	if err := Prune(dir, 0); err != nil {
		t.Fatalf("Prune(keep=0): %v", err)
	}
	if names, _ := ArchiveNames(dir); len(names) != 2 {
		t.Errorf("keep=7 少于总数时不应清理，得到 %v", names)
	}
}

func TestPruneMissingDirIsNoop(t *testing.T) {
	if err := Prune(filepath.Join(t.TempDir(), "nope"), 3); err == nil {
		t.Log("目录不存在时返回错误（调用方只记日志，不影响备份成果）")
	}
	if names, err := ArchiveNames(filepath.Join(t.TempDir(), "nope")); err == nil || names != nil {
		t.Errorf("目录不存在时 ArchiveNames 应返回错误，得到 %v %v", names, err)
	}
}

func namef(i int) string {
	return "qiansi-20230101-00000" + string(rune('0'+i)) + ".db"
}
