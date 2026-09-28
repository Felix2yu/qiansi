package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
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
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type", "X-Requested-With"},
	}))
	r.Use(api.recoverer)

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
	registerDashboard(r, api)

	return api
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.Router.ServeHTTP(w, r) }

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
