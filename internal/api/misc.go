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
	"github.com/qiansi/app/internal/notify"
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
	// Content-Type 由客户端提供、可伪造，改用前 512 字节嗅探真实类型
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	head = head[:n]
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	mime := http.DetectContentType(head)
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
	written, _ := io.Copy(out, file)
	att := &store.Attachment{
		EntityType: r.FormValue("entity_type"),
		EntityID:   r.FormValue("entity_id"),
		FileName:   header.Filename,
		StoredName: stored,
		Mime:       mime,
		Size:       int(written),
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

// registerNotify 提供通知渠道的测试入口。
func (a *API) registerNotify(r chi.Router) {
	r.Post("/api/v1/notify/test", a.notifyTest)
}

// notifyTest 立即按当前配置推送一条测试消息。
// 之前前端的「测试推送」只保存了配置却提示已触发，属于误导。
func (a *API) notifyTest(w http.ResponseWriter, r *http.Request) {
	urlsStr, _ := a.Store.SettingGet(r.Context(), "apprise_urls")
	if strings.TrimSpace(urlsStr) == "" {
		writeErr(w, 400, "尚未配置 apprise 推送地址")
		return
	}
	urls := strings.FieldsFunc(urlsStr, func(r rune) bool { return r == ',' || r == '\n' })
	if len(urls) == 0 {
		writeErr(w, 400, "尚未配置 apprise 推送地址")
		return
	}
	if err := notify.Push(r.Context(), urls, "牵丝 · 测试推送", "这是一条测试消息，收到说明渠道配置正确。"); err != nil {
		writeErr(w, 500, "推送失败: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "channels": len(urls)})
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
