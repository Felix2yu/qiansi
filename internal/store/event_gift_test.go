package store

import (
	"context"
	"testing"
)

// ===== 事件自带的那笔礼金（N2）=====

func egCount(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.DB.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func egCreate(t *testing.T, s *Store, e *Event, people []string) {
	t.Helper()
	if err := s.EventCreate(context.Background(), e, people, nil); err != nil {
		t.Fatalf("EventCreate: %v", err)
	}
}

// 保存往来就结账：金额落在 transactions 上，事件只是它的投影
func TestEventGift_CreateBooksOneTransaction(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	host := evMustPerson(t, s, "老王")

	e := &Event{Title: "老王儿子婚礼", EventDate: "2026-10-01",
		GiftAmountFen: 80000, GiftDirection: "out", GiftPersonID: host.ID}
	egCreate(t, s, e, []string{host.ID})

	if e.GiftTransactionID == "" {
		t.Fatalf("EventCreate 未回填礼金账目 id")
	}
	if n := egCount(t, s, "SELECT COUNT(*) FROM transactions WHERE event_id=?", e.ID); n != 1 {
		t.Fatalf("事件下的账目数 = %d, want 1", n)
	}
	var kind, dir, person, title, occurred string
	var amount, settled int
	err := s.DB.QueryRowContext(ctx,
		"SELECT kind,direction,person_id,amount_fen,title,settled,occurred_at FROM transactions WHERE id=?", e.GiftTransactionID).
		Scan(&kind, &dir, &person, &amount, &title, &settled, &occurred)
	if err != nil {
		t.Fatalf("读取礼金账目失败: %v", err)
	}
	if kind != "gift" || dir != "out" || person != host.ID || amount != 80000 {
		t.Fatalf("礼金账目 = %s/%s/%s/%d", kind, dir, person, amount)
	}
	if title != e.Title || settled != 1 {
		t.Fatalf("礼金账目 title/settled = %q/%d, want 事件标题/已结", title, settled)
	}
	if occurred != e.EventDate {
		t.Fatalf("礼金日期 = %q, want 跟着往来的 %s", occurred, e.EventDate)
	}

	got, err := s.EventGet(ctx, e.ID)
	if err != nil {
		t.Fatalf("EventGet: %v", err)
	}
	if got.GiftAmountFen != 80000 || got.GiftDirection != "out" || got.GiftPersonID != host.ID {
		t.Fatalf("事件上的礼金投影 = %+v", got)
	}
	// 随出去的钱不再重复计入「花费」，否则一张卡上 800 出现两次
	if got.ExpenseFen != 0 {
		t.Fatalf("ExpenseFen = %d, want 0（礼金由 gift_amount_fen 单独显示）", got.ExpenseFen)
	}
}

// 改金额是改同一条账，不是再补一条
func TestEventGift_UpdateEditsSameRow(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	host := evMustPerson(t, s, "老张")
	other := evMustPerson(t, s, "老张的爱人")

	e := &Event{Title: "寿宴", EventDate: "2026-10-02",
		GiftAmountFen: 60000, GiftDirection: "out", GiftPersonID: host.ID}
	egCreate(t, s, e, []string{host.ID})
	first := e.GiftTransactionID

	e.GiftAmountFen = 100000
	e.GiftDirection = "in"
	e.GiftPersonID = other.ID
	if err := s.EventUpdate(ctx, e, []string{host.ID}, nil); err != nil {
		t.Fatalf("EventUpdate: %v", err)
	}
	if e.GiftTransactionID != first {
		t.Fatalf("改金额换了一条账: %s -> %s", first, e.GiftTransactionID)
	}
	if n := egCount(t, s, "SELECT COUNT(*) FROM transactions WHERE event_id=?", e.ID); n != 1 {
		t.Fatalf("事件下的账目数 = %d, want 1", n)
	}
	got, _ := s.EventGet(ctx, e.ID)
	if got.GiftAmountFen != 100000 || got.GiftDirection != "in" || got.GiftPersonID != other.ID {
		t.Fatalf("改后投影 = %+v", got)
	}
}

// 清空金额 = 这笔没随过；账目和指针都要走
func TestEventGift_ClearRemovesRow(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	host := evMustPerson(t, s, "小李")
	e := &Event{Title: "乔迁", EventDate: "2026-10-03",
		GiftAmountFen: 20000, GiftDirection: "out", GiftPersonID: host.ID}
	egCreate(t, s, e, []string{host.ID})
	id := e.GiftTransactionID

	e.GiftAmountFen = 0
	if err := s.EventUpdate(ctx, e, []string{host.ID}, nil); err != nil {
		t.Fatalf("EventUpdate: %v", err)
	}
	if n := egCount(t, s, "SELECT COUNT(*) FROM transactions WHERE id=?", id); n != 0 {
		t.Fatalf("清空礼金后账目仍在: %d", n)
	}
	got, _ := s.EventGet(ctx, e.ID)
	if got.GiftTransactionID != "" || got.GiftAmountFen != 0 {
		t.Fatalf("清空后指针未归位: %+v", got)
	}
}

// 礼单是别人的账：一场宴席手工挂了 3 笔 gift，事件表单再保存也不该动它们
func TestEventGift_DoesNotTouchManualGiftList(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	me := evMustPerson(t, s, "我")
	e := &Event{Title: "我的宴席", EventDate: "2026-10-04", GiftAmountFen: 0}
	egCreate(t, s, e, []string{me.ID})

	var guestIDs []string
	for _, g := range []string{"甲", "乙", "丙"} {
		p := evMustPerson(t, s, g)
		guestIDs = append(guestIDs, p.ID)
		tx := &Transaction{PersonID: p.ID, Kind: "gift", Direction: "in", AmountFen: 50000,
			Title: "礼金 " + g, OccurredAt: "2026-10-04", EventID: e.ID}
		if err := s.TransactionCreate(ctx, tx); err != nil {
			t.Fatalf("礼单入账: %v", err)
		}
	}

	e.GiftAmountFen = 30000
	e.GiftDirection = "in"
	e.GiftPersonID = guestIDs[0]
	if err := s.EventUpdate(ctx, e, []string{me.ID}, nil); err != nil {
		t.Fatalf("EventUpdate: %v", err)
	}
	if n := egCount(t, s, "SELECT COUNT(*) FROM transactions WHERE event_id=?", e.ID); n != 4 {
		t.Fatalf("事件下账目数 = %d, want 4（礼单 3 笔 + 事件自带 1 笔）", n)
	}
	// 清掉事件自带那笔时，只删它自己
	e.GiftAmountFen = 0
	if err := s.EventUpdate(ctx, e, []string{me.ID}, nil); err != nil {
		t.Fatalf("EventUpdate 清空: %v", err)
	}
	if n := egCount(t, s, "SELECT COUNT(*) FROM transactions WHERE event_id=? AND kind='gift'", e.ID); n != 3 {
		t.Fatalf("清空后礼单剩 %d 笔, want 3", n)
	}
}

// 账目被改挂到别的事件上之后，事件不能再把它抢回来
func TestEventGift_DoesNotStealReassignedRow(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	host := evMustPerson(t, s, "老王")
	e := &Event{Title: "婚礼", EventDate: "2026-10-05",
		GiftAmountFen: 80000, GiftDirection: "out", GiftPersonID: host.ID}
	egCreate(t, s, e, []string{host.ID})
	owned := e.GiftTransactionID

	other := &Event{Title: "另一场", EventDate: "2026-10-06", GiftAmountFen: 10000, GiftDirection: "out", GiftPersonID: host.ID}
	egCreate(t, s, other, []string{host.ID})
	if _, err := s.DB.ExecContext(ctx, "UPDATE transactions SET event_id=? WHERE id=?", other.ID, owned); err != nil {
		t.Fatalf("改挂账目: %v", err)
	}

	e.GiftAmountFen = 90000
	if err := s.EventUpdate(ctx, e, []string{host.ID}, nil); err != nil {
		t.Fatalf("EventUpdate: %v", err)
	}
	if e.GiftTransactionID == owned {
		t.Fatalf("把已经属于别的往来的账抢了回来")
	}
	var evID string
	if err := s.DB.QueryRowContext(ctx, "SELECT event_id FROM transactions WHERE id=?", owned).Scan(&evID); err != nil {
		t.Fatalf("读取被改挂的账: %v", err)
	}
	if evID != other.ID {
		t.Fatalf("改挂后的账目被抢回 %s, want 仍属于 %s", evID, other.ID)
	}
	if n := egCount(t, s, "SELECT COUNT(*) FROM transactions WHERE event_id=?", e.ID); n != 1 {
		t.Fatalf("本事件下账目 = %d, want 1（新记的那笔）", n)
	}
}

// 礼金账被彻底删除（回收站里清空）：外键把指针清成 NULL，下次保存重新记一笔，
// 不能留下悬空引用。收进回收站是另一回事，由 trash_test.go 覆盖。
func TestEventGift_SelfHealsAfterTxDeleted(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	host := evMustPerson(t, s, "小赵")
	e := &Event{Title: "满月酒", EventDate: "2026-10-07",
		GiftAmountFen: 2600, GiftDirection: "out", GiftPersonID: host.ID}
	egCreate(t, s, e, []string{host.ID})

	if err := s.TransactionPurge(ctx, e.GiftTransactionID); err != nil {
		t.Fatalf("TransactionPurge: %v", err)
	}
	got, _ := s.EventGet(ctx, e.ID)
	if got.GiftTransactionID != "" || got.GiftAmountFen != 0 {
		t.Fatalf("删掉账目后事件仍认领它: %+v", got)
	}

	e.GiftAmountFen = 3000
	if err := s.EventUpdate(ctx, e, []string{host.ID}, nil); err != nil {
		t.Fatalf("EventUpdate: %v", err)
	}
	if n := egCount(t, s, "SELECT COUNT(*) FROM transactions WHERE event_id=? AND kind='gift'", e.ID); n != 1 {
		t.Fatalf("补记后礼金账目 = %d 笔, want 1", n)
	}
}

// 删往来留账：钱确实花过。进回收站时账目还挂着（恢复事件要连账一起回来），
// 从回收站彻底删掉才解绑（礼金与开销同一套做法）。
func TestEventGift_DeleteEventKeepsGiftRow(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	host := evMustPerson(t, s, "老陈")
	e := &Event{Title: "婚礼", EventDate: "2026-10-08",
		GiftAmountFen: 100000, GiftDirection: "out", GiftPersonID: host.ID}
	egCreate(t, s, e, []string{host.ID})

	if err := s.EventDelete(ctx, e.ID); err != nil {
		t.Fatalf("EventDelete: %v", err)
	}
	var evID *string
	if err := s.DB.QueryRowContext(ctx, "SELECT event_id FROM transactions WHERE kind='gift' AND person_id=?", host.ID).Scan(&evID); err != nil {
		t.Fatalf("礼金账目随事件一起消失了: %v", err)
	}
	if evID == nil || *evID != e.ID {
		t.Fatalf("事件进回收站后账目应仍挂在 %s，got %v", e.ID, evID)
	}
	if err := s.EventPurge(ctx, e.ID); err != nil {
		t.Fatalf("EventPurge: %v", err)
	}
	evID = nil
	if err := s.DB.QueryRowContext(ctx, "SELECT event_id FROM transactions WHERE kind='gift' AND person_id=?", host.ID).Scan(&evID); err != nil {
		t.Fatalf("彻底删除后礼金账目消失了: %v", err)
	}
	if evID != nil {
		t.Fatalf("删事件后账目仍挂在它上面: %v", *evID)
	}
}
