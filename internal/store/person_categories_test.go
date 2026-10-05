package store

import (
	"strings"
	"testing"
)

// ===== 圈子多对多 =====

func catNames(list []*PersonCategory) string {
	parts := make([]string, len(list))
	for i, c := range list {
		parts[i] = c.Name
	}
	return strings.Join(parts, ",")
}

func TestPersonMultipleCircles(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	tongshi := &Category{Name: "同事", Color: "#2563eb", Icon: "briefcase", SortOrder: 2}
	tongxue := &Category{Name: "同学", Color: "#e11d48", Icon: "users", SortOrder: 1}
	pMustCategory(t, s, ctx, tongshi)
	pMustCategory(t, s, ctx, tongxue)

	// 写入顺序故意与 sort_order 相反，回读应当按 sort_order 排，拼色的分段顺序才稳定
	p := pMustCreate(t, s, &Person{Name: "交叉身份", CategoryIDs: []int{tongshi.ID, tongxue.ID, tongxue.ID, 0}})
	got, err := s.PersonGet(ctx, p.ID)
	if err != nil {
		t.Fatalf("PersonGet: %v", err)
	}
	if len(got.Categories) != 2 {
		t.Fatalf("去重后应剩两个圈子: %+v", got.Categories)
	}
	if got.Categories[0].Name != "同学" || got.Categories[1].Name != "同事" {
		t.Fatalf("应按 sort_order 排: %s", catNames(got.Categories))
	}
	if got.Categories[0].Color != "#e11d48" {
		t.Fatalf("应带出圈子颜色: %+v", got.Categories[0])
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM person_categories WHERE person_id=?", p.ID); n != 2 {
		t.Fatalf("关联表应有 2 行，得到 %d", n)
	}
	// 按圈子筛选：同一个人能被任一圈子筛出来
	for _, cid := range []int{tongshi.ID, tongxue.ID} {
		list, err := s.PersonList(ctx, "", cid, 0, false, 0, 50, 0)
		if err != nil {
			t.Fatalf("PersonList(category=%d): %v", cid, err)
		}
		if len(list) != 1 || list[0].ID != p.ID {
			t.Fatalf("圈子筛选结果 = %v", pNames(list))
		}
	}

	// 整行覆盖：只留同事
	got.CategoryIDs = []int{tongshi.ID}
	if err := s.PersonUpdate(ctx, got); err != nil {
		t.Fatalf("PersonUpdate: %v", err)
	}
	again, _ := s.PersonGet(ctx, p.ID)
	// CategoryIDs 只用于写入，回读只有带名字的 Categories
	if len(again.Categories) != 1 || again.Categories[0].Name != "同事" {
		t.Fatalf("更新后应只剩同事: %+v", again.Categories)
	}
	if list, err := s.PersonList(ctx, "", tongxue.ID, 0, false, 0, 50, 0); err != nil || len(list) != 0 {
		t.Fatalf("已移出同学圈，按同学筛选应为空，得到 %v %v", pNames(list), err)
	}
}

// 一人多圈子会让 JOIN 出一人多行，分页必须仍按「人」数，否则前端 hasMore 判断会失真。
func TestPersonListPaginationCountsPeopleNotRows(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	c1 := &Category{Name: "同学", Color: "#f00", SortOrder: 1}
	c2 := &Category{Name: "同事", Color: "#00f", SortOrder: 2}
	pMustCategory(t, s, ctx, c1)
	pMustCategory(t, s, ctx, c2)
	for _, name := range []string{"甲", "乙", "丙"} {
		pMustCreate(t, s, &Person{Name: name, Grade: 3, CategoryIDs: []int{c1.ID, c2.ID}})
	}

	page1, err := s.PersonList(ctx, "", 0, 0, false, 0, 2, 0)
	if err != nil {
		t.Fatalf("PersonList(page1): %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("第一页应正好 2 人，得到 %d: %v", len(page1), pNames(page1))
	}
	page2, err := s.PersonList(ctx, "", 0, 0, false, 0, 2, 2)
	if err != nil {
		t.Fatalf("PersonList(page2): %v", err)
	}
	if len(page2) != 1 {
		t.Fatalf("第二页应剩 1 人，得到 %d: %v", len(page2), pNames(page2))
	}
	for _, p := range append(page1, page2...) {
		if len(p.Categories) != 2 {
			t.Fatalf("%s 的圈子应补全: %+v", p.Name, p.Categories)
		}
	}

	// 按圈子筛选时同样不能因为一人两圈而重复出现
	filtered, err := s.PersonList(ctx, "", c1.ID, 0, false, 0, 50, 0)
	if err != nil {
		t.Fatalf("PersonList(filter): %v", err)
	}
	if len(filtered) != 3 {
		t.Fatalf("筛选应得 3 人，得到 %d: %v", len(filtered), pNames(filtered))
	}
}

func TestPeopleAddCategories(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	c1 := &Category{Name: "球友", Color: "#0f0", SortOrder: 1}
	c2 := &Category{Name: "牌友", Color: "#0ff", SortOrder: 2}
	pMustCategory(t, s, ctx, c1)
	pMustCategory(t, s, ctx, c2)
	a := pMustCreate(t, s, &Person{Name: "阿一", CategoryIDs: []int{c1.ID}})
	b := pMustCreate(t, s, &Person{Name: "阿二"})
	c := pMustCreate(t, s, &Person{Name: "阿三"})

	// 只增不减：阿一已在球友圈，再点一次不该报错也不该翻倍
	added, err := s.PeopleAddCategories(ctx, []string{a.ID, b.ID, c.ID, a.ID, "", "ghost-id"}, []int{c1.ID, c2.ID, c2.ID})
	if err != nil {
		t.Fatalf("PeopleAddCategories: %v", err)
	}
	// 阿一二三各进球友（阿一已有→2 新），各进牌友（3 新）
	if added != 5 {
		t.Fatalf("新增成员关系条数 = %d, want 5", added)
	}
	for _, p := range []*Person{a, b, c} {
		got, err := s.PersonGet(ctx, p.ID)
		if err != nil {
			t.Fatalf("PersonGet(%s): %v", p.Name, err)
		}
		if len(got.Categories) != 2 {
			t.Fatalf("%s 应有两个圈子: %+v", p.Name, got.Categories)
		}
	}

	// 空入参不动任何东西
	if n, err := s.PeopleAddCategories(ctx, nil, []int{c1.ID}); err != nil || n != 0 {
		t.Fatalf("空 ids 应返回 0/nil，得到 %d %v", n, err)
	}
	if n, err := s.PeopleAddCategories(ctx, []string{a.ID}, nil); err != nil || n != 0 {
		t.Fatalf("空 category_ids 应返回 0/nil，得到 %d %v", n, err)
	}
}

func TestPersonMergeUnionsCircles(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	c1 := &Category{Name: "同学", Color: "#f00", SortOrder: 1}
	c2 := &Category{Name: "同事", Color: "#00f", SortOrder: 2}
	pMustCategory(t, s, ctx, c1)
	pMustCategory(t, s, ctx, c2)
	source := pMustCreate(t, s, &Person{ID: "src", Name: "源", CategoryIDs: []int{c1.ID}})
	target := pMustCreate(t, s, &Person{ID: "dst", Name: "目标", CategoryIDs: []int{c1.ID, c2.ID}})

	if err := s.PersonMerge(ctx, source.ID, target.ID); err != nil {
		t.Fatalf("PersonMerge: %v", err)
	}
	got, err := s.PersonGet(ctx, target.ID)
	if err != nil {
		t.Fatalf("PersonGet: %v", err)
	}
	if len(got.Categories) != 2 {
		t.Fatalf("合并后应取圈子并集: %+v", got.Categories)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM person_categories WHERE person_id=?", source.ID); n != 0 {
		t.Fatalf("source 的成员关系应清干净，得到 %d", n)
	}
}

func TestCircleMembershipCascades(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)

	cat := &Category{Name: "邻居", Color: "#ffd", SortOrder: 1}
	pMustCategory(t, s, ctx, cat)
	keep := pMustCreate(t, s, &Person{Name: "留下", CategoryIDs: []int{cat.ID}})
	gone := pMustCreate(t, s, &Person{Name: "删掉", CategoryIDs: []int{cat.ID}})

	if err := s.PersonDelete(ctx, gone.ID); err != nil {
		t.Fatalf("PersonDelete: %v", err)
	}
	// 进回收站的人不动成员关系：恢复后他还在原来的圈子里
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM person_categories"); n != 2 {
		t.Fatalf("软删除不该动成员关系，剩余 %d", n)
	}
	if err := s.PersonPurge(ctx, gone.ID); err != nil {
		t.Fatalf("PersonPurge: %v", err)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM person_categories"); n != 1 {
		t.Fatalf("彻底删人应级联清掉他的成员关系，剩余 %d", n)
	}
	if err := s.CategoryDelete(ctx, cat.ID); err != nil {
		t.Fatalf("CategoryDelete: %v", err)
	}
	if n := pRawScanInt(t, s, "SELECT COUNT(*) FROM person_categories"); n != 0 {
		t.Fatalf("删圈子应级联清掉成员关系，剩余 %d", n)
	}
	got, err := s.PersonGet(ctx, keep.ID)
	if err != nil {
		t.Fatalf("PersonGet: %v", err)
	}
	if len(got.Categories) != 0 {
		t.Fatalf("删圈子后人不应再挂该圈子: %+v", got.Categories)
	}
}

// people.category_id 这一列已经没了：任何残留读写它的代码都会在测试里直接报错。
func TestPeopleTableHasNoCategoryColumn(t *testing.T) {
	ctx := pctx(t)
	s := newTestStore(t)
	rows, err := s.DB.QueryContext(ctx, "SELECT name FROM pragma_table_info('people')")
	if err != nil {
		t.Fatalf("pragma_table_info: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if col == "category_id" {
			t.Fatalf("people 仍带 category_id 列，迁移 007 没删掉")
		}
	}
}
