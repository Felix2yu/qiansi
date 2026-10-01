package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	qiansiLunar "github.com/qiansi/app/internal/lunar"
)

// ===== 公共 helper =====

func evMustPerson(t *testing.T, s *Store, name string) *Person {
	t.Helper()
	p := &Person{Name: name, Grade: 3}
	if err := s.PersonCreate(context.Background(), p); err != nil {
		t.Fatalf("PersonCreate(%s): %v", name, err)
	}
	return p
}

func evLocalStamp(offsetDays int) string {
	return time.Now().AddDate(0, 0, offsetDays).Format("2006-01-02T15:04:05")
}

func evDate(offsetDays int) string {
	return time.Now().AddDate(0, 0, offsetDays).Format("2006-01-02")
}

// ===== Events CRUD =====

func TestEventStore_EventCRUDWithParticipantsAndExpenses(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// 事件类型供 TypeName/TypeColor 关联
	if _, err := s.DB.Exec(`INSERT INTO event_types(id,name,color) VALUES(1,'聚会','#123456')`); err != nil {
		t.Fatalf("seed event_types: %v", err)
	}
	p1 := evMustPerson(t, s, "甲")
	p2 := evMustPerson(t, s, "乙")

	typeID := 1
	e := &Event{
		Title:     "新年聚餐",
		TypeID:    &typeID,
		EventDate: "2026-03-01",
		Locations: []string{" 上海 ", "虹口", ""},
		Gift:      "两盒茶",
		HasGift:   true,
		Summary:   "老朋友聚会",
	}
	if err := s.EventCreate(ctx, e, []string{p1.ID, p2.ID}); err != nil {
		t.Fatalf("EventCreate: %v", err)
	}
	if e.ID == "" || e.CreatedAt == "" || e.UpdatedAt == "" {
		t.Fatalf("EventCreate 未回填 ID/时间: %+v", e)
	}

	// 重复 ID → INSERT 错误分支
	dup := &Event{ID: e.ID, Title: "x", EventDate: "2026-03-01"}
	if err := s.EventCreate(ctx, dup, nil); err == nil {
		t.Fatalf("重复 ID 的 EventCreate 应该报错")
	}
	// 参与人不存在 → FK 错误分支（participant 循环内 err）
	bad := &Event{Title: "坏参与人", EventDate: "2026-03-01"}
	if err := s.EventCreate(ctx, bad, []string{"no-such-person"}); err == nil {
		t.Fatalf("非法参与人 EventCreate 应该报错")
	}
	if bad.ID == "" {
		t.Fatalf("bad event 应有 ID")
	}

	got, err := s.EventGet(ctx, e.ID)
	if err != nil {
		t.Fatalf("EventGet: %v", err)
	}
	if got.TypeName == nil || *got.TypeName != "聚会" || got.TypeColor == nil || *got.TypeColor != "#123456" {
		t.Fatalf("类型关联未生效: %+v", got)
	}
	if len(got.Locations) != 2 || got.Locations[0] != "上海" || got.Locations[1] != "虹口" {
		t.Fatalf("locations 解码错误: %#v", got.Locations)
	}
	if len(got.Participants) != 2 {
		t.Fatalf("participants 数量 = %d, 期望 2", len(got.Participants))
	}
	if !got.HasGift || got.Gift != "两盒茶" {
		t.Fatalf("gift 字段丢失: %+v", got)
	}

	// 挂开销交易：只有 direction=out 计入 expense_fen
	tx1 := &Transaction{PersonID: p1.ID, Kind: "expense", Direction: "out", AmountFen: 8000, Title: "餐费", OccurredAt: "2026-03-01", EventID: e.ID}
	if err := s.TransactionCreate(ctx, tx1); err != nil {
		t.Fatalf("TransactionCreate out: %v", err)
	}
	if tx1.EventTitle != "新年聚餐" {
		t.Fatalf("TransactionCreate 应回填 EventTitle, got %q", tx1.EventTitle)
	}
	tx2 := &Transaction{PersonID: p2.ID, Kind: "gift", Direction: "in", AmountFen: 3000, Title: "礼金", OccurredAt: "2026-03-02", EventID: e.ID}
	if err := s.TransactionCreate(ctx, tx2); err != nil {
		t.Fatalf("TransactionCreate in: %v", err)
	}

	got, err = s.EventGet(ctx, e.ID)
	if err != nil {
		t.Fatalf("EventGet(带开销): %v", err)
	}
	if got.ExpenseFen != 8000 {
		t.Fatalf("ExpenseFen = %d, 期望 8000（只算 out）", got.ExpenseFen)
	}
	if len(got.Expenses) != 2 {
		t.Fatalf("Expenses = %d, 期望 2", len(got.Expenses))
	}
	// EventExpenses 按 occurred_at DESC：礼金(03-02) 在前
	if got.Expenses[0].ID != tx2.ID || got.Expenses[0].PersonName == "" {
		t.Fatalf("EventExpenses 排序/人名回填错误: %+v", got.Expenses[0])
	}
	if got.Expenses[0].EventID != e.ID {
		t.Fatalf("EventExpenses 应回填 EventID")
	}

	// 更新：改标题、换参与人、清空 locations 但保留 location 兼容字段
	e.Title = "新年聚餐2"
	e.Locations = nil
	e.Location = "静安"
	e.TypeID = nil
	if err := s.EventUpdate(ctx, e, []string{p2.ID}); err != nil {
		t.Fatalf("EventUpdate: %v", err)
	}
	got, err = s.EventGet(ctx, e.ID)
	if err != nil {
		t.Fatalf("EventGet(更新后): %v", err)
	}
	if got.Title != "新年聚餐2" || got.Location != "静安" || len(got.Locations) != 1 || got.Locations[0] != "静安" {
		t.Fatalf("更新后的字段不符: %+v", got)
	}
	if got.TypeName != nil {
		t.Fatalf("type_id 置空后 TypeName 应为 nil: %v", *got.TypeName)
	}
	if len(got.Participants) != 1 || got.Participants[0].ID != p2.ID {
		t.Fatalf("参与人未重建: %+v", got.Participants)
	}

	// 更新时非法参与人 → 循环内错误分支
	if err := s.EventUpdate(ctx, e, []string{"ghost"}); err == nil {
		t.Fatalf("非法参与人 EventUpdate 应该报错")
	}

	// 删除事件后关联交易 event_id 应被解除
	if err := s.EventDelete(ctx, e.ID); err != nil {
		t.Fatalf("EventDelete: %v", err)
	}
	if _, err := s.EventGet(ctx, e.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("删除后 EventGet 应 ErrNoRows, got %v", err)
	}
	tg, err := s.TransactionGet(ctx, tx1.ID)
	if err != nil {
		t.Fatalf("TransactionGet: %v", err)
	}
	if tg.EventID != "" {
		t.Fatalf("EventDelete 应解除 transactions.event_id, got %q", tg.EventID)
	}
}

func TestEventStore_EventListFilters(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	a := evMustPerson(t, s, "阿明")
	b := evMustPerson(t, s, "小红")

	e1 := &Event{Title: "茶馆偶遇", EventDate: "2026-05-01", Location: "西湖", Summary: "喝茶"}
	if err := s.EventCreate(ctx, e1, []string{a.ID, b.ID}); err != nil {
		t.Fatalf("EventCreate e1: %v", err)
	}
	e2 := &Event{Title: "聚餐", EventDate: "2026-06-01", Location: "饭店"}
	if err := s.EventCreate(ctx, e2, []string{b.ID}); err != nil {
		t.Fatalf("EventCreate e2: %v", err)
	}
	e3 := &Event{Title: "独处", EventDate: "2026-07-01"}
	if err := s.EventCreate(ctx, e3, nil); err != nil {
		t.Fatalf("EventCreate e3: %v", err)
	}
	// locations 为非法 JSON 的脏数据 → 回退到 location 兼容字段
	if _, err := s.DB.Exec(`INSERT INTO events(id,title,event_date,location,locations,has_gift,gift,summary,created_at,updated_at) VALUES('raw1','坏JSON','2026-04-01','老地方','{坏json',0,'','','t','t')`); err != nil {
		t.Fatalf("raw insert: %v", err)
	}

	list, err := s.EventList(ctx, "", "", 0, 0) // limit<=0 → 默认 100
	if err != nil {
		t.Fatalf("EventList: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("EventList 数量 = %d, 期望 4", len(list))
	}
	if list[0].ID != e3.ID {
		t.Fatalf("应按 event_date DESC 排序, got %s", list[0].Title)
	}
	if list[0].Locations != nil {
		t.Fatalf("空事件 locations 应为 nil, got %#v", list[0].Locations)
	}
	var raw *Event
	for _, e := range list {
		if e.ID == "raw1" {
			raw = e
		}
	}
	if raw == nil || len(raw.Locations) != 1 || raw.Locations[0] != "老地方" {
		t.Fatalf("坏 JSON locations 应回退 location, got %#v", raw)
	}
	// e1 有两位参与人（hydrate 分支）
	for _, e := range list {
		if e.ID == e1.ID && len(e.Participants) != 2 {
			t.Fatalf("e1 participants = %d, 期望 2", len(e.Participants))
		}
	}

	// person_id 过滤
	byPerson, err := s.EventList(ctx, a.ID, "", 10, 0)
	if err != nil || len(byPerson) != 1 || byPerson[0].ID != e1.ID {
		t.Fatalf("person_id 过滤错误: %v / %+v", err, byPerson)
	}
	// keyword 过滤（标题/摘要/地点）
	byQ, err := s.EventList(ctx, "", "饭店", 10, 0)
	if err != nil || len(byQ) != 1 || byQ[0].ID != e2.ID {
		t.Fatalf("keyword 过滤错误: %v / %+v", err, byQ)
	}
	bySummary, err := s.EventList(ctx, "", "喝茶", 10, 0)
	if err != nil || len(bySummary) != 1 || bySummary[0].ID != e1.ID {
		t.Fatalf("摘要关键词过滤错误: %v / %+v", err, bySummary)
	}
	// 组合过滤 + limit/offset
	both, err := s.EventList(ctx, b.ID, "聚餐", 5, 0)
	if err != nil || len(both) != 1 || both[0].ID != e2.ID {
		t.Fatalf("组合过滤错误: %v / %+v", err, both)
	}
	paged, err := s.EventList(ctx, "", "", 1, 1)
	if err != nil || len(paged) != 1 || paged[0].ID != e2.ID {
		t.Fatalf("分页错误: %v / %+v", err, paged)
	}
	// 空结果路径
	none, err := s.EventList(ctx, "", "不存在的关键词", 10, 0)
	if err != nil || len(none) != 0 {
		t.Fatalf("空结果期望 0, got %v / %d", err, len(none))
	}
}

// ===== Memos =====

func TestEventStore_Memos(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := evMustPerson(t, s, "小美")

	m := &Memo{PersonID: p.ID, Content: "答应帮他改简历", SaidAt: "2026-03-01T10:00:00", IsPromise: true, DueDate: "2026-03-20"}
	if err := s.MemoCreate(ctx, m); err != nil {
		t.Fatalf("MemoCreate: %v", err)
	}
	if m.ID == "" || m.Speaker != "other" || m.Status != "open" || m.CreatedAt == "" {
		t.Fatalf("MemoCreate 默认值未回填: %+v", m)
	}
	m2 := &Memo{Content: "随手记", SaidAt: "2026-02-01T10:00:00", Speaker: "me", Status: "fulfilled"}
	if err := s.MemoCreate(ctx, m2); err != nil {
		t.Fatalf("MemoCreate m2: %v", err)
	}
	// 指定 ID + 重复 → 错误分支
	m3 := &Memo{ID: m2.ID, Content: "dup", SaidAt: "2026-02-01T10:00:00"}
	if err := s.MemoCreate(ctx, m3); err == nil {
		t.Fatalf("重复 memo ID 应报错")
	}

	list, err := s.MemoList(ctx, "", false, 0, 0)
	if err != nil || len(list) != 2 {
		t.Fatalf("MemoList = %d, 期望 2 (%v)", len(list), err)
	}
	if list[0].SaidAt < list[1].SaidAt {
		t.Fatalf("MemoList 应按 said_at DESC")
	}
	promise, err := s.MemoList(ctx, p.ID, true, 10, 0)
	if err != nil || len(promise) != 1 || promise[0].DueDate != "2026-03-20" || promise[0].PersonID != p.ID {
		t.Fatalf("承诺过滤错误: %v / %+v", err, promise)
	}
	other, err := s.MemoList(ctx, "ghost-person", false, 10, 0)
	if err != nil || len(other) != 0 {
		t.Fatalf("未知人物应为空: %v / %d", err, len(other))
	}

	m.Status = "fulfilled"
	m.DueDate = ""
	if err := s.MemoUpdate(ctx, m); err != nil {
		t.Fatalf("MemoUpdate: %v", err)
	}
	up, err := s.MemoList(ctx, p.ID, true, 10, 0)
	if err != nil || len(up) != 1 || up[0].Status != "fulfilled" || up[0].DueDate != "" {
		t.Fatalf("更新后不符: %v / %+v", err, up)
	}
	if err := s.MemoDelete(ctx, m.ID); err != nil {
		t.Fatalf("MemoDelete: %v", err)
	}
	if l, err := s.MemoList(ctx, "", false, 50, 0); err != nil || len(l) != 1 {
		t.Fatalf("删除后应剩 1: %v / %d", err, len(l))
	}
}

// ===== Relationships =====

func TestEventStore_Relationships(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	a := evMustPerson(t, s, "张三")
	b := evMustPerson(t, s, "李四")

	r := &Relationship{FromPerson: a.ID, ToPerson: b.ID, Type: "同事", Remark: "同项目组"}
	if err := s.RelationshipCreate(ctx, r); err != nil {
		t.Fatalf("RelationshipCreate: %v", err)
	}
	if r.ID == "" {
		t.Fatalf("应生成 ID")
	}
	// 同一对人可以挂多种类型：旧版这里是 INSERT OR REPLACE，前一条会被静默顶掉
	r2 := &Relationship{FromPerson: a.ID, ToPerson: b.ID, Type: "好友"}
	if err := s.RelationshipCreate(ctx, r2); err != nil {
		t.Fatalf("RelationshipCreate(另一种类型): %v", err)
	}
	if r2.ID == r.ID {
		t.Fatalf("新建应拿到独立的 id: %s / %s", r.ID, r2.ID)
	}
	// 完全同名才拦
	dup := &Relationship{FromPerson: a.ID, ToPerson: b.ID, Type: "同事"}
	if err := s.RelationshipCreate(ctx, dup); !errors.Is(err, ErrRelationshipExists) {
		t.Fatalf("重复关系应报 ErrRelationshipExists，得到 %v", err)
	}
	list, err := s.RelationshipList(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("RelationshipList = %d (%v)", len(list), err)
	}
	// 列表没有稳定排序，按类型取，别依赖插入顺序
	byType := map[string]*Relationship{}
	for _, rel := range list {
		byType[rel.Type] = rel
	}
	tongshi, haoyou := byType["同事"], byType["好友"]
	if tongshi == nil || haoyou == nil {
		t.Fatalf("两条关系没都在: %+v", list)
	}
	if tongshi.FromName != "张三" || tongshi.ToName != "李四" || tongshi.Remark != "同项目组" {
		t.Fatalf("关系姓名/备注不符: %+v", tongshi)
	}
	// 更新撞名同样要拦，且不能把原来那条改掉
	haoyou.Type = "同事"
	if err := s.RelationshipUpdate(ctx, haoyou); !errors.Is(err, ErrRelationshipExists) {
		t.Fatalf("更新成已有类型应报 ErrRelationshipExists，得到 %v", err)
	}
	again, err := s.RelationshipList(ctx)
	if err != nil || len(again) != 2 {
		t.Fatalf("冲突的更新不该改条数: %d (%v)", len(again), err)
	}
	survived := map[string]bool{}
	for _, rel := range again {
		survived[rel.Type] = true
	}
	if !survived["同事"] || !survived["好友"] {
		t.Fatalf("冲突的更新不该落库: %v", again)
	}
	of, err := s.RelationshipsOf(ctx, b.ID)
	if err != nil || len(of) != 2 {
		t.Fatalf("RelationshipsOf = %d (%v)", len(of), err)
	}
	noneOf, err := s.RelationshipsOf(ctx, "ghost")
	if err != nil || len(noneOf) != 0 {
		t.Fatalf("未知人物 RelationshipsOf 应空: %v", err)
	}
	people, rels, graphTags, err := s.RelationshipGraph(ctx)
	if err != nil || len(people) != 2 || len(rels) != 2 {
		t.Fatalf("RelationshipGraph: %v / %d / %d", err, len(people), len(rels))
	}
	if graphTags == nil {
		t.Fatalf("RelationshipGraph 应返回 tags map（空也非 nil）")
	}
	// 后面的单条更新流程只留一条，免得计数绕来绕去
	if err := s.RelationshipDelete(ctx, haoyou.ID); err != nil {
		t.Fatalf("RelationshipDelete(好友): %v", err)
	}
	// 更新：类型、备注与两端都可改，ID 不变
	if err := s.RelationshipUpdate(ctx, &Relationship{
		ID: r.ID, FromPerson: b.ID, ToPerson: a.ID, Type: "老友", Remark: "重逢",
	}); err != nil {
		t.Fatalf("RelationshipUpdate: %v", err)
	}
	got, err := s.RelationshipList(ctx)
	if err != nil || len(got) != 1 {
		t.Fatalf("更新后 = %d (%v)", len(got), err)
	}
	if got[0].Type != "老友" || got[0].Remark != "重逢" || got[0].FromName != "李四" || got[0].ToName != "张三" {
		t.Fatalf("更新后内容不符: %+v", got[0])
	}
	// 备注置空落库为 NULL
	if err := s.RelationshipUpdate(ctx, &Relationship{ID: r.ID, FromPerson: a.ID, ToPerson: b.ID, Type: "同事"}); err != nil {
		t.Fatalf("RelationshipUpdate(清备注): %v", err)
	}
	if l, _ := s.RelationshipList(ctx); l[0].Remark != "" {
		t.Fatalf("清空备注失败: %+v", l[0])
	}
	// 更新不存在的 id => sql.ErrNoRows
	if err := s.RelationshipUpdate(ctx, &Relationship{ID: "ghost", FromPerson: a.ID, ToPerson: b.ID, Type: "t"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("更新未知关系错误 = %v，期望 sql.ErrNoRows", err)
	}
	if err := s.RelationshipDelete(ctx, r.ID); err != nil {
		t.Fatalf("RelationshipDelete: %v", err)
	}
	if l, _ := s.RelationshipList(ctx); len(l) != 0 {
		t.Fatalf("删除后应空")
	}
}

// ===== Transactions & Repayments =====

func TestEventStore_Transactions(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := evMustPerson(t, s, "老王")

	tx1 := &Transaction{PersonID: p.ID, Direction: "out", AmountFen: 1000, Title: "借款给老王", OccurredAt: "2026-01-01", DueDate: "2026-06-01"}
	if err := s.TransactionCreate(ctx, tx1); err != nil {
		t.Fatalf("TransactionCreate: %v", err)
	}
	if tx1.Kind != "other" || tx1.ID == "" || tx1.CreatedAt == "" {
		t.Fatalf("默认值未回填: %+v", tx1)
	}
	if err := s.TransactionCreate(ctx, &Transaction{ID: tx1.ID, PersonID: p.ID, Direction: "out", AmountFen: 1, OccurredAt: "2026-01-01"}); err == nil {
		t.Fatalf("重复 ID 应报错")
	}
	// settled=true 且未填 settled_at → 用 occurred_at
	tx2 := &Transaction{PersonID: p.ID, Kind: "gift", Direction: "in", AmountFen: 500, OccurredAt: "2026-02-02", Settled: true}
	if err := s.TransactionCreate(ctx, tx2); err != nil {
		t.Fatalf("TransactionCreate tx2: %v", err)
	}
	if tx2.SettledAt != "2026-02-02" {
		t.Fatalf("settled_at 应回填 occurred_at, got %q", tx2.SettledAt)
	}
	// 指向不存在事件的 event_id：回填查询失败但不报错
	tx3 := &Transaction{PersonID: p.ID, Kind: "expense", Direction: "out", AmountFen: 100, OccurredAt: "2026-02-03", EventID: "ghost-event"}
	if err := s.TransactionCreate(ctx, tx3); err != nil {
		t.Fatalf("TransactionCreate tx3: %v", err)
	}
	if tx3.EventTitle != "" {
		t.Fatalf("幽灵事件不应有标题")
	}

	got, err := s.TransactionGet(ctx, tx1.ID)
	if err != nil {
		t.Fatalf("TransactionGet: %v", err)
	}
	if got.PersonName != "老王" || got.Title != "借款给老王" || got.DueDate != "2026-06-01" || got.RepaidFen != 0 {
		t.Fatalf("扫描字段不符: %+v", got)
	}
	if _, err := s.TransactionGet(ctx, "nope"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("未知交易应 ErrNoRows, got %v", err)
	}

	// 还款：RepaidFen 子查询
	rp := &Repayment{TransactionID: tx1.ID, AmountFen: 400, OccurredAt: "2026-03-01", Note: "微信转账"}
	if err := s.RepaymentCreate(ctx, rp); err != nil {
		t.Fatalf("RepaymentCreate: %v", err)
	}
	if rp.ID == "" {
		t.Fatalf("RepaymentCreate 应生成 ID")
	}
	fixed := &Repayment{ID: "rp-fixed", TransactionID: tx1.ID, AmountFen: 200, OccurredAt: "2026-04-01"}
	if err := s.RepaymentCreate(ctx, fixed); err != nil {
		t.Fatalf("RepaymentCreate fixed: %v", err)
	}
	got, _ = s.TransactionGet(ctx, tx1.ID)
	if got.RepaidFen != 600 {
		t.Fatalf("RepaidFen = %d, 期望 600", got.RepaidFen)
	}
	rps, err := s.RepaymentsOf(ctx, tx1.ID)
	if err != nil || len(rps) != 2 || rps[0].ID != fixed.ID || rps[1].Note != "微信转账" {
		t.Fatalf("RepaymentsOf: %v / %+v", err, rps)
	}
	empty, err := s.RepaymentsOf(ctx, "no-tx")
	if err != nil || len(empty) != 0 {
		t.Fatalf("无还款应空: %v", err)
	}
	if err := s.RepaymentDelete(ctx, rp.ID); err != nil {
		t.Fatalf("RepaymentDelete: %v", err)
	}
	got, _ = s.TransactionGet(ctx, tx1.ID)
	if got.RepaidFen != 200 {
		t.Fatalf("删除还款后 RepaidFen = %d, 期望 200", got.RepaidFen)
	}

	// 更新各字段 + 置空可选字段
	tx1.Title = ""
	tx1.DueDate = ""
	tx1.Settled = true
	tx1.SettledAt = "2026-05-01"
	tx1.Kind = "loan"
	if err := s.TransactionUpdate(ctx, tx1); err != nil {
		t.Fatalf("TransactionUpdate: %v", err)
	}
	got, _ = s.TransactionGet(ctx, tx1.ID)
	if got.Title != "" || got.DueDate != "" || !got.Settled || got.SettledAt != "2026-05-01" || got.Kind != "loan" {
		t.Fatalf("更新后不符: %+v", got)
	}

	// 列表：全量 / 按人 / 分页 / 空
	all, err := s.TransactionList(ctx, "", 0, 0)
	if err != nil || len(all) != 3 {
		t.Fatalf("TransactionList 全量 = %d (%v)", len(all), err)
	}
	if all[0].OccurredAt < all[1].OccurredAt {
		t.Fatalf("应按 occurred_at DESC")
	}
	byP, err := s.TransactionList(ctx, p.ID, 2, 1)
	if err != nil || len(byP) != 2 {
		t.Fatalf("按人分页 = %d (%v)", len(byP), err)
	}
	none, err := s.TransactionList(ctx, "ghost", 10, 0)
	if err != nil || len(none) != 0 {
		t.Fatalf("未知人应空: %v", err)
	}

	if err := s.TransactionDelete(ctx, tx1.ID); err != nil {
		t.Fatalf("TransactionDelete: %v", err)
	}
	if _, err := s.TransactionGet(ctx, tx1.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("删除后应 ErrNoRows")
	}
	// 还款随事务级联删除
	if l, err := s.RepaymentsOf(ctx, tx1.ID); err != nil || len(l) != 0 {
		t.Fatalf("级联删除还款失败: %v / %d", err, len(l))
	}
}

// ===== Anniversaries =====

func TestEventStore_AnniversaryCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := evMustPerson(t, s, "阿珍")

	a := &Anniversary{PersonID: p.ID, Title: "结婚纪念日", Date: "2020-05-20", RepeatYearly: true}
	if err := s.AnniversaryCreate(ctx, a); err != nil {
		t.Fatalf("AnniversaryCreate: %v", err)
	}
	if a.ID == "" || a.RemindDays != "7,3,1,0" || a.CreatedAt == "" {
		t.Fatalf("默认值未回填: %+v", a)
	}
	b := &Anniversary{Title: "无归属日", Date: "2021-01-01", RemindDays: "0"}
	if err := s.AnniversaryCreate(ctx, b); err != nil {
		t.Fatalf("AnniversaryCreate b: %v", err)
	}
	list, err := s.AnniversaryList(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("AnniversaryList = %d (%v)", len(list), err)
	}
	if list[0].Date != "2020-05-20" {
		t.Fatalf("应按 date 排序")
	}
	if list[0].PersonName != "阿珍" || list[0].PersonID != p.ID {
		t.Fatalf("关联人物不符: %+v", list[0])
	}
	if list[1].PersonName != "" || list[1].PersonID != "" {
		t.Fatalf("无归属纪念日应为空: %+v", list[1])
	}

	a.Title = "十周年"
	a.IsLunar = true
	a.PersonID = ""
	if err := s.AnniversaryUpdate(ctx, a); err != nil {
		t.Fatalf("AnniversaryUpdate: %v", err)
	}
	list, _ = s.AnniversaryList(ctx)
	for _, x := range list {
		if x.ID == a.ID {
			if !x.IsLunar || x.Title != "十周年" || x.PersonID != "" {
				t.Fatalf("更新后不符: %+v", x)
			}
		}
	}
	if err := s.AnniversaryDelete(ctx, b.ID); err != nil {
		t.Fatalf("AnniversaryDelete: %v", err)
	}
	if l, _ := s.AnniversaryList(ctx); len(l) != 1 {
		t.Fatalf("删除后应剩 1")
	}
}

func TestEventStore_AnniversaryUpcomingLunarAndDismiss(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	today := qiansiLunar.NowLocal()

	// 阳历：日期恰为明天，remind_days 里 1/3/7 中只有 0 和 1 落在今天之后
	p := evMustPerson(t, s, "小林")
	tomorrow := today.AddDays(1)
	solar := &Anniversary{PersonID: p.ID, Title: "相识", Date: tomorrow.String(), RepeatYearly: true, RemindDays: "7,3,1,0"}
	if err := s.AnniversaryCreate(ctx, solar); err != nil {
		t.Fatalf("create solar: %v", err)
	}
	// 农历：挑一个未来 30 天内可精确回换算的农历日，保证确定性
	lunarDate := qiansiLunar.YMD{}
	for d := 1; d <= 30; d++ {
		cand := today.AddDays(d)
		lm, ld := qiansiLunar.SolarToLunar(cand)
		if lm <= 0 || ld <= 0 {
			continue // 闰月（负值）无法用 YYYY-MM-DD 表达
		}
		if qiansiLunar.LunarToSolar(lm, ld, today) == cand {
			lunarDate = qiansiLunar.YMD{Year: cand.Year, Month: lm, Day: ld}
			break
		}
	}
	if lunarDate.Year == 0 {
		t.Skipf("找不到 30 天内可回换算的农历日")
	}
	lunar := &Anniversary{Title: "农历生日", Date: lunarDate.String(), IsLunar: true, RemindDays: "0"}
	if err := s.AnniversaryCreate(ctx, lunar); err != nil {
		t.Fatalf("create lunar: %v", err)
	}
	// 标题/日期为空的脏数据 → continue 分支
	dirty := &Anniversary{Title: "", Date: "", RemindDays: "0"}
	if err := s.AnniversaryCreate(ctx, dirty); err != nil {
		t.Fatalf("create dirty: %v", err)
	}

	list, err := s.AnniversaryUpcoming(ctx, 30)
	if err != nil {
		t.Fatalf("AnniversaryUpcoming: %v", err)
	}
	// solar: offset 0（明天）+ offset 1（今天）→ 2 条；lunar 1 条；dirty 0 条
	if len(list) != 3 {
		t.Fatalf("数量 = %d, 期望 3: %+v", len(list), list)
	}
	var solarToday, solarTomorrow, lunarItem *Reminder
	for _, r := range list {
		switch {
		case r.RefID == solar.ID && r.DueAt[:10] == tomorrow.String():
			solarTomorrow = r
		case r.RefID == solar.ID && r.DueAt[:10] == today.String():
			solarToday = r
		case r.RefID == lunar.ID:
			lunarItem = r
		}
	}
	if solarToday == nil || solarTomorrow == nil {
		t.Fatalf("未按提前量生成提醒: %+v", list)
	}
	if solarTomorrow.Title == "" || solarTomorrow.RefType != "anniversary" || solarTomorrow.PersonName != "小林" {
		t.Fatalf("衍生提醒字段不符: %+v", solarTomorrow)
	}
	if !strings.Contains(solarToday.Title, "还有 1 天") {
		t.Fatalf("提前提醒标题不符: %s", solarToday.Title)
	}
	if !strings.Contains(solarTomorrow.Title, "今天") {
		t.Fatalf("当日提醒标题不符: %s", solarTomorrow.Title)
	}
	expectedLunar := qiansiLunar.LunarToSolar(lunarDate.Month, lunarDate.Day, today)
	if lunarItem == nil || !strings.Contains(lunarItem.Title, "农历") || !strings.Contains(lunarItem.Title, expectedLunar.String()) {
		t.Fatalf("农历换算不符: %+v 期望 %s", lunarItem, expectedLunar)
	}
	if solarTomorrow.ID != "anniv:"+solar.ID+":"+tomorrow.String()+":0" {
		t.Fatalf("衍生提醒 ID 结构不符: %s", solarTomorrow.ID)
	}

	// horizon=0 → 不设上限（cutoff 仅在 horizonDays>0 时生效），只应剔除过去的日期
	refToday := qiansiLunar.NowLocal()
	narrow, err := s.AnniversaryUpcoming(ctx, 0)
	if err != nil {
		t.Fatalf("horizon 0: %v", err)
	}
	for _, r := range narrow {
		if r.DueAt[:10] < refToday.String() {
			t.Fatalf("horizon=0 不应包含已过期的提醒: %+v", r)
		}
	}

	// dismiss 当日这一次（occurrence = "<next>:<offset>"）
	occ := tomorrow.String() + ":0"
	if err := s.AnniversaryDismiss(ctx, solar.ID, occ); err != nil {
		t.Fatalf("AnniversaryDismiss: %v", err)
	}
	// 空参数分支：不写入、不报错
	if err := s.AnniversaryDismiss(ctx, "", occ); err != nil {
		t.Fatalf("空 ID dismiss 应无错: %v", err)
	}
	after, err := s.AnniversaryUpcoming(ctx, 30)
	if err != nil {
		t.Fatalf("dismiss 后查询: %v", err)
	}
	for _, r := range after {
		if r.ID == "anniv:"+solar.ID+":"+occ {
			t.Fatalf("dismiss 后仍出现该提醒")
		}
	}
	if len(after) != 2 {
		t.Fatalf("dismiss 后数量 = %d, 期望 2", len(after))
	}
	// 重复 dismiss 同一 occurrence → INSERT OR IGNORE 不报错
	if err := s.AnniversaryDismiss(ctx, solar.ID, occ); err != nil {
		t.Fatalf("重复 dismiss: %v", err)
	}
}

// ===== Reminders =====

func TestEventStore_Reminders(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := evMustPerson(t, s, "阿强")

	r1 := &Reminder{PersonID: p.ID, RefType: "custom", RefID: "ref-1", Title: "还书", DueAt: evLocalStamp(2)}
	if err := s.ReminderCreate(ctx, r1); err != nil {
		t.Fatalf("ReminderCreate r1: %v", err)
	}
	if r1.ID == "" || r1.Status != "pending" || r1.CreatedAt == "" {
		t.Fatalf("默认值未回填: %+v", r1)
	}
	// RefType 为空 → custom；无人物无 ref
	r2 := &Reminder{Title: "远期的事", DueAt: evLocalStamp(40)}
	if err := s.ReminderCreate(ctx, r2); err != nil {
		t.Fatalf("ReminderCreate r2: %v", err)
	}
	if r2.RefType != "custom" {
		t.Fatalf("RefType 默认 custom, got %q", r2.RefType)
	}
	// 重复 ID → 错误
	if err := s.ReminderCreate(ctx, &Reminder{ID: r1.ID, Title: "dup", DueAt: evLocalStamp(1)}); err == nil {
		t.Fatalf("重复 reminder ID 应报错")
	}

	// Upcoming：horizon=7 只含 r1
	up7, err := s.ReminderUpcoming(ctx, 7)
	if err != nil {
		t.Fatalf("ReminderUpcoming(7): %v", err)
	}
	if len(up7) != 1 || up7[0].ID != r1.ID || up7[0].PersonName != "阿强" || up7[0].RefID != "ref-1" {
		t.Fatalf("horizon=7 结果不符: %+v", up7)
	}
	// horizon=0 → 不限
	upAll, err := s.ReminderUpcoming(ctx, 0)
	if err != nil || len(upAll) != 2 || upAll[0].ID != r1.ID || upAll[1].ID != r2.ID {
		t.Fatalf("horizon=0 应含全部且有序: %v / %+v", err, upAll)
	}

	// List：状态过滤 + 默认 limit + offset
	pend, err := s.ReminderList(ctx, "pending", 0, 0)
	if err != nil || len(pend) != 2 {
		t.Fatalf("pending list = %d (%v)", len(pend), err)
	}
	all, err := s.ReminderList(ctx, "", 1, 1)
	if err != nil || len(all) != 1 || all[0].ID != r2.ID {
		t.Fatalf("分页 list 不符: %v / %+v", err, all)
	}
	done, err := s.ReminderList(ctx, "done", 10, 0)
	if err != nil || len(done) != 0 {
		t.Fatalf("done 应为空: %v", err)
	}

	// Update 全字段
	r1.Status = "snooze"
	r1.RefID = ""
	r1.PersonID = ""
	r1.CompletedAt = evLocalStamp(0)
	if err := s.ReminderUpdate(ctx, r1); err != nil {
		t.Fatalf("ReminderUpdate: %v", err)
	}
	l2, _ := s.ReminderList(ctx, "", 10, 0)
	for _, r := range l2 {
		if r.ID == r1.ID {
			if r.Status != "snooze" || r.RefID != "" || r.PersonID != "" || r.CompletedAt == "" {
				t.Fatalf("更新后不符: %+v", r)
			}
		}
	}

	// Done
	if err := s.ReminderDone(ctx, r1.ID); err != nil {
		t.Fatalf("ReminderDone: %v", err)
	}
	up, err := s.ReminderUpcoming(ctx, 0)
	if err != nil || len(up) != 1 || up[0].ID != r2.ID {
		t.Fatalf("done 后 upcoming 应只剩 r2: %v / %+v", err, up)
	}
	// 未知 ID 的 Done 影响 0 行但不报错
	if err := s.ReminderDone(ctx, "ghost"); err != nil {
		t.Fatalf("ReminderDone(ghost): %v", err)
	}
	if err := s.ReminderDelete(ctx, r2.ID); err != nil {
		t.Fatalf("ReminderDelete: %v", err)
	}
	if l, _ := s.ReminderList(ctx, "", 10, 0); len(l) != 1 {
		t.Fatalf("删除后应剩 1")
	}
}

func TestEventStore_ReminderUpcomingMergesAnniversaries(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	today := qiansiLunar.NowLocal()

	rem := &Reminder{Title: "买花", DueAt: today.AddDays(2).String() + "T08:00:00"}
	if err := s.ReminderCreate(ctx, rem); err != nil {
		t.Fatalf("ReminderCreate: %v", err)
	}
	ann := &Anniversary{Title: "生日", Date: today.AddDays(2).String(), RemindDays: "0"}
	if err := s.AnniversaryCreate(ctx, ann); err != nil {
		t.Fatalf("AnniversaryCreate: %v", err)
	}
	up, err := s.ReminderUpcoming(ctx, 7)
	if err != nil {
		t.Fatalf("ReminderUpcoming: %v", err)
	}
	if len(up) != 2 {
		t.Fatalf("应合并纪念日衍生: %+v", up)
	}
	if up[0].DueAt > up[1].DueAt {
		t.Fatalf("合并后应按 due_at 排序: %s > %s", up[0].DueAt, up[1].DueAt)
	}
	if up[0].RefType != "custom" || up[1].RefType != "anniversary" {
		t.Fatalf("排序后类型不符: %+v", up)
	}
}

// ===== Search =====

func TestEventStore_Search(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := &Person{Name: "王小明", Phone: "13800000000", Wechat: "wx-xiaoming", Grade: 4}
	if err := s.PersonCreate(ctx, p); err != nil {
		t.Fatalf("PersonCreate: %v", err)
	}
	p2 := &Person{Name: "小芳姐", Wechat: "wx-fang"}
	if err := s.PersonCreate(ctx, p2); err != nil {
		t.Fatalf("PersonCreate p2: %v", err)
	}
	e := &Event{Title: "与小明喝茶", EventDate: "2026-04-01", Summary: "聊了近况"}
	if err := s.EventCreate(ctx, e, nil); err != nil {
		t.Fatalf("EventCreate: %v", err)
	}
	m := &Memo{Content: "小明说过年来", SaidAt: "2026-01-01T10:00:00", DueDate: "2026-12-01"}
	if err := s.MemoCreate(ctx, m); err != nil {
		t.Fatalf("MemoCreate: %v", err)
	}
	tx := &Transaction{PersonID: p.ID, Kind: "loan", Direction: "out", AmountFen: 100, Title: "借给小明", OccurredAt: "2026-02-01"}
	if err := s.TransactionCreate(ctx, tx); err != nil {
		t.Fatalf("TransactionCreate: %v", err)
	}
	an := &Anniversary{Title: "小明生日", Date: "1990-06-01"}
	if err := s.AnniversaryCreate(ctx, an); err != nil {
		t.Fatalf("AnniversaryCreate: %v", err)
	}

	// 空查询
	if res, err := s.Search(ctx, "   ", 3); err != nil || len(res) != 0 {
		t.Fatalf("空查询应空: %v / %d", err, len(res))
	}

	res, err := s.Search(ctx, "小", 0) // perType<=0 → 默认 5
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	byType := map[string]int{}
	for _, r := range res {
		byType[r.Type]++
	}
	for _, typ := range []string{"person", "event", "memo", "transaction", "anniversary"} {
		if byType[typ] == 0 {
			t.Fatalf("Search 缺少类型 %s: %+v", typ, res)
		}
	}
	for _, r := range res {
		switch r.Type {
		case "person":
			if r.Path != "/people/"+r.ID {
				t.Fatalf("person path: %s", r.Path)
			}
			if r.ID == p.ID && (!strings.Contains(r.Subtitle, "13800000000") || !strings.Contains(r.Subtitle, "微信 wx-xiaoming")) {
				t.Fatalf("person subtitle: %s", r.Subtitle)
			}
			if r.ID == p2.ID && !strings.HasPrefix(r.Subtitle, "微信 ") {
				t.Fatalf("仅微信 subtitle: %s", r.Subtitle)
			}
		case "event":
			if r.Path != "/events" || r.Date != "2026-04-01" || r.Subtitle != "聊了近况" {
				t.Fatalf("event 结果不符: %+v", r)
			}
		case "memo":
			if r.Path != "/memos" || r.Subtitle != "2026-12-01" {
				t.Fatalf("memo 结果不符: %+v", r)
			}
		case "transaction":
			if r.Path != "/money" {
				t.Fatalf("transaction path: %s", r.Path)
			}
		case "anniversary":
			if r.Path != "/anniversaries" || r.Date != "1990-06-01" {
				t.Fatalf("anniversary 结果不符: %+v", r)
			}
		}
	}

	// 无匹配 → 空结果路径
	none, err := s.Search(ctx, "查无此人", 2)
	if err != nil || len(none) != 0 {
		t.Fatalf("无匹配应空: %v / %+v", err, none)
	}
}

// ===== Timeline =====

func TestEventStore_Timelines(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := evMustPerson(t, s, "大毛")
	q := evMustPerson(t, s, "二毛")

	e := &Event{Title: "爬山", EventDate: "2026-03-05"}
	if err := s.EventCreate(ctx, e, []string{p.ID, q.ID}); err != nil {
		t.Fatalf("EventCreate: %v", err)
	}
	m := &Memo{PersonID: p.ID, Content: "约好下周见面", SaidAt: "2026-04-01T10:00:00"}
	if err := s.MemoCreate(ctx, m); err != nil {
		t.Fatalf("MemoCreate: %v", err)
	}
	tx := &Transaction{PersonID: p.ID, Kind: "gift", Direction: "in", AmountFen: 200, Title: "收到特产", OccurredAt: "2026-05-01"}
	if err := s.TransactionCreate(ctx, tx); err != nil {
		t.Fatalf("TransactionCreate: %v", err)
	}
	an := &Anniversary{PersonID: p.ID, Title: "认识十周年", Date: "2026-06-01"}
	if err := s.AnniversaryCreate(ctx, an); err != nil {
		t.Fatalf("AnniversaryCreate: %v", err)
	}

	tl, err := s.PersonTimeline(ctx, p.ID)
	if err != nil {
		t.Fatalf("PersonTimeline: %v", err)
	}
	if len(tl) != 4 {
		t.Fatalf("PersonTimeline = %d, 期望 4: %+v", len(tl), tl)
	}
	if tl[0].Date < tl[3].Date {
		t.Fatalf("PersonTimeline 应降序")
	}
	for _, it := range tl {
		// GROUP_CONCAT 顺序不保证，只校验两位都在
		if it.Type == "event" && (!strings.Contains(it.PersonName, "大毛") || !strings.Contains(it.PersonName, "二毛")) {
			t.Fatalf("event 参与人拼接不符: %q", it.PersonName)
		}
		// 非 event 行不回填 PersonName（scan 分支只认 event）
		if it.Type == "memo" && (it.PersonName != "" || it.PersonID != p.ID) {
			t.Fatalf("memo 行 PersonID/PersonName 不符: %+v", it)
		}
		if it.Type != "event" && it.ID == "" {
			t.Fatalf("时间线行缺少 ID")
		}
	}
	// 只参与了事件的人 → 仅 1 条
	empty, err := s.PersonTimeline(ctx, q.ID)
	if err != nil || len(empty) != 1 {
		t.Fatalf("q 只参与了事件: %v / %d", err, len(empty))
	}

	g, err := s.GlobalTimeline(ctx, 0) // limit<=0 → 100
	if err != nil || len(g) != 4 {
		t.Fatalf("GlobalTimeline = %d (%v)", len(g), err)
	}
	if g[0].Date < g[len(g)-1].Date {
		t.Fatalf("GlobalTimeline 应降序")
	}
	if g[0].PersonID != "" {
		t.Fatalf("GlobalTimeline 不应带 PersonID")
	}
	one, err := s.GlobalTimeline(ctx, 1)
	if err != nil || len(one) != 1 {
		t.Fatalf("GlobalTimeline limit=1: %v / %d", err, len(one))
	}
}

// ===== Dashboard / Stats =====

func TestEventStore_DashboardStats(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	// 空库路径
	st, err := s.DashboardStats(ctx)
	if err != nil || st == nil {
		t.Fatalf("空库 DashboardStats: %v", err)
	}
	if st["total_people"].(int) != 0 || st["total_events"].(int) != 0 {
		t.Fatalf("空库计数应为 0: %+v", st)
	}

	a := evMustPerson(t, s, "在编")
	b := evMustPerson(t, s, "已归档")
	if err := s.PersonArchive(ctx, b.ID); err != nil {
		t.Fatalf("PersonArchive: %v", err)
	}
	if err := s.PersonUnarchive(ctx, b.ID); err != nil {
		t.Fatalf("PersonUnarchive: %v", err)
	}
	if err := s.PersonArchive(ctx, b.ID); err != nil {
		t.Fatalf("PersonArchive again: %v", err)
	}
	e := &Event{Title: "聚会", EventDate: evDate(0)}
	if err := s.EventCreate(ctx, e, []string{a.ID}); err != nil {
		t.Fatalf("EventCreate: %v", err)
	}
	m := &Memo{PersonID: a.ID, Content: "欠他一顿饭", SaidAt: evLocalStamp(0), IsPromise: true}
	if err := s.MemoCreate(ctx, m); err != nil {
		t.Fatalf("MemoCreate: %v", err)
	}
	// 今天到期 + 3 天后到期
	if err := s.ReminderCreate(ctx, &Reminder{Title: "今天", DueAt: evDate(0) + "T09:00:00"}); err != nil {
		t.Fatalf("ReminderCreate today: %v", err)
	}
	if err := s.ReminderCreate(ctx, &Reminder{Title: "三天后", DueAt: evLocalStamp(3)}); err != nil {
		t.Fatalf("ReminderCreate 3d: %v", err)
	}
	// 借出未结 1000 已还 400 → lend 600；借入未结 500 → borrow 500
	lend := &Transaction{PersonID: a.ID, Kind: "loan", Direction: "out", AmountFen: 1000, OccurredAt: "2026-01-01"}
	if err := s.TransactionCreate(ctx, lend); err != nil {
		t.Fatalf("tx lend: %v", err)
	}
	if err := s.RepaymentCreate(ctx, &Repayment{TransactionID: lend.ID, AmountFen: 400, OccurredAt: "2026-02-01"}); err != nil {
		t.Fatalf("repay: %v", err)
	}
	borrow := &Transaction{PersonID: a.ID, Kind: "loan", Direction: "in", AmountFen: 500, OccurredAt: "2026-01-02"}
	if err := s.TransactionCreate(ctx, borrow); err != nil {
		t.Fatalf("tx borrow: %v", err)
	}
	// 已结清不应计入
	paid := &Transaction{PersonID: a.ID, Kind: "loan", Direction: "out", AmountFen: 9999, OccurredAt: "2026-01-03", Settled: true, SettledAt: "2026-01-03"}
	if err := s.TransactionCreate(ctx, paid); err != nil {
		t.Fatalf("tx paid: %v", err)
	}

	st, err = s.DashboardStats(ctx)
	if err != nil {
		t.Fatalf("DashboardStats: %v", err)
	}
	if got := st["total_people"].(int); got != 1 {
		t.Fatalf("total_people = %d, 期望 1（排除归档）", got)
	}
	if got := st["total_events"].(int); got != 1 {
		t.Fatalf("total_events = %d", got)
	}
	if got := st["pending_promises"].(int); got != 1 {
		t.Fatalf("pending_promises = %d", got)
	}
	if got := st["upcoming_days7"].(int); got != 2 {
		t.Fatalf("upcoming_days7 = %d, 期望 2", got)
	}
	if got := st["due_today"].(int); got != 1 {
		t.Fatalf("due_today = %d, 期望 1", got)
	}
	if got := st["lend_fen"].(int64); got != 600 {
		t.Fatalf("lend_fen = %d, 期望 600", got)
	}
	if got := st["borrow_fen"].(int64); got != 500 {
		t.Fatalf("borrow_fen = %d, 期望 500", got)
	}
}

func TestEventStore_StatsByMonthAndGrades(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	// 空库
	list, err := s.StatsByMonth(ctx, 0)
	if err != nil || len(list) != 0 {
		t.Fatalf("空库 StatsByMonth: %v / %+v", err, list)
	}
	gd, err := s.GradeDistribution(ctx)
	if err != nil || len(gd) != 0 {
		t.Fatalf("空库 GradeDistribution: %v", err)
	}

	e1 := &Event{Title: "一月聚会", EventDate: "2026-01-10"}
	if err := s.EventCreate(ctx, e1, nil); err != nil {
		t.Fatalf("EventCreate: %v", err)
	}
	e2 := &Event{Title: "无日期", EventDate: ""}
	if err := s.EventCreate(ctx, e2, nil); err != nil {
		t.Fatalf("EventCreate e2: %v", err)
	}
	m := &Memo{Content: "二月备忘", SaidAt: "2026-02-05T10:00:00"}
	if err := s.MemoCreate(ctx, m); err != nil {
		t.Fatalf("MemoCreate: %v", err)
	}
	tx := &Transaction{PersonID: evMustPerson(t, s, "某人").ID, Kind: "expense", Direction: "out", AmountFen: 1, OccurredAt: "2026-02-20"}
	if err := s.TransactionCreate(ctx, tx); err != nil {
		t.Fatalf("TransactionCreate: %v", err)
	}

	list, err = s.StatsByMonth(ctx, 12)
	if err != nil {
		t.Fatalf("StatsByMonth: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("月份数 = %d, 期望 2（空日期被剔除）: %+v", len(list), list)
	}
	if list[0]["month"] != "2026-02" || list[0]["memo_count"].(int) != 1 || list[0]["tx_count"].(int) != 1 {
		t.Fatalf("2026-02 统计不符: %+v", list[0])
	}
	if list[1]["month"] != "2026-01" || list[1]["event_count"].(int) != 1 {
		t.Fatalf("2026-01 统计不符: %+v", list[1])
	}

	// GradeDistribution：不同等级
	for _, g := range []int{3, 3, 5} {
		p := &Person{Name: "grade" + time.Now().Format("150405.00000") + string(rune('a'+g)), Grade: g}
		if err := s.PersonCreate(ctx, p); err != nil {
			t.Fatalf("PersonCreate grade %d: %v", g, err)
		}
	}
	gd, err = s.GradeDistribution(ctx)
	if err != nil || len(gd) < 2 {
		t.Fatalf("GradeDistribution: %v / %+v", err, gd)
	}
	var g3, g5 *GradeDist
	for _, x := range gd {
		if x.Grade == 3 {
			g3 = x
		}
		if x.Grade == 5 {
			g5 = x
		}
	}
	if g3 == nil || g3.Count < 3 || g5 == nil || g5.Count < 1 {
		t.Fatalf("等级分布不符: %+v", gd)
	}
}

// ===== Attachments =====

func TestEventStore_Attachments(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	a := &Attachment{EntityType: "event", EntityID: "ev-1", FileName: "合影.jpg", StoredName: "u1.jpg", Mime: "image/jpeg", Size: 1234}
	if err := s.AttachmentCreate(ctx, a); err != nil {
		t.Fatalf("AttachmentCreate: %v", err)
	}
	if a.ID == "" || a.CreatedAt == "" {
		t.Fatalf("附件默认字段未回填")
	}
	b := &Attachment{ID: "att-fixed", EntityType: "event", EntityID: "ev-1", FileName: "b.txt", StoredName: "u2.txt", Mime: "text/plain", Size: 2}
	if err := s.AttachmentCreate(ctx, b); err != nil {
		t.Fatalf("AttachmentCreate b: %v", err)
	}
	// 错误分支：重复 ID
	if err := s.AttachmentCreate(ctx, &Attachment{ID: "att-fixed", EntityType: "event", EntityID: "ev-1", FileName: "x", StoredName: "y", Mime: "z"}); err == nil {
		t.Fatalf("重复附件 ID 应报错")
	}

	list, err := s.AttachmentsOf(ctx, "event", "ev-1")
	if err != nil || len(list) != 2 {
		t.Fatalf("AttachmentsOf = %d (%v)", len(list), err)
	}
	other, err := s.AttachmentsOf(ctx, "person", "ghost")
	if err != nil || len(other) != 0 {
		t.Fatalf("无附件应空: %v", err)
	}
	deleted, err := s.AttachmentDelete(ctx, a.ID)
	if err != nil || deleted.ID != a.ID || deleted.FileName != "合影.jpg" {
		t.Fatalf("AttachmentDelete 应返回被删行: %v / %+v", err, deleted)
	}
	if _, err := s.AttachmentDelete(ctx, "ghost"); err == nil {
		t.Fatalf("删除未知附件应报错")
	}
	l2, _ := s.AttachmentsOf(ctx, "event", "ev-1")
	if len(l2) != 1 {
		t.Fatalf("删除后应剩 1")
	}
}

// ===== PersonDetail / Intimacy / WordCloud =====

func TestEventStore_PersonDetailIntimacyWordCloud(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := &Person{Name: "小赵", Grade: 5}
	if err := s.PersonCreate(ctx, p); err != nil {
		t.Fatalf("PersonCreate: %v", err)
	}
	if _, err := s.DB.Exec(`INSERT INTO person_fields(id,person_id,label,value,sort_order) VALUES('f1',?,?,?,1)`, p.ID, "公司", "某厂"); err != nil {
		t.Fatalf("seed person_fields: %v", err)
	}
	person, fields, err := s.PersonDetail(ctx, p.ID)
	if err != nil || person == nil || person.Name != "小赵" || len(fields) != 1 || fields[0].Value != "某厂" {
		t.Fatalf("PersonDetail: %v / %+v / %+v", err, person, fields)
	}
	if _, _, err := s.PersonDetail(ctx, "ghost"); err == nil {
		t.Fatalf("未知人 PersonDetail 应报错")
	}

	// 近 60 天两场事件 → recentCount=2；score 5*20+2*3+10=116 → 截断 100
	for _, d := range []string{evDate(-1), evDate(-30)} {
		e := &Event{Title: "聚会" + d, EventDate: d}
		if err := s.EventCreate(ctx, e, []string{p.ID}); err != nil {
			t.Fatalf("EventCreate: %v", err)
		}
	}
	// 快照：昨天一个点 → 趋势补今天
	if err := s.PersonIntimacySnapshot(ctx, p.ID, evDate(-1), 42); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	in, err := s.PersonIntimacy(ctx, p.ID)
	if err != nil {
		t.Fatalf("PersonIntimacy: %v", err)
	}
	if in.CurrentScore != 100 || in.Grade != 5 || in.RecentEvents != 2 || in.LastInteraction != evDate(-1) {
		t.Fatalf("亲密度不符: %+v", in)
	}
	if len(in.Trend) != 2 || in.Trend[0].Day != evDate(-1) || in.Trend[0].Score != 42 || in.Trend[1].Day != evDate(0) {
		t.Fatalf("趋势不符: %+v", in.Trend)
	}
	// 今天已有快照 → 不再补点
	if err := s.PersonIntimacySnapshot(ctx, p.ID, evDate(0), 77); err != nil {
		t.Fatalf("snapshot today: %v", err)
	}
	in2, _ := s.PersonIntimacy(ctx, p.ID)
	if len(in2.Trend) != 2 || in2.Trend[1].Score != 77 {
		t.Fatalf("今日快照应覆盖: %+v", in2.Trend)
	}
	// 归档人物 -15：3*20 + 10 - 15 = 55
	q := evMustPerson(t, s, "归档的人")
	if err := s.PersonArchive(ctx, q.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	inQ, err := s.PersonIntimacy(ctx, q.ID)
	if err != nil {
		t.Fatalf("PersonIntimacy(q): %v", err)
	}
	if inQ.CurrentScore != 55 {
		t.Fatalf("归档人物分数 = %d, 期望 55", inQ.CurrentScore)
	}
	// 没有快照也没有事件的人：trend 至少 1 点
	r := evMustPerson(t, s, "空的人")
	inR, _ := s.PersonIntimacy(ctx, r.ID)
	if len(inR.Trend) != 1 || inR.Trend[0].Day != evDate(0) {
		t.Fatalf("空人物 trend 应补今天: %+v", inR.Trend)
	}
	if _, err := s.PersonIntimacy(ctx, "ghost"); err == nil {
		t.Fatalf("未知人应报错")
	}

	// 词云
	for _, c := range []string{"hello world 聊天 聊天 聊天", "a b !!"} {
		mo := &Memo{PersonID: p.ID, Content: c, SaidAt: evLocalStamp(0)}
		if err := s.MemoCreate(ctx, mo); err != nil {
			t.Fatalf("MemoCreate: %v", err)
		}
	}
	wc, err := s.PersonWordCloudText(ctx, p.ID)
	if err != nil || len(wc) == 0 {
		t.Fatalf("词云为空: %v", err)
	}
	if wc[0]["word"] != "聊天" || wc[0]["count"].(int) != 3 {
		t.Fatalf("词云排序不符: %+v", wc)
	}
	if l, err := s.PersonWordCloudText(ctx, "ghost"); err != nil || len(l) != 0 {
		t.Fatalf("无人物词云应空: %v", err)
	}

	// tokenizeSimple 边界（按字节扫描，非 ASCII 视为词内字符）
	toks := tokenizeSimple("a  bc 你好 de")
	want := map[string]bool{"bc": true, "你好": true, "de": true}
	if len(toks) != 3 {
		t.Fatalf("tokenizeSimple = %q", toks)
	}
	for _, tk := range toks {
		if !want[tk] {
			t.Fatalf("意外 token %q", tk)
		}
	}
	if tokenizeSimple("") != nil {
		t.Fatalf("空串应返回 nil")
	}
}

// ===== SQL 错误分支（关闭的 DB） =====

func TestEventStore_ClosedDBErrors(t *testing.T) {
	db := newTestDB(t)
	s := New(db)
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	ctx := context.Background()
	wantErr := func(name string, err error) {
		if err == nil {
			t.Errorf("%s: 期望关闭库报错", name)
		}
	}

	e := &Event{ID: "x", Title: "t", EventDate: "2026-01-01"}
	wantErr("EventCreate", s.EventCreate(ctx, e, nil))
	wantErr("EventUpdate", s.EventUpdate(ctx, e, nil))
	wantErr("EventDelete", s.EventDelete(ctx, "x"))
	_, err := s.EventGet(ctx, "x")
	wantErr("EventGet", err)
	_, err = s.EventList(ctx, "", "q", 10, 0)
	wantErr("EventList", err)
	_, err = s.EventExpenses(ctx, "x")
	wantErr("EventExpenses", err)

	wantErr("MemoCreate", s.MemoCreate(ctx, &Memo{ID: "m", Content: "c", SaidAt: "2026-01-01"}))
	wantErr("MemoUpdate", s.MemoUpdate(ctx, &Memo{ID: "m", Content: "c", SaidAt: "2026-01-01"}))
	wantErr("MemoDelete", s.MemoDelete(ctx, "m"))
	_, err = s.MemoList(ctx, "", false, 10, 0)
	wantErr("MemoList", err)

	wantErr("RelationshipCreate", s.RelationshipCreate(ctx, &Relationship{ID: "r", FromPerson: "a", ToPerson: "b", Type: "t"}))
	wantErr("RelationshipUpdate", s.RelationshipUpdate(ctx, &Relationship{ID: "r", FromPerson: "a", ToPerson: "b", Type: "t"}))
	wantErr("RelationshipDelete", s.RelationshipDelete(ctx, "r"))
	_, err = s.RelationshipList(ctx)
	wantErr("RelationshipList", err)
	_, err = s.RelationshipsOf(ctx, "a")
	wantErr("RelationshipsOf", err)
	_, _, _, err = s.RelationshipGraph(ctx)
	wantErr("RelationshipGraph", err)

	tx := &Transaction{ID: "t1", PersonID: "p", Kind: "loan", Direction: "out", AmountFen: 1, OccurredAt: "2026-01-01"}
	wantErr("TransactionCreate", s.TransactionCreate(ctx, tx))
	wantErr("TransactionUpdate", s.TransactionUpdate(ctx, tx))
	wantErr("TransactionDelete", s.TransactionDelete(ctx, "t1"))
	_, err = s.TransactionGet(ctx, "t1")
	wantErr("TransactionGet", err)
	_, err = s.TransactionList(ctx, "", 10, 0)
	wantErr("TransactionList", err)

	wantErr("RepaymentCreate", s.RepaymentCreate(ctx, &Repayment{ID: "rp", TransactionID: "t1", AmountFen: 1, OccurredAt: "2026-01-01"}))
	wantErr("RepaymentDelete", s.RepaymentDelete(ctx, "rp"))
	_, err = s.RepaymentsOf(ctx, "t1")
	wantErr("RepaymentsOf", err)

	an := &Anniversary{ID: "a1", Title: "t", Date: "2026-01-01"}
	wantErr("AnniversaryCreate", s.AnniversaryCreate(ctx, an))
	wantErr("AnniversaryUpdate", s.AnniversaryUpdate(ctx, an))
	wantErr("AnniversaryDelete", s.AnniversaryDelete(ctx, "a1"))
	_, err = s.AnniversaryList(ctx)
	wantErr("AnniversaryList", err)
	_, err = s.AnniversaryUpcoming(ctx, 7)
	wantErr("AnniversaryUpcoming", err)
	wantErr("AnniversaryDismiss", s.AnniversaryDismiss(ctx, "a1", "2026-01-01:0"))

	rm := &Reminder{ID: "r1", Title: "t", DueAt: "2026-01-01T09:00:00"}
	wantErr("ReminderCreate", s.ReminderCreate(ctx, rm))
	wantErr("ReminderUpdate", s.ReminderUpdate(ctx, rm))
	wantErr("ReminderDelete", s.ReminderDelete(ctx, "r1"))
	wantErr("ReminderDone", s.ReminderDone(ctx, "r1"))
	_, err = s.ReminderList(ctx, "pending", 10, 0)
	wantErr("ReminderList", err)
	_, err = s.ReminderUpcoming(ctx, 7)
	wantErr("ReminderUpcoming", err)

	// Search / DashboardStats 内部吞掉各分支错误，只返回结果
	res, err := s.Search(ctx, "关键词", 3)
	if err != nil || len(res) != 0 {
		t.Errorf("Search(关闭库) 应静默返回空: %v / %+v", err, res)
	}
	stats, err := s.DashboardStats(ctx)
	if err != nil || stats == nil {
		t.Errorf("DashboardStats(关闭库) 应返回 map: %v", err)
	}

	_, err = s.PersonTimeline(ctx, "p")
	wantErr("PersonTimeline", err)
	_, err = s.GlobalTimeline(ctx, 10)
	wantErr("GlobalTimeline", err)
	_, err = s.StatsByMonth(ctx, 6)
	wantErr("StatsByMonth", err)
	_, err = s.GradeDistribution(ctx)
	wantErr("GradeDistribution", err)

	_, _, err = s.PersonDetail(ctx, "p")
	wantErr("PersonDetail", err)
	wantErr("PersonArchive", s.PersonArchive(ctx, "p"))
	wantErr("PersonUnarchive", s.PersonUnarchive(ctx, "p"))
	_, err = s.PersonIntimacy(ctx, "p")
	wantErr("PersonIntimacy", err)
	_, err = s.PersonWordCloudText(ctx, "p")
	wantErr("PersonWordCloudText", err)
	wantErr("PersonIntimacySnapshot", s.PersonIntimacySnapshot(ctx, "p", evDate(0), 1))

	wantErr("AttachmentCreate", s.AttachmentCreate(ctx, &Attachment{ID: "at", EntityType: "event", EntityID: "e", FileName: "f", StoredName: "s", Mime: "m"}))
	_, err = s.AttachmentDelete(ctx, "at")
	wantErr("AttachmentDelete", err)
	_, err = s.AttachmentsOf(ctx, "event", "e")
	wantErr("AttachmentsOf", err)
}
