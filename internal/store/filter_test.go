package store

import (
	"context"
	"testing"
)

// O6 的两个列表筛选。这里重点盯两件事：
// 一是「零值＝不限」真的不限，二是没写日期的那条记录不会混进任何时间区间。

func TestEventFilter_TypeAndDateRange(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if _, err := s.DB.Exec(`INSERT INTO event_types(id,name,color) VALUES(71,'聚会','#111111'),(72,'运动','#222222')`); err != nil {
		t.Fatalf("seed event_types: %v", err)
	}
	party, sport := 71, 72
	events := []struct {
		title string
		date  string
		typ   *int
	}{
		{"春节聚餐", "2026-02-10", &party},
		{"春日爬山", "2026-04-05", &sport},
		{"夏日饭局", "2026-07-01", &party},
		{"忘了哪天", "", &party},
	}
	for _, seed := range events {
		e := &Event{Title: seed.title, EventDate: seed.date, TypeID: seed.typ}
		if err := s.EventCreate(ctx, e, nil, nil); err != nil {
			t.Fatalf("EventCreate(%s): %v", seed.title, err)
		}
	}

	count := func(name string, f EventFilter, want int) {
		t.Helper()
		list, err := s.EventList(ctx, f)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(list) != want {
			t.Fatalf("%s 命中 %d 条，期望 %d：%+v", name, len(list), want, list)
		}
	}

	count("全量", EventFilter{}, 4)
	count("按类型", EventFilter{TypeID: party}, 3)
	count("起 3 月", EventFilter{From: "2026-03-01"}, 2)
	count("止 3 月", EventFilter{To: "2026-03-01"}, 1)
	count("区间", EventFilter{From: "2026-03-01", To: "2026-06-30"}, 1)
	count("类型+区间", EventFilter{TypeID: party, From: "2026-06-01"}, 1)
	// 边界含端点
	count("端点重合", EventFilter{From: "2026-04-05", To: "2026-04-05"}, 1)
	// 没日期的一条只在「不给区间」时出现
	list, err := s.EventList(ctx, EventFilter{})
	if err != nil {
		t.Fatalf("EventList: %v", err)
	}
	var undated *Event
	for _, e := range list {
		if e.Title == "忘了哪天" {
			undated = e
		}
	}
	if undated == nil {
		t.Fatalf("无日期的一条应在全量结果里")
	}
	filtered, err := s.EventList(ctx, EventFilter{From: "2020-01-01", To: "2030-01-01"})
	if err != nil {
		t.Fatalf("EventList 区间: %v", err)
	}
	for _, e := range filtered {
		if e.ID == undated.ID {
			t.Fatalf("给了区间还捞出无日期的一条：%+v", e)
		}
	}
	// 与既有条件共存：类型 + 关键字
	count("类型+关键字", EventFilter{TypeID: party, Q: "夏日"}, 1)
	// 区间不改变分页语义
	count("区间+分页", EventFilter{From: "2026-01-01", Limit: 1, Offset: 1}, 1)
}

func TestTxFilter_KindSettledAndRange(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := evMustPerson(t, s, "老李")
	seeds := []struct {
		kind     string
		occurred string
		settled  bool
	}{
		{"loan", "2026-01-05", false},
		{"gift", "2026-03-05", true},
		{"loan", "2026-05-05", true},
		{"expense", "", false},
	}
	for _, seed := range seeds {
		tx := &Transaction{
			PersonID: p.ID, Kind: seed.kind, Direction: "out", AmountFen: 1000,
			OccurredAt: seed.occurred, Settled: seed.settled,
		}
		if err := s.TransactionCreate(ctx, tx); err != nil {
			t.Fatalf("TransactionCreate(%s): %v", seed.kind, err)
		}
	}

	count := func(name string, f TxFilter, want int) {
		t.Helper()
		list, err := s.TransactionList(ctx, f)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(list) != want {
			t.Fatalf("%s 命中 %d 条，期望 %d：%+v", name, len(list), want, list)
		}
	}

	yes, no := true, false
	count("全量", TxFilter{}, 4)
	count("借还", TxFilter{Kind: "loan"}, 2)
	count("未结清", TxFilter{Settled: &no}, 2)
	count("已结清", TxFilter{Settled: &yes}, 2)
	count("未结清的借还", TxFilter{Kind: "loan", Settled: &no}, 1)
	count("起 2 月", TxFilter{From: "2026-02-01"}, 2)
	count("止 2 月", TxFilter{To: "2026-02-01"}, 1)
	count("区间", TxFilter{From: "2026-02-01", To: "2026-04-01"}, 1)
	// 空 occurred_at 不属于任何区间
	count("宽区间也不捞空日期", TxFilter{From: "2000-01-01", To: "2099-01-01"}, 3)
	// 按人过滤仍然有效
	count("按人", TxFilter{PersonID: p.ID}, 4)
	count("查无此人", TxFilter{PersonID: "ghost"}, 0)
	// 区间与分页共存
	count("区间+分页", TxFilter{From: "2026-01-01", Limit: 1, Offset: 1}, 1)
}
