package api

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	bsched "github.com/qiansi/app/internal/backup/scheduler"
	"github.com/qiansi/app/internal/config"
	"github.com/qiansi/app/internal/store"
)

type API struct {
	Store  *store.Store
	Cfg    *config.Config
	Router *chi.Mux
	// backup 自动备份调度器。必须与常驻循环（main 里 go Run）是同一个实例：
	// 各自 new 的话互斥锁也是各自的，「立即执行」与定时触发可以同时进 VACUUM INTO
	backup *bsched.Runner
}

func New(s *store.Store, cfg *config.Config) *API {
	api := &API{Store: s, Cfg: cfg, Router: chi.NewRouter(), backup: bsched.New(s, cfg)}
	r := api.Router
	// 默认不再对任意来源开放：CORS 只放行显式配置的来源（同源请求不带 Origin，不受影响），
	// 避免任意网站跨站读写本机数据。需要跨域时用 QIANSI_CORS_ORIGINS 指定。
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Requested-With"},
		AllowCredentials: false,
	}))
	r.Use(api.recoverer)
	// 文本类响应统一 gzip：前端产物去掉压缩后是 MB 级，公网访问时这是主要耗时。
	// 只压缩文本类型，图片等已压缩的内容不受影响。
	r.Use(middleware.Compress(5))
	r.Use(api.csrfGuard)
	r.Use(api.authGuard)

	r.Get("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	api.registerAuth(r)
	api.registerSettings(r)
	api.registerCategories(r)
	api.registerTags(r)
	api.registerEventTypes(r)
	api.registerTaggings(r)
	api.registerPeople(r)
	api.registerRelationships(r)
	api.registerEvents(r)
	api.registerHuozhi(r)
	api.registerMemos(r)
	api.registerTransactions(r)
	api.registerAnniversaries(r)
	api.registerReminders(r)
	api.registerRhythm(r)
	api.registerMeetings(r)
	api.registerTrash(r)
	api.registerAttachments(r)
	api.registerNotify(r)
	api.registerBackup(r)
	api.registerExport(r)
	registerDashboard(r, api)

	return api
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.Router.ServeHTTP(w, r) }

// authGuard 可选的访问令牌校验。
//
// 未配置 QIANSI_TOKEN 时完全放行，保持自托管单机场景的开箱即用；
// 一旦配置，所有 /api 与 /uploads 请求都必须携带正确的 Bearer 令牌。
// 静态资源（SPA 页面本身）不校验，因为页面不含数据。
func (a *API) authGuard(next http.Handler) http.Handler {
	token := a.Cfg.Token
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if !strings.HasPrefix(p, "/api/") && !strings.HasPrefix(p, "/uploads/") {
			next.ServeHTTP(w, r)
			return
		}
		if !a.authorized(r, token) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authorized 先认请求头里的令牌，再退一步认会话 cookie。
//
// cookie 通道只对 GET/HEAD 开放：图片与「点一下就下载」的链接没法带自定义头，
// 这是它们在令牌模式下唯一可用的通道；而它们都只读数据，不改动数据。
// 写请求仍必须带 Authorization，配合 cookie 的 SameSite=Strict，
// 第三方页面既借不到 cookie，也伪造不出带头的请求。
func (a *API) authorized(r *http.Request, token string) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" {
		got = r.Header.Get("X-Qiansi-Token")
	}
	if got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1 {
		return true
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	c, err := r.Cookie(sessionCookieName)
	return err == nil && validSession(token, c.Value, time.Now())
}

// csrfGuard 拦截浏览器发起的跨站写请求。
//
// 未配置 QIANSI_TOKEN 的默认模式下 API 对本机全开，而 CORS 中间件只决定
// 「响应能否被跨站脚本读到」，拦不住 no-cors 简单请求把数据写进来——
// 任意网页都能 POST /api/v1/import/json 把整库静默替换掉。
// 令牌模式下写请求必须带 Authorization 头（第三方页面借不到），本守卫是双保险。
//
// 判定依据浏览器自带的来源自述：Origin 与请求同 Host（或在显式配置的
// CORS 白名单里）、或 Sec-Fetch-Site 为 same-origin/none 则放行；
// 两个标都没有的是 curl 这类非浏览器客户端，不受影响。
func (a *API) csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if !a.writeOriginAllowed(r) {
			writeErr(w, http.StatusForbidden, "跨站请求被拒绝")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// writeOriginAllowed 判定一次非读请求是否来自允许的来源。
// 同 Host 比对覆盖反代直传（Host 原样转发）与 SPA 本机调用；
// Sec-Fetch-Site 兜住带 Origin 又比对不上的老浏览器。
func (a *API) writeOriginAllowed(r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		if u, err := url.Parse(origin); err == nil && u.Host != "" && u.Host == r.Host {
			return true
		}
		for _, o := range a.Cfg.CORSOrigins {
			if o == "*" || o == origin {
				return true
			}
		}
		// Origin 在，但既非同源也不在白名单——无论浏览器自述什么都拦下
		return false
	}
	// 无 Origin（非浏览器客户端，或老浏览器的同源表单）：看 Fetch Metadata
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return true
	default:
		// cross-site / same-site（同站不同源可被他人注册子域）都拦
		return false
	}
}

func (a *API) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[panic] %v", rec)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ===== helpers =====

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeNotFound 统一「找不到」的 404 文案。以前各处理函数随手写
// "not found" 或直接把 err.Error()（"sql: no rows in result set"）当响应体，
// 同一个语义在中文界面里冒出英文，还与 writeStoreErr 的中文 404 并存。
func writeNotFound(w http.ResponseWriter, what string) {
	writeErr(w, http.StatusNotFound, what+"不存在，可能已经在别处被删除")
}

// writeStoreErr 把 store 返回的错误翻译成状态码与中文文案。
//
// 三种情况分别处理：目标行不存在 → 404（以前是 500 甚至 200 假成功）；
// 约束冲突 → 400/409，这类是用户能自己纠正的，不该看到 SQLite 的英文原文；
// 其余保持 500 + 原文，便于排查真正的内部错误。
func writeStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeErr(w, http.StatusNotFound, "记录不存在，可能已经在别处被删除")
	case errors.Is(err, store.ErrRelationshipExists):
		writeErr(w, http.StatusConflict, "这两个联系人之间已经是这种关系了")
	case isConstraintErr(err, "UNIQUE"):
		writeErr(w, http.StatusConflict, "这个名字已经存在")
	case errors.Is(err, store.ErrRefMissing):
		writeErr(w, http.StatusBadRequest, "关联的记录不存在，可能已被删除")
	case isConstraintErr(err, "FOREIGN KEY"):
		writeErr(w, http.StatusBadRequest, "关联的记录不存在，可能已被删除；请先选好联系人或往来")
	default:
		// 未知故障不回传 err.Error()：SQLite 的原文（"database is locked"、
		// "NOT NULL constraint failed: …"）对用户没有可操作性，还可能带出
		// 内部结构。真码记服务端日志，响应只给一句人话。
		log.Printf("[api] store error: %v", err)
		writeErr(w, http.StatusInternalServerError, "服务器开小差了，请稍后重试")
	}
}

// isConstraintErr 识别 SQLite 的约束错误文本（驱动没有导出更细的错误类型）。
func isConstraintErr(err error, kind string) bool {
	return strings.Contains(err.Error(), kind+" constraint")
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func parseIntQuery(r *http.Request, name string, def int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		// 负数与 queryPositiveInt 的口径一致：不认识就退默认值。
		// 负 LIMIT 在 SQLite 里等价「不限行数」，负 OFFSET 则可能进到 Go 侧切片
		return def
	}
	return n
}
