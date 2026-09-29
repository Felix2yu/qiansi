package api

import (
	"net/http"
	"strings"
	"testing"
)

// ===== 批量加入圈子 =====

// apiCircleNames 取出联系人响应里的圈子名（按后端 sort_order 排），顺带检查颜色有没有带出来。
func apiCircleNames(t *testing.T, body map[string]any) string {
	t.Helper()
	list, _ := body["categories"].([]any)
	parts := make([]string, 0, len(list))
	for _, it := range list {
		m, ok := it.(map[string]any)
		if !ok {
			t.Fatalf("categories 元素不是对象: %v", it)
		}
		if s, _ := m["color"].(string); s == "" {
			t.Fatalf("圈子应带颜色供图谱拼色: %v", m)
		}
		parts = append(parts, m["name"].(string))
	}
	return strings.Join(parts, ",")
}

// apiPersonOf 取详情接口里包了一层的 person 对象。
func apiPersonOf(s *testServer, id string) map[string]any {
	s.t.Helper()
	body, ok := s.get("/api/v1/people/" + id)["person"].(map[string]any)
	if !ok {
		s.t.Fatalf("GET /api/v1/people/%s 未返回 person: %v", id, body)
	}
	return body
}

func TestAPIPeopleBulkCategories(t *testing.T) {
	s := newTestServer(t)

	catA := int(decodeMap(t, s.do(http.MethodPost, "/api/v1/categories/", map[string]any{
		"name": "球友", "color": "#22c55e", "sort_order": 1,
	}))["id"].(float64))
	catB := int(decodeMap(t, s.do(http.MethodPost, "/api/v1/categories/", map[string]any{
		"name": "牌友", "color": "#a855f7", "sort_order": 2,
	}))["id"].(float64))

	a := apiCreatePersonMap(t, s, map[string]any{"name": "阿一", "notes": "原本就在球友圈", "category_ids": []any{catA}})
	b := apiCreatePersonMap(t, s, map[string]any{"name": "阿二"})
	c := apiCreatePersonMap(t, s, map[string]any{"name": "阿三"})

	const path = "/api/v1/people/bulk-categories"
	rec := s.do(http.MethodPost, path, map[string]any{
		// ids 里故意重复、夹一个不存在的人：重复不该翻倍，幽灵 id 不该计入
		"ids":          []any{a["id"], b["id"], c["id"], a["id"], "ghost-id"},
		"category_ids": []any{catA, catB, catB},
	})
	apiWantStatus(t, http.MethodPost, path, rec, http.StatusOK)
	if added := int(decodeMap(t, rec)["added"].(float64)); added != 5 {
		t.Fatalf("added = %d, want 5（球友 2 条新增 + 牌友 3 条）", added)
	}

	for _, id := range []string{a["id"].(string), b["id"].(string), c["id"].(string)} {
		got := apiPersonOf(s, id)
		if names := apiCircleNames(t, got); names != "球友,牌友" {
			t.Fatalf("%s 的圈子 = %q", id, names)
		}
	}
	// 只动关联表，不该顺手改联系人自己的字段
	if got := apiPersonOf(s, a["id"].(string)); got["notes"] != "原本就在球友圈" {
		t.Fatalf("批量加圈子把 notes 改了: %v", got["notes"])
	}
	// 列表按新圈子筛选应当能一次筛出三人
	if names := apiNames(t, apiArray(s, "/api/v1/people/?category_id="+itoa(catB))); len(names) != 3 {
		t.Fatalf("按牌友筛选 = %v", names)
	}

	// 重复调用：一条都不再加
	rec = s.do(http.MethodPost, path, map[string]any{"ids": []any{a["id"]}, "category_ids": []any{catA, catB}})
	apiWantStatus(t, http.MethodPost, path+" 幂等", rec, http.StatusOK)
	if added := int(decodeMap(t, rec)["added"].(float64)); added != 0 {
		t.Fatalf("重复加入应返回 0，得到 %d", added)
	}

	// 全是幽灵 id：200 且 0 条
	rec = s.do(http.MethodPost, path, map[string]any{"ids": []any{"ghost-id"}, "category_ids": []any{catA}})
	apiWantStatus(t, http.MethodPost, path+" 幽灵 id", rec, http.StatusOK)
	if added := int(decodeMap(t, rec)["added"].(float64)); added != 0 {
		t.Fatalf("幽灵 id 应返回 0，得到 %d", added)
	}

	// 入参校验：两侧都不能空，否则等于把全库扫一遍再什么都不加
	for _, tc := range []struct {
		desc string
		body any
	}{
		{"空 ids", map[string]any{"ids": []any{}, "category_ids": []any{catA}}},
		{"缺 category_ids", map[string]any{"ids": []any{a["id"]}}},
	} {
		apiWantError(t, http.MethodPost, path+" "+tc.desc, s.do(http.MethodPost, path, tc.body), http.StatusBadRequest, "不能为空")
	}
	apiWantStatus(t, http.MethodPost, path+" 非法 JSON",
		s.raw(http.MethodPost, path, []byte("nope"), "application/json"), http.StatusBadRequest)
}
