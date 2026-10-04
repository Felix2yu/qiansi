package api

import (
	"net/http"
	"testing"
)

// ===== 按人往来小结（N1）=====

// apiStats 取出 stats 对象。这个字段是按人聚合后才挂上的，缺省即「没有往来」。
func apiStats(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	st, ok := body["stats"].(map[string]any)
	if !ok {
		t.Fatalf("响应未带 stats: %v", body)
	}
	return st
}

func apiNum(t *testing.T, body map[string]any, key string) float64 {
	t.Helper()
	v, ok := body[key].(float64)
	if !ok {
		t.Fatalf("stats.%s 不是数字: %v", key, body[key])
	}
	return v
}

func TestAPIPersonStats(t *testing.T) {
	s := newTestServer(t)

	laoWang := apiCreatePersonMap(t, s, map[string]any{"name": "老王"})
	quiet := apiCreatePersonMap(t, s, map[string]any{"name": "清静人"})

	mustPost := func(path string, body map[string]any) {
		t.Helper()
		apiWantStatus(t, http.MethodPost, path, s.do(http.MethodPost, path, body), http.StatusOK)
	}
	mustPost("/api/v1/transactions", map[string]any{
		"person_id": laoWang["id"], "kind": "gift", "direction": "out",
		"amount_fen": 80000, "title": "儿子结婚", "occurred_at": "2026-01-05",
	})
	mustPost("/api/v1/transactions", map[string]any{
		"person_id": laoWang["id"], "kind": "gift", "direction": "in",
		"amount_fen": 30000, "title": "乔迁", "occurred_at": "2026-02-02",
	})
	mustPost("/api/v1/transactions", map[string]any{
		"person_id": laoWang["id"], "kind": "loan", "direction": "out",
		"amount_fen": 500000, "title": "借款", "occurred_at": "2026-03-03",
	})

	// 详情页：一次 GET /people/{id} 就够，不用前端再打第二个接口
	detail := apiPersonOf(s, laoWang["id"].(string))
	st := apiStats(t, detail)
	if apiNum(t, st, "gift_out_fen") != 80000 || apiNum(t, st, "gift_in_fen") != 30000 {
		t.Fatalf("详情礼金合计 = %v", st)
	}
	if net := apiNum(t, st, "net_fen"); net != 50000 {
		t.Fatalf("净额 = %v, want 50000（借款不进这本账）", net)
	}
	if got, _ := st["last_contact"].(string); got != "2026-03-03" {
		t.Fatalf("最近一次接触 = %q, want 2026-03-03（借款也算一次接触）", got)
	}

	// 人物卡：列表整页带出，避免 60 张卡发 60 次请求
	list := apiDecodeArray(t, "/api/v1/people?limit=50", s.do(http.MethodGet, "/api/v1/people?limit=50", nil))
	seen := map[string]map[string]any{}
	for _, it := range list {
		m, ok := it.(map[string]any)
		if !ok {
			t.Fatalf("列表元素不是对象: %v", it)
		}
		seen[m["id"].(string)] = m
	}
	if _, ok := seen[laoWang["id"].(string)]; !ok {
		t.Fatalf("列表里没有老王: %v", seen)
	}
	hot := apiStats(t, seen[laoWang["id"].(string)])
	if apiNum(t, hot, "net_fen") != 50000 {
		t.Fatalf("列表净额 = %v", hot)
	}
	// 没记录的人不带 stats 字段，前端按缺省跳过这一行
	if _, ok := seen[quiet["id"].(string)]["stats"]; ok {
		t.Fatalf("清静人不该带 stats: %v", seen[quiet["id"].(string)])
	}

	// 删掉礼金记录，数字要跟着回去，不能留一份缓存里的旧数
	txList := apiDecodeArray(t, "/api/v1/transactions", s.do(http.MethodGet, "/api/v1/transactions", nil))
	for _, it := range txList {
		m := it.(map[string]any)
		if m["kind"].(string) == "gift" {
			apiWantStatus(t, http.MethodDelete, "/api/v1/transactions/"+m["id"].(string),
				s.do(http.MethodDelete, "/api/v1/transactions/"+m["id"].(string), nil), http.StatusNoContent)
		}
	}
	after := apiStats(t, apiPersonOf(s, laoWang["id"].(string)))
	if apiNum(t, after, "gift_out_fen") != 0 || apiNum(t, after, "gift_in_fen") != 0 {
		t.Fatalf("删掉礼金后仍有余额: %v", after)
	}
	if got, _ := after["last_contact"].(string); got != "2026-03-03" {
		t.Fatalf("删掉礼金不该影响最近接触, got %q", got)
	}
}
