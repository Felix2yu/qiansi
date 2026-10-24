package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// 有账单时开销以账单为准，解除后回到手填口径。
// 这条用例锁的是 eventColumns 里那个 CASE 分支，改 SQL 时最容易漏。
func TestEventBills_ExpenseFollowsBills(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := evMustPerson(t, s, "老王")

	e := &Event{Title: "喝茶", EventDate: "2026-10-20"}
	e.ID = ""
	if err := s.EventCreate(ctx, e, []string{p.ID}, &EventExpense{
		PersonID: p.ID, AmountFen: 5000, Title: "喝茶", OccurredAt: "2026-10-20",
	}); err != nil {
		t.Fatalf("EventCreate: %v", err)
	}

	got, err := s.EventGet(ctx, e.ID)
	if err != nil {
		t.Fatalf("EventGet: %v", err)
	}
	if got.ExpenseFen != 5000 {
		t.Fatalf("手填开销 = %d, want 5000", got.ExpenseFen)
	}

	// 挂第一张账单：手填的 5000 不再计入，避免同一笔钱算两次
	if _, err := s.EventBillLink(ctx, &EventBill{
		EventID: e.ID, RemoteID: "11", RemoteBase: "https://hz.test",
		AmountFen: 8800, Type: "expense", TxDate: "2026-10-20",
		Description: "茶馆", CategoryName: "餐饮", AccountName: "现金", BookName: "日常",
	}); err != nil {
		t.Fatalf("EventBillLink: %v", err)
	}
	got, _ = s.EventGet(ctx, e.ID)
	if got.ExpenseFen != 8800 {
		t.Fatalf("挂 1 张后开销 = %d, want 8800", got.ExpenseFen)
	}
	if got.BillCount != 1 || len(got.Bills) != 1 {
		t.Fatalf("账单数 = %d/%d, want 1/1", got.BillCount, len(got.Bills))
	}

	// 第二张累加
	if _, err := s.EventBillLink(ctx, &EventBill{
		EventID: e.ID, RemoteID: "12", AmountFen: 1200, Type: "expense", TxDate: "2026-10-19",
		Description: "打车",
	}); err != nil {
		t.Fatalf("EventBillLink 2: %v", err)
	}
	got, _ = s.EventGet(ctx, e.ID)
	if got.ExpenseFen != 10000 {
		t.Fatalf("挂 2 张后开销 = %d, want 10000", got.ExpenseFen)
	}

	// 全部解除 → 回到手填的 5000（那笔账一直没删，只是不再显示）
	bills := got.Bills
	for _, b := range bills {
		if err := s.EventBillUnlink(ctx, e.ID, b.ID); err != nil {
			t.Fatalf("EventBillUnlink: %v", err)
		}
	}
	got, _ = s.EventGet(ctx, e.ID)
	if got.ExpenseFen != 5000 {
		t.Fatalf("解除后开销 = %d, want 5000", got.ExpenseFen)
	}
	if got.BillCount != 0 {
		t.Fatalf("解除后账单数 = %d, want 0", got.BillCount)
	}
}

func TestEventBills_LinkIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := evMustPerson(t, s, "老李")
	e := &Event{Title: "吃饭", EventDate: "2026-10-21"}
	if err := s.EventCreate(ctx, e, []string{p.ID}, nil); err != nil {
		t.Fatalf("EventCreate: %v", err)
	}

	first, err := s.EventBillLink(ctx, &EventBill{EventID: e.ID, RemoteID: "7", AmountFen: 3000, TxDate: "2026-10-21", Description: "旧"})
	if err != nil {
		t.Fatalf("link 1: %v", err)
	}
	// 同一张账单再挂一次：只刷新快照，不新增行、不换 id
	again, err := s.EventBillLink(ctx, &EventBill{EventID: e.ID, RemoteID: "7", AmountFen: 4500, TxDate: "2026-10-21", Description: "新"})
	if err != nil {
		t.Fatalf("link 2: %v", err)
	}
	if again.ID != first.ID {
		t.Fatalf("重复关联换了 id：%s → %s", first.ID, again.ID)
	}
	if again.AmountFen != 4500 || again.Description != "新" {
		t.Fatalf("快照没被更新: %+v", again)
	}
	list, _ := s.EventBills(ctx, e.ID)
	if len(list) != 1 {
		t.Fatalf("账单行数 = %d, want 1", len(list))
	}
}

// 开销合计只数支出账单：入账/转账账单不是这次往来的花费
func TestEventBills_SumCountsExpenseOnly(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := evMustPerson(t, s, "老钱")
	e := &Event{Title: "饭局", EventDate: "2026-10-22"}
	if err := s.EventCreate(ctx, e, []string{p.ID}, &EventExpense{PersonID: p.ID, AmountFen: 5000, Title: "饭局", OccurredAt: e.EventDate}); err != nil {
		t.Fatalf("EventCreate: %v", err)
	}
	if _, err := s.EventBillLink(ctx, &EventBill{EventID: e.ID, RemoteID: "e1", AmountFen: 8800, Type: "expense", TxDate: "2026-10-22"}); err != nil {
		t.Fatalf("link expense: %v", err)
	}
	// 同一场里混一张入账账单：不应计入开销，也不该抢走「账单权威」
	if _, err := s.EventBillLink(ctx, &EventBill{EventID: e.ID, RemoteID: "i1", AmountFen: 99900, Type: "income", TxDate: "2026-10-22"}); err != nil {
		t.Fatalf("link income: %v", err)
	}
	got, err := s.EventGet(ctx, e.ID)
	if err != nil {
		t.Fatalf("EventGet: %v", err)
	}
	if got.ExpenseFen != 8800 {
		t.Fatalf("ExpenseFen = %d, 期望 8800（只数支出账单）", got.ExpenseFen)
	}
	// 只有支出账单在，手填开销的编辑权才被禁掉
	if has, _ := s.EventHasBills(ctx, e.ID); !has {
		t.Fatal("有支出账单时 EventHasBills 应为 true")
	}
	// 只剩入账账单时不该禁手填
	if err := s.EventBillUnlink(ctx, e.ID, mustBillID(t, s, e.ID, "e1")); err != nil {
		t.Fatalf("unlink: %v", err)
	}
	if has, _ := s.EventHasBills(ctx, e.ID); has {
		t.Fatal("只剩入账账单时 EventHasBills 应为 false")
	}
	got, _ = s.EventGet(ctx, e.ID)
	if got.ExpenseFen != 5000 {
		t.Fatalf("回到手填口径后期望 5000, got %d", got.ExpenseFen)
	}
}

func mustBillID(t *testing.T, s *Store, eventID, remoteID string) string {
	t.Helper()
	list, err := s.EventBills(context.Background(), eventID)
	if err != nil {
		t.Fatalf("EventBills: %v", err)
	}
	for _, b := range list {
		if b.RemoteID == remoteID {
			return b.ID
		}
	}
	t.Fatalf("bill %s not found", remoteID)
	return ""
}

func TestEventBills_UnlinkScopedToEvent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := evMustPerson(t, s, "老张")
	e1 := &Event{Title: "A", EventDate: "2026-10-01"}
	e2 := &Event{Title: "B", EventDate: "2026-10-02"}
	for _, e := range []*Event{e1, e2} {
		if err := s.EventCreate(ctx, e, []string{p.ID}, nil); err != nil {
			t.Fatalf("EventCreate: %v", err)
		}
	}
	b, err := s.EventBillLink(ctx, &EventBill{EventID: e1.ID, RemoteID: "9", AmountFen: 100, TxDate: "2026-10-01"})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	// 用别的往来 id 去解除，必须解不掉（否则任何事件都能删别人的关联）
	if err := s.EventBillUnlink(ctx, e2.ID, b.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("跨往来解除应返回 ErrNoRows, got %v", err)
	}
	if has, _ := s.EventHasBills(ctx, e1.ID); !has {
		t.Fatal("跨往来解除把原关联也删了")
	}
}
