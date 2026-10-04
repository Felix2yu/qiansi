package api

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// ===== 令牌模式下的只读会话 cookie（M5）=====

func TestSessionValueRoundTrip(t *testing.T) {
	now := time.Now()
	exp := now.Add(time.Hour).Unix()
	v := sessionValue("secret", exp)
	if !validSession("secret", v, now) {
		t.Fatalf("自己签发的 cookie 判为无效: %q", v)
	}
	if validSession("other", v, now) {
		t.Fatal("换了令牌后旧 cookie 仍被判有效")
	}
	// 篡改签名末位
	bad := v[:len(v)-1] + "0"
	if v[len(v)-1] == '0' {
		bad = v[:len(v)-1] + "1"
	}
	if validSession("secret", bad, now) {
		t.Fatal("改了签名的 cookie 仍被判有效")
	}
	for _, malformed := range []string{"", "no-dot", "abc.sig", strings.Split(v, ".")[0] + ".", strings.Split(v, ".")[0] + ".deadbeef"} {
		if validSession("secret", malformed, now) {
			t.Fatalf("畸形值 %q 被判有效", malformed)
		}
	}
}

func TestSessionExpires(t *testing.T) {
	now := time.Now()
	if validSession("secret", sessionValue("secret", now.Add(-time.Minute).Unix()), now) {
		t.Fatal("过期 cookie 仍被接受")
	}
	// 正好到点即失效，边界上不多放行一秒
	if validSession("secret", sessionValue("secret", now.Unix()), now) {
		t.Fatal("到期瞬间仍被接受")
	}
}

// issueSession 走一遍真实签发流程，返回签发出的 cookie。
func issueSession(t *testing.T, a *API, authHeader map[string]string) *http.Cookie {
	t.Helper()
	rec := apiCall(a, http.MethodPost, "/api/v1/auth/session", authHeader)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /auth/session => %d: %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	t.Fatalf("响应里没有 %s cookie，Set-Cookie=%q", sessionCookieName, rec.Result().Header.Get("Set-Cookie"))
	return nil
}

func TestAPIAuthSessionFlow(t *testing.T) {
	a := apiTokenServer(t, "secret")

	// 签发本身必须用真令牌：cookie 换不到 cookie，否则一次泄露就能自我续期
	if rec := apiCall(a, http.MethodPost, "/api/v1/auth/session", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("无令牌签发会话 => %d，期望 401", rec.Code)
	}

	cookie := issueSession(t, a, map[string]string{"Authorization": "Bearer secret"})
	if !cookie.HttpOnly {
		t.Fatal("会话 cookie 必须 HttpOnly，否则页面脚本能把它读走")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("SameSite = %v，期望 Strict（第三方页面借不到这条通道）", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Fatalf("Path = %q，期望 /", cookie.Path)
	}
	if strings.Contains(cookie.Value, "secret") {
		t.Fatalf("cookie 值里不应出现原始令牌: %q", cookie.Value)
	}
	withCookie := map[string]string{"Cookie": sessionCookieName + "=" + cookie.Value}

	// 只读通道：头像 <img> 与「点一下就下载」的链接
	if rec := apiCall(a, http.MethodGet, "/api/v1/health", withCookie); rec.Code != http.StatusOK {
		t.Fatalf("GET 带会话 cookie => %d，期望 200: %s", rec.Code, rec.Body.String())
	}
	// HEAD 没有被任何路由注册（chi 返回 405），这里要验的是鉴权放行，不是路由
	if rec := apiCall(a, http.MethodHead, "/api/v1/health", withCookie); rec.Code == http.StatusUnauthorized {
		t.Fatalf("HEAD 带会话 cookie 仍被拒: %s", rec.Body.String())
	}
	// 测试实例没挂 /uploads 静态路由，放行后到不了鉴权那一层，只会 404
	if rec := apiCall(a, http.MethodGet, "/uploads/a.png", withCookie); rec.Code == http.StatusUnauthorized {
		t.Fatalf("GET /uploads 带会话 cookie 仍被拒: %s", rec.Body.String())
	}

	// 写操作不认 cookie —— 这是 cookie 通道不构成 CSRF 面的前提
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if rec := apiCall(a, m, "/api/v1/health", withCookie); rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s 只带会话 cookie => %d，期望 401", m, rec.Code)
		}
	}

	// 换令牌后旧 cookie 立即作废（密钥就是令牌本身）
	other := apiTokenServer(t, "another-token")
	if rec := apiCall(other, http.MethodGet, "/api/v1/health", withCookie); rec.Code != http.StatusUnauthorized {
		t.Fatalf("令牌不同却接受了旧 cookie => %d，期望 401", rec.Code)
	}
}

func TestAPIAuthSessionNoTokenMode(t *testing.T) {
	// 未配置令牌时是空操作：不该发 cookie，也不该要求鉴权
	a := apiTokenServer(t, "")
	rec := apiCall(a, http.MethodPost, "/api/v1/auth/session", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("无令牌模式 POST /auth/session => %d，期望 204", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			t.Fatal("无令牌模式下不应签发会话 cookie")
		}
	}
}
