package store

import (
	"context"
	"database/sql"
	"testing"
)

// 联系节奏（N5）：档位怎么回落默认、到期日按哪个锚点算、勾掉一条待办把谁推后。

func TestContactRhythmDefaultsAndNormalize(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	want := map[int]int{5: 7, 4: 7, 3: 30, 2: 30, 1: 90, 0: 90}
	// 顺序也要固定：前端照这个顺序渲染六行，不再自己排
	order := []int{5, 4, 3, 2, 1, 0}
	check := func(name string, tiers []RhythmTier) {
		t.Helper()
		if len(tiers) != 6 {
			t.Fatalf("%s: 六档要齐，收到 %d 档 %+v", name, len(tiers), tiers)
		}
		for i, g := range order {
			if tiers[i].Grade != g {
				t.Fatalf("%s: 第 %d 行是 %d 级，应为 %d 级", name, i, tiers[i].Grade, g)
			}
		}
		got := map[int]int{}
		for _, tier := range tiers {
			got[tier.Grade] = tier.Days
		}
		for g, d := range want {
			if got[g] != d {
				t.Errorf("%s: %d 级 = %d 天，应为 %d 天", name, g, got[g], d)
			}
		}
	}

	// 没配过：全是默认
	loaded, err := s.ContactRhythm(ctx)
	if err != nil {
		t.Fatalf("ContactRhythm: %v", err)
	}
	check("默认", loaded)

	// 只改一级，其余照旧
	if err := s.ContactRhythmSet(ctx, []RhythmTier{{Grade: 5, Days: 14}}); err != nil {
		t.Fatalf("ContactRhythmSet: %v", err)
	}
	loaded, err = s.ContactRhythm(ctx)
	if err != nil {
		t.Fatalf("ContactRhythm: %v", err)
	}
	want[5] = 14
	check("单改 ♥×5", loaded)
	want[5] = 7

	// 越界钳住、不存在的等级丢掉、0 天当没填
	if err := s.ContactRhythmSet(ctx, []RhythmTier{
		{Grade: 3, Days: MaxRhythmDays + 100},
		{Grade: 9, Days: 5},
		{Grade: 1, Days: 0},
	}); err != nil {
		t.Fatalf("ContactRhythmSet: %v", err)
	}
	loaded, err = s.ContactRhythm(ctx)
	if err != nil {
		t.Fatalf("ContactRhythm: %v", err)
	}
	if loaded[2].Days != MaxRhythmDays {
		t.Errorf("♥×3 = %d 天，应钳到 %d", loaded[2].Days, MaxRhythmDays)
	}
	for _, tier := range loaded {
		if tier.Grade == 9 {
			t.Fatalf("9 级不该存在：%+v", loaded)
		}
		if tier.Grade == 1 && tier.Days != 90 {
			t.Errorf("♥×1 填 0 天应回落 90，收到 %d", tier.Days)
		}
	}

	// 恢复默认
	if err := s.ContactRhythmReset(ctx); err != nil {
		t.Fatalf("ContactRhythmReset: %v", err)
	}
	loaded, err = s.ContactRhythm(ctx)
	if err != nil {
		t.Fatalf("ContactRhythm: %v", err)
	}
	check("恢复默认", loaded)

	// settings 里躺着一坨读不懂的 JSON：引擎不能整个哑掉
	if err := s.SettingSet(ctx, rhythmKey, "{这不是JSON"); err != nil {
		t.Fatalf("SettingSet: %v", err)
	}
	loaded, err = s.ContactRhythm(ctx)
	if err != nil {
		t.Fatalf("ContactRhythm: %v", err)
	}
	check("坏 JSON 回落默认", loaded)
}

func TestDriftListAnchorAndOrdering(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	add := func(name string, grade int, archived bool) *Person {
		t.Helper()
		p := &Person{Name: name, Grade: grade, Archived: archived}
		if err := s.PersonCreate(ctx, p); err != nil {
			t.Fatalf("PersonCreate(%s): %v", name, err)
		}
		return p
	}
	closely := add("密友", 5, false) // 7 天一联系
	fresh := add("刚建档", 0, false)  // 今天建档，90 天
	soonish := add("浅交", 3, false) // 30 天一联系，10 天前聊过
	ghost := add("档案袋里", 5, true)  // 归档了，不该再催

	tx := &Transaction{PersonID: closely.ID, Kind: "gift", Direction: "out", AmountFen: 100, Title: "随礼", OccurredAt: DaysAgoLocal(20)}
	if err := s.TransactionCreate(ctx, tx); err != nil {
		t.Fatalf("TransactionCreate: %v", err)
	}
	memo := &Memo{PersonID: soonish.ID, Content: "约了钓鱼", SaidAt: DaysAgoLocal(10), Status: "open"}
	if err := s.MemoCreate(ctx, memo); err != nil {
		t.Fatalf("MemoCreate: %v", err)
	}

	list, err := s.DriftList(ctx, DriftFilter{})
	if err != nil {
		t.Fatalf("DriftList: %v", err)
	}
	byID := map[string]*DriftPerson{}
	for _, d := range list {
		byID[d.PersonID] = d
	}
	if len(list) != 3 {
		t.Fatalf("名单 %d 人（归档的不该出现）：%+v", len(list), list)
	}
	if _, ok := byID[ghost.ID]; ok {
		t.Error("归档的人进了渐远名单")
	}

	d := byID[closely.ID]
	if d == nil {
		t.Fatalf("密友不在名单：%+v", list)
	}
	if !d.Overdue || d.OverdueDays != 13 {
		t.Errorf("密友应逾期 13 天（20 天前联系，节奏 7 天），收到 overdue=%v days=%d", d.Overdue, d.OverdueDays)
	}
	if d.Anchor != DaysAgoLocal(20) || d.AnchorFrom != "contact" {
		t.Errorf("密友锚点 = %s(%s)，应为最近一次往来", d.Anchor, d.AnchorFrom)
	}
	if d.Days != 7 || d.DaysSince != 20 {
		t.Errorf("密友 节奏=%d 距锚点=%d 天", d.Days, d.DaysSince)
	}
	if d.DueAt != DaysAgoLocal(13) {
		t.Errorf("密友 到期日 = %s，应为 %s", d.DueAt, DaysAgoLocal(13))
	}

	n := byID[fresh.ID]
	if n == nil {
		t.Fatalf("刚建档的人不在名单：%+v", list)
	}
	if n.Overdue {
		t.Errorf("刚建档的人第一天就逾期了：%+v", n)
	}
	if n.AnchorFrom != "created" || n.Anchor != TodayLocal() {
		t.Errorf("刚建档 锚点 = %s(%s)，应退回建档日", n.Anchor, n.AnchorFrom)
	}
	if n.LastContact != "" {
		t.Errorf("从没记过往来，last_contact 应为空，收到 %q", n.LastContact)
	}

	q := byID[soonish.ID]
	if q == nil {
		t.Fatalf("浅交的人不在名单：%+v", list)
	}
	if q.Overdue {
		t.Errorf("10 天前聊过、30 天一联系，不该逾期：%+v", q)
	}
	if q.AnchorFrom != "contact" || q.Anchor != DaysAgoLocal(10) {
		t.Errorf("浅交 锚点 = %s(%s)，应取对话日期", q.Anchor, q.AnchorFrom)
	}

	// 逾期的排最前
	if list[0].PersonID != closely.ID {
		t.Errorf("首位应是逾期最久的密友，收到 %s", list[0].Name)
	}

	only, err := s.DriftList(ctx, DriftFilter{OnlyOverdue: true})
	if err != nil {
		t.Fatalf("DriftList(only): %v", err)
	}
	if len(only) != 1 || only[0].PersonID != closely.ID {
		t.Fatalf("只筛逾期：%+v", only)
	}

	paged, err := s.DriftList(ctx, DriftFilter{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("DriftList(分页): %v", err)
	}
	if len(paged) != 1 || paged[0].PersonID == closely.ID {
		t.Fatalf("第 2 页第 1 条 = %+v", paged)
	}
	empty, err := s.DriftList(ctx, DriftFilter{Limit: 5, Offset: 99})
	if err != nil {
		t.Fatalf("DriftList(越界): %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("offset 越界应回空，收到 %+v", empty)
	}
}

func TestContactCheckinMovesAnchor(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := &Person{Name: "该联系的人", Grade: 5}
	if err := s.PersonCreate(ctx, p); err != nil {
		t.Fatalf("PersonCreate: %v", err)
	}
	tx := &Transaction{PersonID: p.ID, Kind: "gift", Direction: "out", AmountFen: 100, Title: "随礼", OccurredAt: DaysAgoLocal(30)}
	if err := s.TransactionCreate(ctx, tx); err != nil {
		t.Fatalf("TransactionCreate: %v", err)
	}

	upcoming, err := s.ContactUpcoming(ctx)
	if err != nil {
		t.Fatalf("ContactUpcoming: %v", err)
	}
	if len(upcoming) != 1 {
		t.Fatalf("逾期的人应派生 1 条待办，收到 %+v", upcoming)
	}
	r := upcoming[0]
	if r.ID != "contact:"+p.ID || r.RefType != "contact" || r.PersonID != p.ID {
		t.Errorf("派生待办 = %+v", r)
	}
	if r.Title != "该联系 该联系的人 了" {
		t.Errorf("标题 = %q", r.Title)
	}

	// 勾掉它：记一次打卡，锚点前移，待办消失
	if err := s.ContactCheckin(ctx, p.ID); err != nil {
		t.Fatalf("ContactCheckin: %v", err)
	}
	upcoming, err = s.ContactUpcoming(ctx)
	if err != nil {
		t.Fatalf("ContactUpcoming: %v", err)
	}
	if len(upcoming) != 0 {
		t.Errorf("打卡后仍被催：%+v", upcoming)
	}
	list, err := s.DriftList(ctx, DriftFilter{})
	if err != nil {
		t.Fatalf("DriftList: %v", err)
	}
	if len(list) != 1 || list[0].Anchor != TodayLocal() || list[0].AnchorFrom != "contact" {
		t.Fatalf("打卡没成为新的接触锚点：%+v", list)
	}
	if list[0].DueAt != DaysAgoLocal(-7) {
		t.Errorf("下次到期 = %s，应为今天起第 7 天", list[0].DueAt)
	}

	// 同一天连点两次只算一次：不然点两下就把节奏挪成两轮
	for i := 0; i < 2; i++ {
		if err := s.ContactCheckin(ctx, p.ID); err != nil {
			t.Fatalf("ContactCheckin(%d): %v", i, err)
		}
	}
	var count int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM contact_checkins WHERE person_id=?", p.ID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("同一天的打卡写了 %d 行，应幂等成 1 行", count)
	}

	// 不存在的人、以及进了回收站的人：都是 404，不留孤儿打卡
	if err := s.ContactCheckin(ctx, "没有这个人"); err != sql.ErrNoRows {
		t.Errorf("陌生人打卡 = %v，应为 ErrNoRows", err)
	}
	if err := s.PersonDelete(ctx, p.ID); err != nil {
		t.Fatalf("PersonDelete: %v", err)
	}
	if err := s.ContactCheckin(ctx, p.ID); err != sql.ErrNoRows {
		t.Errorf("已删除的人打卡 = %v，应为 ErrNoRows", err)
	}
	upcoming, err = s.ContactUpcoming(ctx)
	if err != nil {
		t.Fatalf("ContactUpcoming: %v", err)
	}
	if len(upcoming) != 0 {
		t.Errorf("回收站里的人还在催：%+v", upcoming)
	}
}

// 脏 grade 不能把人变成「一建档就逾期」。
func TestDriftListWithDirtyGrade(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := &Person{Name: "乱_grade", Grade: 42}
	if err := s.PersonCreate(ctx, p); err != nil {
		t.Fatalf("PersonCreate: %v", err)
	}
	list, err := s.DriftList(ctx, DriftFilter{})
	if err != nil {
		t.Fatalf("DriftList: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("名单 = %+v", list)
	}
	if list[0].Days != 90 || list[0].Overdue {
		t.Errorf("42 级应退回未分级的 90 天，收到 节奏=%d 逾期=%v", list[0].Days, list[0].Overdue)
	}
}
