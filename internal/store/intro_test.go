package store

import (
	"context"
	"testing"
)

// ===== 认识路径按需单接口（O1）=====

func ipMustRel(t *testing.T, s *Store, from, to, typ string) {
	t.Helper()
	if err := s.RelationshipCreate(context.Background(), &Relationship{FromPerson: from, ToPerson: to, Type: typ}); err != nil {
		t.Fatalf("RelationshipCreate(%s→%s %s): %v", from, to, typ, err)
	}
}

func ipPath(t *testing.T, s *Store, self, target string) *IntroPath {
	t.Helper()
	p, err := s.IntroPath(context.Background(), self, target)
	if err != nil {
		t.Fatalf("IntroPath: %v", err)
	}
	return p
}

func ipNames(p *IntroPath) []string {
	names := []string{}
	for _, h := range p.Chain {
		names = append(names, h.Name)
	}
	return names
}

func personNames(ps []*Person) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

func evTitles(es []*Event) []string {
	out := []string{}
	for _, e := range es {
		out = append(out, e.Title)
	}
	return out
}

func TestIntroPath_ChainEdgesAndDirectTypes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	me := evMustPerson(t, s, "我")
	jia := &Person{Name: "同事甲", Grade: 3, IntroducedByPersonID: me.ID}
	if err := s.PersonCreate(ctx, jia); err != nil {
		t.Fatalf("PersonCreate 甲: %v", err)
	}
	yi := &Person{Name: "客户乙", Grade: 3, IntroducedByPersonID: jia.ID}
	if err := s.PersonCreate(ctx, yi); err != nil {
		t.Fatalf("PersonCreate 乙: %v", err)
	}
	bing := &Person{Name: "朋友丙", Grade: 3, IntroducedByPersonID: yi.ID}
	if err := s.PersonCreate(ctx, bing); err != nil {
		t.Fatalf("PersonCreate 丙: %v", err)
	}

	// 有向边故意反向存，验证配对时不看方向
	ipMustRel(t, s, jia.ID, me.ID, "同事")
	ipMustRel(t, s, jia.ID, yi.ID, "大学同学")
	ipMustRel(t, s, yi.ID, bing.ID, "老东家")
	// 经人认识后又自己熟起来的，走 direct_types 而不是链上的边
	ipMustRel(t, s, bing.ID, me.ID, "闺蜜")
	// 链上不该出现的噪音：甲↔丙 之间的关系与这条路径无关
	ipMustRel(t, s, jia.ID, bing.ID, "牌友")

	p := ipPath(t, s, me.ID, bing.ID)
	if !p.ReachesSelf || p.Broken || p.Cyclic {
		t.Fatalf("该走到本人且不断不环，得到 %+v", p)
	}
	if got := ipNames(p); len(got) != 4 || got[0] != "我" || got[3] != "朋友丙" {
		t.Fatalf("链应从本人排到此人，得到 %v", got)
	}
	if len(p.Chain[0].EdgeTypes) != 0 {
		t.Fatalf("首跳没有来路，edge_types 该为空，得到 %v", p.Chain[0].EdgeTypes)
	}
	if got := p.Chain[1].EdgeTypes; len(got) != 1 || got[0] != "同事" {
		t.Fatalf("我→甲 的边 = %v, want [同事]", got)
	}
	if got := p.Chain[2].EdgeTypes; len(got) != 1 || got[0] != "大学同学" {
		t.Fatalf("甲→乙 的边 = %v, want [大学同学]", got)
	}
	if got := p.Chain[3].EdgeTypes; len(got) != 1 || got[0] != "老东家" {
		t.Fatalf("乙→丙 的边 = %v, want [老东家]", got)
	}
	if got := p.DirectTypes; len(got) != 1 || got[0] != "闺蜜" {
		t.Fatalf("直达关系 = %v, want [闺蜜]", got)
	}
}

func TestIntroPath_MultiTypeEdge(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	me := evMustPerson(t, s, "我")
	a := &Person{Name: "甲", Grade: 3, IntroducedByPersonID: me.ID}
	if err := s.PersonCreate(ctx, a); err != nil {
		t.Fatalf("PersonCreate: %v", err)
	}
	ipMustRel(t, s, me.ID, a.ID, "同事")
	ipMustRel(t, s, me.ID, a.ID, "牌友")

	p := ipPath(t, s, me.ID, a.ID)
	if got := p.Chain[1].EdgeTypes; len(got) != 2 {
		t.Fatalf("同一对人可以并存多种关系，得到 %v", got)
	}
}

func TestIntroPath_BrokenChain(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	me := evMustPerson(t, s, "我")
	ghost := evMustPerson(t, s, "已删的人")
	victim := &Person{Name: "小李", Grade: 3, IntroducedByPersonID: ghost.ID}
	if err := s.PersonCreate(ctx, victim); err != nil {
		t.Fatalf("PersonCreate: %v", err)
	}
	// 外键会拦住这类写入，这里绕过去模拟历史脏数据
	pRaw(t, s, "PRAGMA foreign_keys=OFF")
	pRaw(t, s, "DELETE FROM people WHERE id=?", ghost.ID)
	pRaw(t, s, "PRAGMA foreign_keys=ON")

	p := ipPath(t, s, me.ID, victim.ID)
	if p.ReachesSelf {
		t.Fatal("断链不该报告走到本人")
	}
	if !p.Broken {
		t.Fatalf("引荐人缺失应标记 broken，得到 %+v", p)
	}
	if got := ipNames(p); len(got) != 1 || got[0] != "小李" {
		t.Fatalf("断链只走到断点为止，得到 %v", got)
	}
}

func TestIntroPath_UnrelatedChain(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	me := evMustPerson(t, s, "我")
	a := evMustPerson(t, s, "甲")
	b := &Person{Name: "乙", Grade: 3, IntroducedByPersonID: a.ID}
	if err := s.PersonCreate(ctx, b); err != nil {
		t.Fatalf("PersonCreate: %v", err)
	}

	// 甲自己与我无关（没有引荐人），链走到头就停，不算断链
	p := ipPath(t, s, me.ID, b.ID)
	if p.ReachesSelf || p.Broken || p.Cyclic {
		t.Fatalf("与我无关的链应 reaches=false、broken=false，得到 %+v", p)
	}
	if got := ipNames(p); len(got) != 2 || got[0] != "甲" || got[1] != "乙" {
		t.Fatalf("链应停在能追到的最远处，得到 %v", got)
	}
}

func TestIntroPath_CycleStops(t *testing.T) {
	s := newTestStore(t)
	me := evMustPerson(t, s, "我")
	a := evMustPerson(t, s, "甲")
	b := evMustPerson(t, s, "乙")
	// 写路径会拦环，这里绕过去模拟历史脏数据
	pRaw(t, s, "UPDATE people SET introduced_by_person_id=? WHERE id=?", b.ID, a.ID)
	pRaw(t, s, "UPDATE people SET introduced_by_person_id=? WHERE id=?", a.ID, b.ID)

	p := ipPath(t, s, me.ID, a.ID)
	if !p.Cyclic {
		t.Fatalf("环应被识别出来，得到 %+v", p)
	}
	if p.ReachesSelf {
		t.Fatal("环里没有本人，不该报告走到了")
	}
	if len(p.Chain) != 2 {
		t.Fatalf("环上只该留第一次经过的两跳，得到 %v", ipNames(p))
	}
}

func TestIntroPath_SelfHasNoChain(t *testing.T) {
	s := newTestStore(t)
	me := evMustPerson(t, s, "我")

	p := ipPath(t, s, me.ID, me.ID)
	if !p.ReachesSelf {
		t.Fatalf("查自己应立刻命中，得到 %+v", p)
	}
	if len(p.Chain) != 1 || p.Chain[0].ID != me.ID {
		t.Fatalf("自己的链就一跳，得到 %v", ipNames(p))
	}
	if len(p.DirectTypes) != 0 || p.Chain[0].EdgeTypes == nil {
		t.Fatal("空数组该序列化成 []，不能是 null")
	}
}

// ===== 列表参与人一次 IN 批量补齐（O1）=====

func TestEventList_ParticipantsBatched(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := evMustPerson(t, s, "甲")
	b := evMustPerson(t, s, "乙")
	c := evMustPerson(t, s, "丙")

	e1 := &Event{Title: "第一场", EventDate: "2026-10-01"}
	egCreate(t, s, e1, []string{a.ID, c.ID, b.ID})
	e2 := &Event{Title: "第二场", EventDate: "2026-10-02"}
	egCreate(t, s, e2, []string{c.ID, a.ID})
	e3 := &Event{Title: "没人参加", EventDate: "2026-10-03"}
	egCreate(t, s, e3, nil)

	list, err := s.EventList(ctx, "", "", 50, 0)
	if err != nil {
		t.Fatalf("EventList: %v", err)
	}
	byTitle := map[string]*Event{}
	for _, e := range list {
		byTitle[e.Title] = e
	}
	// 参与人按登记顺序，不是按姓名排序
	if got := personNames(byTitle["第一场"].Participants); len(got) != 3 ||
		got[0] != "甲" || got[1] != "丙" || got[2] != "乙" {
		t.Fatalf("第一场参与人 = %v", got)
	}
	if got := personNames(byTitle["第二场"].Participants); len(got) != 2 || got[0] != "丙" || got[1] != "甲" {
		t.Fatalf("第二场参与人 = %v", got)
	}
	if len(byTitle["第二场"].Participants[0].Name) == 0 {
		t.Fatal("批量补齐丢了姓名")
	}
	if byTitle["没人参加"].Participants != nil {
		t.Fatalf("无人参加的事件不该带参与人数组，得到 %v", byTitle["没人参加"].Participants)
	}
}

func TestEventList_PersonFilterStillHydrates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := evMustPerson(t, s, "甲")
	b := evMustPerson(t, s, "乙")

	egCreate(t, s, &Event{Title: "有甲", EventDate: "2026-10-01"}, []string{a.ID, b.ID})
	egCreate(t, s, &Event{Title: "没甲", EventDate: "2026-10-02"}, []string{b.ID})

	list, err := s.EventList(ctx, a.ID, "", 50, 0)
	if err != nil {
		t.Fatalf("EventList: %v", err)
	}
	if len(list) != 1 || list[0].Title != "有甲" {
		t.Fatalf("按人过滤后 = %v", evTitles(list))
	}
	if got := personNames(list[0].Participants); len(got) != 2 {
		t.Fatalf("过滤路径也要补齐参与人，得到 %v", got)
	}
}
