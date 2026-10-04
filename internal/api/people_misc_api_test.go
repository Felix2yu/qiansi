package api

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qiansi/app/internal/store"
)

// ===== people 测试辅助 =====

// apiCreatePersonMap 建一个联系人并返回响应体。
func apiCreatePersonMap(t *testing.T, s *testServer, body map[string]any) map[string]any {
	t.Helper()
	rec := s.do(http.MethodPost, "/api/v1/people/", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/people/ %v => %d: %s", body, rec.Code, rec.Body.String())
	}
	m := decodeMap(t, rec)
	if id, ok := m["id"].(string); !ok || id == "" {
		t.Fatalf("create person 未返回 id: %v", m)
	}
	return m
}

// apiNames 把联系人列表转成 name 集合，便于断言过滤结果。
func apiNames(t *testing.T, list []any) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, it := range list {
		m, ok := it.(map[string]any)
		if !ok {
			t.Fatalf("列表元素不是对象: %v", it)
		}
		name, _ := m["name"].(string)
		out[name] = true
	}
	return out
}

// relById 从关系列表里按 id 取一条，取不到直接失败。
func relById(t *testing.T, list []any, id string) map[string]any {
	t.Helper()
	for _, it := range list {
		if m, ok := it.(map[string]any); ok && m["id"] == id {
			return m
		}
	}
	t.Fatalf("关系列表里没有 id=%s: %v", id, list)
	return nil
}

// apiResultsByType 把 /api/v1/search 或建议列表按 type 归类。
func apiResultsByType(list []any) map[string]int {
	out := map[string]int{}
	for _, it := range list {
		if m, ok := it.(map[string]any); ok {
			tp, _ := m["type"].(string)
			out[tp]++
		}
	}
	return out
}

func apiDate(offsetDays int) string {
	return time.Now().AddDate(0, 0, offsetDays).Format("2006-01-02")
}

// ===== people.go: 创建 =====

func TestAPIPeopleCreateNameFallbacks(t *testing.T) {
	s := newTestServer(t)

	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{"中文姓+名", map[string]any{"family_name": "张", "given_name": "三"}, "张三"},
		{"西文名+姓", map[string]any{"family_name": "Li", "given_name": "Wei"}, "Wei Li"},
		{"只有姓", map[string]any{"family_name": "王"}, "王"},
		{"只有名", map[string]any{"given_name": "Mary"}, "Mary"},
		{"显式 name 优先", map[string]any{"name": "老王", "family_name": "王"}, "老王"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := apiCreatePersonMap(t, s, c.body)
			if got["name"] != c.want {
				t.Fatalf("create 返回 name = %v, want %q", got["name"], c.want)
			}
			detail := s.get("/api/v1/people/" + got["id"].(string))
			if p := detail["person"].(map[string]any); p["name"] != c.want {
				t.Fatalf("落库 name = %v, want %q", p["name"], c.want)
			}
		})
	}

	// 默认 grade=0（未设置亲密度），显式 grade 生效
	def := apiCreatePersonMap(t, s, map[string]any{"name": "默认等级"})
	if int(def["grade"].(float64)) != 0 {
		t.Fatalf("默认 grade = %v", def["grade"])
	}
	custom := apiCreatePersonMap(t, s, map[string]any{"name": "高等级", "grade": 5})
	if int(custom["grade"].(float64)) != 5 {
		t.Fatalf("grade = %v, want 5", custom["grade"])
	}
	if custom["created_at"] == "" || custom["updated_at"] == "" {
		t.Fatalf("审计字段缺失: %v", custom)
	}
}

func TestAPIPeopleCreateValidation(t *testing.T) {
	s := newTestServer(t)

	apiWantError(t, http.MethodPost, "/api/v1/people/", s.do(http.MethodPost, "/api/v1/people/", map[string]any{}), http.StatusBadRequest, "name required")
	apiWantError(t, http.MethodPost, "/api/v1/people/", s.do(http.MethodPost, "/api/v1/people/", map[string]any{
		"name": "", "family_name": "", "given_name": "",
	}), http.StatusBadRequest, "name required")
	apiWantStatus(t, http.MethodPost, "/api/v1/people/", s.raw(http.MethodPost, "/api/v1/people/", []byte("{oops"), "application/json"), http.StatusBadRequest)

	// 生日应自动生成纪念日
	got := apiCreatePersonMap(t, s, map[string]any{"name": "生日人", "birthday": "1991-03-04", "nickname": "小年"})
	if annivID, _ := got["birthday_anniversary_id"].(string); annivID == "" {
		t.Fatalf("未回写 birthday_anniversary_id: %v", got)
	}
	var count int
	if err := s.Store.DB.QueryRow("SELECT COUNT(*) FROM anniversaries WHERE person_id=? AND source='birthday' AND date='1991-03-04'", got["id"]).Scan(&count); err != nil {
		t.Fatalf("query anniversaries: %v", err)
	}
	if count != 1 {
		t.Fatalf("生日纪念日数量 = %d, want 1", count)
	}

	// 再建一条不带生日的，确保不会误生成
	other := apiCreatePersonMap(t, s, map[string]any{"name": "无生日"})
	var c2 int
	if err := s.Store.DB.QueryRow("SELECT COUNT(*) FROM anniversaries WHERE person_id=?", other["id"]).Scan(&c2); err != nil {
		t.Fatalf("query: %v", err)
	}
	if c2 != 0 {
		t.Fatalf("无生日却建了纪念日: %d", c2)
	}
}

// ===== people.go: 读取 / 更新 / 删除 =====

func TestAPIPeopleGetUpdateDelete(t *testing.T) {
	s := newTestServer(t)

	created := apiCreatePersonMap(t, s, map[string]any{
		"name": "林静", "gender": "女", "phone": "13500000001", "wechat": "linjing",
		"location": "杭州", "notes": "大学室友", "grade": 4,
	})
	id := created["id"].(string)

	detail := s.get("/api/v1/people/" + id)
	p := detail["person"].(map[string]any)
	for k, want := range map[string]any{
		"name": "林静", "gender": "女", "phone": "13500000001", "wechat": "linjing",
		"location": "杭州", "notes": "大学室友",
	} {
		if p[k] != want {
			t.Fatalf("person[%s] = %v, want %v", k, p[k], want)
		}
	}
	if int(p["grade"].(float64)) != 4 {
		t.Fatalf("grade = %v", p["grade"])
	}
	if _, ok := detail["fields"].([]any); !ok {
		t.Fatalf("detail.fields 不是数组: %v", detail["fields"])
	}

	// 未知 id => 404
	rec := s.do(http.MethodGet, "/api/v1/people/no-such-id", nil)
	apiWantStatus(t, http.MethodGet, "people get unknown", rec, http.StatusNotFound)
	if apiErrBody(t, rec) == "" {
		t.Fatal("404 应带 error 文案")
	}

	// 更新
	rec = s.do(http.MethodPut, "/api/v1/people/"+id, map[string]any{
		"id": id, "name": "林静怡", "grade": 5, "notes": "改过的备注",
	})
	apiWantStatus(t, http.MethodPut, "people update", rec, http.StatusOK)
	p2 := s.get("/api/v1/people/" + id)["person"].(map[string]any)
	if p2["name"] != "林静怡" || p2["notes"] != "改过的备注" || int(p2["grade"].(float64)) != 5 {
		t.Fatalf("update 未持久化: %v", p2)
	}

	// 更新时按姓/名回退拼显示名
	s.do(http.MethodPut, "/api/v1/people/"+id, map[string]any{"family_name": "陈", "given_name": "小明"})
	if got := s.get("/api/v1/people/" + id)["person"].(map[string]any)["name"]; got != "陈小明" {
		t.Fatalf("name = %v, want 陈小明", got)
	}

	// 更新校验
	apiWantError(t, http.MethodPut, "people update", s.do(http.MethodPut, "/api/v1/people/"+id, map[string]any{"name": ""}), http.StatusBadRequest, "name required")
	apiWantStatus(t, http.MethodPut, "people update", s.raw(http.MethodPut, "/api/v1/people/"+id, []byte("bad"), "application/json"), http.StatusBadRequest)
	// 更新不存在的 id：写 0 行不再是「改成功了」，返回 404
	apiWantStatus(t, http.MethodPut, "people update unknown", s.do(http.MethodPut, "/api/v1/people/ghost-id", map[string]any{"name": "幽灵"}), http.StatusNotFound)

	// 清空生日 => 关联的自动纪念日被移除
	birth := apiCreatePersonMap(t, s, map[string]any{"name": "有生日", "birthday": "1988-06-06"})
	bid := birth["id"].(string)
	annivID, _ := birth["birthday_anniversary_id"].(string)
	rec = s.do(http.MethodPut, "/api/v1/people/"+bid, map[string]any{
		"name": "有生日", "birthday": "", "birthday_anniversary_id": annivID,
	})
	apiWantStatus(t, http.MethodPut, "clear birthday", rec, http.StatusOK)
	var left int
	if err := s.Store.DB.QueryRow("SELECT COUNT(*) FROM anniversaries WHERE id=?", annivID).Scan(&left); err != nil {
		t.Fatalf("query: %v", err)
	}
	if left != 0 {
		t.Fatalf("清空生日后纪念日仍在: %d", left)
	}
	if got := s.get("/api/v1/people/" + bid)["person"].(map[string]any)["birthday_anniversary_id"]; got != nil {
		t.Fatalf("birthday_anniversary_id 应清空, got %v", got)
	}

	// 删除
	rec = s.do(http.MethodDelete, "/api/v1/people/"+id, nil)
	apiWantStatus(t, http.MethodDelete, "people delete", rec, http.StatusNoContent)
	apiWantStatus(t, http.MethodGet, "people get after delete", s.do(http.MethodGet, "/api/v1/people/"+id, nil), http.StatusNotFound)
	// 删除未知 id => 404
	apiWantStatus(t, http.MethodDelete, "people delete unknown", s.do(http.MethodDelete, "/api/v1/people/ghost", nil), http.StatusNotFound)
}

func TestAPIPeopleListFilters(t *testing.T) {
	s := newTestServer(t)

	catID := int(decodeMap(t, s.do(http.MethodPost, "/api/v1/categories/", map[string]any{"name": "家人"}))["id"].(float64))
	tagID := int(decodeMap(t, s.do(http.MethodPost, "/api/v1/tags/", map[string]any{"name": "高中"}))["id"].(float64))

	apiCreatePersonMap(t, s, map[string]any{"name": "赵敏", "grade": 5, "category_ids": []any{catID}, "notes": "周芷若的对手"})
	b := apiCreatePersonMap(t, s, map[string]any{"name": "张无忌", "grade": 2})
	c := apiCreatePersonMap(t, s, map[string]any{"name": "小昭", "grade": 2})
	cid := c["id"].(string)

	apiWantStatus(t, http.MethodPost, "taggings/add", s.do(http.MethodPost, "/api/v1/taggings/add", map[string]any{
		"target_type": "person", "target_id": cid, "tag_id": tagID,
	}), http.StatusNoContent)

	if got := apiArray(s, "/api/v1/people/"); len(got) != 3 {
		t.Fatalf("默认列表 = %v", got)
	}

	cases := []struct {
		path string
		want []string
		miss []string
	}{
		{"/api/v1/people/?q=赵", []string{"赵敏"}, []string{"张无忌", "小昭"}},
		{"/api/v1/people/?q=芷若", []string{"赵敏"}, []string{"张无忌"}},
		{"/api/v1/people/?grade=5", []string{"赵敏"}, []string{"张无忌", "小昭"}},
		{"/api/v1/people/?grade=2", []string{"张无忌", "小昭"}, []string{"赵敏"}},
		{"/api/v1/people/?category_id=" + itoa(catID), []string{"赵敏"}, []string{"小昭"}},
		{"/api/v1/people/?tag_id=" + itoa(tagID), []string{"小昭"}, []string{"赵敏", "张无忌"}},
		{"/api/v1/people/?category_id=999", []string{}, []string{"赵敏", "张无忌", "小昭"}},
		{"/api/v1/people/?q=不存在的人", []string{}, []string{"赵敏", "张无忌", "小昭"}},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			names := apiNames(t, apiArray(s, tc.path))
			for _, n := range tc.want {
				if !names[n] {
					t.Fatalf("%s 缺少 %s: %v", tc.path, n, names)
				}
			}
			for _, n := range tc.miss {
				if names[n] {
					t.Fatalf("%s 不该包含 %s: %v", tc.path, n, names)
				}
			}
		})
	}

	// limit / offset
	if got := apiArray(s, "/api/v1/people/?limit=1"); len(got) != 1 {
		t.Fatalf("limit=1 => %d", len(got))
	}
	all := apiNames(t, apiArray(s, "/api/v1/people/"))
	first := apiNames(t, apiArray(s, "/api/v1/people/?limit=1"))
	offset := apiNames(t, apiArray(s, "/api/v1/people/?limit=2&offset=1"))
	if len(first) != 1 || len(offset) != 2 {
		t.Fatalf("分页结果数量异常: %v / %v", first, offset)
	}
	for n := range offset {
		if !all[n] {
			t.Fatalf("offset 结果出现未知联系人 %s", n)
		}
	}

	// archived：默认不含归档，archived=1 时包含
	archived := b["id"].(string)
	apiWantStatus(t, http.MethodPost, "archive", s.do(http.MethodPost, "/api/v1/people/"+archived+"/archive", nil), http.StatusNoContent)
	if names := apiNames(t, apiArray(s, "/api/v1/people/")); names["张无忌"] {
		t.Fatal("默认列表不应包含归档联系人")
	}
	if names := apiNames(t, apiArray(s, "/api/v1/people/?archived=1")); !names["张无忌"] {
		t.Fatalf("archived=1 应包含归档联系人: %v", names)
	}
}

func TestAPIPeopleCount(t *testing.T) {
	s := newTestServer(t)

	if got := s.get("/api/v1/people/count"); int(got["count"].(float64)) != 0 {
		t.Fatalf("初始 count = %v", got)
	}
	a := apiCreatePersonMap(t, s, map[string]any{"name": "甲"})
	apiCreatePersonMap(t, s, map[string]any{"name": "乙"})
	if got := s.get("/api/v1/people/count"); int(got["count"].(float64)) != 2 {
		t.Fatalf("count = %v, want 2", got)
	}
	// 归档后不计入
	s.do(http.MethodPost, "/api/v1/people/"+a["id"].(string)+"/archive", nil)
	if got := s.get("/api/v1/people/count"); int(got["count"].(float64)) != 1 {
		t.Fatalf("归档后 count = %v, want 1", got)
	}
}

func TestAPIPeopleSearch(t *testing.T) {
	s := newTestServer(t)

	p := apiCreatePersonMap(t, s, map[string]any{"name": "周伯通", "phone": "13900001111", "wechat": "zbt"})
	pid := p["id"].(string)
	ctx := context.Background()

	if err := s.Store.EventCreate(ctx, &store.Event{Title: "和周伯通喝酒", EventDate: apiDate(-1), Summary: "醉拳"}, []string{pid}, nil); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	if err := s.Store.MemoCreate(ctx, &store.Memo{PersonID: pid, Content: "周伯通说要教我空明拳", SaidAt: apiDate(0)}); err != nil {
		t.Fatalf("seed memo: %v", err)
	}
	if err := s.Store.TransactionCreate(ctx, &store.Transaction{
		PersonID: pid, Kind: "loan", Direction: "out", AmountFen: 3000, Title: "借给周伯通", OccurredAt: apiDate(0),
	}); err != nil {
		t.Fatalf("seed transaction: %v", err)
	}
	if _, err := s.Store.DB.ExecContext(ctx,
		"INSERT INTO anniversaries(id,person_id,title,date,created_at) VALUES('ann-1',?,'周伯通寿辰',?,'2026-01-01')", pid, apiDate(1)); err != nil {
		t.Fatalf("seed anniversary: %v", err)
	}

	list := apiArray(s, "/api/v1/search?q=周伯通")
	types := apiResultsByType(list)
	for _, want := range []string{"person", "event", "memo", "transaction", "anniversary"} {
		if types[want] == 0 {
			t.Fatalf("search 未命中 %s: %v", want, types)
		}
	}
	for _, it := range list {
		m := it.(map[string]any)
		if m["path"] == nil || m["path"] == "" {
			t.Fatalf("搜索结果缺 path: %v", m)
		}
	}

	// limit 生效 + 非法值回落
	if got := apiArray(s, "/api/v1/search?q=周伯通&limit=1"); len(got) > 5 {
		t.Fatalf("limit=1 结果过多: %d", len(got))
	}
	apiWantStatus(t, http.MethodGet, "search bad limit", s.do(http.MethodGet, "/api/v1/search?q=周伯通&limit=abc", nil), http.StatusOK)

	// 空关键词 => 空列表
	if got := apiArray(s, "/api/v1/search"); len(got) != 0 {
		t.Fatalf("空 q 应返回空: %v", got)
	}
	// 无命中
	if got := apiArray(s, "/api/v1/search?q=查无此人"); len(got) != 0 {
		t.Fatalf("无命中应返回空: %v", got)
	}
}

func TestAPIPeopleDuplicates(t *testing.T) {
	s := newTestServer(t)

	a := apiCreatePersonMap(t, s, map[string]any{"name": "陈明", "phone": "13800000000", "wechat": "cm1", "nickname": "老陈"})
	b := apiCreatePersonMap(t, s, map[string]any{"name": "陈明"})
	aid := a["id"].(string)

	cases := []struct {
		path string
		want int
	}{
		{"/api/v1/people/duplicates?name=陈明", 2},
		{"/api/v1/people/duplicates?name=老陈", 1},
		{"/api/v1/people/duplicates?phone=13800000000", 1},
		{"/api/v1/people/duplicates?wechat=cm1", 1},
		{"/api/v1/people/duplicates?name=陈明&exclude_id=" + aid, 1},
		{"/api/v1/people/duplicates?name=陈明&phone=13800000000&wechat=cm1", 2},
		{"/api/v1/people/duplicates", 0},
		{"/api/v1/people/duplicates?name=%20%20%20", 0},
		{"/api/v1/people/duplicates?name=陌生人", 0},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			if got := len(apiArray(s, c.path)); got != c.want {
				t.Fatalf("%s => %d, want %d", c.path, got, c.want)
			}
		})
	}
	// exclude_id 精确排除自己
	names := apiNames(t, apiArray(s, "/api/v1/people/duplicates?name=陈明&exclude_id="+aid))
	if names["陈明"] && len(names) != 1 {
		t.Fatalf("exclude 结果异常: %v", names)
	}
	if got := apiArray(s, "/api/v1/people/duplicates?name=陈明&exclude_id="+b["id"].(string)); len(got) != 1 {
		t.Fatalf("exclude b => %v", got)
	}
}

func TestAPIPeopleArchiveUnarchive(t *testing.T) {
	s := newTestServer(t)

	p := apiCreatePersonMap(t, s, map[string]any{"name": "归档对象"})
	pid := p["id"].(string)

	apiWantStatus(t, http.MethodPost, "archive", s.do(http.MethodPost, "/api/v1/people/"+pid+"/archive", nil), http.StatusNoContent)
	if archived := s.get("/api/v1/people/" + pid)["person"].(map[string]any)["archived"]; archived != true {
		t.Fatalf("archive 后 archived = %v", archived)
	}
	apiWantStatus(t, http.MethodDelete, "unarchive", s.do(http.MethodDelete, "/api/v1/people/"+pid+"/archive", nil), http.StatusNoContent)
	if archived := s.get("/api/v1/people/" + pid)["person"].(map[string]any)["archived"]; archived != false {
		t.Fatalf("unarchive 后 archived = %v", archived)
	}
	// 未知 id => 404，不能再让前端以为归档上了
	apiWantStatus(t, http.MethodPost, "archive unknown", s.do(http.MethodPost, "/api/v1/people/ghost/archive", nil), http.StatusNotFound)
	apiWantStatus(t, http.MethodDelete, "unarchive unknown", s.do(http.MethodDelete, "/api/v1/people/ghost/archive", nil), http.StatusNotFound)
}

func TestAPIPeopleListArchivedOnly(t *testing.T) {
	s := newTestServer(t)

	keep := apiCreatePersonMap(t, s, map[string]any{"name": "在册人"})["id"].(string)
	gone := apiCreatePersonMap(t, s, map[string]any{"name": "隐藏人"})["id"].(string)
	if r := s.do(http.MethodPost, "/api/v1/people/"+gone+"/archive", nil); r.Code != http.StatusNoContent {
		t.Fatalf("archive => %d: %s", r.Code, r.Body.String())
	}
	def := apiNames(t, apiArray(s, "/api/v1/people/"))
	if !def["在册人"] {
		t.Fatalf("默认列表缺少未归档联系人 %q (创建返回 id=%s)", "在册人", keep)
	}
	if def["隐藏人"] {
		t.Errorf("默认列表不该出现归档联系人: %v", def)
	}
	// 「已归档」筛选页：只翻被隐藏掉的人，好把他们找回来
	only := apiNames(t, apiArray(s, "/api/v1/people/?archived=only"))
	if !only["隐藏人"] {
		t.Errorf("archived=only 应含归档联系人: %v", only)
	}
	if only["在册人"] {
		t.Errorf("archived=only 混进了未归档的人: %v", only)
	}
	if n := len(only); n != 1 {
		t.Errorf("archived=only 条数 = %d, want 1", n)
	}
}

func TestAPIPeopleMerge(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	target := apiCreatePersonMap(t, s, map[string]any{"name": "合并目标"})["id"].(string)
	source := apiCreatePersonMap(t, s, map[string]any{"name": "合并来源"})["id"].(string)
	third := apiCreatePersonMap(t, s, map[string]any{"name": "第三方"})["id"].(string)

	// source 的各类关联数据
	rel := decodeMap(t, s.do(http.MethodPost, "/api/v1/relationships/", map[string]any{
		"from_person_id": source, "to_person_id": third, "type": "同事",
	}))
	if rel["id"] == nil {
		t.Fatalf("relationship 未创建: %v", rel)
	}
	field := decodeMap(t, s.do(http.MethodPost, "/api/v1/people/"+source+"/fields", map[string]any{
		"label": "公司", "value": "某某所", "sort_order": 1,
	}))
	if fid, _ := field["id"].(string); fid == "" {
		t.Fatalf("自定义字段未创建: %v", field)
	}
	if err := s.Store.EventCreate(ctx, &store.Event{Title: "来源的饭局", EventDate: apiDate(-2)}, []string{source}, nil); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	if err := s.Store.MemoCreate(ctx, &store.Memo{PersonID: source, Content: "来源的备忘", SaidAt: apiDate(-1)}); err != nil {
		t.Fatalf("seed memo: %v", err)
	}
	if err := s.Store.TransactionCreate(ctx, &store.Transaction{
		PersonID: source, Kind: "gift", Direction: "in", AmountFen: 800, Title: "来源的礼金", OccurredAt: apiDate(-1),
	}); err != nil {
		t.Fatalf("seed transaction: %v", err)
	}
	if _, err := s.Store.DB.ExecContext(ctx,
		"INSERT INTO anniversaries(id,person_id,title,date,created_at) VALUES('ann-src',?,'来源纪念日',?,'2026-01-01')", source, apiDate(5)); err != nil {
		t.Fatalf("seed anniversary: %v", err)
	}
	if _, err := s.Store.DB.ExecContext(ctx,
		"INSERT INTO reminders(id,person_id,ref_type,title,due_at,status,created_at) VALUES('rem-src',?,'manual','来源提醒',?,'pending','2026-01-01')", source, apiDate(1)+"T09:00:00"); err != nil {
		t.Fatalf("seed reminder: %v", err)
	}
	tagID := int(decodeMap(t, s.do(http.MethodPost, "/api/v1/tags/", map[string]any{"name": "老同事"}))["id"].(float64))
	apiWantStatus(t, http.MethodPost, "taggings/add", s.do(http.MethodPost, "/api/v1/taggings/add", map[string]any{
		"target_type": "person", "target_id": source, "tag_id": tagID,
	}), http.StatusNoContent)

	rec := s.do(http.MethodPost, "/api/v1/people/"+target+"/merge", map[string]any{"from": source})
	apiWantStatus(t, http.MethodPost, "merge", rec, http.StatusOK)
	m := decodeMap(t, rec)
	if m["ok"] != true || m["merged_into"] != target {
		t.Fatalf("merge 响应 = %v", m)
	}

	// 来源已消失
	apiWantStatus(t, http.MethodGet, "source gone", s.do(http.MethodGet, "/api/v1/people/"+source, nil), http.StatusNotFound)

	// 关系迁到目标
	rels := apiArray(s, "/api/v1/relationships/of/"+target)
	if len(rels) != 1 || rels[0].(map[string]any)["to_person_id"] != third {
		t.Fatalf("目标关系未迁移: %v", rels)
	}
	if got := apiArray(s, "/api/v1/relationships/of/"+source); len(got) != 0 {
		t.Fatalf("来源仍有关系: %v", got)
	}

	// 自定义字段迁移
	fields := apiArray(s, "/api/v1/people/"+target+"/fields")
	if len(fields) != 1 || fields[0].(map[string]any)["label"] != "公司" {
		t.Fatalf("字段未迁移: %v", fields)
	}

	// 时间线（事件 + 备忘 + 交易 + 纪念日都迁到目标）
	types := apiResultsByType(apiArray(s, "/api/v1/people/"+target+"/timeline"))
	for _, want := range []string{"event", "memo", "transaction", "anniversary"} {
		if types[want] == 0 {
			t.Fatalf("timeline 缺少 %s: %v", want, types)
		}
	}

	// 标签迁移
	tags := apiArray(s, "/api/v1/taggings/of?target_type=person&target_id="+target)
	if len(tags) != 1 || tags[0].(map[string]any)["name"] != "老同事" {
		t.Fatalf("标签未迁移: %v", tags)
	}

	// 备忘 / 交易 / 纪念日 / 提醒 的 person_id 改写
	for table, col := range map[string]string{"memos": "person_id", "transactions": "person_id", "anniversaries": "person_id", "reminders": "person_id"} {
		var n int
		if err := s.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE "+col+"=?", target).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n == 0 {
			t.Fatalf("%s 未迁移到目标", table)
		}
	}

	// 合并入参校验
	apiWantError(t, http.MethodPost, "merge", s.do(http.MethodPost, "/api/v1/people/"+target+"/merge", map[string]any{}), http.StatusBadRequest, "from required")
	apiWantStatus(t, http.MethodPost, "merge", s.raw(http.MethodPost, "/api/v1/people/"+target+"/merge", []byte("}"), "application/json"), http.StatusBadRequest)
	apiWantStatus(t, http.MethodPost, "merge self", s.do(http.MethodPost, "/api/v1/people/"+target+"/merge", map[string]any{"from": target}), http.StatusBadRequest)
	// 未知来源：SQL 各步 0 行，不报错
	apiWantStatus(t, http.MethodPost, "merge unknown source", s.do(http.MethodPost, "/api/v1/people/"+target+"/merge", map[string]any{"from": "ghost"}), http.StatusOK)
}

func TestAPIPeopleFieldsCRUD(t *testing.T) {
	s := newTestServer(t)

	pid := apiCreatePersonMap(t, s, map[string]any{"name": "字段人"})["id"].(string)
	other := apiCreatePersonMap(t, s, map[string]any{"name": "另一人"})["id"].(string)

	rec := s.do(http.MethodPost, "/api/v1/people/"+pid+"/fields", map[string]any{"label": "公司", "value": "阿里", "sort_order": 2})
	apiWantStatus(t, http.MethodPost, "field upsert", rec, http.StatusOK)
	f := decodeMap(t, rec)
	fid, _ := f["id"].(string)
	if fid == "" {
		t.Fatalf("字段未返回 id: %v", f)
	}
	if f["person_id"] != pid {
		t.Fatalf("person_id 未回填: %v", f)
	}

	if got := apiArray(s, "/api/v1/people/"+pid+"/fields"); len(got) != 1 {
		t.Fatalf("字段列表 = %v", got)
	}

	// 同 id 再写 => UPDATE 分支
	apiWantStatus(t, http.MethodPost, "field upsert", s.do(http.MethodPost, "/api/v1/people/"+pid+"/fields", map[string]any{
		"id": fid, "label": "公司", "value": "腾讯", "sort_order": 3,
	}), http.StatusOK)
	list := apiArray(s, "/api/v1/people/"+pid+"/fields")
	if len(list) != 1 {
		t.Fatalf("重复 upsert 后 = %v", list)
	}
	got := list[0].(map[string]any)
	if got["value"] != "腾讯" || int(got["sort_order"].(float64)) != 3 {
		t.Fatalf("字段更新未持久化: %v", got)
	}
	// 详情里也带字段
	detail := s.get("/api/v1/people/" + pid)
	if fl := detail["fields"].([]any); len(fl) != 1 {
		t.Fatalf("detail.fields = %v", fl)
	}

	// 另一人不受影响
	if got := apiArray(s, "/api/v1/people/"+other+"/fields"); len(got) != 0 {
		t.Fatalf("other fields = %v", got)
	}

	apiWantStatus(t, http.MethodPost, "field bad json", s.raw(http.MethodPost, "/api/v1/people/"+pid+"/fields", []byte("[]"), "application/json"), http.StatusBadRequest)
	// 未知联系人 => 外键失败，是入参问题不是服务器故障
	apiWantStatus(t, http.MethodPost, "field unknown person", s.do(http.MethodPost, "/api/v1/people/ghost/fields", map[string]any{"label": "x", "value": "y"}), http.StatusBadRequest)

	apiWantStatus(t, http.MethodDelete, "field delete", s.do(http.MethodDelete, "/api/v1/people/"+pid+"/fields/"+fid, nil), http.StatusNoContent)
	if got := apiArray(s, "/api/v1/people/"+pid+"/fields"); len(got) != 0 {
		t.Fatalf("删除后 = %v", got)
	}
	apiWantStatus(t, http.MethodDelete, "field delete unknown", s.do(http.MethodDelete, "/api/v1/people/"+pid+"/fields/ghost", nil), http.StatusNotFound)
}

func TestAPIPeopleRelationships(t *testing.T) {
	s := newTestServer(t)

	a := apiCreatePersonMap(t, s, map[string]any{"name": "甲一"})["id"].(string)
	b := apiCreatePersonMap(t, s, map[string]any{"name": "乙一"})["id"].(string)

	rec := s.do(http.MethodPost, "/api/v1/relationships/", map[string]any{
		"from_person_id": a, "to_person_id": b, "type": "同学", "remark": "高中同班",
	})
	apiWantStatus(t, http.MethodPost, "rel create", rec, http.StatusOK)
	rel := decodeMap(t, rec)
	rid, _ := rel["id"].(string)
	if rid == "" {
		t.Fatalf("关系未返回 id: %v", rel)
	}

	// 落库校验
	list := apiArray(s, "/api/v1/relationships/of/"+a)
	if len(list) != 1 {
		t.Fatalf("relsOf = %v", list)
	}
	r0 := list[0].(map[string]any)
	if r0["id"] != rid || r0["type"] != "同学" || r0["remark"] != "高中同班" {
		t.Fatalf("关系内容不符: %v", r0)
	}
	if r0["from_name"] != "甲一" || r0["to_name"] != "乙一" {
		t.Fatalf("关系未带姓名: %v", r0)
	}
	// 另一端也能查到
	if got := apiArray(s, "/api/v1/relationships/of/"+b); len(got) != 1 {
		t.Fatalf("relsOf(b) = %v", got)
	}

	// 校验分支
	apiWantError(t, http.MethodPost, "rel", s.do(http.MethodPost, "/api/v1/relationships/", map[string]any{"type": "同学"}), http.StatusBadRequest, "需要指定关系的双方")
	apiWantError(t, http.MethodPost, "rel", s.do(http.MethodPost, "/api/v1/relationships/", map[string]any{"from_person_id": a, "type": "同学"}), http.StatusBadRequest, "需要指定关系的双方")
	apiWantError(t, http.MethodPost, "rel self", s.do(http.MethodPost, "/api/v1/relationships/", map[string]any{
		"from_person_id": a, "to_person_id": a, "type": "同学",
	}), http.StatusBadRequest, "不能与自己建立关系")
	apiWantError(t, http.MethodPost, "rel no type", s.do(http.MethodPost, "/api/v1/relationships/", map[string]any{
		"from_person_id": a, "to_person_id": b, "type": "   ",
	}), http.StatusBadRequest, "type required")
	apiWantStatus(t, http.MethodPost, "rel bad json", s.raw(http.MethodPost, "/api/v1/relationships/", []byte("not-json"), "application/json"), http.StatusBadRequest)
	// 未知联系人 => 外键失败 400
	apiWantStatus(t, http.MethodPost, "rel unknown person", s.do(http.MethodPost, "/api/v1/relationships/", map[string]any{
		"from_person_id": a, "to_person_id": "ghost", "type": "陌生人",
	}), http.StatusBadRequest)

	// 同一对人可以挂多种类型的关系；重复的同名关系不再静默顶掉前一条，而是 409
	again := decodeMap(t, s.do(http.MethodPost, "/api/v1/relationships/", map[string]any{
		"from_person_id": a, "to_person_id": b, "type": "老友",
	}))
	current := apiArray(s, "/api/v1/relationships/of/"+a)
	if len(current) != 2 {
		t.Fatalf("新增另一种类型应再多一条: %v", current)
	}
	apiWantError(t, http.MethodPost, "rel duplicate", s.do(http.MethodPost, "/api/v1/relationships/", map[string]any{
		"from_person_id": a, "to_person_id": b, "type": "同学",
	}), http.StatusConflict, "已经有同名的关系")

	// 后面按 rid 走单条更新流程，rid 取「老友」这条
	rid = again["id"].(string)
	if rid == "" {
		t.Fatalf("新建的关系无 id: %v", again)
	}
	// 更新撞上也已存在的类型，同样报 409 而不是把那条顶掉
	apiWantError(t, http.MethodPut, "rel update dup", s.do(http.MethodPut, "/api/v1/relationships/"+rid, map[string]any{
		"from_person_id": a, "to_person_id": b, "type": "同学",
	}), http.StatusConflict, "已经有同名的关系")

	// 更新：改类型与备注，id 与两端不变
	created := relById(t, current, rid)["created_at"].(string)
	apiWantStatus(t, http.MethodPut, "rel update", s.do(http.MethodPut, "/api/v1/relationships/"+rid, map[string]any{
		"from_person_id": a, "to_person_id": b, "type": "老同事", "remark": "同组",
	}), http.StatusOK)
	upd := apiArray(s, "/api/v1/relationships/of/"+a)
	if len(upd) != 2 {
		t.Fatalf("更新后条数 = %d: %v", len(upd), upd)
	}
	u0 := relById(t, upd, rid)
	if u0["type"] != "老同事" || u0["remark"] != "同组" {
		t.Fatalf("更新未生效: %v", u0)
	}
	if u0["created_at"] != created {
		t.Fatalf("更新不应改 created_at: %v / %v", u0["created_at"], created)
	}
	// 备注置空 => 落库为 NULL，列表里不返回 remark
	s.do(http.MethodPut, "/api/v1/relationships/"+rid, map[string]any{
		"from_person_id": a, "to_person_id": b, "type": "老同事",
	})
	if got := relById(t, apiArray(s, "/api/v1/relationships/of/"+a), rid); got["remark"] != nil {
		t.Fatalf("空备注应为 nil: %v", got)
	}
	// 更新同样走校验
	apiWantError(t, http.MethodPut, "rel update self", s.do(http.MethodPut, "/api/v1/relationships/"+rid, map[string]any{
		"from_person_id": a, "to_person_id": a, "type": "同学",
	}), http.StatusBadRequest, "不能与自己建立关系")
	apiWantError(t, http.MethodPut, "rel update no type", s.do(http.MethodPut, "/api/v1/relationships/"+rid, map[string]any{
		"from_person_id": a, "to_person_id": b, "type": " ",
	}), http.StatusBadRequest, "type required")
	apiWantError(t, http.MethodPut, "rel update missing side", s.do(http.MethodPut, "/api/v1/relationships/"+rid, map[string]any{
		"type": "同学",
	}), http.StatusBadRequest, "需要指定关系的双方")
	apiWantStatus(t, http.MethodPut, "rel update bad json", s.raw(http.MethodPut, "/api/v1/relationships/"+rid, []byte("not-json"), "application/json"), http.StatusBadRequest)
	// body 里的 id 不作数，以 URL 为准
	apiWantStatus(t, http.MethodPut, "rel update ghost", s.do(http.MethodPut, "/api/v1/relationships/ghost", map[string]any{
		"from_person_id": a, "to_person_id": b, "type": "陌生人",
	}), http.StatusNotFound)

	// 图
	graph := s.get("/api/v1/relationships/")
	people, ok := graph["people"].([]any)
	if !ok {
		t.Fatalf("graph.people 不是数组: %v", graph["people"])
	}
	if len(people) != 2 {
		t.Fatalf("graph.people = %v", people)
	}
	rels := graph["relationships"].([]any)
	if len(rels) != 2 {
		t.Fatalf("graph.relationships = %v", rels)
	}

	// 删除：只带走被点名的那条，另一条类型不受影响
	apiWantStatus(t, http.MethodDelete, "rel delete", s.do(http.MethodDelete, "/api/v1/relationships/"+rid, nil), http.StatusNoContent)
	left := apiArray(s, "/api/v1/relationships/of/"+a)
	if len(left) != 1 || left[0].(map[string]any)["id"] == rid {
		t.Fatalf("删除后应只剩另一种类型: %v", left)
	}
	for _, it := range left {
		apiWantStatus(t, http.MethodDelete, "rel delete rest", s.do(http.MethodDelete, "/api/v1/relationships/"+it.(map[string]any)["id"].(string), nil), http.StatusNoContent)
	}
	if got := apiArray(s, "/api/v1/relationships/of/"+a); len(got) != 0 {
		t.Fatalf("删除后 = %v", got)
	}
	apiWantStatus(t, http.MethodDelete, "rel delete unknown", s.do(http.MethodDelete, "/api/v1/relationships/ghost", nil), http.StatusNotFound)
	// 空图
	empty := s.get("/api/v1/relationships/")
	if len(empty["relationships"].([]any)) != 0 || len(empty["people"].([]any)) != 2 {
		t.Fatalf("空图: %v", empty)
	}
}

func TestAPIPeopleTimelineIntimacyWordCloud(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	pid := apiCreatePersonMap(t, s, map[string]any{"name": "亲密人", "grade": 5})["id"].(string)
	if err := s.Store.EventCreate(ctx, &store.Event{Title: "登山", EventDate: apiDate(-1)}, []string{pid}, nil); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	if err := s.Store.EventCreate(ctx, &store.Event{Title: "看展", EventDate: apiDate(-3)}, []string{pid}, nil); err != nil {
		t.Fatalf("seed event2: %v", err)
	}
	if err := s.Store.MemoCreate(ctx, &store.Memo{PersonID: pid, Content: "喜欢 登山 登山 摄影", SaidAt: apiDate(0)}); err != nil {
		t.Fatalf("seed memo: %v", err)
	}
	if err := s.Store.TransactionCreate(ctx, &store.Transaction{
		PersonID: pid, Kind: "loan", Direction: "out", AmountFen: 1000, Title: "索道票", OccurredAt: apiDate(-1),
	}); err != nil {
		t.Fatalf("seed transaction: %v", err)
	}

	tl := apiArray(s, "/api/v1/people/"+pid+"/timeline")
	if len(tl) < 4 {
		t.Fatalf("timeline 条目过少: %v", tl)
	}
	types := apiResultsByType(tl)
	for _, want := range []string{"event", "memo", "transaction"} {
		if types[want] == 0 {
			t.Fatalf("timeline 缺 %s: %v", want, types)
		}
	}
	// 事件的参与人姓名
	for _, it := range tl {
		m := it.(map[string]any)
		if m["type"] == "event" && m["person_name"] != "亲密人" {
			t.Fatalf("event timeline person_name = %v", m["person_name"])
		}
	}

	// 无数据联系人：时间线为空数组
	emptyID := apiCreatePersonMap(t, s, map[string]any{"name": "空白"})["id"].(string)
	if got := apiArray(s, "/api/v1/people/"+emptyID+"/timeline"); len(got) != 0 {
		t.Fatalf("空时间线 = %v", got)
	}

	// 亲密度
	intimacy := s.get("/api/v1/people/" + pid + "/intimacy")
	if intimacy["grade"] == nil {
		t.Fatalf("intimacy.grade 缺失: %v", intimacy)
	}
	score := int(intimacy["current_score"].(float64))
	if score <= 0 || score > 100 {
		t.Fatalf("current_score = %d", score)
	}
	if int(intimacy["recent_events"].(float64)) != 2 {
		t.Fatalf("recent_events = %v", intimacy["recent_events"])
	}
	if intimacy["last_interaction"] != apiDate(-1) {
		t.Fatalf("last_interaction = %v", intimacy["last_interaction"])
	}
	trend := intimacy["trend"].([]any)
	if len(trend) != 1 {
		t.Fatalf("trend = %v", trend)
	}
	if p := trend[0].(map[string]any); p["day"] == nil || int(p["score"].(float64)) != score {
		t.Fatalf("trend[0] = %v (score %d)", p, score)
	}
	// 历史快照 => trend 两点
	if err := s.Store.PersonIntimacySnapshot(ctx, pid, apiDate(-3), 42); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	trend2 := s.get("/api/v1/people/" + pid + "/intimacy")["trend"].([]any)
	if len(trend2) != 2 {
		t.Fatalf("补快照后 trend = %v", trend2)
	}
	if first := trend2[0].(map[string]any); first["day"] != apiDate(-3) || int(first["score"].(float64)) != 42 {
		t.Fatalf("历史点不符: %v", first)
	}
	// 归档后分数下降（低等级联系人不会被 100 分上限抹平）
	lowID := apiCreatePersonMap(t, s, map[string]any{"name": "泛泛之交", "grade": 1})["id"].(string)
	before := int(s.get("/api/v1/people/" + lowID + "/intimacy")["current_score"].(float64))
	apiWantStatus(t, http.MethodPost, "archive", s.do(http.MethodPost, "/api/v1/people/"+lowID+"/archive", nil), http.StatusNoContent)
	after := int(s.get("/api/v1/people/" + lowID + "/intimacy")["current_score"].(float64))
	if after >= before {
		t.Fatalf("归档后亲密度应下降: before %d after %d", before, after)
	}
	// 高等级联系人受 100 分上限约束
	highScore := int(s.get("/api/v1/people/" + pid + "/intimacy")["current_score"].(float64))
	if highScore != 100 {
		t.Fatalf("grade=5 且两次近期往来应封顶 100, got %d", highScore)
	}
	// 未知联系人 => 500
	apiWantStatus(t, http.MethodGet, "intimacy unknown", s.do(http.MethodGet, "/api/v1/people/ghost/intimacy", nil), http.StatusNotFound)

	// 词云
	words := apiArray(s, "/api/v1/people/"+pid+"/wordcloud")
	if len(words) == 0 {
		t.Fatalf("词云为空")
	}
	var found bool
	for _, w := range words {
		m := w.(map[string]any)
		if m["word"] == "登山" {
			found = true
			if int(m["count"].(float64)) != 2 {
				t.Fatalf("登山 count = %v", m["count"])
			}
		}
	}
	if !found {
		t.Fatalf("词云缺少 登山: %v", words)
	}
	// 无数据联系人 => 空词云
	if got := apiArray(s, "/api/v1/people/"+emptyID+"/wordcloud"); len(got) != 0 {
		t.Fatalf("空词云 = %v", got)
	}
}

func TestAPIPeopleDBErrors(t *testing.T) {
	s := apiClosedDBServer(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   any
		want   int
	}{
		{"list", http.MethodGet, "/api/v1/people/", nil, http.StatusInternalServerError},
		{"count", http.MethodGet, "/api/v1/people/count", nil, http.StatusInternalServerError},
		{"create", http.MethodPost, "/api/v1/people/", map[string]any{"name": "x"}, http.StatusInternalServerError},
		{"get", http.MethodGet, "/api/v1/people/1", nil, http.StatusNotFound},
		{"update", http.MethodPut, "/api/v1/people/1", map[string]any{"name": "x"}, http.StatusInternalServerError},
		{"delete", http.MethodDelete, "/api/v1/people/1", nil, http.StatusInternalServerError},
		{"timeline", http.MethodGet, "/api/v1/people/1/timeline", nil, http.StatusInternalServerError},
		{"intimacy", http.MethodGet, "/api/v1/people/1/intimacy", nil, http.StatusInternalServerError},
		{"wordcloud", http.MethodGet, "/api/v1/people/1/wordcloud", nil, http.StatusInternalServerError},
		{"fields list", http.MethodGet, "/api/v1/people/1/fields", nil, http.StatusInternalServerError},
		{"fields upsert", http.MethodPost, "/api/v1/people/1/fields", map[string]any{"label": "a"}, http.StatusInternalServerError},
		{"fields delete", http.MethodDelete, "/api/v1/people/1/fields/2", nil, http.StatusInternalServerError},
		{"archive", http.MethodPost, "/api/v1/people/1/archive", nil, http.StatusInternalServerError},
		{"unarchive", http.MethodDelete, "/api/v1/people/1/archive", nil, http.StatusInternalServerError},
		{"merge", http.MethodPost, "/api/v1/people/1/merge", map[string]any{"from": "2"}, http.StatusBadRequest},
		{"duplicates", http.MethodGet, "/api/v1/people/duplicates?name=x", nil, http.StatusInternalServerError},
		{"rels of", http.MethodGet, "/api/v1/relationships/of/1", nil, http.StatusInternalServerError},
		{"rels create", http.MethodPost, "/api/v1/relationships/", map[string]any{"from_person_id": "1", "to_person_id": "2", "type": "t"}, http.StatusInternalServerError},
		{"rels update", http.MethodPut, "/api/v1/relationships/1", map[string]any{"from_person_id": "1", "to_person_id": "2", "type": "t"}, http.StatusInternalServerError},
		{"rels delete", http.MethodDelete, "/api/v1/relationships/1", nil, http.StatusInternalServerError},
		{"graph", http.MethodGet, "/api/v1/relationships/", nil, http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := s.do(c.method, c.path, c.body)
			if rec.Code != c.want {
				t.Fatalf("%s %s => %d, want %d: %s", c.method, c.path, rec.Code, c.want, rec.Body.String())
			}
		})
	}

	// 空关键词检索走 Search 的早退分支，仍 200
	apiWantStatus(t, http.MethodGet, "/api/v1/search", s.do(http.MethodGet, "/api/v1/search", nil), http.StatusOK)
}

// ===== misc.go: 附件 =====

// apiMultipartBody 构造 multipart 表单，file 字段用给定文件名与内容。
func apiMultipartBody(t *testing.T, fields map[string]string, filename string, content []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return buf.Bytes(), w.FormDataContentType()
}

// apiPNGBytes 带真实 PNG 签名的小文件，供 http.DetectContentType 嗅探。
func apiPNGBytes() []byte {
	out := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	for i := 0; i < 64; i++ {
		out = append(out, byte(i%251))
	}
	return out
}

func TestAPIAttachUploadAndDelete(t *testing.T) {
	s := newTestServer(t)
	if err := os.MkdirAll(s.Cfg.Uploads, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}

	content := apiPNGBytes()
	body, ct := apiMultipartBody(t, map[string]string{"entity_type": "person", "entity_id": "p-upload"}, "photo.png", content)
	rec := s.raw(http.MethodPost, "/api/v1/attachments/", body, ct)
	apiWantStatus(t, http.MethodPost, "/api/v1/attachments/", rec, http.StatusOK)

	out := decodeMap(t, rec)
	att := out["attachment"].(map[string]any)
	attID, _ := att["id"].(string)
	if attID == "" {
		t.Fatalf("附件未返回 id: %v", att)
	}
	if att["entity_type"] != "person" || att["entity_id"] != "p-upload" || att["file_name"] != "photo.png" {
		t.Fatalf("附件元数据不符: %v", att)
	}
	if att["mime"] != "image/png" {
		t.Fatalf("mime = %v, want image/png", att["mime"])
	}
	if int(att["size"].(float64)) != len(content) {
		t.Fatalf("size = %v, want %d", att["size"], len(content))
	}
	urlStr, _ := out["url"].(string)
	stored := strings.TrimPrefix(urlStr, "/uploads/")
	if stored == "" || stored == urlStr {
		t.Fatalf("url 异常: %v", out["url"])
	}
	if att["stored_name"] != stored {
		t.Fatalf("stored_name = %v, url=%v", att["stored_name"], urlStr)
	}

	// 文件真实落盘
	data, err := os.ReadFile(filepath.Join(s.Cfg.Uploads, stored))
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Fatalf("落盘内容与上传不一致")
	}

	// 数据库行存在
	var fileName, mime string
	if err := s.Store.DB.QueryRow("SELECT file_name,mime FROM attachments WHERE id=?", attID).Scan(&fileName, &mime); err != nil {
		t.Fatalf("query attachment: %v", err)
	}
	if fileName != "photo.png" || mime != "image/png" {
		t.Fatalf("DB 行不符: %s %s", fileName, mime)
	}

	// 删除 => 204 + 文件移除 + 行清除
	apiWantStatus(t, http.MethodDelete, "attachment delete", s.do(http.MethodDelete, "/api/v1/attachments/"+attID, nil), http.StatusNoContent)
	if _, err := os.Stat(filepath.Join(s.Cfg.Uploads, stored)); !os.IsNotExist(err) {
		t.Fatalf("文件未被删除: %v", err)
	}
	var n int
	if err := s.Store.DB.QueryRow("SELECT COUNT(*) FROM attachments WHERE id=?", attID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatal("附件行未删除")
	}
	// 再删一次 => AttachmentDelete ErrNoRows => 404
	apiWantStatus(t, http.MethodDelete, "attachment delete twice", s.do(http.MethodDelete, "/api/v1/attachments/"+attID, nil), http.StatusNotFound)
}

func TestAPIAttachUploadValidation(t *testing.T) {
	s := newTestServer(t)
	if err := os.MkdirAll(s.Cfg.Uploads, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	before, _ := os.ReadDir(s.Cfg.Uploads)

	// 非图片
	body, ct := apiMultipartBody(t, map[string]string{"entity_type": "person", "entity_id": "p1"}, "note.txt", []byte("纯文本内容，不是图片"))
	rec := s.raw(http.MethodPost, "/api/v1/attachments/", body, ct)
	apiWantError(t, http.MethodPost, "attach upload", rec, http.StatusBadRequest, "只接受")

	// SVG 是图片但能带脚本，同样按「不是位图」拒
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"></svg>`)
	body, ct = apiMultipartBody(t, nil, "icon.svg", svg)
	apiWantStatus(t, http.MethodPost, "attach svg", s.raw(http.MethodPost, "/api/v1/attachments/", body, ct), http.StatusBadRequest)

	// polyglot：PNG 头 + HTML 体，文件名还自称 .html —— 落盘名必须由嗅探结果决定
	poly := append(apiPNGBytes(), []byte("<html><script>alert(1)</script></html>")...)
	body, ct = apiMultipartBody(t, nil, "evil.html", poly)
	rec = s.raw(http.MethodPost, "/api/v1/attachments/", body, ct)
	apiWantStatus(t, http.MethodPost, "attach polyglot", rec, http.StatusOK)
	polyURL := decodeMap(t, rec)["url"].(string)
	if !strings.HasSuffix(polyURL, ".png") {
		t.Fatalf("polyglot 落盘名应取自嗅探结果而不是文件名: %s", polyURL)
	}
	if err := os.Remove(filepath.Join(s.Cfg.Uploads, strings.TrimPrefix(polyURL, "/uploads/"))); err != nil {
		t.Fatalf("清理 polyglot: %v", err)
	}

	// 超过 20MB 上限
	body, ct = apiMultipartBody(t, nil, "big.png", append(apiPNGBytes(), make([]byte, 21<<20)...))
	apiWantStatus(t, http.MethodPost, "attach too big", s.raw(http.MethodPost, "/api/v1/attachments/", body, ct), http.StatusRequestEntityTooLarge)

	// 只有字段，没有 file
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("entity_type", "person")
	_ = w.Close()
	rec = s.raw(http.MethodPost, "/api/v1/attachments/", buf.Bytes(), w.FormDataContentType())
	apiWantError(t, http.MethodPost, "attach upload", rec, http.StatusBadRequest, "file required")

	// 根本不是 multipart
	rec = s.raw(http.MethodPost, "/api/v1/attachments/", []byte("hello"), "text/plain")
	apiWantStatus(t, http.MethodPost, "attach upload", rec, http.StatusBadRequest)

	// 未落盘任何文件
	after, _ := os.ReadDir(s.Cfg.Uploads)
	if len(after) != len(before) {
		t.Fatalf("非法上传仍写了文件: %v -> %v", before, after)
	}

	// uploads 目录不存在 => os.Create 失败 => 500
	missing := newTestServer(t)
	body, ct = apiMultipartBody(t, nil, "a.png", apiPNGBytes())
	rec = missing.raw(http.MethodPost, "/api/v1/attachments/", body, ct)
	apiWantStatus(t, http.MethodPost, "attach upload no dir", rec, http.StatusInternalServerError)
}

func TestAPIAttachUploadStoreError(t *testing.T) {
	s := apiClosedDBServer(t)
	if err := os.MkdirAll(s.Cfg.Uploads, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}

	body, ct := apiMultipartBody(t, map[string]string{"entity_type": "person", "entity_id": "p1"}, "x.png", apiPNGBytes())
	rec := s.raw(http.MethodPost, "/api/v1/attachments/", body, ct)
	apiWantStatus(t, http.MethodPost, "attach upload", rec, http.StatusInternalServerError)

	// 落库失败时磁盘文件应被回滚清理
	left, err := os.ReadDir(s.Cfg.Uploads)
	if err != nil {
		t.Fatalf("read uploads: %v", err)
	}
	if len(left) != 0 {
		t.Fatalf("孤儿文件未清理: %v", left)
	}

	// 删除未知 / 关闭的库 => 500
	apiWantStatus(t, http.MethodDelete, "attach delete", s.do(http.MethodDelete, "/api/v1/attachments/ghost", nil), http.StatusInternalServerError)
}

func TestAPIAttachDeleteUnknownAndRows(t *testing.T) {
	s := newTestServer(t)
	if err := os.MkdirAll(s.Cfg.Uploads, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}

	// 未知 id => sql.ErrNoRows => 404
	apiWantStatus(t, http.MethodDelete, "attach delete", s.do(http.MethodDelete, "/api/v1/attachments/nope", nil), http.StatusNotFound)

	// 手工插入一条附件（文件在磁盘上），删除应连带清理文件
	if err := os.WriteFile(filepath.Join(s.Cfg.Uploads, "stored-1.png"), apiPNGBytes(), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	if _, err := s.Store.DB.Exec(
		"INSERT INTO attachments(id,entity_type,entity_id,file_name,stored_name,mime,size,created_at) VALUES('att-1','person','p1','seed.png','stored-1.png','image/png',10,'2026-01-01')"); err != nil {
		t.Fatalf("seed attachment: %v", err)
	}
	apiWantStatus(t, http.MethodDelete, "attach delete", s.do(http.MethodDelete, "/api/v1/attachments/att-1", nil), http.StatusNoContent)
	if _, err := os.Stat(filepath.Join(s.Cfg.Uploads, "stored-1.png")); !os.IsNotExist(err) {
		t.Fatalf("文件未删除")
	}

	// 联系人删除时也应清理其附件文件与行
	pid := apiCreatePersonMap(t, s, map[string]any{"name": "带头像的人"})["id"].(string)
	body, ct := apiMultipartBody(t, map[string]string{"entity_type": "person", "entity_id": pid}, "avatar.png", apiPNGBytes())
	out := decodeMap(t, s.raw(http.MethodPost, "/api/v1/attachments/", body, ct))
	stored := strings.TrimPrefix(out["url"].(string), "/uploads/")
	if _, err := s.Store.DB.Exec("UPDATE people SET avatar_attachment_id=? WHERE id=?", out["attachment"].(map[string]any)["id"], pid); err != nil {
		t.Fatalf("set avatar: %v", err)
	}
	apiWantStatus(t, http.MethodDelete, "people delete", s.do(http.MethodDelete, "/api/v1/people/"+pid, nil), http.StatusNoContent)
	if _, err := os.Stat(filepath.Join(s.Cfg.Uploads, stored)); !os.IsNotExist(err) {
		t.Fatalf("联系人删除后附件文件仍在: %v", err)
	}
	var n int
	if err := s.Store.DB.QueryRow("SELECT COUNT(*) FROM attachments WHERE stored_name=?", stored).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatal("联系人删除后附件行未清理")
	}
}

// ===== misc.go: 通知 / randStr / dashboard =====

func TestAPIRandStr(t *testing.T) {
	if got := randStr(0); got != "" {
		t.Fatalf("randStr(0) = %q", got)
	}
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		got := randStr(12)
		if len(got) != 12 {
			t.Fatalf("len = %d", len(got))
		}
		for _, r := range got {
			if !strings.ContainsRune(alphabet, r) {
				t.Fatalf("非法字符 %q in %q", r, got)
			}
		}
		seen[got] = true
	}
	if len(seen) < 10 {
		t.Fatalf("随机性不足: %d 个不同值", len(seen))
	}
}

func TestAPINotifyTest(t *testing.T) {
	s := newTestServer(t)

	// 未配置
	apiWantError(t, http.MethodPost, "/api/v1/notify/test", s.do(http.MethodPost, "/api/v1/notify/test", nil), http.StatusBadRequest, "尚未配置")

	ctx := context.Background()
	// 只有分隔符，FieldsFunc 解析出 0 个 URL
	if err := s.Store.SettingSet(ctx, "apprise_urls", ",,,"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	apiWantError(t, http.MethodPost, "/api/v1/notify/test", s.do(http.MethodPost, "/api/v1/notify/test", nil), http.StatusBadRequest, "尚未配置")
	// 纯空白同理
	if err := s.Store.SettingSet(ctx, "apprise_urls", "   "); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	apiWantStatus(t, http.MethodPost, "/api/v1/notify/test", s.do(http.MethodPost, "/api/v1/notify/test", nil), http.StatusBadRequest)

	// 不认识的协议：apprise 本地即拒绝添加，Send 返回「无目标」，无需网络
	if err := s.Store.SettingSet(ctx, "apprise_urls", "qiansi-not-a-channel://localhost"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	rec := s.do(http.MethodPost, "/api/v1/notify/test", nil)
	apiWantStatus(t, http.MethodPost, "/api/v1/notify/test", rec, http.StatusInternalServerError)
	if !strings.Contains(rec.Body.String(), "推送失败") {
		t.Fatalf("body = %s", rec.Body.String())
	}

	// 多个非法 URL（逗号分隔）同样走失败分支
	if err := s.Store.SettingSet(ctx, "apprise_urls", "bogus1://a,bogus2://b"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	apiWantStatus(t, http.MethodPost, "/api/v1/notify/test", s.do(http.MethodPost, "/api/v1/notify/test", nil), http.StatusInternalServerError)
}

func TestAPIDashboardStatsAndTimeline(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	a := apiCreatePersonMap(t, s, map[string]any{"name": "统计甲", "grade": 4})
	pid := a["id"].(string)
	apiCreatePersonMap(t, s, map[string]any{"name": "统计乙"})
	archived := apiCreatePersonMap(t, s, map[string]any{"name": "统计丙"})["id"].(string)
	s.do(http.MethodPost, "/api/v1/people/"+archived+"/archive", nil)

	if err := s.Store.EventCreate(ctx, &store.Event{Title: "统计饭局", EventDate: apiDate(-1)}, []string{pid}, nil); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	if err := s.Store.MemoCreate(ctx, &store.Memo{PersonID: pid, Content: "统计备忘", SaidAt: apiDate(0), IsPromise: true, DueDate: apiDate(-2)}); err != nil {
		t.Fatalf("seed memo: %v", err)
	}
	if err := s.Store.TransactionCreate(ctx, &store.Transaction{
		PersonID: pid, Kind: "loan", Direction: "out", AmountFen: 12000, Title: "统计借款", OccurredAt: apiDate(0),
	}); err != nil {
		t.Fatalf("seed transaction: %v", err)
	}
	if _, err := s.Store.DB.ExecContext(ctx,
		"INSERT INTO anniversaries(id,person_id,title,date,remind_days,created_at) VALUES('ann-d',?,'统计纪念日',?,'0','2026-01-01')", pid, apiDate(2)); err != nil {
		t.Fatalf("seed anniversary: %v", err)
	}

	stats := s.get("/api/v1/dashboard/stats")
	if int(stats["total_people"].(float64)) != 2 {
		t.Fatalf("total_people = %v (归档不应计入)", stats["total_people"])
	}
	if int(stats["total_events"].(float64)) != 1 {
		t.Fatalf("total_events = %v", stats["total_events"])
	}
	if int(stats["pending_promises"].(float64)) != 1 {
		t.Fatalf("pending_promises = %v", stats["pending_promises"])
	}
	if int(stats["lend_fen"].(float64)) != 12000 {
		t.Fatalf("lend_fen = %v", stats["lend_fen"])
	}
	if int(stats["borrow_fen"].(float64)) != 0 {
		t.Fatalf("borrow_fen = %v", stats["borrow_fen"])
	}
	if int(stats["upcoming_days7"].(float64)) < 1 {
		t.Fatalf("upcoming_days7 = %v", stats["upcoming_days7"])
	}
	if stats["due_today"] == nil {
		t.Fatalf("缺少 due_today: %v", stats)
	}

	tl := apiArray(s, "/api/v1/dashboard/timeline")
	types := apiResultsByType(tl)
	for _, want := range []string{"event", "memo", "transaction", "anniversary"} {
		if types[want] == 0 {
			t.Fatalf("全局时间线缺少 %s: %v", want, types)
		}
	}
	if got := len(apiArray(s, "/api/v1/dashboard/timeline?limit=1")); got != 1 {
		t.Fatalf("limit=1 => %d", got)
	}
	// 非法 limit 回落默认
	if got := len(apiArray(s, "/api/v1/dashboard/timeline?limit=abc")); got == 0 {
		t.Fatal("非法 limit 应回落默认值")
	}
}

func TestAPIDashboardByMonthAndGrade(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	pid := apiCreatePersonMap(t, s, map[string]any{"name": "月度人", "grade": 5})["id"].(string)
	apiCreatePersonMap(t, s, map[string]any{"name": "另一甲", "grade": 5})
	apiCreatePersonMap(t, s, map[string]any{"name": "另一乙", "grade": 1})

	thisMonth := apiDate(-1)
	if _, err := s.Store.DB.ExecContext(ctx,
		"INSERT INTO events(id,title,event_date,created_at,updated_at) VALUES('ev-m1','本月事件',?,'2026-01-01','2026-01-01')", thisMonth); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	if err := s.Store.EventCreate(ctx, &store.Event{Title: "关联事件", EventDate: thisMonth}, []string{pid}, nil); err != nil {
		t.Fatalf("seed event2: %v", err)
	}
	if err := s.Store.MemoCreate(ctx, &store.Memo{PersonID: pid, Content: "本月备忘", SaidAt: thisMonth}); err != nil {
		t.Fatalf("seed memo: %v", err)
	}
	if err := s.Store.TransactionCreate(ctx, &store.Transaction{
		PersonID: pid, Kind: "expense", Direction: "in", AmountFen: 500, Title: "本月收款", OccurredAt: thisMonth,
	}); err != nil {
		t.Fatalf("seed tx: %v", err)
	}

	list := apiArray(s, "/api/v1/dashboard/by-month")
	if len(list) == 0 {
		t.Fatal("by-month 为空")
	}
	row := list[0].(map[string]any)
	if row["month"] == nil || row["month"] != thisMonth[:7] {
		t.Fatalf("by-month[0] = %v (期望 %s)", row, thisMonth[:7])
	}
	if int(row["event_count"].(float64)) != 2 || int(row["memo_count"].(float64)) != 1 || int(row["tx_count"].(float64)) != 1 {
		t.Fatalf("by-month 计数不符: %v", row)
	}
	// months 非法值回落
	if got := len(apiArray(s, "/api/v1/dashboard/by-month?months=abc")); got != len(list) {
		t.Fatalf("months=abc => %d 条", got)
	}
	if got := len(apiArray(s, "/api/v1/dashboard/by-month?months=1")); got != 1 {
		t.Fatalf("months=1 => %d", got)
	}

	dist := apiArray(s, "/api/v1/dashboard/grade-distribution")
	counts := map[int]int{}
	for _, it := range dist {
		m := it.(map[string]any)
		counts[int(m["grade"].(float64))] = int(m["count"].(float64))
	}
	if counts[5] != 2 || counts[1] != 1 {
		t.Fatalf("grade 分布不符: %v", counts)
	}
}

func TestAPIDashboardSuggestions(t *testing.T) {
	s := newTestServer(t)

	// 空库 => 空数组
	if got := apiArray(s, "/api/v1/dashboard/suggestions"); len(got) != 0 {
		t.Fatalf("空库建议 = %v", got)
	}

	ctx := context.Background()
	// 1. 久未联系：updated_at 放到 30 天前
	stale := apiCreatePersonMap(t, s, map[string]any{"name": "久未联系者"})["id"].(string)
	if _, err := s.Store.DB.ExecContext(ctx, "UPDATE people SET updated_at=? WHERE id=?", apiDate(-30)+"T10:00:00+08:00", stale); err != nil {
		t.Fatalf("age person: %v", err)
	}
	// 2. 临近纪念日
	soon := apiCreatePersonMap(t, s, map[string]any{"name": "寿星"})["id"].(string)
	if _, err := s.Store.DB.ExecContext(ctx,
		"INSERT INTO anniversaries(id,person_id,title,date,remind_days,created_at) VALUES('ann-s',?,'寿星的生日',?,'0','2026-01-01')", soon, apiDate(3)); err != nil {
		t.Fatalf("seed anniversary: %v", err)
	}
	// 3. 未还借款（含部分还款，剩余应为 7000）
	borrower := apiCreatePersonMap(t, s, map[string]any{"name": "借钱人"})["id"].(string)
	if err := s.Store.TransactionCreate(ctx, &store.Transaction{
		ID: "tx-1", PersonID: borrower, Kind: "loan", Direction: "out", AmountFen: 10000, Title: "借款", OccurredAt: apiDate(-5),
	}); err != nil {
		t.Fatalf("seed tx: %v", err)
	}
	if _, err := s.Store.DB.ExecContext(ctx,
		"INSERT INTO repayments(id,transaction_id,amount_fen,occurred_at,note) VALUES('rep-1','tx-1',3000,?,'部分还款')", apiDate(-1)); err != nil {
		t.Fatalf("seed repayment: %v", err)
	}
	// 已结清的不该出现在建议里
	if err := s.Store.TransactionCreate(ctx, &store.Transaction{
		ID: "tx-2", PersonID: borrower, Kind: "loan", Direction: "out", AmountFen: 999, Title: "已还清", OccurredAt: apiDate(-4), Settled: true,
	}); err != nil {
		t.Fatalf("seed settled tx: %v", err)
	}
	// 4. 过期未兑现的承诺（内容较长以覆盖截断）
	if err := s.Store.MemoCreate(ctx, &store.Memo{
		PersonID: borrower, Content: "答应把相机借给他一周内一定兑现的承诺内容比较长需要被截断处理一下下", SaidAt: apiDate(-10),
		IsPromise: true, DueDate: apiDate(-3), Status: "open",
	}); err != nil {
		t.Fatalf("seed promise: %v", err)
	}

	list := apiArray(s, "/api/v1/dashboard/suggestions")
	types := apiResultsByType(list)
	for _, want := range []string{"久未联系", "纪念日临近", "有借款未还", "承诺到期未兑现"} {
		if types[want] == 0 {
			t.Fatalf("建议缺少 %s: %v", want, list)
		}
	}
	var unpaidSeen bool
	for _, it := range list {
		m := it.(map[string]any)
		if m["type"] == "有借款未还" {
			if m["message"] != "待还 ¥70.00" {
				t.Fatalf("借款建议文案应为扣减还款后的 70 元: %v", m["message"])
			}
			if m["person_name"] != "借钱人" {
				t.Fatalf("借款建议缺人名: %v", m)
			}
			unpaidSeen = true
		}
		if m["type"] == "承诺到期未兑现" && !strings.Contains(m["message"].(string), "…") {
			t.Fatalf("长承诺应被截断: %v", m["message"])
		}
	}
	if !unpaidSeen {
		t.Fatal("未生成借款建议")
	}
	// 已结清不应再出现
	for _, it := range list {
		m := it.(map[string]any)
		if strings.Contains(m["message"].(string), "9.99") {
			t.Fatalf("已结清借款出现在建议中: %v", m)
		}
	}
}

func TestAPIDashboardDBErrors(t *testing.T) {
	s := apiClosedDBServer(t)

	for _, path := range []string{
		"/api/v1/dashboard/timeline",
		"/api/v1/dashboard/timeline?limit=abc",
		"/api/v1/dashboard/by-month",
		"/api/v1/dashboard/by-month?months=xyz",
		"/api/v1/dashboard/grade-distribution",
	} {
		t.Run(path, func(t *testing.T) {
			rec := s.do(http.MethodGet, path, nil)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("GET %s => %d: %s", path, rec.Code, rec.Body.String())
			}
		})
	}
	// stats 底层 DashboardStats 吞掉所有查询错误，即使库已关闭仍返回 200（全 0）
	stats := s.get("/api/v1/dashboard/stats")
	if int(stats["total_people"].(float64)) != 0 {
		t.Fatalf("closed db stats = %v", stats)
	}

	// 建议不查错误分支，仍返回 200 空数组
	if got := apiArray(s, "/api/v1/dashboard/suggestions"); len(got) != 0 {
		t.Fatalf("closed db suggestions = %v", got)
	}
}

// apiTrigger 在测试库上建/删触发器，用于制造「主写入成功、后续同步失败」的分支。
func apiTrigger(t *testing.T, s *testServer, stmt string) {
	t.Helper()
	if _, err := s.Store.DB.ExecContext(context.Background(), stmt); err != nil {
		t.Fatalf("exec %q: %v", stmt, err)
	}
}

func TestAPIPeopleBirthdaySyncErrors(t *testing.T) {
	s := newTestServer(t)

	apiTrigger(t, s, "CREATE TRIGGER blk_anniv BEFORE INSERT ON anniversaries BEGIN SELECT RAISE(ABORT,'trigger boom'); END")
	t.Cleanup(func() {
		_, _ = s.Store.DB.Exec("DROP TRIGGER IF EXISTS blk_anniv")
	})

	// 人物已落库，但纪念日生成失败 => 专属 500 文案
	rec := s.do(http.MethodPost, "/api/v1/people/", map[string]any{"name": "生日失败", "birthday": "1990-01-01"})
	apiWantError(t, http.MethodPost, "/api/v1/people/", rec, http.StatusInternalServerError, "人物已保存，但生日纪念日生成失败")

	// 更新路径同理
	pid := apiCreatePersonMap(t, s, map[string]any{"name": "无生日"})["id"].(string)
	rec = s.do(http.MethodPut, "/api/v1/people/"+pid, map[string]any{"name": "无生日", "birthday": "2000-02-02"})
	apiWantError(t, http.MethodPut, "/api/v1/people/{id}", rec, http.StatusInternalServerError, "人物已保存，但生日纪念日同步失败")

	// 撤掉触发器后同一请求成功，且纪念日确实生成
	apiTrigger(t, s, "DROP TRIGGER IF EXISTS blk_anniv")
	rec = s.do(http.MethodPut, "/api/v1/people/"+pid, map[string]any{"name": "无生日", "birthday": "2000-02-02"})
	apiWantStatus(t, http.MethodPut, "/api/v1/people/{id}", rec, http.StatusOK)
	var n int
	if err := s.Store.DB.QueryRow("SELECT COUNT(*) FROM anniversaries WHERE person_id=? AND source='birthday' AND date='2000-02-02'", pid).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("纪念日数量 = %d, want 1", n)
	}
}

func TestAPIPeopleDeleteStoreError(t *testing.T) {
	s := newTestServer(t)
	if err := os.MkdirAll(s.Cfg.Uploads, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}

	pid := apiCreatePersonMap(t, s, map[string]any{"name": "删不掉"})["id"].(string)
	// 挂上头像：删除失败前这一步已把 avatar_attachment_id 置为 NULL
	body, ct := apiMultipartBody(t, map[string]string{"entity_type": "person", "entity_id": pid}, "avatar.png", apiPNGBytes())
	rec := s.raw(http.MethodPost, "/api/v1/attachments/", body, ct)
	apiWantStatus(t, http.MethodPost, "/api/v1/attachments/", rec, http.StatusOK)
	attID := decodeMap(t, rec)["attachment"].(map[string]any)["id"].(string)
	if _, err := s.Store.DB.Exec("UPDATE people SET avatar_attachment_id=? WHERE id=?", attID, pid); err != nil {
		t.Fatalf("set avatar: %v", err)
	}

	apiTrigger(t, s, "CREATE TRIGGER blk_del BEFORE DELETE ON people BEGIN SELECT RAISE(ABORT,'no delete'); END")
	t.Cleanup(func() {
		_, _ = s.Store.DB.Exec("DROP TRIGGER IF EXISTS blk_del")
	})

	// 摘附件成功、DELETE people 失败 => 500
	rec = s.do(http.MethodDelete, "/api/v1/people/"+pid, nil)
	apiWantStatus(t, http.MethodDelete, "/api/v1/people/{id}", rec, http.StatusInternalServerError)
	// 该次失败已把 avatar_attachment_id 置空，此处只按 SQL 校验记录仍在
	if n := apiPeopleRowCount(t, s); n != 1 {
		t.Fatalf("删除失败后联系人数量 = %d, want 1", n)
	}
	// 回归：NULL 头像不能让读取接口挂掉（曾经因扫描 NULL 到 string 而 500）
	if rec := s.do(http.MethodGet, "/api/v1/people/"+pid, nil); rec.Code != http.StatusOK {
		t.Errorf("删除失败后 GET /people/{id} => %d: %s", rec.Code, rec.Body.String())
	}
	if rec := s.do(http.MethodGet, "/api/v1/people/", nil); rec.Code != http.StatusOK {
		t.Errorf("删除失败后 GET /people => %d: %s", rec.Code, rec.Body.String())
	}

	apiTrigger(t, s, "DROP TRIGGER IF EXISTS blk_del")
	apiWantStatus(t, http.MethodDelete, "/api/v1/people/{id}", s.do(http.MethodDelete, "/api/v1/people/"+pid, nil), http.StatusNoContent)
	if n := apiPeopleRowCount(t, s); n != 0 {
		t.Fatalf("删除后仍有 %d 条记录", n)
	}
}

// apiPeopleRowCount 直接按 SQL 统计联系人，绕开 API 响应结构，断言更贴近落库结果。
func apiPeopleRowCount(t *testing.T, s *testServer) int {
	t.Helper()
	var n int
	if err := s.Store.DB.QueryRow("SELECT COUNT(*) FROM people").Scan(&n); err != nil {
		t.Fatalf("count people: %v", err)
	}
	return n
}

// TestIntroducedByAPI 引荐人非法输入在 API 层就是 400，而不是 500；
// 合法引荐人落库并在详情里带回姓名。
func TestIntroducedByAPI(t *testing.T) {
	s := newTestServer(t)

	a := apiCreatePersonMap(t, s, map[string]any{"name": "同学甲"})
	b := apiCreatePersonMap(t, s, map[string]any{"name": "闺蜜乙", "introduced_by_person_id": a["id"]})
	if b["introduced_by_person_id"] != a["id"] {
		t.Fatalf("创建响应应带回引荐人: %v", b["introduced_by_person_id"])
	}
	detail := s.get("/api/v1/people/" + b["id"].(string))["person"].(map[string]any)
	if detail["introduced_by_person_id"] != a["id"] || detail["introduced_by_name"] != "同学甲" {
		t.Fatalf("详情引荐人回读不符: %v / %v", detail["introduced_by_person_id"], detail["introduced_by_name"])
	}

	// 指向不存在的人 → 400
	apiWantError(t, http.MethodPost, "intro missing", s.do(http.MethodPost, "/api/v1/people/",
		map[string]any{"name": "幽灵引荐", "introduced_by_person_id": "ghost"}), http.StatusBadRequest, "引荐人不存在")
	// 引荐人是自己 → 400
	apiWantError(t, http.MethodPut, "intro self", s.do(http.MethodPut, "/api/v1/people/"+a["id"].(string),
		map[string]any{"name": "同学甲", "introduced_by_person_id": a["id"]}), http.StatusBadRequest, "循环")

	// 关系图 payload 带 tags 字段
	graph := s.get("/api/v1/relationships/")
	if _, ok := graph["tags"].(map[string]any); !ok {
		t.Fatalf("关系图应返回 tags map: %v", graph)
	}
	// 已用类型接口
	types := apiArray(s, "/api/v1/relationships/types")
	if len(types) != 0 {
		t.Fatalf("还没建边，类型应为空: %v", types)
	}
}
