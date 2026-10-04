package store

import (
	"fmt"
	"testing"
)

// gtSeed 用 SQL 直接灌人，updated_at 跟着序号递增。
// 图是按「等级、最近更新」取前 graphNodeLimit 人的，时间戳写死才知道谁落在页外。
// 列必须和 PersonCreate 一致补上空串：queryPeople 用裸 string 扫这些列，NULL 会直接扫失败。
func gtSeed(t *testing.T, s *Store, n int) []string {
	t.Helper()
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("gt-%03d", i)
		stamp := fmt.Sprintf("2020-01-01T%02d:%02d:00Z", i/60, i%60)
		pRaw(t, s, `INSERT INTO people(id,name,family_name,given_name,nickname,gender,birthday,
birthday_is_lunar,avatar_attachment_id,phone,wechat,location,notes,grade,archived,x_abuid,
created_at,updated_at,introduced_by_person_id)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, fmt.Sprintf("路人%03d", i), "", "", "", "", "", 0, "", "", "", "", "", 0, 0, "",
			stamp, stamp, nil)
		ids[i] = id
	}
	return ids
}

func gtHas(list []*Person, id string) bool {
	for _, p := range list {
		if p.ID == id {
			return true
		}
	}
	return false
}

// 人一旦多过上限，「图上找不到人」必须是明说的，不能悄悄少画。
func TestRelationshipGraph_TruncationIsReported(t *testing.T) {
	s := newTestStore(t)
	ctx := pctx(t)
	ids := gtSeed(t, s, graphNodeLimit+2)
	// 最新的两人稳在页内，最旧的 ids[0] 被截掉
	ipMustRel(t, s, ids[graphNodeLimit+1], ids[graphNodeLimit], "同学")
	ipMustRel(t, s, ids[graphNodeLimit+1], ids[0], "网友")

	g, err := s.RelationshipGraph(ctx)
	if err != nil {
		t.Fatalf("RelationshipGraph: %v", err)
	}
	if g.TotalPeople != graphNodeLimit+2 {
		t.Fatalf("total_people = %d，期望 %d", g.TotalPeople, graphNodeLimit+2)
	}
	if len(g.People) != graphNodeLimit {
		t.Fatalf("people = %d 条，期望正好一页 %d", len(g.People), graphNodeLimit)
	}
	if !g.Truncated {
		t.Fatalf("超过上限却没报 truncated")
	}
	if !gtHas(g.People, ids[graphNodeLimit+1]) || gtHas(g.People, ids[0]) {
		t.Fatalf("截断留下的不是按最近更新排的前一页：%v 在，%v 也在", ids[graphNodeLimit+1], ids[0])
	}
	// 端点在页外的边不能返回，否则图上挂一条没有节点的线
	if len(g.Relationships) != 1 || g.Relationships[0].ToPerson != ids[graphNodeLimit] {
		t.Fatalf("relationships = %+v，只该留下两端都在页内的那条", g.Relationships)
	}
	if g.DroppedEdges != 1 {
		t.Fatalf("dropped_edges = %d，期望 1", g.DroppedEdges)
	}
	// Tags 按页内的人取，页外的人不该占位
	if _, ok := g.Tags[ids[0]]; ok {
		t.Fatalf("被截掉的人不该出现在 tags 里")
	}
}

// 没超过上限时不能报截断，边一条都不能少。
func TestRelationshipGraph_NoTruncationUnderLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := pctx(t)
	a := evMustPerson(t, s, "甲")
	b := evMustPerson(t, s, "乙")
	c := evMustPerson(t, s, "丙")
	ipMustRel(t, s, a.ID, b.ID, "同事")
	ipMustRel(t, s, b.ID, c.ID, "同学")

	g, err := s.RelationshipGraph(ctx)
	if err != nil {
		t.Fatalf("RelationshipGraph: %v", err)
	}
	if g.Truncated || g.DroppedEdges != 0 {
		t.Fatalf("三个人就报截断：truncated=%v dropped_edges=%d", g.Truncated, g.DroppedEdges)
	}
	if g.TotalPeople != 3 || len(g.People) != 3 || len(g.Relationships) != 2 {
		t.Fatalf("图 = 人 %d / 边 %d / 共 %d，期望 3 / 2 / 3", len(g.People), len(g.Relationships), g.TotalPeople)
	}
}
