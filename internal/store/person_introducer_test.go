package store

import (
	"errors"
	"testing"
)

// ===== 引荐人（认识来源） =====

func TestIntroducerRoundtrip(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	a := pMustCreate(t, s, &Person{Name: "同学甲"})
	c := pMustCreate(t, s, &Person{Name: "闺蜜丙", IntroducedByPersonID: a.ID})

	got, err := s.PersonGet(ctx, c.ID)
	if err != nil {
		t.Fatalf("PersonGet: %v", err)
	}
	if got.IntroducedByPersonID != a.ID || got.IntroducedByName != "同学甲" {
		t.Fatalf("引荐人回读 = %q / %q", got.IntroducedByPersonID, got.IntroducedByName)
	}
	// 列表路径也要带出引荐人 id（关系图靠它还原认识链）
	list, err := s.PersonList(ctx, "", 0, 0, false, 0, 50, 0)
	if err != nil {
		t.Fatalf("PersonList: %v", err)
	}
	var found bool
	for _, p := range list {
		if p.ID == c.ID {
			found = p.IntroducedByPersonID == a.ID
		}
	}
	if !found {
		t.Fatal("列表里丙的引荐人没带出来")
	}

	// 清空引荐人
	c.IntroducedByPersonID = ""
	if err := s.PersonUpdate(ctx, c); err != nil {
		t.Fatalf("清空引荐人: %v", err)
	}
	again, err := s.PersonGet(ctx, c.ID)
	if err != nil {
		t.Fatalf("PersonGet: %v", err)
	}
	if again.IntroducedByPersonID != "" {
		t.Fatalf("清空后引荐人应为空，得到 %q", again.IntroducedByPersonID)
	}
}

func TestIntroducerValidation(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	a := pMustCreate(t, s, &Person{Name: "甲"})
	b := pMustCreate(t, s, &Person{Name: "乙"})

	// 指向不存在的人
	if err := s.PersonUpdate(ctx, &Person{ID: a.ID, Name: "甲", IntroducedByPersonID: "ghost"}); !errors.Is(err, ErrIntroMissing) {
		t.Fatalf("缺失引荐人应 ErrIntroMissing，得到 %v", err)
	}
	// 指向自己
	if err := s.PersonUpdate(ctx, &Person{ID: a.ID, Name: "甲", IntroducedByPersonID: a.ID}); !errors.Is(err, ErrIntroCycle) {
		t.Fatalf("引荐人是自己应 ErrIntroCycle，得到 %v", err)
	}

	// 搭链 a → b，再让 b 指回 a，形成环
	if err := s.PersonUpdate(ctx, &Person{ID: a.ID, Name: "甲", IntroducedByPersonID: b.ID}); err != nil {
		t.Fatalf("a→b: %v", err)
	}
	if err := s.PersonUpdate(ctx, &Person{ID: b.ID, Name: "乙", IntroducedByPersonID: a.ID}); !errors.Is(err, ErrIntroCycle) {
		t.Fatalf("b→a 成环应 ErrIntroCycle，得到 %v", err)
	}

	// 更长的环：a→b、c→a 已存在，b 想指 c → b→c→a→b
	c := pMustCreate(t, s, &Person{Name: "丙", IntroducedByPersonID: a.ID})
	if err := s.PersonUpdate(ctx, &Person{ID: b.ID, Name: "乙", IntroducedByPersonID: c.ID}); !errors.Is(err, ErrIntroCycle) {
		t.Fatalf("b→c（沿 c→a→b 回到自己）应 ErrIntroCycle，得到 %v", err)
	}
}

func TestIntroducerMergeRedirect(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	x := pMustCreate(t, s, &Person{Name: "源"})
	y := pMustCreate(t, s, &Person{Name: "目标"})
	a := pMustCreate(t, s, &Person{Name: "经由源认识", IntroducedByPersonID: x.ID})

	if err := s.PersonMerge(ctx, x.ID, y.ID); err != nil {
		t.Fatalf("PersonMerge: %v", err)
	}
	got, err := s.PersonGet(ctx, a.ID)
	if err != nil {
		t.Fatalf("PersonGet: %v", err)
	}
	if got.IntroducedByPersonID != y.ID {
		t.Fatalf("合并后引荐人应重定向到目标，得到 %q", got.IntroducedByPersonID)
	}

	// target 原本通过 source 认识：source 又有上游 z，合并后 target 应续上 z
	z := pMustCreate(t, s, &Person{Name: "上游"})
	src := pMustCreate(t, s, &Person{Name: "源二"})
	tgt := pMustCreate(t, s, &Person{Name: "目标二", IntroducedByPersonID: src.ID})
	src.IntroducedByPersonID = z.ID
	if err := s.PersonUpdate(ctx, src); err != nil {
		t.Fatalf("src→z: %v", err)
	}
	if err := s.PersonMerge(ctx, src.ID, tgt.ID); err != nil {
		t.Fatalf("PersonMerge 二: %v", err)
	}
	gotTgt, err := s.PersonGet(ctx, tgt.ID)
	if err != nil {
		t.Fatalf("PersonGet 目标二: %v", err)
	}
	if gotTgt.IntroducedByPersonID != z.ID {
		t.Fatalf("target 经 source 的链应续到上游 z，得到 %q", gotTgt.IntroducedByPersonID)
	}
}

func TestRelationshipTypesDistinct(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	a := pMustCreate(t, s, &Person{Name: "甲"})
	b := pMustCreate(t, s, &Person{Name: "乙"})
	c := pMustCreate(t, s, &Person{Name: "丙"})

	if err := s.RelationshipCreate(ctx, &Relationship{FromPerson: a.ID, ToPerson: b.ID, Type: "闺蜜"}); err != nil {
		t.Fatalf("rel1: %v", err)
	}
	if err := s.RelationshipCreate(ctx, &Relationship{FromPerson: b.ID, ToPerson: a.ID, Type: "闺蜜"}); err != nil {
		t.Fatalf("rel2: %v", err)
	}
	if err := s.RelationshipCreate(ctx, &Relationship{FromPerson: a.ID, ToPerson: c.ID, Type: "对象"}); err != nil {
		t.Fatalf("rel3: %v", err)
	}
	got, err := s.RelationshipTypes(ctx)
	if err != nil {
		t.Fatalf("RelationshipTypes: %v", err)
	}
	if len(got) != 2 || got[0] != "对象" || got[1] != "闺蜜" {
		t.Fatalf("类型应去重（SQLite 按码点排序），得到 %v", got)
	}
}

func TestRelationshipGraphTags(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	a := pMustCreate(t, s, &Person{Name: "甲"})
	pMustCreate(t, s, &Person{Name: "乙"})

	tag := &Tag{Name: "爱猫", Color: "#f59e0b"}
	if err := s.TagUpsert(ctx, tag); err != nil {
		t.Fatalf("TagUpsert: %v", err)
	}
	pRaw(t, s, "INSERT INTO taggings(tag_id,target_type,target_id) VALUES(?,'person',?)", tag.ID, a.ID)

	_, _, tags, err := s.RelationshipGraph(ctx)
	if err != nil {
		t.Fatalf("RelationshipGraph: %v", err)
	}
	if len(tags[a.ID]) != 1 || tags[a.ID][0].Name != "爱猫" {
		t.Fatalf("甲应带出一个标签: %+v", tags[a.ID])
	}
}
