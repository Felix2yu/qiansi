package api

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/qiansi/app/internal/config"
	"github.com/qiansi/app/internal/store"
)

type API struct {
	Store  *store.Store
	Cfg    *config.Config
	Router *chi.Mux
}

func New(s *store.Store, cfg *config.Config) *API {
	api := &API{Store: s, Cfg: cfg, Router: chi.NewRouter()}
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
	r.Use(api.authGuard)

	r.Get("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	api.registerSettings(r)
	api.registerCategories(r)
	api.registerTags(r)
	api.registerEventTypes(r)
	api.registerTaggings(r)
	api.registerPeople(r)
	api.registerRelationships(r)
	api.registerEvents(r)
	api.registerMemos(r)
	api.registerTransactions(r)
	api.registerAnniversaries(r)
	api.registerReminders(r)
	api.registerAttachments(r)
	api.registerNotify(r)
	api.registerBackup(r)
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
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got == "" {
			got = r.Header.Get("X-Qiansi-Token")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
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
	if err != nil {
		return def
	}
	return n
}
