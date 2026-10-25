package store

import (
	"testing"
)

// 对话列表上的「谁说的」由后端直接带出来：前端选人改成按需搜索之后，
// 不能再为了几个名字把整本通讯录拉下来。
func TestMemoList_CarriesPersonName(t *testing.T) {
	s := newTestStore(t)
	ctx := pctx(t)
	me := evMustPerson(t, s, "小美")

	tied := &Memo{PersonID: me.ID, Content: "答应帮她带特产", SaidAt: "2026-03-02T10:00:00"}
	if err := s.MemoCreate(ctx, tied); err != nil {
		t.Fatalf("MemoCreate: %v", err)
	}
	// 没有归属的随手记：名字留空，不能因为 JOIN 落空而报错
	loose := &Memo{Content: "想起个选题", SaidAt: "2026-03-01T10:00:00"}
	if err := s.MemoCreate(ctx, loose); err != nil {
		t.Fatalf("MemoCreate loose: %v", err)
	}

	list, err := s.MemoList(ctx, "", false, 0, 0)
	if err != nil {
		t.Fatalf("MemoList: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("MemoList = %d 条，期望 2", len(list))
	}
	if list[0].ID != tied.ID || list[0].PersonName != "小美" {
		t.Fatalf("带归属的那条应回填 person_name：%+v", list[0])
	}
	if list[1].PersonName != "" || list[1].PersonID != "" {
		t.Fatalf("无归属的备忘不该有名字：%+v", list[1])
	}

	// 按人筛时同样带名字，详情页不必再单独查一次
	one, err := s.MemoList(ctx, me.ID, false, 0, 0)
	if err != nil || len(one) != 1 || one[0].PersonName != "小美" {
		t.Fatalf("按人 MemoList = %+v (%v)，期望 1 条且带名字", one, err)
	}
}

// 联系人进了回收站，他名下的对话一起藏起来；联系人被彻底删除时，
// 对话随人一起删掉——外键是 ON DELETE SET NULL，不显式删的话它们会以
// 「无归属」状态重新出现在列表里，与回收站的牵连口径矛盾（旧口径已废弃）。
func TestMemoList_NameFollowsPersonDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := pctx(t)
	me := evMustPerson(t, s, "老王")
	if err := s.MemoCreate(ctx, &Memo{PersonID: me.ID, Content: "借了他相机", SaidAt: "2026-03-01T10:00:00"}); err != nil {
		t.Fatalf("MemoCreate: %v", err)
	}
	if err := s.PersonDelete(ctx, me.ID); err != nil {
		t.Fatalf("PersonDelete: %v", err)
	}
	list, err := s.MemoList(ctx, "", false, 0, 0)
	if err != nil {
		t.Fatalf("MemoList: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("备忘条数 = %d，联系人还在回收站时期望整体藏起来", len(list))
	}
	if err := s.PersonPurge(ctx, me.ID); err != nil {
		t.Fatalf("PersonPurge: %v", err)
	}
	list, err = s.MemoList(ctx, "", false, 0, 0)
	if err != nil {
		t.Fatalf("MemoList(彻底删除后): %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("备忘条数 = %d，期望随联系人一起删掉", len(list))
	}
}
