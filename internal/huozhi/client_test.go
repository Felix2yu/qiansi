package huozhi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 货殖对外只有 /api/public/bills 一组接口（幕间与牵丝共用），
// 字段口径以真机响应为准：金额双写、日期带时分秒、字典给名称。
func fakePublicBills(t *testing.T, key string, data map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != key {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":401,"message":"bad key"}`))
			return
		}
		if r.URL.Path == "/api/public/bills/404" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":404,"message":"账单不存在"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "ok", "data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClient_NormalizesPublicContract(t *testing.T) {
	// 未升级的货殖：只有 amount（元），没有 amount_fen
	bill := map[string]any{
		"id": 101, "amount": 88.1, "currency": "CNY", "type": "expense",
		"tx_date": "2026-10-20 12:30:00", "description": "茶馆",
		"category": "餐饮", "account": "现金", "book": "日常", "merchant": "",
	}
	srv := fakePublicBills(t, "k", map[string]any{
		"list":       []any{bill},
		"pagination": map[string]any{"page": 1, "page_size": 20, "total": 1},
	})

	c, err := New(srv.URL, "k")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := c.List(context.Background(), Query{Type: "expense", PageSize: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("账单数 = %d, want 1", len(res.Items))
	}
	got := res.Items[0]
	// 88.1 元 → 8810 分，不能出现 8809（浮点截断）
	if got.AmountFen != 8810 {
		t.Fatalf("AmountFen = %d, want 8810（元→分需四舍五入）", got.AmountFen)
	}
	if got.TxDate != "2026-10-20" {
		t.Fatalf("TxDate = %q, want 日期部分", got.TxDate)
	}
	if got.CategoryName != "餐饮" || got.AccountName != "现金" || got.BookName != "日常" {
		t.Fatalf("字典名称未解析: %+v", got)
	}
	if res.Total != 1 {
		t.Fatalf("Total = %d, want 1", res.Total)
	}
}

func TestClient_Errors(t *testing.T) {
	srv := fakePublicBills(t, "k", map[string]any{"list": []any{}, "pagination": map[string]any{}})

	c, err := New(srv.URL, "k")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.One(context.Background(), "404"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的账单应返回 ErrNotFound, got %v", err)
	}

	bad, _ := New(srv.URL, "wrong")
	if _, err := bad.List(context.Background(), Query{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("错密钥应返回 ErrUnauthorized, got %v", err)
	}

	if _, err := New("", "k"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("缺地址应返回 ErrNotConfigured, got %v", err)
	}
}

func TestClient_NormalizesBaseURL(t *testing.T) {
	cases := map[string]string{
		"https://hz.example.com":      "https://hz.example.com",
		"https://hz.example.com/":     "https://hz.example.com",
		"https://hz.example.com/api":  "https://hz.example.com",
		"https://hz.example.com/api/": "https://hz.example.com",
		"hz.example.com":              "https://hz.example.com",
	}
	for in, want := range cases {
		c, err := New(in, "k")
		if err != nil {
			t.Fatalf("New(%q): %v", in, err)
		}
		if c.Base() != want {
			t.Fatalf("New(%q).Base() = %q, want %q", in, c.Base(), want)
		}
	}
}
