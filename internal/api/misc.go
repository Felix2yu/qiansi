package api

import (
	"errors"
	"fmt"
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

// 上传白名单：落盘扩展名只由嗅探出的真实类型决定，绝不用客户端文件名。
// 否则「PNG 头 + HTML 体」的 polyglot 会以 .html 存进同源目录，
// 被 /uploads 直接当页面渲染 —— 单用户自托管也躲不开自己踩自己的 XSS。
const maxUploadSize = 20 << 20

var uploadExts = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
	"image/bmp":  ".bmp",
}

func (a *API) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	// 先把整个请求体封顶，否则超限的表单会先落一份临时文件才被解析阶段发现。
	// multipart 的边界与字段头也要算进预算，所以留 1MB 余量。
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize+(1<<20))
	err := r.ParseMultipartForm(maxUploadSize)
	var tooBig *http.MaxBytesError
	if err != nil {
		if errors.As(err, &tooBig) {
			writeErr(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("图片超过 %d MB", maxUploadSize>>20))
			return
		}
		writeErr(w, 400, "表单解析失败: "+err.Error())
		return
	}
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
	ext, ok := uploadExts[mime]
	if !ok {
		// SVG 会带脚本、HTML/文本会带可执行内容，一律按「不是位图」拒掉
		writeErr(w, 400, "只接受 PNG / JPEG / GIF / WEBP / BMP 位图")
		return
	}
	stored := time.Now().UTC().Format("20060102150405") + randStr(8) + ext
	outPath := filepath.Join(a.Cfg.Uploads, stored)
	out, err := os.Create(outPath)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer out.Close()
	// io.Copy 不限量，嗅探通过后仍可塞进任意大的正文；CopyN 多读 1 字节用于判超限
	written, err := io.CopyN(out, file, maxUploadSize+1)
	if err != nil && !errors.Is(err, io.EOF) {
		os.Remove(outPath)
		writeErr(w, 500, err.Error())
		return
	}
	if written > maxUploadSize {
		os.Remove(outPath)
		writeErr(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("图片超过 %d MB", maxUploadSize>>20))
		return
	}
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
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"attachment": att, "url": "/uploads/" + stored})
}

func (a *API) deleteAttachment(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	att, err := a.Store.AttachmentDelete(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
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
