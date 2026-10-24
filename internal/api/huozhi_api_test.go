package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// ===== 货殖账单关联（1:N）=====

// fakeHuozhi 起一个只有读接口的假货殖，复刻 /api/public/bills 的真实契约：
// 信封 {code,message,data}，列表放在 data.list、分页在 data.pagination，
// 金额同时给「元」与「分」，字典名称在 category/account/book。
// 这层结构不能简化——否则客户端的解析路径根本没被测到。
func fakeHuozhi(t *testing.T, key string, bills []map[string]any) *httptest.Server {
	t.Helper()
	byID := map[string]map[string]any{}
	for _, b := range bills {
		byID[itoaAny(b["id"])] = b
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != key {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":401,"message":"bad key"}`))
			return
		}
		items := []map[string]any{}
		switch {
		case r.URL.Path == "/api/public/bills":
			if ids := r.URL.Query().Get("ids"); ids != "" {
				for _, id := range strings.Split(ids, ",") {
					if b, ok := byID[id]; ok {
						items = append(items, b)
					}
				}
			} else {
				kw := r.URL.Query().Get("keyword")
				typ := r.URL.Query().Get("type")
				for _, b := range bills {
					if kw != "" && !strings.Contains(itoaAny(b["description"]), kw) {
						continue
					}
					if typ != "" && itoaAny(b["type"]) != typ {
						continue
					}
					items = append(items, b)
				}
			}
		case strings.HasPrefix(r.URL.Path, "/api/public/bills/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/public/bills/")
			b, ok := byID[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"code":404,"message":"not found"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "ok", "data": b})
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "message": "ok",
			"data": map[string]any{
				"list":       items,
				"pagination": map[string]any{"page": 1, "page_size": 20, "total": len(items)},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func itoaAny(v any) string {
	switch n := v.(type) {
	case int:
		return strconv.Itoa(n)
	case string:
		return n
	default:
		return ""
	}
}

func TestAPAHuozhiBillLink(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	key := "k-test"

	srv := fakeHuozhi(t, key, []map[string]any{
		{"id": 101, "amount": 88, "amount_fen": 8800, "type": "expense", "tx_date": "2026-10-20 12:30:00",
			"description": "茶馆", "category": "餐饮", "account": "现金", "book": "日常"},
		{"id": 102, "amount": 12.5, "amount_fen": 1250, "type": "expense", "tx_date": "2026-10-20 18:00:00",
			"description": "打车", "category": "交通", "account": "微信", "book": "日常"},
	})
	if err := s.Store.SettingSet(ctx, settingHuozhiBase, srv.URL); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SettingSet(ctx, settingHuozhiKey, key); err != nil {
		t.Fatal(err)
	}

	host := apiCreatePersonMap(t, s, map[string]any{"name": "老王"})
	rec := s.do(http.MethodPost, "/api/v1/events/", map[string]any{
		"title": "喝茶", "event_date": "2026-10-20",
		"participant_ids": []any{host["id"]},
	})
	apiWantStatus(t, http.MethodPost, "/api/v1/events/", rec, http.StatusOK)
	evID, _ := decodeMap(t, rec)["id"].(string)

	// 一次关联两张，其中 999 在货殖里不存在 → 必须如实回报 missing
	rec = s.do(http.MethodPost, "/api/v1/events/"+evID+"/bills", map[string]any{
		"remote_ids": []string{"101", "102", "999"},
	})
	apiWantStatus(t, http.MethodPost, "/api/v1/events/"+evID+"/bills", rec, http.StatusOK)
	body := decodeMap(t, rec)
	if n := len(body["bills"].([]any)); n != 2 {
		t.Fatalf("关联后账单数 = %d, want 2: %v", n, body)
	}
	missing, _ := body["missing"].([]any)
	if len(missing) != 1 || missing[0] != "999" {
		t.Fatalf("missing = %v, want [999]", body["missing"])
	}

	detail := s.get("/api/v1/events/" + evID)
	if v, _ := detail["expense_fen"].(float64); v != 10050 {
		t.Fatalf("开销 = %v, want 10050（8800 + 1250 两张账单之和）", detail["expense_fen"])
	}
	bills, _ := detail["bills"].([]any)
	if len(bills) != 2 {
		t.Fatalf("详情里账单数 = %d, want 2", len(bills))
	}
	first, _ := bills[0].(map[string]any)
	if first["category_name"] == "" || first["remote_id"] == "" {
		t.Fatalf("快照字段缺失: %v", first)
	}

	// 有账单在手填开销 → 409，别让两套口径同时写
	rec = s.do(http.MethodPut, "/api/v1/events/"+evID, map[string]any{
		"title": "喝茶", "event_date": "2026-10-20",
		"participant_ids": []any{host["id"]}, "expense_fen": 5000,
	})
	apiWantStatus(t, http.MethodPut, "/api/v1/events/"+evID, rec, http.StatusConflict)

	// 更新时不带开销（前端只读态的提交形态）应当放行
	rec = s.do(http.MethodPut, "/api/v1/events/"+evID, map[string]any{
		"title": "喝茶（改）", "event_date": "2026-10-20",
		"participant_ids": []any{host["id"]}, "expense_fen": 0,
	})
	apiWantStatus(t, http.MethodPut, "/api/v1/events/"+evID, rec, http.StatusOK)

	// 刷新：假货殖价格变了，快照跟着变
	srv.Close()
	srv2 := fakeHuozhi(t, key, []map[string]any{
		// 只给「元」不给「分」：模拟未升级的货殖，客户端要按 99 元折成 9900 分
		{"id": 101, "amount": 99, "type": "expense", "tx_date": "2026-10-20 12:30:00", "description": "茶馆（涨价）"},
		// 102 在货殖里被删了
	})
	_ = s.Store.SettingSet(ctx, settingHuozhiBase, srv2.URL)
	rec = s.do(http.MethodPost, "/api/v1/events/"+evID+"/bills/refresh", nil)
	apiWantStatus(t, http.MethodPost, "/api/v1/events/"+evID+"/bills/refresh", rec, http.StatusOK)
	refreshed := decodeMap(t, rec)
	stale, _ := refreshed["stale"].([]any)
	if len(stale) != 1 || stale[0] != "102" {
		t.Fatalf("stale = %v, want [102]", refreshed["stale"])
	}
	if v, _ := s.get("/api/v1/events/" + evID)["expense_fen"].(float64); v != 11150 {
		t.Fatalf("刷新后开销 = %v, want 11150（9900 + 旧快照 1250）", v)
	}

	// 解除货殖里已被删掉的那条（102），剩下 101 的 9900
	var target string
	for _, it := range refreshed["bills"].([]any) {
		m := it.(map[string]any)
		if m["remote_id"] == "102" {
			target, _ = m["id"].(string)
		}
	}
	if target == "" {
		t.Fatalf("没找到 remote_id=102 的关联: %v", refreshed["bills"])
	}
	rec = s.do(http.MethodDelete, "/api/v1/events/"+evID+"/bills/"+target, nil)
	apiWantStatus(t, http.MethodDelete, "/api/v1/events/"+evID+"/bills/"+target, rec, http.StatusOK)
	if v, _ := s.get("/api/v1/events/" + evID)["expense_fen"].(float64); v != 9900 {
		t.Fatalf("解除后开销 = %v, want 9900", v)
	}
}

func TestAPAHuozhiNotConfigured(t *testing.T) {
	s := newTestServer(t)
	rec := s.do(http.MethodGet, "/api/v1/huozhi/bills", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未配置时期望 400，实际 %d: %s", rec.Code, rec.Body.String())
	}
	status := s.get("/api/v1/huozhi/status")
	if status["configured"] != false || status["ok"] != false {
		t.Fatalf("未配置时状态不对: %v", status)
	}
}

func TestAPAHuozhiWrongKey(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	srv := fakeHuozhi(t, "right-key", nil)
	_ = s.Store.SettingSet(ctx, settingHuozhiBase, srv.URL)
	_ = s.Store.SettingSet(ctx, settingHuozhiKey, "wrong-key")

	rec := s.do(http.MethodGet, "/api/v1/huozhi/bills", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("密钥错误期望 502，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "API Key") {
		t.Fatalf("错误提示没提到 API Key: %s", rec.Body.String())
	}
	status := s.get("/api/v1/huozhi/status")
	if status["configured"] != true || status["ok"] != false {
		t.Fatalf("密钥错误时状态不对: %v", status)
	}
}

// 代理搜索必须把货殖的字段名（category/account/book）翻成牵丝前端使用的
// category_name/…，否则关联选择器里分类与账户会静默变成空白。
func TestAPAHuozhiSearchContract(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	key := "k-search"
	srv := fakeHuozhi(t, key, []map[string]any{
		{"id": 201, "amount": 30, "amount_fen": 3000, "type": "expense", "tx_date": "2026-10-19 09:00:00",
			"description": "早饭", "category": "餐饮", "account": "现金", "book": "日常"},
	})
	_ = s.Store.SettingSet(ctx, settingHuozhiBase, srv.URL)
	_ = s.Store.SettingSet(ctx, settingHuozhiKey, key)

	body := s.get("/api/v1/huozhi/bills?q=&type=expense")
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("搜索结果数 = %d, want 1: %v", len(items), body)
	}
	it := items[0].(map[string]any)
	if it["category_name"] != "餐饮" || it["account_name"] != "现金" || it["book_name"] != "日常" {
		t.Fatalf("字段名未映射到前端契约: %v", it)
	}
	if _, leaked := it["category"]; leaked {
		t.Fatalf("不应透传货殖的上游字段名: %v", it)
	}
	if it["amount_fen"].(float64) != 3000 || it["tx_date"] != "2026-10-19" {
		t.Fatalf("金额或日期不符合预期: %v", it)
	}
}
