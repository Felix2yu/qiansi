package store

import (
	"context"
	"testing"
)

// ===== 按人的往来小结（N1）=====

func stMustTx(t *testing.T, s *Store, tx *Transaction) {
	t.Helper()
	if err := s.TransactionCreate(context.Background(), tx); err != nil {
		t.Fatalf("TransactionCreate(%s %s %d): %v", tx.Kind, tx.Direction, tx.AmountFen, err)
	}
}

func stMustMemo(t *testing.T, s *Store, m *Memo) {
	t.Helper()
	if err := s.MemoCreate(context.Background(), m); err != nil {
		t.Fatalf("MemoCreate(%s): %v", m.Content, err)
	}
}

func TestPersonStats_GiftNetIgnoresLoanAndExpense(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	laoWang := evMustPerson(t, s, "老王")

	stMustTx(t, s, &Transaction{PersonID: laoWang.ID, Kind: "gift", Direction: "out", AmountFen: 80000, Title: "儿子结婚", OccurredAt: "2026-01-05"})
	stMustTx(t, s, &Transaction{PersonID: laoWang.ID, Kind: "gift", Direction: "out", AmountFen: 20000, Title: "满月酒", OccurredAt: "2026-03-01"})
	stMustTx(t, s, &Transaction{PersonID: laoWang.ID, Kind: "gift", Direction: "in", AmountFen: 30000, Title: "乔迁", OccurredAt: "2026-02-02"})
	// 借还与日常花销不进「该回多少」这本账
	stMustTx(t, s, &Transaction{PersonID: laoWang.ID, Kind: "loan", Direction: "out", AmountFen: 500000, Title: "借款", OccurredAt: "2026-02-09"})
	stMustTx(t, s, &Transaction{PersonID: laoWang.ID, Kind: "expense", Direction: "out", AmountFen: 12000, Title: "饭钱", OccurredAt: "2026-02-10"})

	m, err := s.PersonStatsFor(ctx, []string{laoWang.ID})
	if err != nil {
		t.Fatalf("PersonStatsFor: %v", err)
	}
	got := m[laoWang.ID]
	if got == nil {
		t.Fatalf("PersonStatsFor 没返回老王: %+v", m)
	}
	if got.GiftOutFen != 100000 || got.GiftInFen != 30000 {
		t.Fatalf("礼金合计 = 出 %d / 收 %d, want 100000 / 30000", got.GiftOutFen, got.GiftInFen)
	}
	if got.NetFen != 70000 {
		t.Fatalf("净额 = %d, want 70000（随出多于收到）", got.NetFen)
	}
}

// 净额可以为负：收得多、还没回够，同样要看得出来的方向
func TestPersonStats_NetCanBeNegative(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := evMustPerson(t, s, "小李")
	stMustTx(t, s, &Transaction{PersonID: p.ID, Kind: "gift", Direction: "in", AmountFen: 60000, OccurredAt: "2026-04-01"})
	stMustTx(t, s, &Transaction{PersonID: p.ID, Kind: "gift", Direction: "out", AmountFen: 20000, OccurredAt: "2026-04-02"})

	m, err := s.PersonStatsFor(ctx, []string{p.ID})
	if err != nil {
		t.Fatalf("PersonStatsFor: %v", err)
	}
	if got := m[p.ID]; got == nil || got.NetFen != -40000 {
		t.Fatalf("净额 = %+v, want -40000", got)
	}
}

// 「最近一次接触」跨三张表取最晚的一天，且带时间的字段不能被时间尾巴带偏
func TestPersonStats_LastContactAcrossTables(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	a := evMustPerson(t, s, "甲") // 只有往来
	b := evMustPerson(t, s, "乙") // 只有对话
	c := evMustPerson(t, s, "丙") // 只有借款：进最近接触，不进礼金
	d := evMustPerson(t, s, "丁") // 什么都没有

	e := &Event{Title: "老同事聚会", EventDate: "2026-05-20"}
	if err := s.EventCreate(ctx, e, []string{a.ID}, nil); err != nil {
		t.Fatalf("EventCreate: %v", err)
	}
	stMustMemo(t, s, &Memo{PersonID: b.ID, Speaker: "other", Content: "答应帮忙带特产", SaidAt: "2026-06-01T21:04:05"})
	stMustMemo(t, s, &Memo{PersonID: b.ID, Speaker: "other", Content: "更早的一次", SaidAt: "2026-04-11T08:00:00"})
	stMustTx(t, s, &Transaction{PersonID: c.ID, Kind: "loan", Direction: "out", AmountFen: 1000, OccurredAt: "2026-08-08"})

	m, err := s.PersonStatsFor(ctx, []string{a.ID, b.ID, c.ID, d.ID, "ghost-id"})
	if err != nil {
		t.Fatalf("PersonStatsFor: %v", err)
	}
	if got := m[a.ID]; got == nil || got.LastContact != "2026-05-20" {
		t.Fatalf("甲（只有往来）最近接触 = %+v, want 2026-05-20", got)
	}
	if got := m[b.ID]; got == nil || got.LastContact != "2026-06-01" {
		t.Fatalf("乙（对话带时间戳）最近接触 = %+v, want 2026-06-01（只取到日）", got)
	}
	// 借钱不算礼金，但算一次接触
	if got := m[c.ID]; got == nil || got.LastContact != "2026-08-08" || got.GiftOutFen != 0 {
		t.Fatalf("丙（只有借款）= %+v, want 最近 2026-08-08 且礼金为零", got)
	}
	if got := m[d.ID]; got == nil || got.LastContact != "" {
		t.Fatalf("丁（无任何记录）= %+v, want 零值", got)
	}
	if _, ok := m["ghost-id"]; ok {
		t.Fatalf("不存在的 id 不该出现在结果里: %+v", m["ghost-id"])
	}
}

// 卡片与详情页都靠 Person 上挂的 stats 渲染：详情单取、列表整页补，都不该再发第二个请求
func TestPersonStats_AttachedOnGetAndList(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	withTx := evMustPerson(t, s, "有往来")
	quiet := evMustPerson(t, s, "清静人")
	stMustTx(t, s, &Transaction{PersonID: withTx.ID, Kind: "gift", Direction: "out", AmountFen: 8800, OccurredAt: "2026-07-07"})

	p, err := s.PersonGet(ctx, withTx.ID)
	if err != nil {
		t.Fatalf("PersonGet: %v", err)
	}
	if p.Stats == nil || p.Stats.GiftOutFen != 8800 || p.Stats.NetFen != 8800 || p.Stats.LastContact != "2026-07-07" {
		t.Fatalf("详情未带上小结: %+v", p.Stats)
	}
	if p, err = s.PersonGet(ctx, quiet.ID); err != nil {
		t.Fatalf("PersonGet(quiet): %v", err)
	}
	if p.Stats != nil {
		t.Fatalf("无任何记录的人不该带 stats（前端按缺省不渲染这一行）: %+v", p.Stats)
	}

	list, err := s.PersonList(ctx, "", 0, 0, false, 0, 50, 0)
	if err != nil {
		t.Fatalf("PersonList: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("列表人数 = %d, want 2", len(list))
	}
	for _, one := range list {
		switch one.Name {
		case "有往来":
			if one.Stats == nil || one.Stats.GiftOutFen != 8800 {
				t.Fatalf("列表未批量补上小结: %+v", one.Stats)
			}
		case "清静人":
			if one.Stats != nil {
				t.Fatalf("列表里空记录的人不该带 stats: %+v", one.Stats)
			}
		}
	}
}

// 全局未结清借款口径不受影响：按人小结只计礼金，两处数字不该互相污染
func TestPersonStats_DoesNotTouchDashboardLoanBalance(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := evMustPerson(t, s, "借款人")
	stMustTx(t, s, &Transaction{PersonID: p.ID, Kind: "loan", Direction: "out", AmountFen: 3000, OccurredAt: "2026-01-01", DueDate: "2026-06-01"})
	stMustTx(t, s, &Transaction{PersonID: p.ID, Kind: "gift", Direction: "out", AmountFen: 5000, OccurredAt: "2026-01-02"})

	byPerson, err := s.PersonStatsFor(ctx, []string{p.ID})
	if err != nil {
		t.Fatalf("PersonStatsFor: %v", err)
	}
	if got := byPerson[p.ID]; got == nil || got.GiftOutFen != 5000 {
		t.Fatalf("按人小结 = %+v, want 只算礼金的 5000", got)
	}
	d, err := s.DashboardStats(ctx)
	if err != nil {
		t.Fatalf("DashboardStats: %v", err)
	}
	if d["lend_fen"].(int64) != 3000 {
		t.Fatalf("首页未结清借出 = %v, want 3000（不受礼金影响）", d["lend_fen"])
	}
}
