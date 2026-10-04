package api

import (
	"net/http"
	"testing"
)

// 选人控件改成「输入即搜」之后，这几个查询参数就是前后端的契约：
// q 命中姓名或昵称、limit 控住一屏、archived=1 才把归档的人一起搜出来。
// 少一个都不成立：归档的人搜不到，用户就只能看见「没有匹配的人」。
func TestAPIPersonSearchContract(t *testing.T) {
	s := newTestServer(t)
	apiCreatePersonMap(t, s, map[string]any{"name": "李雷", "nickname": "雷子"})
	han := apiCreatePersonMap(t, s, map[string]any{"name": "韩梅梅"})
	hanID, _ := han["id"].(string)

	names := apiNames(t, apiArray(s, "/api/v1/people?q=雷子&limit=20"))
	if !names["李雷"] {
		t.Fatalf("按昵称搜不到人：%v", names)
	}

	if rec := s.do(http.MethodPost, "/api/v1/people/"+hanID+"/archive", map[string]any{}); rec.Code != 204 {
		t.Fatalf("归档 = %d: %s", rec.Code, rec.Body.String())
	}
	if apiNames(t, apiArray(s, "/api/v1/people?q=韩梅梅&limit=20"))["韩梅梅"] {
		t.Fatalf("没带 archived=1 却搜出了归档的人")
	}
	if !apiNames(t, apiArray(s, "/api/v1/people?q=韩梅梅&limit=20&archived=1"))["韩梅梅"] {
		t.Fatalf("archived=1 也没搜出归档的人")
	}

	if list := apiArray(s, "/api/v1/people?limit=1"); len(list) != 1 {
		t.Fatalf("limit=1 返回 %d 条", len(list))
	}
}

// 图谱响应里的截断三兄弟必须一直在：前端据此提示「图里只有前 N 人」，
// 而不是让人以为通讯录里那些人凭空消失了。
func TestAPIGraphTruncationFields(t *testing.T) {
	s := newTestServer(t)
	a := apiCreatePersonMap(t, s, map[string]any{"name": "甲"})
	b := apiCreatePersonMap(t, s, map[string]any{"name": "乙"})
	if rec := s.do(http.MethodPost, "/api/v1/relationships", map[string]any{
		"from_person_id": a["id"], "to_person_id": b["id"], "type": "同事",
	}); rec.Code != 200 {
		t.Fatalf("建关系 = %d: %s", rec.Code, rec.Body.String())
	}

	g := s.get("/api/v1/relationships")
	total, ok := g["total_people"].(float64)
	if !ok || int(total) != 2 {
		t.Fatalf("total_people = %v，期望 2", g["total_people"])
	}
	if truncated, ok := g["truncated"].(bool); !ok || truncated {
		t.Fatalf("truncated = %v（%v），两人不该报截断", g["truncated"], ok)
	}
	if dropped, ok := g["dropped_edges"].(float64); !ok || dropped != 0 {
		t.Fatalf("dropped_edges = %v（%v）", g["dropped_edges"], ok)
	}
	people, ok := g["people"].([]any)
	if !ok || len(people) != 2 {
		t.Fatalf("people = %v", g["people"])
	}
	rels, ok := g["relationships"].([]any)
	if !ok || len(rels) != 1 {
		t.Fatalf("relationships = %v", g["relationships"])
	}
}

// 对话列表的「谁说的」由后端带出来（前端不再为这几个名字拉整本通讯录），
// 所以 JSON 字段名同样是契约。
func TestAPIMemoListPersonName(t *testing.T) {
	s := newTestServer(t)
	p := apiCreatePersonMap(t, s, map[string]any{"name": "小美"})
	if rec := s.do(http.MethodPost, "/api/v1/memos", map[string]any{
		"person_id": p["id"], "content": "答应帮她带特产", "said_at": "2026-03-01T10:00:00",
	}); rec.Code != 200 {
		t.Fatalf("建对话 = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := s.do(http.MethodPost, "/api/v1/memos", map[string]any{
		"content": "随手记一条", "said_at": "2026-03-02T10:00:00",
	}); rec.Code != 200 {
		t.Fatalf("建无归属对话 = %d: %s", rec.Code, rec.Body.String())
	}

	list := evAPIList(t, s.do(http.MethodGet, "/api/v1/memos", nil))
	if len(list) != 2 {
		t.Fatalf("memos = %d 条，期望 2", len(list))
	}
	// said_at DESC：无归属的那条在前
	if evAPIStr(list[0], "person_name") != "" {
		t.Fatalf("无归属的对话却有名字：%v", list[0])
	}
	if got := evAPIStr(list[1], "person_name"); got != "小美" {
		t.Fatalf("person_name = %q，期望 小美", got)
	}
}
