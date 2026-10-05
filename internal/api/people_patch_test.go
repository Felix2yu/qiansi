package api

import (
	"context"
	"net/http"
	"testing"
)

// 局部更新的全部意义在于「只动点名的那一列」，所以每个用例都要顺带确认
// 其余列没被写成零值——那是 PUT 整行覆盖会干的事。

func TestAPIPeoplePatchKeepsOtherColumns(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	id := apiCreatePersonMap(t, s, map[string]any{
		"name": "局部更新", "grade": 4, "location": "杭州", "notes": "别弄丢",
	})["id"].(string)
	before := s.get("/api/v1/people/" + id)["person"].(map[string]any)

	rec := s.do(http.MethodPatch, "/api/v1/people/"+id, map[string]any{"gender": "女"})
	got := decodeMap(t, rec)
	if got["gender"] != "F" {
		t.Fatalf("PATCH gender=女 => %v, want F", got["gender"])
	}
	for _, c := range []struct {
		key  string
		want any
	}{
		{"name", "局部更新"}, {"grade", float64(4)}, {"location", "杭州"}, {"notes", "别弄丢"},
		{"updated_at", before["updated_at"]},
	} {
		if got[c.key] != c.want {
			t.Fatalf("%s = %v, want %v（局部更新把别的列写坏了）", c.key, got[c.key], c.want)
		}
	}

	// 响应里的值要真的落库，不是处理器临时拼出来的
	p, err := s.Store.PersonGet(ctx, id)
	if err != nil {
		t.Fatalf("PersonGet: %v", err)
	}
	if p.Gender != "F" || p.Notes != "别弄丢" {
		t.Fatalf("落库 gender=%q notes=%q", p.Gender, p.Notes)
	}
}

func TestAPIPeoplePatchRejectsUnknownColumnAndPerson(t *testing.T) {
	s := newTestServer(t)
	id := apiCreatePersonMap(t, s, map[string]any{"name": "校验收口"})["id"].(string)

	for _, c := range []struct {
		name, want string
		body       map[string]any
	}{
		// 未知键必须报错：静默跳过的话界面会提示「已更新」而那一列一个字没动
		{"未知字段", "不支持这样改的字段", map[string]any{"nickname": "偷改"}},
		{"脏性别", "gender 只能是", map[string]any{"gender": "未知"}},
		{"超档亲密度", "亲密度等级只能是 0–5", map[string]any{"grade": 9}},
		{"空 set", "set 要至少一个字段", map[string]any{}},
	} {
		rec := s.do(http.MethodPatch, "/api/v1/people/"+id, c.body)
		apiWantError(t, c.name, "/api/v1/people/"+id, rec, http.StatusBadRequest, c.want)
	}

	// 回收站里的人当作不存在：否则旧标签页会「保存成功」却什么都看不见
	rec := s.do(http.MethodDelete, "/api/v1/people/", map[string]any{"ids": []string{id}})
	apiWantStatus(t, "软删除", "/api/v1/people/", rec, http.StatusOK)
	rec = s.do(http.MethodPatch, "/api/v1/people/"+id, map[string]any{"gender": "M"})
	apiWantError(t, "PATCH 回收站里的人", "/api/v1/people/"+id, rec, http.StatusNotFound, "人物不存在")
}

func TestAPIPeopleBulkUpdateOnlyEmpty(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	filled := apiCreatePersonMap(t, s, map[string]any{"name": "已填过", "gender": "F", "grade": 2})["id"].(string)
	blank1 := apiCreatePersonMap(t, s, map[string]any{"name": "空着1"})["id"].(string)
	blank2 := apiCreatePersonMap(t, s, map[string]any{"name": "空着2"})["id"].(string)
	ghost := apiCreatePersonMap(t, s, map[string]any{"name": "进回收站"})["id"].(string)
	updatedAt := s.get("/api/v1/people/" + blank1)["person"].(map[string]any)["updated_at"]
	apiWantStatus(t, "软删除", "/api/v1/people/",
		s.do(http.MethodDelete, "/api/v1/people/", map[string]any{"ids": []string{ghost}}), http.StatusOK)

	ids := []string{filled, blank1, blank2, ghost}
	rec := s.do(http.MethodPost, "/api/v1/people/bulk-update", map[string]any{
		"ids": ids, "set": map[string]any{"gender": "M"}, "only_empty": true,
	})
	// 只填还没填的两个：已填的要保住原值，回收站里那个不算
	if got := decodeMap(t, rec)["updated"]; got != float64(2) {
		t.Fatalf("only_empty 更新了 %v 人, want 2", got)
	}
	if p, _ := s.Store.PersonGet(ctx, filled); p.Gender != "F" {
		t.Fatalf("已填过的人被覆盖成 %q", p.Gender)
	}
	if p, _ := s.Store.PersonGet(ctx, blank1); p.Gender != "M" || p.UpdatedAt != updatedAt {
		t.Fatalf("空着的人 gender=%q updated_at=%v（应补上 M 且不动更新时间）", p.Gender, p.UpdatedAt)
	}
	// 直接读列：PersonGet 走 live_people 视图，看不到回收站里那条到底被动没动
	if g := patchTestGender(t, s, ghost); g != "" {
		t.Fatalf("回收站里的人被改成了 %q", g)
	}

	// 不勾「只填空着的」就是整批覆盖，回收站里那个仍然不动
	rec = s.do(http.MethodPost, "/api/v1/people/bulk-update", map[string]any{
		"ids": ids, "set": map[string]any{"gender": "F"},
	})
	if got := decodeMap(t, rec)["updated"]; got != float64(3) {
		t.Fatalf("整批覆盖更新了 %v 人, want 3", got)
	}
	for _, id := range []string{filled, blank1, blank2} {
		if p, _ := s.Store.PersonGet(ctx, id); p.Gender != "F" {
			t.Fatalf("%s 的 gender = %q, want F", id, p.Gender)
		}
	}

	rec = s.do(http.MethodPost, "/api/v1/people/bulk-update", map[string]any{
		"ids": []string{}, "set": map[string]any{"gender": "F"},
	})
	apiWantError(t, "空 ids", "/api/v1/people/bulk-update", rec, http.StatusBadRequest, "ids 不能为空")
	rec = s.do(http.MethodPost, "/api/v1/people/bulk-update", map[string]any{
		"ids": ids, "set": map[string]any{"notes": "整批覆盖备注"},
	})
	apiWantError(t, "白名单外字段", "/api/v1/people/bulk-update", rec, http.StatusBadRequest, "不支持这样改的字段")
}

// patchTestGender 直读 people.gender，绕开 live_people 视图。
func patchTestGender(t *testing.T, s *testServer, id string) string {
	t.Helper()
	var g string
	if err := s.Store.DB.QueryRow("SELECT COALESCE(gender,'') FROM people WHERE id=?", id).Scan(&g); err != nil {
		t.Fatalf("读 %s 的 gender: %v", id, err)
	}
	return g
}
