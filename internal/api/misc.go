package api

import (
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/qiansi/app/internal/config"
	"github.com/qiansi/app/internal/store"
)

func (a *API) registerAttachments(r chi.Router) {
	r.Route("/api/v1/attachments", func(r chi.Router) {
		r.Post("/", a.uploadAttachment)
		r.Delete("/{id}", a.deleteAttachment)
	})
	// uploads static path registered in main
}

func (a *API) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(10 << 20) // 10MB
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "file required")
		return
	}
	defer file.Close()
	mime := header.Header.Get("Content-Type")
	if !strings.HasPrefix(mime, "image/") {
		writeErr(w, 400, "only image/* accepted")
		return
	}
	ext := filepath.Ext(header.Filename)
	stored := time.Now().UTC().Format("20060102150405") + randStr(8) + ext
	outPath := filepath.Join(a.Cfg.Uploads, stored)
	out, err := os.Create(outPath)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer out.Close()
	n, _ := io.Copy(out, file)
	att := &store.Attachment{
		EntityType: r.FormValue("entity_type"),
		EntityID:   r.FormValue("entity_id"),
		FileName:   header.Filename,
		StoredName: stored,
		Mime:       mime,
		Size:       int(n),
	}
	if err := a.Store.AttachmentCreate(r.Context(), att); err != nil {
		os.Remove(outPath)
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"attachment": att, "url": "/uploads/" + stored})
}

func (a *API) deleteAttachment(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	att, err := a.Store.AttachmentDelete(r.Context(), id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	path := filepath.Join(a.Cfg.Uploads, att.StoredName)
	if filepath.Clean(path) != path {
		writeErr(w, 400, "bad path")
		return
	}
	os.Remove(path)
	w.WriteHeader(204)
}

func randStr(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func registerDashboard(r chi.Router, a *API) {
	r.Route("/api/v1/dashboard", func(r chi.Router) {
		r.Get("/stats", func(w http.ResponseWriter, r *http.Request) {
			d, err := a.Store.DashboardStats(r.Context())
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, d)
		})
		r.Get("/timeline", func(w http.ResponseWriter, r *http.Request) {
			list, err := a.Store.GlobalTimeline(r.Context(), parseIntQuery(r, "limit", 100))
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, list)
		})
		r.Get("/by-month", func(w http.ResponseWriter, r *http.Request) {
			list, err := a.Store.StatsByMonth(r.Context(), parseIntQuery(r, "months", 12))
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, list)
		})
		r.Get("/grade-distribution", func(w http.ResponseWriter, r *http.Request) {
			list, err := a.Store.GradeDistribution(r.Context())
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, list)
		})
		r.Get("/suggestions", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, a.suggest())
		})
	})
	_ = config.Config{} // silence unused
}
