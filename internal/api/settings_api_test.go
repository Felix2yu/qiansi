package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiansi/app/internal/config"
	"github.com/qiansi/app/internal/store"
	"github.com/qiansi/app/internal/testdb"
)

// ===== 通用测试辅助 =====

// apiDecodeArray 解码返回 JSON 数组的响应体（testServer.get 只处理对象）。
func apiDecodeArray(t *testing.T, path string, rec *httptest.ResponseRecorder) []any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s => %d: %s", path, rec.Code, rec.Body.String())
	}
	var out []any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode array %s: %v (%s)", rec.Body.String(), err, path)
	}
	return out
}

// apiArray 请求一个返回列表的端点并断言 200。
func apiArray(s *testServer, path string) []any {
	s.t.Helper()
	return apiDecodeArray(s.t, path, s.do(http.MethodGet, path, nil))
}

// apiWantStatus 断言状态码并返回响应体字符串。
func apiWantStatus(t *testing.T, method, path string, rec *httptest.ResponseRecorder, want int) string {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("%s %s => %d, want %d: %s", method, path, rec.Code, want, rec.Body.String())
	}
	return rec.Body.String()
}

// apiWantError 断言状态码并检查 error 文案。
func apiWantError(t *testing.T, method, path string, rec *httptest.ResponseRecorder, want int, wantMsg string) {
	t.Helper()
	body := apiWantStatus(t, method, path, rec, want)
	if !strings.Contains(body, wantMsg) {
		t.Fatalf("%s %s body=%q, want error containing %q", method, path, body, wantMsg)
	}
}

// apiErrBody 取出 {"error":"..."} 里的文案。
func apiErrBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	m := decodeMap(t, rec)
	s, _ := m["error"].(string)
	return s
}

// apiTokenServer 构造带 Token 的 API（鉴权必须在 api.New 之前写进 config）。
func apiTokenServer(t *testing.T, token string) *API {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir: dir,
		DBPath:  filepath.Join(dir, "qiansi.db"),
		Uploads: filepath.Join(dir, "uploads"),
		Backups: filepath.Join(dir, "backups"),
		Token:   token,
	}
	return New(store.New(testdb.New(t)), cfg)
}

func apiCall(a *API, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	return rec
}

// apiClosedDBServer 返回一个底层 DB 已关闭的服务器，
// 用于覆盖各处理器的 500 分支（store 返回 "sql: database is closed"）。
func apiClosedDBServer(t *testing.T) *testServer {
	t.Helper()
	s := newTestServer(t)
	if err := s.Store.DB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	return s
}

// ===== api.go =====

func TestAPIAuthHealth(t *testing.T) {
	s := newTestServer(t)
	rec := s.do(http.MethodGet, "/api/v1/health", nil)
	apiWantStatus(t, http.MethodGet, "/api/v1/health", rec, http.StatusOK)
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	m := decodeMap(t, rec)
	if m["status"] != "ok" {
		t.Fatalf("health body = %v, want status ok", m)
	}
}

func TestAPIAuthGuard(t *testing.T) {
	a := apiTokenServer(t, "secret")

	cases := []struct {
		name    string
		method  string
		path    string
		headers map[string]string
		want    int
	}{
		{"api 无 token", http.MethodGet, "/api/v1/health", nil, http.StatusUnauthorized},
		{"api 错误 token", http.MethodGet, "/api/v1/health", map[string]string{"Authorization": "Bearer nope"}, http.StatusUnauthorized},
		{"api 空 Bearer", http.MethodGet, "/api/v1/health", map[string]string{"Authorization": "Bearer "}, http.StatusUnauthorized},
		{"api Bearer 正确", http.MethodGet, "/api/v1/health", map[string]string{"Authorization": "Bearer secret"}, http.StatusOK},
		{"api X-Qiansi-Token 正确", http.MethodGet, "/api/v1/health", map[string]string{"X-Qiansi-Token": "secret"}, http.StatusOK},
		{"api 空 Bearer 回落 header", http.MethodGet, "/api/v1/health", map[string]string{"Authorization": "Bearer ", "X-Qiansi-Token": "secret"}, http.StatusOK},
		{"uploads 无 token", http.MethodGet, "/uploads/a.png", nil, http.StatusUnauthorized},
		{"uploads 有 token 但路由不存在", http.MethodGet, "/uploads/a.png", map[string]string{"X-Qiansi-Token": "secret"}, http.StatusNotFound},
		{"静态资源不校验", http.MethodGet, "/", nil, http.StatusNotFound},
		{"静态资源带错 token 也放行", http.MethodGet, "/index.html", map[string]string{"Authorization": "Bearer wrong"}, http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := apiCall(a, c.method, c.path, c.headers)
			if rec.Code != c.want {
				t.Fatalf("%s %s => %d, want %d: %s", c.method, c.path, rec.Code, c.want, rec.Body.String())
			}
			if c.want == http.StatusUnauthorized {
				if got := apiErrBody(t, rec); got != "unauthorized" {
					t.Fatalf("error = %q, want unauthorized", got)
				}
			}
		})
	}

	// 未配置 Token 时完全放行
	open := apiTokenServer(t, "")
	if rec := apiCall(open, http.MethodGet, "/api/v1/health", nil); rec.Code != http.StatusOK {
		t.Fatalf("token 为空时应放行, got %d", rec.Code)
	}
}

func TestAPIRecoverer(t *testing.T) {
	a := apiTokenServer(t, "")

	panicHandler := a.recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	panicHandler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/whatever", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic => %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "internal error") {
		t.Fatalf("panic body = %q", rec.Body.String())
	}

	okHandler := a.recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	rec2 := httptest.NewRecorder()
	okHandler.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/api/v1/whatever", nil))
	if rec2.Code != http.StatusCreated {
		t.Fatalf("no panic => %d, want 201", rec2.Code)
	}
}

func TestWriteJSONAndErrShape(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusAccepted, map[string]any{"a": 1})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}
	if got := apiErrBody(t, rec); got != "" {
		t.Fatalf("writeJSON body should have no error, got %q", got)
	}

	rec2 := httptest.NewRecorder()
	writeErr(rec2, http.StatusBadRequest, "坏输入")
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec2.Code)
	}
	if got := apiErrBody(t, rec2); got != "坏输入" {
		t.Fatalf("error = %q", got)
	}

	// decode：合法与非法 JSON
	var v map[string]any
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"k":"v"}`))
	if err := decode(req, &v); err != nil {
		t.Fatalf("decode valid: %v", err)
	}
	if v["k"] != "v" {
		t.Fatalf("decoded = %v", v)
	}
	var out store.Category
	bad := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{oops`))
	if err := decode(bad, &out); err == nil {
		t.Fatal("decode invalid JSON should fail")
	}
}

func TestParseIntQueryViaHandler(t *testing.T) {
	s := newTestServer(t)
	apiCreatePersonMap(t, s, map[string]any{"name": "甲"})
	apiCreatePersonMap(t, s, map[string]any{"name": "乙"})

	// 默认值
	if got := apiArray(s, "/api/v1/people/"); len(got) != 2 {
		t.Fatalf("默认 limit 下应有 2 条, got %d", len(got))
	}
	// 非法值回落默认（limit=abc、offset= 空串）
	for _, q := range []string{"?limit=abc", "?limit=", "?offset=xyz&limit=abc", "?limit=-1"} {
		rec := s.do(http.MethodGet, "/api/v1/people/"+q, nil)
		apiWantStatus(t, http.MethodGet, "/api/v1/people/"+q, rec, http.StatusOK)
		if got := len(apiDecodeArray(t, q, rec)); got > 2 {
			t.Fatalf("%s => %d 条, 超出数据量", q, got)
		}
	}
	// 合法值生效
	if got := apiArray(s, "/api/v1/people/?limit=1"); len(got) != 1 {
		t.Fatalf("limit=1 => %d 条", len(got))
	}
	if got := apiArray(s, "/api/v1/people/?limit=1&offset=1"); len(got) != 1 {
		t.Fatalf("limit=1&offset=1 => %d 条", len(got))
	}
	if got := apiArray(s, "/api/v1/people/?limit=0"); len(got) != 2 {
		t.Fatalf("limit=0 应回落到默认 50, got %d", len(got))
	}
}

func TestAPIIsNoRows(t *testing.T) {
	if !isNoRows(sql.ErrNoRows) {
		t.Fatal("isNoRows(sql.ErrNoRows) = false")
	}
	if isNoRows(errors.New("other")) {
		t.Fatal("isNoRows(other) = true")
	}
	if isNoRows(nil) {
		t.Fatal("isNoRows(nil) = true")
	}
}

// ===== settings.go =====

func TestAPISettingsRoundTrip(t *testing.T) {
	s := newTestServer(t)

	if got := s.get("/api/v1/settings/"); len(got) != 0 {
		t.Fatalf("初始 settings = %v, want empty", got)
	}

	rec := s.do(http.MethodPut, "/api/v1/settings/theme", map[string]any{"value": "dark"})
	apiWantStatus(t, http.MethodPut, "/api/v1/settings/theme", rec, http.StatusOK)
	m := decodeMap(t, rec)
	if m["key"] != "theme" || m["value"] != "dark" {
		t.Fatalf("put 响应 = %v", m)
	}
	if got := s.get("/api/v1/settings/")["theme"]; got != "dark" {
		t.Fatalf("settings 未持久化, got %v", got)
	}

	// 覆盖写（upsert 分支）
	s.do(http.MethodPut, "/api/v1/settings/theme", map[string]any{"value": "light"})
	if got := s.get("/api/v1/settings/")["theme"]; got != "light" {
		t.Fatalf("覆盖后 = %v", got)
	}

	// 空值也允许
	rec = s.do(http.MethodPut, "/api/v1/settings/empty_key", map[string]any{"value": ""})
	apiWantStatus(t, http.MethodPut, "/api/v1/settings/empty_key", rec, http.StatusOK)
	if got := s.get("/api/v1/settings/"); got["empty_key"] != "" {
		t.Fatalf("空值未落库: %v", got)
	}

	// bulk
	rec = s.do(http.MethodPost, "/api/v1/settings/bulk", map[string]string{"push_time_hour": "8", "digest": "on"})
	apiWantStatus(t, http.MethodPost, "/api/v1/settings/bulk", rec, http.StatusOK)
	if decodeMap(t, rec)["ok"] != "1" {
		t.Fatalf("bulk 响应 = %s", rec.Body.String())
	}
	all := s.get("/api/v1/settings/")
	if all["push_time_hour"] != "8" || all["digest"] != "on" {
		t.Fatalf("bulk 未持久化: %v", all)
	}

	// 非法 body
	rec = s.raw(http.MethodPut, "/api/v1/settings/x", []byte(`{bad`), "application/json")
	apiWantStatus(t, http.MethodPut, "/api/v1/settings/x", rec, http.StatusBadRequest)
	rec = s.raw(http.MethodPost, "/api/v1/settings/bulk", []byte(`"not an object"`), "application/json")
	apiWantStatus(t, http.MethodPost, "/api/v1/settings/bulk", rec, http.StatusBadRequest)
	rec = s.raw(http.MethodPut, "/api/v1/settings/x", []byte(`{"value":1}`), "application/json")
	apiWantStatus(t, http.MethodPut, "/api/v1/settings/x", rec, http.StatusBadRequest)
}

func TestAPISettingsCategoriesCRUD(t *testing.T) {
	s := newTestServer(t)

	rec := s.do(http.MethodPost, "/api/v1/categories/", map[string]any{
		"name": "家人", "color": "#ff0000", "icon": "home", "sort_order": 2,
	})
	apiWantStatus(t, http.MethodPost, "/api/v1/categories/", rec, http.StatusOK)
	created := decodeMap(t, rec)
	id := int(created["id"].(float64))
	if id <= 0 {
		t.Fatalf("category id 未回填: %v", created)
	}
	if created["name"] != "家人" || created["color"] != "#ff0000" || created["icon"] != "home" {
		t.Fatalf("create 响应 = %v", created)
	}

	list := apiArray(s, "/api/v1/categories/")
	if len(list) != 1 {
		t.Fatalf("list = %v", list)
	}

	// 更新（走 UPDATE 分支）
	rec = s.do(http.MethodPut, "/api/v1/categories/"+itoa(id), map[string]any{
		"id": id, "name": "亲人", "color": "#00ff00", "icon": "heart", "sort_order": 1,
	})
	apiWantStatus(t, http.MethodPut, "category update", rec, http.StatusOK)
	after := apiArray(s, "/api/v1/categories/")
	if len(after) != 1 {
		t.Fatalf("update 后数量 = %d", len(after))
	}
	c0 := after[0].(map[string]any)
	if c0["name"] != "亲人" || c0["color"] != "#00ff00" || c0["icon"] != "heart" || int(c0["sort_order"].(float64)) != 1 {
		t.Fatalf("update 未持久化: %v", c0)
	}

	// 挂到人身上后删除圈子，成员的圈子应被级联清掉
	rec = s.do(http.MethodPost, "/api/v1/people/", map[string]any{"name": "小明", "category_ids": []any{id}})
	apiWantStatus(t, http.MethodPost, "/api/v1/people/", rec, http.StatusOK)
	person := decodeMap(t, rec)
	personID := person["id"].(string)
	if personID == "" {
		t.Fatalf("person 未返回 id: %v", person)
	}
	detail := s.get("/api/v1/people/" + personID)
	if cats, _ := detail["person"].(map[string]any)["categories"].([]any); len(cats) != 1 || cats[0].(map[string]any)["name"] != "亲人" {
		t.Fatalf("person 未带圈子: %v", detail["person"])
	}

	rec = s.do(http.MethodDelete, "/api/v1/categories/"+itoa(id), nil)
	apiWantStatus(t, http.MethodDelete, "category delete", rec, http.StatusNoContent)
	if got := apiArray(s, "/api/v1/categories/"); len(got) != 0 {
		t.Fatalf("删除后 = %v", got)
	}
	getRec := s.do(http.MethodGet, "/api/v1/people/"+personID, nil)
	apiWantStatus(t, http.MethodGet, "person get", getRec, http.StatusOK)
	if cats := decodeMap(t, getRec)["person"].(map[string]any)["categories"]; cats != nil {
		t.Fatalf("删除圈子后 categories 应清空, got %v", cats)
	}

	// 非法 body / 未知 id
	apiWantStatus(t, http.MethodPost, "category create", s.raw(http.MethodPost, "/api/v1/categories/", []byte("[1,2]"), "application/json"), http.StatusBadRequest)
	apiWantStatus(t, http.MethodPut, "category update", s.raw(http.MethodPut, "/api/v1/categories/1", []byte("nope"), "application/json"), http.StatusBadRequest)
	apiWantStatus(t, http.MethodDelete, "category delete unknown", s.do(http.MethodDelete, "/api/v1/categories/999999", nil), http.StatusNoContent)
	apiWantStatus(t, http.MethodDelete, "category delete non-numeric", s.do(http.MethodDelete, "/api/v1/categories/abc", nil), http.StatusNoContent)
	apiWantStatus(t, http.MethodPut, "category update non-numeric", s.do(http.MethodPut, "/api/v1/categories/abc", map[string]any{"name": "x"}), http.StatusOK)
}

func TestAPISettingsTagsCRUD(t *testing.T) {
	s := newTestServer(t)

	rec := s.do(http.MethodPost, "/api/v1/tags/", map[string]any{"name": "同学", "color": "#111111"})
	apiWantStatus(t, http.MethodPost, "/api/v1/tags/", rec, http.StatusOK)
	tag := decodeMap(t, rec)
	id := int(tag["id"].(float64))
	if id <= 0 || tag["name"] != "同学" {
		t.Fatalf("tag = %v", tag)
	}

	// 重名触发 UNIQUE => 500
	dup := s.do(http.MethodPost, "/api/v1/tags/", map[string]any{"name": "同学"})
	apiWantStatus(t, http.MethodPost, "duplicate tag", dup, http.StatusInternalServerError)
	if !strings.Contains(dup.Body.String(), "UNIQUE") {
		t.Fatalf("dup body = %s", dup.Body.String())
	}

	s.do(http.MethodPost, "/api/v1/tags/", map[string]any{"name": "同事"})

	// 更新
	rec = s.do(http.MethodPut, "/api/v1/tags/"+itoa(id), map[string]any{"id": id, "name": "老同学", "color": "#222222"})
	apiWantStatus(t, http.MethodPut, "tag update", rec, http.StatusOK)
	list := apiArray(s, "/api/v1/tags/")
	if len(list) != 2 {
		t.Fatalf("tags = %v", list)
	}
	// tags 按名字排序：老同学 < 同事（按字节），检查更新生效即可
	found := false
	for _, it := range list {
		m := it.(map[string]any)
		if m["name"] == "老同学" && m["color"] == "#222222" {
			found = true
		}
	}
	if !found {
		t.Fatalf("tag 更新未持久化: %v", list)
	}

	// 更新成已存在的名字 => 500
	apiWantStatus(t, http.MethodPut, "tag rename dup", s.do(http.MethodPut, "/api/v1/tags/"+itoa(id), map[string]any{"id": id, "name": "同事"}), http.StatusInternalServerError)

	apiWantStatus(t, http.MethodPost, "tag create bad json", s.raw(http.MethodPost, "/api/v1/tags/", []byte("{x"), "application/json"), http.StatusBadRequest)
	apiWantStatus(t, http.MethodPut, "tag update bad json", s.raw(http.MethodPut, "/api/v1/tags/1", []byte("{x"), "application/json"), http.StatusBadRequest)
	apiWantStatus(t, http.MethodPut, "tag update non-numeric", s.do(http.MethodPut, "/api/v1/tags/abc", map[string]any{"name": "z"}), http.StatusOK)
	apiWantStatus(t, http.MethodDelete, "tag delete unknown", s.do(http.MethodDelete, "/api/v1/tags/999999", nil), http.StatusNoContent)
	apiWantStatus(t, http.MethodDelete, "tag delete non-numeric", s.do(http.MethodDelete, "/api/v1/tags/xyz", nil), http.StatusNoContent)

	// 删除带打标的标签：taggings 应级联清理
	personID := apiCreatePersonMap(t, s, map[string]any{"name": "阿强"})["id"].(string)
	apiWantStatus(t, http.MethodPost, "tag add", s.do(http.MethodPost, "/api/v1/taggings/add", map[string]any{
		"target_type": "person", "target_id": personID, "tag_id": id,
	}), http.StatusNoContent)
	apiWantStatus(t, http.MethodDelete, "tag delete", s.do(http.MethodDelete, "/api/v1/tags/"+itoa(id), nil), http.StatusNoContent)
	if got := apiArray(s, "/api/v1/taggings/of?target_type=person&target_id="+personID); len(got) != 0 {
		t.Fatalf("删除标签后 taggings 未清理: %v", got)
	}
}

func TestAPISettingsEventTypesCRUD(t *testing.T) {
	s := newTestServer(t)

	rec := s.do(http.MethodPost, "/api/v1/event-types/", map[string]any{
		"name": "聚餐", "color": "#3366ff", "icon": "utensils", "is_default": true, "sort_order": 3,
	})
	apiWantStatus(t, http.MethodPost, "/api/v1/event-types/", rec, http.StatusOK)
	et := decodeMap(t, rec)
	id := int(et["id"].(float64))
	if id <= 0 || et["name"] != "聚餐" || et["is_default"] != true {
		t.Fatalf("event type = %v", et)
	}

	list := apiArray(s, "/api/v1/event-types/")
	if len(list) != 1 {
		t.Fatalf("list = %v", list)
	}

	rec = s.do(http.MethodPut, "/api/v1/event-types/"+itoa(id), map[string]any{
		"id": id, "name": "饭局", "color": "#00cc99", "icon": "bowl", "is_default": false, "sort_order": 4,
	})
	apiWantStatus(t, http.MethodPut, "event type update", rec, http.StatusOK)
	after := apiArray(s, "/api/v1/event-types/")[0].(map[string]any)
	if after["name"] != "饭局" || after["color"] != "#00cc99" || after["icon"] != "bowl" || after["is_default"] != false {
		t.Fatalf("update 未持久化: %v", after)
	}
	if int(after["sort_order"].(float64)) != 4 {
		t.Fatalf("sort_order = %v", after["sort_order"])
	}

	// 事件引用该类型后删除，事件的 type_id 置空
	_, err := s.Store.DB.Exec("INSERT INTO events(id,title,type_id,event_date,created_at,updated_at) VALUES('e1','饭',?,'2026-01-02','2026-01-02','2026-01-02')", id)
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}
	apiWantStatus(t, http.MethodDelete, "event type delete", s.do(http.MethodDelete, "/api/v1/event-types/"+itoa(id), nil), http.StatusNoContent)
	if got := apiArray(s, "/api/v1/event-types/"); len(got) != 0 {
		t.Fatalf("删除后 = %v", got)
	}
	var typeID any
	if err := s.Store.DB.QueryRow("SELECT type_id FROM events WHERE id='e1'").Scan(&typeID); err != nil {
		t.Fatalf("query event: %v", err)
	}
	if typeID != nil {
		t.Fatalf("删除类型后 type_id 应置空, got %v", typeID)
	}

	apiWantStatus(t, http.MethodPost, "bad json", s.raw(http.MethodPost, "/api/v1/event-types/", []byte("]"), "application/json"), http.StatusBadRequest)
	apiWantStatus(t, http.MethodPut, "bad json", s.raw(http.MethodPut, "/api/v1/event-types/1", []byte("]"), "application/json"), http.StatusBadRequest)
	apiWantStatus(t, http.MethodPut, "non-numeric id", s.do(http.MethodPut, "/api/v1/event-types/abc", map[string]any{"name": "x"}), http.StatusOK)
	apiWantStatus(t, http.MethodDelete, "unknown id", s.do(http.MethodDelete, "/api/v1/event-types/999999", nil), http.StatusNoContent)
	apiWantStatus(t, http.MethodDelete, "non-numeric delete", s.do(http.MethodDelete, "/api/v1/event-types/zz", nil), http.StatusNoContent)
}

func TestAPISettingsTaggings(t *testing.T) {
	s := newTestServer(t)

	tagID := int(decodeMap(t, s.do(http.MethodPost, "/api/v1/tags/", map[string]any{"name": "球友"}))["id"].(float64))
	personID := apiCreatePersonMap(t, s, map[string]any{"name": "大刘"})["id"].(string)

	addBody := map[string]any{"target_type": "person", "target_id": personID, "tag_id": tagID}
	apiWantStatus(t, http.MethodPost, "taggings/add", s.do(http.MethodPost, "/api/v1/taggings/add", addBody), http.StatusNoContent)
	// 重复添加走 INSERT OR IGNORE
	apiWantStatus(t, http.MethodPost, "taggings/add again", s.do(http.MethodPost, "/api/v1/taggings/add", addBody), http.StatusNoContent)

	ofPath := "/api/v1/taggings/of?target_type=person&target_id=" + personID
	got := apiArray(s, ofPath)
	if len(got) != 1 {
		t.Fatalf("tags of = %v", got)
	}
	if got[0].(map[string]any)["name"] != "球友" {
		t.Fatalf("tag 名称不符: %v", got)
	}

	apiWantStatus(t, http.MethodPost, "taggings/remove", s.do(http.MethodPost, "/api/v1/taggings/remove", addBody), http.StatusNoContent)
	if got := apiArray(s, ofPath); len(got) != 0 {
		t.Fatalf("remove 后 = %v", got)
	}
	// 再删一次不报错
	apiWantStatus(t, http.MethodPost, "taggings/remove again", s.do(http.MethodPost, "/api/v1/taggings/remove", addBody), http.StatusNoContent)
	// 未知目标
	if got := apiArray(s, "/api/v1/taggings/of?target_type=memo&target_id=none"); len(got) != 0 {
		t.Fatalf("未知目标 = %v", got)
	}
	if got := apiArray(s, "/api/v1/taggings/of"); len(got) != 0 {
		t.Fatalf("无参数 = %v", got)
	}

	apiWantStatus(t, http.MethodPost, "add bad json", s.raw(http.MethodPost, "/api/v1/taggings/add", []byte("<xml"), "application/json"), http.StatusBadRequest)
	apiWantStatus(t, http.MethodPost, "remove bad json", s.raw(http.MethodPost, "/api/v1/taggings/remove", []byte("<xml"), "application/json"), http.StatusBadRequest)
	// 外键不存在的 tag_id => 500
	apiWantStatus(t, http.MethodPost, "add unknown tag", s.do(http.MethodPost, "/api/v1/taggings/add", map[string]any{
		"target_type": "person", "target_id": personID, "tag_id": 424242,
	}), http.StatusInternalServerError)
}

func TestAPISettingsDBErrors(t *testing.T) {
	s := apiClosedDBServer(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"settings all", http.MethodGet, "/api/v1/settings/", nil},
		{"settings set", http.MethodPut, "/api/v1/settings/k", map[string]any{"value": "v"}},
		{"settings bulk", http.MethodPost, "/api/v1/settings/bulk", map[string]string{"a": "b"}},
		{"category list", http.MethodGet, "/api/v1/categories/", nil},
		{"category create", http.MethodPost, "/api/v1/categories/", map[string]any{"name": "x"}},
		{"category update", http.MethodPut, "/api/v1/categories/1", map[string]any{"name": "x"}},
		{"category delete", http.MethodDelete, "/api/v1/categories/1", nil},
		{"tag list", http.MethodGet, "/api/v1/tags/", nil},
		{"tag create", http.MethodPost, "/api/v1/tags/", map[string]any{"name": "x"}},
		{"tag update", http.MethodPut, "/api/v1/tags/1", map[string]any{"name": "x"}},
		{"tag delete", http.MethodDelete, "/api/v1/tags/1", nil},
		{"event type list", http.MethodGet, "/api/v1/event-types/", nil},
		{"event type create", http.MethodPost, "/api/v1/event-types/", map[string]any{"name": "x"}},
		{"event type update", http.MethodPut, "/api/v1/event-types/1", map[string]any{"name": "x"}},
		{"event type delete", http.MethodDelete, "/api/v1/event-types/1", nil},
		{"tagging add", http.MethodPost, "/api/v1/taggings/add", map[string]any{"target_type": "person", "target_id": "p", "tag_id": 1}},
		{"tagging remove", http.MethodPost, "/api/v1/taggings/remove", map[string]any{"target_type": "person", "target_id": "p", "tag_id": 1}},
		{"tagging of", http.MethodGet, "/api/v1/taggings/of?target_type=person&target_id=p", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := s.do(c.method, c.path, c.body)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("%s %s => %d, want 500: %s", c.method, c.path, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "closed") {
				t.Fatalf("body = %s", rec.Body.String())
			}
		})
	}
}
