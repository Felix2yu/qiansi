package notify

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/qiansi/app/internal/store"
	"github.com/qiansi/app/internal/testdb"
)

func newNotifyStore(t *testing.T) *store.Store {
	t.Helper()
	return store.New(testdb.New(t))
}

func TestNotifyPushEmptyURLList(t *testing.T) {
	// 空列表直接返回 nil，不触碰任何网络
	if err := Push(context.Background(), nil, "t", "b"); err != nil {
		t.Errorf("Push(nil) = %v, want nil", err)
	}
	if err := Push(context.Background(), []string{}, "t", "b"); err != nil {
		t.Errorf("Push(empty) = %v, want nil", err)
	}
}

func TestNotifyPushBlankURLs(t *testing.T) {
	// 全空白 URL 会被跳过，client 没有目标 → Send 返回 ErrNoTargets
	err := Push(context.Background(), []string{"", "   ", "\t"}, "t", "b")
	if err == nil {
		t.Fatal("expected error when all URLs are blank")
	}
}

func TestNotifyPushInvalidSchemeSkipped(t *testing.T) {
	// 非法 scheme 在 Add 阶段被记录日志并跳过，不会 panic；
	// 最终因为没有可用目标而返回 ErrNoTargets。
	err := Push(context.Background(), []string{"bogus-scheme://example.invalid", " not-a-url "}, "t", "b")
	if err == nil {
		t.Fatal("expected ErrNoTargets after all URLs skipped")
	}
}

func TestNotifyRunTick(t *testing.T) {
	ctx := context.Background()
	s := newNotifyStore(t)

	// 覆盖三种 push_time_hour 配置：未设置(默认9)、非法值、当前小时。
	// 未设置：走 err==nil && v=="" 分支
	if err := s.SettingSet(ctx, "push_time_hour", ""); err != nil {
		t.Fatalf("SettingSet: %v", err)
	}
	runTick(ctx, s, time.Now())

	// 非法值：解析失败，pushHour 回落到默认
	if err := s.SettingSet(ctx, "push_time_hour", "abc"); err != nil {
		t.Fatalf("SettingSet: %v", err)
	}
	runTick(ctx, s, time.Now())

	// 已过配置时刻 → 触发 digest（apprise_urls 为空 → sendDigest 提前返回，不发网络）
	if err := s.SettingSet(ctx, "push_time_hour", fmt.Sprint(time.Now().Hour())); err != nil {
		t.Fatalf("SettingSet: %v", err)
	}
	runTick(ctx, s, time.Now())
	runTick(ctx, s, time.Now())
}

// M3 回归防护：旧实现是 `Hour()==N && Minute()<5` 的窗口判定，配 30 分钟 ticker，
// tick 相位由进程启动时刻决定，多数部署永远撞不进那个 5 分钟窗口 —— 配了推送也收不到。
func TestNotifyDailyTasksFireAfterTheirHour(t *testing.T) {
	ctx := context.Background()
	s := newNotifyStore(t)
	if err := s.SettingSet(ctx, "push_time_hour", "9"); err != nil {
		t.Fatalf("SettingSet: %v", err)
	}
	if err := s.SettingSet(ctx, "apprise_urls", ""); err != nil {
		t.Fatalf("SettingSet: %v", err)
	}
	p := &store.Person{Name: "甲乙"}
	if err := s.PersonCreate(ctx, p); err != nil {
		t.Fatalf("PersonCreate: %v", err)
	}

	// 相位不在任何 5 分钟窗口里：14:37
	at := func(h, m int) time.Time {
		y, mo, d := time.Now().Local().Date()
		return time.Date(y, mo, d, h, m, 0, 0, time.Local)
	}
	runTick(ctx, s, at(14, 37))

	var snapDay, digestDay string
	want := at(14, 37).Format("2006-01-02")
	if err := s.DB.QueryRow("SELECT day FROM intimacy_snapshots WHERE person_id=?", p.ID).Scan(&snapDay); err != nil {
		t.Fatalf("快照未按 03:00 之后补跑: %v", err)
	}
	if snapDay != want {
		t.Errorf("快照日期 = %s, want 本地日期 %s", snapDay, want)
	}
	if digestDay, _ = s.SettingGet(ctx, keyDigestDay); digestDay != want {
		t.Errorf("摘要记账日 = %q, want %s（14:37 已过 09:00，应补发一次）", digestDay, want)
	}

	// 同一天再来一次：已经做成，不重复推送
	if err := s.SettingSet(ctx, "apprise_urls", "json://127.0.0.1:1"); err != nil {
		t.Fatalf("SettingSet: %v", err)
	}
	if err := s.ReminderCreate(ctx, &store.Reminder{Title: "回礼", DueAt: want + "T09:00:00", Status: "pending"}); err != nil {
		t.Fatalf("ReminderCreate: %v", err)
	}
	runTick(ctx, s, at(15, 3))
	if v, _ := s.SettingGet(ctx, keyDigestDay); v != want {
		t.Errorf("同日重复触发：记账日被改成了 %q", v)
	}

	// 换到第二天：重新排期
	if err := s.SettingSet(ctx, keySnapshotDay, ""); err != nil {
		t.Fatalf("SettingSet: %v", err)
	}
	if err := s.SettingSet(ctx, keyDigestDay, ""); err != nil {
		t.Fatalf("SettingSet: %v", err)
	}
	runTick(ctx, s, at(8, 0)) // 08:00：过 03:00 → 快照跑；未到 09:00 → 摘要不跑
	if v, _ := s.SettingGet(ctx, keySnapshotDay); v == "" {
		t.Errorf("次日 08:00 应补跑快照")
	}
	if v, _ := s.SettingGet(ctx, keyDigestDay); v != "" {
		t.Errorf("次日 08:00 不该发 09:00 的摘要, got %q", v)
	}
}

// 推送失败不记账：否则一次网络抖动就丢掉整天的摘要。
func TestNotifyDigestFailureRetriesSameDay(t *testing.T) {
	ctx := context.Background()
	s := newNotifyStore(t)
	// 合法 scheme、关闭端口 → Send 立刻失败，不出网
	if err := s.SettingSet(ctx, "apprise_urls", "json://127.0.0.1:1"); err != nil {
		t.Fatalf("SettingSet: %v", err)
	}
	if err := s.SettingSet(ctx, "push_time_hour", "9"); err != nil {
		t.Fatalf("SettingSet: %v", err)
	}
	now := time.Now().Local().AddDate(0, 0, 1)
	now = time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, time.Local)
	due := time.Now().Local().AddDate(0, 0, 2).Format("2006-01-02T09:00:00")
	if err := s.ReminderCreate(ctx, &store.Reminder{Title: "还钱给小李", DueAt: due, Status: "pending"}); err != nil {
		t.Fatalf("ReminderCreate: %v", err)
	}

	runTick(ctx, s, now)
	if v, _ := s.SettingGet(ctx, keyDigestDay); v == now.Format("2006-01-02") {
		t.Errorf("推送失败却记了账，今天再也不会重试")
	}
}

func TestNotifySnapshotAll(t *testing.T) {
	ctx := context.Background()

	// PersonList 出错（people 表被删）→ 返回错误，调用方不记账、下次重试
	broken := newNotifyStore(t)
	if _, err := broken.DB.Exec("DROP TABLE people"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := snapshotAll(ctx, broken, time.Now()); err == nil {
		t.Error("people 表缺失时快照应返回错误")
	}

	// 空库：不应 panic、不应写入
	s := newNotifyStore(t)
	if err := snapshotAll(ctx, s, time.Now()); err != nil {
		t.Fatalf("snapshotAll(empty): %v", err)
	}
	var n int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM intimacy_snapshots").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("empty db snapshots = %d, want 0", n)
	}

	// 有人的库：每人写入当日快照
	p1 := &store.Person{Name: "阿甲", Grade: 5}
	p2 := &store.Person{Name: "阿乙", Grade: 3}
	if err := s.PersonCreate(ctx, p1); err != nil {
		t.Fatalf("PersonCreate: %v", err)
	}
	if err := s.PersonCreate(ctx, p2); err != nil {
		t.Fatalf("PersonCreate: %v", err)
	}
	if err := snapshotAll(ctx, s, time.Now()); err != nil {
		t.Fatalf("snapshotAll: %v", err)
	}

	if err := s.DB.QueryRow("SELECT COUNT(*) FROM intimacy_snapshots").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("snapshots = %d, want 2", n)
	}
	// 重复执行走 INSERT OR REPLACE，不应翻倍
	if err := snapshotAll(ctx, s, time.Now()); err != nil {
		t.Fatalf("snapshotAll again: %v", err)
	}
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM intimacy_snapshots").Scan(&n); err != nil {
		t.Fatalf("count again: %v", err)
	}
	if n != 2 {
		t.Errorf("snapshots after re-run = %d, want 2 (INSERT OR REPLACE)", n)
	}
}

func TestNotifySendDigest(t *testing.T) {
	ctx := context.Background()
	s := newNotifyStore(t)

	// 1) 未配置 apprise_urls → 提前返回
	if err := s.SettingSet(ctx, "apprise_urls", ""); err != nil {
		t.Fatalf("SettingSet: %v", err)
	}
	sendDigest(ctx, s)

	// 2) 配置了 URL 但近 7 天没有待办 → 提前返回
	if err := s.SettingSet(ctx, "apprise_urls", "bogus-scheme://example.invalid"); err != nil {
		t.Fatalf("SettingSet: %v", err)
	}
	sendDigest(ctx, s)

	// 3) 有即将到来的提醒 → 构造正文并调用 Push；
	//    URL scheme 非法，Push 在 Add 阶段跳过后返回错误，只会被记录日志，不发网络。
	p := &store.Person{Name: "小丙"}
	if err := s.PersonCreate(ctx, p); err != nil {
		t.Fatalf("PersonCreate: %v", err)
	}
	due := time.Now().AddDate(0, 0, 2).Format("2006-01-02T15:04:05")
	if err := s.ReminderCreate(ctx, &store.Reminder{
		PersonID: p.ID, Title: "给小丙过生日", DueAt: due, Status: "pending",
	}); err != nil {
		t.Fatalf("ReminderCreate: %v", err)
	}
	// 无人名提醒（PersonName=="" 分支）
	if err := s.ReminderCreate(ctx, &store.Reminder{
		Title: "整理相册", DueAt: due, Status: "pending",
	}); err != nil {
		t.Fatalf("ReminderCreate: %v", err)
	}
	sendDigest(ctx, s) // 不应 panic；失败仅记录日志
}

func TestNotifyPushSendFailure(t *testing.T) {
	// json:// 是合法 scheme，Add 成功；目标是本机回环的一个关闭端口，
	// Send 立刻收到 connection refused，走 TargetError 聚合分支。无任何外网请求。
	err := Push(context.Background(), []string{"json://127.0.0.1:1"}, "t", "b")
	if err == nil {
		t.Fatal("expected send failure against closed local port")
	}
}
