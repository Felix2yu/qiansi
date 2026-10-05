package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// ===== 回收站 API =====
//
// 这一层要盯的是路由契约：kind 拼错不能用 500 糊弄，
// 还在使用的记录不能被「彻底删除」误伤，以及删除与恢复的往返。

// apiTrashList 拉回收站列表，按 类型/id 索引，断言时不必关心排序。
func apiTrashList(t *testing.T, s *testServer) map[string]map[string]any {
	t.Helper()
	rec := s.do(http.MethodGet, "/api/v1/trash/", nil)
	body := apiWantStatus(t, http.MethodGet, "/api/v1/trash/", rec, http.StatusOK)
	var list []map[string]any
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("decode trash list: %v (%s)", err, body)
	}
	out := map[string]map[string]any{}
	for _, it := range list {
		id, _ := it["id"].(string)
		out[it["type"].(string)+"/"+id] = it
	}
	return out
}

// apiMustCreate 走接口建一条记录并返回它的 id。
func apiMustCreate(t *testing.T, s *testServer, path string, body map[string]any) string {
	t.Helper()
	rec := s.do(http.MethodPost, path, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s %v => %d: %s", path, body, rec.Code, rec.Body.String())
	}
	m := decodeMap(t, rec)
	id, _ := m["id"].(string)
	if id == "" {
		t.Fatalf("POST %s 未返回 id: %v", path, m)
	}
	return id
}

func TestAPITrashRoundTrip(t *testing.T) {
	s := newTestServer(t)

	pid := apiMustCreate(t, s, "/api/v1/people/", map[string]any{"name": "老王"})
	memoID := apiMustCreate(t, s, "/api/v1/memos/", map[string]any{
		"person_id": pid, "speaker": "me", "content": "说好四月底还相机", "said_at": "2026-05-02T10:00:00"})

	// 联系人进回收站：他本人和他名下的对话都不在列表里，但一行没少
	apiWantStatus(t, http.MethodDelete, "/api/v1/people/{id}", s.do(http.MethodDelete, "/api/v1/people/"+pid, nil), http.StatusNoContent)
	if rec := s.do(http.MethodGet, "/api/v1/people/"+pid, nil); rec.Code != http.StatusNotFound {
		t.Errorf("回收站里的人 GET /people/{id} => %d, want 404", rec.Code)
	}
	if n := len(apiDecodeArray(t, "/api/v1/memos/", s.do(http.MethodGet, "/api/v1/memos/", nil))); n != 0 {
		t.Errorf("归属人被回收后对话列表 = %d 条, want 0", n)
	}
	trash := apiTrashList(t, s)
	// 对话没进回收站：它是被牵连藏起来的，恢复联系人就会回来
	if _, ok := trash["memo/"+memoID]; ok {
		t.Fatal("被牵连的对话不该自己出现在回收站里")
	}
	item, ok := trash["person/"+pid]
	if !ok {
		t.Fatal("回收站里没有这条联系人")
	}
	if item["title"] != "老王" || item["hidden"].(float64) != 1 {
		t.Fatalf("回收站条目不符: %v", item)
	}

	restore := "/api/v1/trash/person/" + pid + "/restore"
	apiWantStatus(t, http.MethodPost, restore, s.do(http.MethodPost, restore, map[string]any{}), http.StatusNoContent)
	if rec := s.do(http.MethodGet, "/api/v1/people/"+pid, nil); rec.Code != http.StatusOK {
		t.Errorf("恢复后 GET /people/{id} => %d: %s", rec.Code, rec.Body.String())
	}
	if n := len(apiDecodeArray(t, "/api/v1/memos/", s.do(http.MethodGet, "/api/v1/memos/", nil))); n != 1 {
		t.Errorf("恢复联系人后名下的对话应跟着回来，列表 %d 条", n)
	}
	if len(apiTrashList(t, s)) != 0 {
		t.Fatal("恢复后回收站没清干净")
	}
	// 已经活着的人不能再「恢复」一次（前端撤销按钮可能撞上这个）
	apiWantStatus(t, http.MethodPost, restore, s.do(http.MethodPost, restore, map[string]any{}), http.StatusNotFound)
}

// 路径里的 kind 来自白名单：不认识的类型是用法错误（400），不是服务端故障。
func TestAPITrashUnknownKind(t *testing.T) {
	s := newTestServer(t)

	for _, m := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/trash/relationship/x/restore"},
		{http.MethodDelete, "/api/v1/trash/relationship/x"},
	} {
		rec := s.do(m.method, m.path, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s %s => %d: %s, want 400", m.method, m.path, rec.Code, rec.Body.String())
		}
	}
	// 认识的 kind、不存在的 id → 404
	apiWantStatus(t, http.MethodPost, "restore", s.do(http.MethodPost, "/api/v1/trash/memo/ghost/restore", map[string]any{}), http.StatusNotFound)
	apiWantStatus(t, http.MethodDelete, "purge", s.do(http.MethodDelete, "/api/v1/trash/memo/ghost", nil), http.StatusNotFound)
}

// 彻底删除只认回收站里的行：还在使用的记录碰不到，误点过期网页也不会抹掉它们。
func TestAPITrashPurgeRejectsLiveRow(t *testing.T) {
	s := newTestServer(t)

	pid := apiMustCreate(t, s, "/api/v1/people/", map[string]any{"name": "还在用"})
	apiWantStatus(t, http.MethodDelete, "purge live", s.do(http.MethodDelete, "/api/v1/trash/person/"+pid, nil), http.StatusNotFound)
	if n := apiPeopleRowCount(t, s); n != 1 {
		t.Fatalf("活人应完好无损，people 表 %d 行", n)
	}
	if rec := s.do(http.MethodGet, "/api/v1/people/"+pid, nil); rec.Code != http.StatusOK {
		t.Errorf("GET /people/{id} => %d: %s", rec.Code, rec.Body.String())
	}
	// 先进回收站，再彻底删除
	apiWantStatus(t, http.MethodDelete, "people delete", s.do(http.MethodDelete, "/api/v1/people/"+pid, nil), http.StatusNoContent)
	apiWantStatus(t, http.MethodDelete, "purge", s.do(http.MethodDelete, "/api/v1/trash/person/"+pid, nil), http.StatusNoContent)
	if n := apiPeopleRowCount(t, s); n != 0 {
		t.Fatalf("彻底删除后仍有 %d 行", n)
	}
}

// 清空回收站：一次点掉全部，回执里的计数要如实。
func TestAPITrashEmpty(t *testing.T) {
	s := newTestServer(t)

	keep := apiMustCreate(t, s, "/api/v1/people/", map[string]any{"name": "留着"})
	gone := apiMustCreate(t, s, "/api/v1/people/", map[string]any{"name": "删掉"})
	memoID := apiMustCreate(t, s, "/api/v1/memos/", map[string]any{
		"person_id": keep, "speaker": "me", "content": "随手记一句", "said_at": "2026-05-03T10:00:00"})

	apiWantStatus(t, http.MethodDelete, "people delete", s.do(http.MethodDelete, "/api/v1/people/"+gone, nil), http.StatusNoContent)
	apiWantStatus(t, http.MethodDelete, "memo delete", s.do(http.MethodDelete, "/api/v1/memos/"+memoID, nil), http.StatusNoContent)
	if len(apiTrashList(t, s)) != 2 {
		t.Fatalf("回收站应有 2 行: %+v", apiTrashList(t, s))
	}
	body := apiWantStatus(t, http.MethodDelete, "/api/v1/trash/", s.do(http.MethodDelete, "/api/v1/trash/", nil), http.StatusOK)
	var res struct {
		Purged map[string]int `json:"purged"`
	}
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatalf("decode empty result: %v (%s)", err, body)
	}
	if res.Purged["person"] != 1 || res.Purged["memo"] != 1 {
		t.Fatalf("计数不符: %+v", res.Purged)
	}
	if len(apiTrashList(t, s)) != 0 {
		t.Fatal("清空后回收站仍有内容")
	}
	// 没进回收站的人与他的记录不受影响
	if rec := s.do(http.MethodGet, "/api/v1/people/"+keep, nil); rec.Code != http.StatusOK {
		t.Errorf("GET /people/{keep} => %d: %s", rec.Code, rec.Body.String())
	}
}
