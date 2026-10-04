package api

import (
	"net/http"
	"testing"
)

// ===== 认识路径单接口（O1）=====

func apRel(t *testing.T, s *testServer, from, to, typ string) {
	t.Helper()
	path := "/api/v1/relationships"
	rec := s.do(http.MethodPost, path, map[string]any{
		"from_person_id": from, "to_person_id": to, "type": typ,
	})
	apiWantStatus(t, http.MethodPost, path, rec, http.StatusOK)
}

func TestAPIIntroPath(t *testing.T) {
	s := newTestServer(t)
	me := apiCreatePersonMap(t, s, map[string]any{"name": "我自己"})
	jia := apiCreatePersonMap(t, s, map[string]any{"name": "甲", "introduced_by_person_id": me["id"]})
	yi := apiCreatePersonMap(t, s, map[string]any{"name": "乙", "introduced_by_person_id": jia["id"]})
	apRel(t, s, me["id"].(string), jia["id"].(string), "同事")
	apRel(t, s, jia["id"].(string), yi["id"].(string), "球友")

	path := "/api/v1/people/" + yi["id"].(string) + "/intro-path"
	// 没设本人时这条链没有终点，接口直接 400，前端也就不画这张卡
	apiWantError(t, http.MethodGet, path, s.do(http.MethodGet, path, nil), http.StatusBadRequest, "我是谁")

	bulk := "/api/v1/settings/bulk"
	apiWantStatus(t, http.MethodPost, bulk,
		s.do(http.MethodPost, bulk, map[string]string{"self_person_id": me["id"].(string)}), http.StatusOK)

	body := s.get(path)
	if body["reaches_self"] != true {
		t.Fatalf("该走到本人，得到 %v", body["reaches_self"])
	}
	chain, _ := body["chain"].([]any)
	if len(chain) != 3 {
		t.Fatalf("链长 = %d, want 3（我→甲→乙）", len(chain))
	}
	if head := chain[0].(map[string]any); head["name"] != "我自己" {
		t.Fatalf("链应从本人起，得到 %v", head["name"])
	}
	if tail := chain[2].(map[string]any); tail["name"] != "乙" {
		t.Fatalf("链应止于此人，得到 %v", tail["name"])
	}
	edge := chain[1].(map[string]any)["edge_types"].([]any)
	if len(edge) != 1 || edge[0] != "同事" {
		t.Fatalf("我→甲 的边 = %v", edge)
	}
	// 空数组必须是 []，前端拿到就直接 .join()
	if chain[0].(map[string]any)["edge_types"] == nil {
		t.Fatal("首跳 edge_types 不能是 null")
	}
	if body["direct_types"] == nil {
		t.Fatal("direct_types 不能是 null")
	}
	if body["broken"] != false || body["cyclic"] != false {
		t.Fatalf("正常链不该带 broken/cyclic，得到 %v / %v", body["broken"], body["cyclic"])
	}
}

func TestAPIIntroPath_PersonNotFound(t *testing.T) {
	s := newTestServer(t)
	me := apiCreatePersonMap(t, s, map[string]any{"name": "我自己"})
	bulk := "/api/v1/settings/bulk"
	apiWantStatus(t, http.MethodPost, bulk,
		s.do(http.MethodPost, bulk, map[string]string{"self_person_id": me["id"].(string)}), http.StatusOK)

	path := "/api/v1/people/no-such-person/intro-path"
	rec := s.do(http.MethodGet, path, nil)
	apiWantStatus(t, http.MethodGet, path, rec, http.StatusOK)
	body := decodeMap(t, rec)
	if body["reaches_self"] != false {
		t.Fatalf("不存在的人不该声称走到本人，得到 %v", body["reaches_self"])
	}
	if chain, _ := body["chain"].([]any); len(chain) != 0 {
		t.Fatalf("不存在的人链该为空，得到 %v", chain)
	}
}
