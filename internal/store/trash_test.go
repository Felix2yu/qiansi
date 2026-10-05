package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// ===== 回收站 =====
//
// 删除只置 deleted_at，读路径由迁移 013 的 live_* 视图裁决「看不看得见」。
// 这几条测试盯的就是两件事：牵连口径（一个人进回收站，他名下的记录当下都不存在）
// 与可逆性（恢复只清标记，一条都不必重建）。

type trashSeed struct {
	person      *Person
	event       *Event
	memo        *Memo
	tx          *Transaction
	anniversary *Anniversary
	reminder    *Reminder
}

// trashMustSeed 造一个人，六类记录里除字典外各挂一条。
func trashMustSeed(t *testing.T, s *Store, ctx context.Context, name string) *trashSeed {
	t.Helper()
	p := pMustCreate(t, s, &Person{Name: name, Grade: 1})
	seed := &trashSeed{person: p}

	ev := &Event{Title: "喝茶", EventDate: "2026-05-01"}
	if err := s.EventCreate(ctx, ev, []string{p.ID}, nil); err != nil {
		t.Fatalf("EventCreate: %v", err)
	}
	seed.event = ev
	seed.memo = &Memo{PersonID: p.ID, Speaker: "me", Content: "说好四月底还相机", SaidAt: "2026-05-02T10:00:00"}
	if err := s.MemoCreate(ctx, seed.memo); err != nil {
		t.Fatalf("MemoCreate: %v", err)
	}
	seed.tx = &Transaction{PersonID: p.ID, Kind: "loan", Direction: "out", AmountFen: 8000, Title: "垫了打车费", OccurredAt: "2026-05-03"}
	if err := s.TransactionCreate(ctx, seed.tx); err != nil {
		t.Fatalf("TransactionCreate: %v", err)
	}
	seed.anniversary = &Anniversary{PersonID: p.ID, Title: "认识的日子", Date: "2020-04-18"}
	if err := s.AnniversaryCreate(ctx, seed.anniversary); err != nil {
		t.Fatalf("AnniversaryCreate: %v", err)
	}
	seed.reminder = &Reminder{PersonID: p.ID, Title: "问他相机修好了没", DueAt: "2026-06-01"}
	if err := s.ReminderCreate(ctx, seed.reminder); err != nil {
		t.Fatalf("ReminderCreate: %v", err)
	}
	return seed
}

// trashVisibleCount 六类记录在各自列表接口里看得见的条数。
func trashVisibleCount(t *testing.T, s *Store, ctx context.Context, seed *trashSeed) int {
	t.Helper()
	people, err := s.PersonList(ctx, "", 0, 0, false, 0, 0, 0)
	if err != nil {
		t.Fatalf("PersonList: %v", err)
	}
	events, err := s.EventList(ctx, "", "", 0, 0)
	if err != nil {
		t.Fatalf("EventList: %v", err)
	}
	memos, err := s.MemoList(ctx, "", false, 0, 0)
	if err != nil {
		t.Fatalf("MemoList: %v", err)
	}
	txs, err := s.TransactionList(ctx, "", 0, 0)
	if err != nil {
		t.Fatalf("TransactionList: %v", err)
	}
	anns, err := s.AnniversaryList(ctx)
	if err != nil {
		t.Fatalf("AnniversaryList: %v", err)
	}
	reminders, err := s.ReminderList(ctx, "", 0, 0)
	if err != nil {
		t.Fatalf("ReminderList: %v", err)
	}
	return len(people) + len(events) + len(memos) + len(txs) + len(anns) + len(reminders)
}

func trashFind(t *testing.T, s *Store, ctx context.Context, kind, id string) *TrashItem {
	t.Helper()
	list, err := s.TrashList(ctx)
	if err != nil {
		t.Fatalf("TrashList: %v", err)
	}
	for _, it := range list {
		if it.Type == kind && it.ID == id {
			return it
		}
	}
	return nil
}

// 联系人进回收站＝他和他名下的记录当下都不存在；恢复之后原样回来。
func TestTrashPersonHidesCollateralAndRestoreBringsBack(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	seed := trashMustSeed(t, s, ctx, "老王")

	if n := trashVisibleCount(t, s, ctx, seed); n != 6 {
		t.Fatalf("删除前应有 6 条可见记录，得到 %d", n)
	}
	if err := s.PersonDelete(ctx, seed.person.ID); err != nil {
		t.Fatalf("PersonDelete: %v", err)
	}
	if n := trashVisibleCount(t, s, ctx, seed); n != 0 {
		t.Fatalf("联系人进回收站后可见记录 = %d，牵连记录应一起藏起来", n)
	}
	// 全局搜索与图谱也不能再把他递出来
	hits, err := s.Search(ctx, "老王", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("回收站里的人仍可被搜索到: %+v", hits)
	}
	graph, err := s.RelationshipGraph(ctx)
	if err != nil {
		t.Fatalf("RelationshipGraph: %v", err)
	}
	if len(graph.People) != 0 || graph.TotalPeople != 0 {
		t.Fatalf("图谱里仍有回收站中的人: %d 人（total %d）", len(graph.People), graph.TotalPeople)
	}
	// 行都还在表里，只是带着标记
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM memos WHERE person_id=?", seed.person.ID); n != 1 {
		t.Fatalf("对话行应留在表里等着恢复，得到 %d", n)
	}
	it := trashFind(t, s, ctx, "person", seed.person.ID)
	if it == nil {
		t.Fatal("回收站里没有这条联系人")
	}
	if it.Title != "老王" || it.Hidden != 5 {
		t.Fatalf("回收站条目不符: %+v（want 恢复后带回 5 条）", it)
	}

	if err := s.TrashRestore(ctx, "person", seed.person.ID); err != nil {
		t.Fatalf("TrashRestore: %v", err)
	}
	if n := trashVisibleCount(t, s, ctx, seed); n != 6 {
		t.Fatalf("恢复后可见记录 = %d，期望 6（一条都不必重建）", n)
	}
	if trashFind(t, s, ctx, "person", seed.person.ID) != nil {
		t.Fatal("恢复后仍在回收站里")
	}
	// 恢复一条已经活着的记录 → ErrNoRows（API 据此回 404）
	if err := s.TrashRestore(ctx, "person", seed.person.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("TrashRestore(活着的人) 应 ErrNoRows，got %v", err)
	}
}

// 六类记录各认自己的 deleted_at：删掉一条对话不该牵连别人的记录消失。
func TestTrashOwnFlagHidesOnlyThatRecord(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	seed := trashMustSeed(t, s, ctx, "小陈")

	if err := s.MemoDelete(ctx, seed.memo.ID); err != nil {
		t.Fatalf("MemoDelete: %v", err)
	}
	if n := trashVisibleCount(t, s, ctx, seed); n != 5 {
		t.Fatalf("删掉一条对话后可见记录 = %d，期望只有它消失", n)
	}
	if err := s.TrashRestore(ctx, "memo", seed.memo.ID); err != nil {
		t.Fatalf("恢复对话: %v", err)
	}
	if n := trashVisibleCount(t, s, ctx, seed); n != 6 {
		t.Fatalf("恢复后可见记录 = %d，期望 6", n)
	}
}

// 归属人还在回收站时，单独恢复一条记录仍然看不见——标记清了，视图还没放行。
func TestTrashRestoreWhileOwnerStillTrashed(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	seed := trashMustSeed(t, s, ctx, "阿康")

	if err := s.PersonDelete(ctx, seed.person.ID); err != nil {
		t.Fatalf("PersonDelete: %v", err)
	}
	if err := s.MemoDelete(ctx, seed.memo.ID); err != nil {
		t.Fatalf("MemoDelete: %v", err)
	}
	// 对话自己带着标记时不算在「恢复联系人能带回几条」里
	it := trashFind(t, s, ctx, "person", seed.person.ID)
	if it == nil || it.Hidden != 4 {
		t.Fatalf("回收站条目不符: %+v（want 带回 4 条）", it)
	}
	if err := s.TrashRestore(ctx, "memo", seed.memo.ID); err != nil {
		t.Fatalf("TrashRestore(memo): %v", err)
	}
	if n := trashVisibleCount(t, s, ctx, seed); n != 0 {
		t.Fatalf("归属人还在回收站时就该全藏起来，可见 %d", n)
	}
	it = trashFind(t, s, ctx, "person", seed.person.ID)
	if it == nil {
		t.Fatal("联系人应仍在回收站里")
	}
	// 标记清掉的对话回到「随联系人一起藏着」那一档，带回条数变成 5
	if it.Hidden != 5 || it.PersonTrashed {
		t.Fatalf("回收站条目不符: %+v", it)
	}
	if err := s.TrashRestore(ctx, "person", seed.person.ID); err != nil {
		t.Fatalf("TrashRestore(person): %v", err)
	}
	if n := trashVisibleCount(t, s, ctx, seed); n != 6 {
		t.Fatalf("两个人都恢复后可见记录 = %d，期望 6", n)
	}
}

// 彻底删除才落刀：行消失、外键级联收尾；活着的记录这个接口碰不到。
func TestTrashPurge(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	seed := trashMustSeed(t, s, ctx, "老郑")

	// 回收站只认回收站里的行：还在使用的人删不掉，也不会被摘走关联记录
	if err := s.TrashPurge(ctx, "person", seed.person.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("TrashPurge(活着的人) 应 ErrNoRows，got %v", err)
	}
	if n := trashVisibleCount(t, s, ctx, seed); n != 6 {
		t.Fatalf("误调用后记录应毫发无损，可见 %d", n)
	}
	// 不认识的类型（关系边、还款这些没进回收站的）一律拒绝
	if err := s.TrashPurge(ctx, "relationship", "whatever"); !errors.Is(err, ErrUnknownTrashKind) {
		t.Fatalf("TrashPurge(未知 kind) 应 ErrUnknownTrashKind，got %v", err)
	}
	if err := s.TrashRestore(ctx, "repayment", "whatever"); !errors.Is(err, ErrUnknownTrashKind) {
		t.Fatalf("TrashRestore(未知 kind) 应 ErrUnknownTrashKind，got %v", err)
	}

	if err := s.PersonDelete(ctx, seed.person.ID); err != nil {
		t.Fatalf("PersonDelete: %v", err)
	}
	if err := s.TrashPurge(ctx, "person", seed.person.ID); err != nil {
		t.Fatalf("TrashPurge: %v", err)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM people WHERE id=?", seed.person.ID); n != 0 {
		t.Fatalf("彻底删除后 people 表仍有 %d 行", n)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM anniversaries WHERE person_id=?", seed.person.ID); n != 0 {
		t.Fatalf("纪念日应随级联消失，剩余 %d", n)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM memos"); n != 1 {
		t.Fatalf("对话应留着（只是没了归属），得到 %d", n)
	}
	if trashFind(t, s, ctx, "person", seed.person.ID) != nil {
		t.Fatal("彻底删除后回收站里还有这条联系人")
	}
}

// 一场往来里只要有一位参与人被回收，这一场整场藏起来；
// 从回收站彻底删掉时才解绑挂账、删掉参与人行。
func TestTrashEventWithParticipants(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	a := evMustPerson(t, s, "甲")
	b := evMustPerson(t, s, "乙")
	ev := &Event{Title: "火锅", EventDate: "2026-05-05"}
	if err := s.EventCreate(ctx, ev, []string{a.ID, b.ID}, nil); err != nil {
		t.Fatalf("EventCreate: %v", err)
	}
	tx := &Transaction{PersonID: a.ID, Kind: "expense", Direction: "out", AmountFen: 3000, OccurredAt: "2026-05-05", EventID: ev.ID}
	if err := s.TransactionCreate(ctx, tx); err != nil {
		t.Fatalf("TransactionCreate: %v", err)
	}

	if err := s.PersonDelete(ctx, b.ID); err != nil {
		t.Fatalf("PersonDelete: %v", err)
	}
	if _, err := s.EventGet(ctx, ev.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("参与人进回收站后这场往来应整场藏起来，got %v", err)
	}
	if err := s.PersonPurge(ctx, b.ID); err != nil {
		t.Fatalf("PersonPurge: %v", err)
	}
	if _, err := s.EventGet(ctx, ev.ID); err != nil {
		t.Fatalf("参与人被彻底删除后这场往来应回来: %v", err)
	}
	if trashFind(t, s, ctx, "event", ev.ID) != nil {
		t.Fatal("只是被牵连藏起来，不该自己进回收站")
	}

	if err := s.EventDelete(ctx, ev.ID); err != nil {
		t.Fatalf("EventDelete: %v", err)
	}
	if got, err := s.TransactionGet(ctx, tx.ID); err != nil || got.EventID != ev.ID {
		t.Fatalf("往来进回收站时挂账应保留: %q (%v)", got.EventID, err)
	}
	if err := s.TrashPurge(ctx, "event", ev.ID); err != nil {
		t.Fatalf("TrashPurge(event): %v", err)
	}
	if got, err := s.TransactionGet(ctx, tx.ID); err != nil || got.EventID != "" {
		t.Fatalf("彻底删除往来后应解绑: %q (%v)", got.EventID, err)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM event_participants WHERE event_id=?", ev.ID); n != 0 {
		t.Fatalf("参与人行应随往来删除，剩余 %d", n)
	}
}

// 账目进回收站时，事件不再认领这笔礼金但也不毁掉它：恢复后指针照旧接得上。
func TestTrashGiftKeepsPointerUntilPurge(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	host := evMustPerson(t, s, "小季")
	e := &Event{Title: "乔迁", EventDate: "2026-05-06",
		GiftAmountFen: 6600, GiftDirection: "out", GiftPersonID: host.ID}
	egCreate(t, s, e, []string{host.ID})
	giftID := e.GiftTransactionID

	if err := s.TransactionDelete(ctx, giftID); err != nil {
		t.Fatalf("TransactionDelete: %v", err)
	}
	got, err := s.EventGet(ctx, e.ID)
	if err != nil {
		t.Fatalf("EventGet: %v", err)
	}
	if got.GiftTransactionID != "" || got.GiftAmountFen != 0 {
		t.Fatalf("回收站里的账目不该被事件认领: %+v", got)
	}
	// 但指针在表里原样留着：表单读到的礼金是 0，按 0 再保存一次也不能把它写成 NULL
	e.GiftAmountFen = 0
	e.GiftTransactionID = ""
	if err := s.EventUpdate(ctx, e, []string{host.ID}, nil); err != nil {
		t.Fatalf("EventUpdate: %v", err)
	}
	if pRawScanStr(t, s, "SELECT COALESCE(gift_transaction_id,'') FROM events WHERE id=?", e.ID) != giftID {
		t.Fatal("保存事件把回收站里的礼金账指针清掉了")
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM transactions WHERE id=?", giftID); n != 1 {
		t.Fatalf("回收站里的礼金账被删了，剩余 %d", n)
	}
	if err := s.TrashRestore(ctx, "transaction", giftID); err != nil {
		t.Fatalf("TrashRestore: %v", err)
	}
	got, err = s.EventGet(ctx, e.ID)
	if err != nil {
		t.Fatalf("EventGet(恢复后): %v", err)
	}
	if got.GiftTransactionID != giftID || got.GiftAmountFen != 6600 {
		t.Fatalf("账目恢复后事件应重新认领它: %+v", got)
	}
}

// 清空回收站：一次点掉全部，各类计数如实上报，牵连的记录一并消失。
func TestTrashEmpty(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	seed := trashMustSeed(t, s, ctx, "老贺")
	loose := trashMustSeed(t, s, ctx, "小韩")
	// 另外造一场没牵连的往来，只删它自己
	orphan := &Event{Title: "看展", EventDate: "2026-05-07"}
	if err := s.EventCreate(ctx, orphan, nil, nil); err != nil {
		t.Fatalf("EventCreate(orphan): %v", err)
	}

	if err := s.PersonDelete(ctx, seed.person.ID); err != nil {
		t.Fatalf("PersonDelete: %v", err)
	}
	if err := s.MemoDelete(ctx, loose.memo.ID); err != nil {
		t.Fatalf("MemoDelete: %v", err)
	}
	if err := s.EventDelete(ctx, orphan.ID); err != nil {
		t.Fatalf("EventDelete: %v", err)
	}
	list, err := s.TrashList(ctx)
	if err != nil {
		t.Fatalf("TrashList: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("回收站应有 3 行，得到 %d", len(list))
	}

	res, err := s.TrashEmpty(ctx)
	if err != nil {
		t.Fatalf("TrashEmpty: %v", err)
	}
	if res.Purged["person"] != 1 || res.Purged["memo"] != 1 || res.Purged["event"] != 1 {
		t.Fatalf("计数不符: %+v", res.Purged)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM people"); n != 1 {
		t.Fatalf("清空后应只剩没进回收站的那位，people 表剩 %d 行", n)
	}
	// 账目与纪念日随联系人级联消失（这直以来都是「彻底删人」的代价）
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM transactions WHERE person_id=?", seed.person.ID); n != 0 {
		t.Fatalf("被牵连的账目应随联系人级联清掉，剩余 %d", n)
	}
	// 对话与待办由外键置空归属后留着：那是用户自己写的记录
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM memos WHERE person_id IS NULL"); n != 1 {
		t.Fatalf("老贺名下的对话应改为无归属留着，得到 %d", n)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM reminders WHERE person_id IS NULL"); n != 1 {
		t.Fatalf("老贺名下的待办应改为无归属留着，得到 %d", n)
	}
	// 他参与的那场往来回到可见状态（参与人行被级联删掉，事件本身没进回收站）
	if _, err := s.EventGet(ctx, seed.event.ID); err != nil {
		t.Fatalf("牵连藏起来的往来应随人消失而回来: %v", err)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM events WHERE deleted_at IS NOT NULL"); n != 0 {
		t.Fatalf("回收站没清空，仍有 %d 场往来", n)
	}
}

// 引荐人进回收站时，认识路径读到断点为止；恢复之后链路自己接上。
func TestTrashIntroPathBreaksAndReconnects(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	self := evMustPerson(t, s, "我自己")
	mid := evMustPerson(t, s, "中间人")
	tail := evMustPerson(t, s, "新认识的人")
	pRaw(t, s, "UPDATE people SET introduced_by_person_id=? WHERE id=?", self.ID, mid.ID)
	pRaw(t, s, "UPDATE people SET introduced_by_person_id=? WHERE id=?", mid.ID, tail.ID)

	path, err := s.IntroPath(ctx, self.ID, tail.ID)
	if err != nil {
		t.Fatalf("IntroPath: %v", err)
	}
	if !path.ReachesSelf || path.Broken || len(path.Chain) != 3 {
		t.Fatalf("本该从自己走到这个人: %+v", path.Chain)
	}

	if err := s.PersonDelete(ctx, mid.ID); err != nil {
		t.Fatalf("PersonDelete: %v", err)
	}
	path, err = s.IntroPath(ctx, self.ID, tail.ID)
	if err != nil {
		t.Fatalf("IntroPath(断链): %v", err)
	}
	if path.ReachesSelf || !path.Broken || len(path.Chain) != 1 {
		t.Fatalf("引荐人进回收站后应只走到断点: %+v broken=%v", path.Chain, path.Broken)
	}
	if err := s.TrashRestore(ctx, "person", mid.ID); err != nil {
		t.Fatalf("TrashRestore: %v", err)
	}
	path, err = s.IntroPath(ctx, self.ID, tail.ID)
	if err != nil {
		t.Fatalf("IntroPath(恢复后): %v", err)
	}
	if !path.ReachesSelf || path.Broken || len(path.Chain) != 3 {
		t.Fatalf("恢复引荐人后链路应自己接上: %+v", path.Chain)
	}
}
