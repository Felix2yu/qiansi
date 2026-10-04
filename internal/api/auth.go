package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// 令牌模式下，浏览器自发起的请求（<img src>、window.location 触发的下载）带不上
// Authorization 头，只认令牌的鉴权会把头像和导出整条链路打死。
// 这里用一次带令牌的 POST 换一枚短期 cookie，并且只允许它放行只读方法：
// 写操作仍然必须显式带头，配合 SameSite=Strict，跨站页面借不到这条通道。
const (
	sessionCookieName = "qiansi_session"
	sessionTTL        = 7 * 24 * time.Hour
)

// sessionValue 生成 "<过期时间戳>.<签名>"。密钥就是访问令牌本身：
// cookie 里不含令牌，换令牌即让所有旧 cookie 当场失效，服务端也无需存状态。
func sessionValue(token string, exp int64) string {
	return strconv.FormatInt(exp, 10) + "." + sessionSig(token, exp)
}

func validSession(token, value string, now time.Time) bool {
	expStr, sig, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || now.Unix() >= exp {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(sessionSig(token, exp)))
}

func sessionSig(token string, exp int64) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *API) registerAuth(r chi.Router) {
	r.Post("/api/v1/auth/session", a.sessionCreate)
}

// sessionCreate 用请求头里的令牌换一枚只读会话 cookie。
// 未配置令牌时是空操作——那种模式下本来就没有鉴权。
func (a *API) sessionCreate(w http.ResponseWriter, r *http.Request) {
	if a.Cfg.Token == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	exp := time.Now().Add(sessionTTL).Unix()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionValue(a.Cfg.Token, exp),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Unix(exp, 0),
		MaxAge:   int(sessionTTL.Seconds()),
	})
	w.WriteHeader(http.StatusNoContent)
}
