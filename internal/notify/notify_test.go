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

	// 覆盖三种 push_time_hour 配置：未设置(默认9)、非法值(Sscanf 失败)、当前小时。
	// 未设置：走 err==nil && v=="" 分支
	if err := s.SettingSet(ctx, "push_time_hour", ""); err != nil {
		t.Fatalf("SettingSet: %v", err)
	}
	runTick(ctx, s)

	// 非法值：Sscanf 失败，pushHour 保持默认
	if err := s.SettingSet(ctx, "push_time_hour", "abc"); err != nil {
		t.Fatalf("SettingSet: %v", err)
	}
	runTick(ctx, s)

	// 当前小时 + 分钟<5 时才触发 digest（apprise_urls 为空 → sendDigest 提前返回，不发网络）
	if err := s.SettingSet(ctx, "push_time_hour", fmt.Sprint(time.Now().Hour())); err != nil {
		t.Fatalf("SettingSet: %v", err)
	}
	runTick(ctx, s)
	runTick(ctx, s)
}

func TestNotifySnapshotAll(t *testing.T) {
	ctx := context.Background()

	// PersonList 出错（people 表被删）→ 静默返回，不 panic
	broken := newNotifyStore(t)
	if _, err := broken.DB.Exec("DROP TABLE people"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	snapshotAll(ctx, broken)

	// 空库：不应 panic、不应写入
	s := newNotifyStore(t)
	snapshotAll(ctx, s)
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
	snapshotAll(ctx, s)

	if err := s.DB.QueryRow("SELECT COUNT(*) FROM intimacy_snapshots").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("snapshots = %d, want 2", n)
	}
	// 重复执行走 INSERT OR REPLACE，不应翻倍
	snapshotAll(ctx, s)
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
