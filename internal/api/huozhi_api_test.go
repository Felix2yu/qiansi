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

// fakeHuozhi 起一个只有读接口的假货殖，用最小的信封结构验证客户端解析。
// 真货殖的响应包了一层 {code,message,data}，这里不能简化掉——否则解析路径测不到。
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
		case r.URL.Path == "/api/integration/bills":
			if ids := r.URL.Query().Get("ids"); ids != "" {
				for _, id := range strings.Split(ids, ",") {
					if b, ok := byID[id]; ok {
						items = append(items, b)
					}
				}
			} else {
				kw := r.URL.Query().Get("keyword")
				for _, b := range bills {
					if kw == "" || strings.Contains(itoaAny(b["description"]), kw) {
						items = append(items, b)
					}
				}
			}
		case strings.HasPrefix(r.URL.Path, "/api/integration/bills/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/integration/bills/")
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
			"data": map[string]any{"items": items, "total": len(items), "page": 1, "page_size": 20},
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
		{"id": 101, "amount_fen": 8800, "type": "expense", "tx_date": "2026-10-20",
			"description": "茶馆", "category_name": "餐饮", "account_name": "现金", "book_name": "日常"},
		{"id": 102, "amount_fen": 1200, "type": "expense", "tx_date": "2026-10-20",
			"description": "打车", "category_name": "交通", "account_name": "微信", "book_name": "日常"},
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
	if v, _ := detail["expense_fen"].(float64); v != 10000 {
		t.Fatalf("开销 = %v, want 10000（两张账单之和）", detail["expense_fen"])
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
		{"id": 101, "amount_fen": 9900, "type": "expense", "tx_date": "2026-10-20", "description": "茶馆（涨价）"},
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
	if v, _ := s.get("/api/v1/events/" + evID)["expense_fen"].(float64); v != 11100 {
		t.Fatalf("刷新后开销 = %v, want 11100（9900 + 旧快照 1200）", v)
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
