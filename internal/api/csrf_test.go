package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qiansi/app/internal/config"
	"github.com/qiansi/app/internal/store"
	"github.com/qiansi/app/internal/testdb"
)

// csrfServer 允许注入 CORS 白名单，其余与 apiTokenServer 相同。
func csrfServer(t *testing.T, token string, corsOrigins []string) *API {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir:     dir,
		DBPath:      dir + "/qiansi.db",
		Uploads:     dir + "/uploads",
		Backups:     dir + "/backups",
		Token:       token,
		CORSOrigins: corsOrigins,
	}
	return New(store.New(testdb.New(t)), cfg)
}

// csrfCall 发一次带自定义头的请求。httptest.NewRequest 的 Host 是 example.com，
// 同源 Origin 也照这个写。
func csrfCall(a *API, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	return rec
}

// postTarget 用一个真实存在的写端点探测守卫：过了守卫会进处理器拿到 401
// （令牌模式），被守卫拦下则是 403——两个码区分「到达」与「拦截」。
func TestCSRFGuard(t *testing.T) {
	const post = "/api/v1/people"

	t.Run("无 Origin 与 Sec-Fetch-Site（curl 等非浏览器客户端）放行", func(t *testing.T) {
		a := csrfServer(t, "secret", nil)
		if rec := csrfCall(a, http.MethodPost, post, map[string]string{"Authorization": "Bearer secret"}); rec.Code != http.StatusBadRequest {
			t.Fatalf("=> %d（%s），期望 400：应到达处理器", rec.Code, rec.Body.String())
		}
	})
	t.Run("同源 Origin 放行", func(t *testing.T) {
		a := csrfServer(t, "secret", nil)
		rec := csrfCall(a, http.MethodPost, post, map[string]string{
			"Origin":         "http://example.com",
			"Authorization":  "Bearer secret",
			"Sec-Fetch-Site": "same-origin",
			"Content-Type":   "application/json",
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("=> %d（%s），期望 400：应到达处理器", rec.Code, rec.Body.String())
		}
	})
	t.Run("跨站 Origin 拦截（no-cors 简单请求形态）", func(t *testing.T) {
		a := csrfServer(t, "", nil) // 默认无令牌模式：此前任意网页可写
		if rec := csrfCall(a, http.MethodPost, post, map[string]string{"Origin": "https://evil.example"}); rec.Code != http.StatusForbidden {
			t.Fatalf("=> %d，期望 403", rec.Code)
		}
	})
	t.Run("Origin 在 CORS 白名单放行", func(t *testing.T) {
		a := csrfServer(t, "", []string{"https://partner.example"})
		if rec := csrfCall(a, http.MethodPost, post, map[string]string{"Origin": "https://partner.example"}); rec.Code == http.StatusForbidden {
			t.Fatalf("=> 403，白名单来源不应被拦")
		}
	})
	t.Run("Sec-Fetch-Site cross-site 且无 Origin 拦截", func(t *testing.T) {
		a := csrfServer(t, "", nil)
		if rec := csrfCall(a, http.MethodPost, post, map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
			t.Fatalf("=> %d，期望 403", rec.Code)
		}
	})
	t.Run("Sec-Fetch-Site same-site（同站不同源）也拦截", func(t *testing.T) {
		a := csrfServer(t, "", nil)
		if rec := csrfCall(a, http.MethodPost, post, map[string]string{"Sec-Fetch-Site": "same-site"}); rec.Code != http.StatusForbidden {
			t.Fatalf("=> %d，期望 403", rec.Code)
		}
	})
	t.Run("Sec-Fetch-Site none（地址栏直达）放行", func(t *testing.T) {
		a := csrfServer(t, "", nil)
		if rec := csrfCall(a, http.MethodPost, post, map[string]string{"Sec-Fetch-Site": "none"}); rec.Code == http.StatusForbidden {
			t.Fatalf("=> 403，none 不应被拦")
		}
	})
	t.Run("读请求不受影响", func(t *testing.T) {
		a := csrfServer(t, "", nil)
		if rec := csrfCall(a, http.MethodGet, "/api/v1/health", map[string]string{"Origin": "https://evil.example"}); rec.Code != http.StatusOK {
			t.Fatalf("GET => %d，期望 200", rec.Code)
		}
	})
	t.Run("令牌模式下跨站 Origin 即便带对令牌也拦", func(t *testing.T) {
		a := csrfServer(t, "secret", nil)
		rec := csrfCall(a, http.MethodPost, post, map[string]string{
			"Origin":        "https://evil.example",
			"Authorization": "Bearer secret",
		})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("=> %d，期望 403（双保险）", rec.Code)
		}
	})
}
