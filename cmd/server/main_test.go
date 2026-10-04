package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestUploadsRouteOnlyServesBitmaps 锁住 M8 的服务端出口：
// uploads 目录里混进可执行内容的文件（历史上落盘扩展名取自客户端文件名）
// 不能被同源渲染成页面。挂载方式与 main 保持一致。
func TestUploadsRouteOnlyServesBitmaps(t *testing.T) {
	root := t.TempDir()
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	files := map[string][]byte{
		"a.png":  png,
		"b.HTML": []byte("<html><script>alert(1)</script></html>"),
		"c.svg":  []byte("<svg xmlns=http://www.w3.org/2000/svg onload=alert(1)></svg>"),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), content, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	rr := chi.NewRouter()
	rr.Get("/uploads/*", http.StripPrefix("/uploads/", safeFileServer(root)).ServeHTTP)

	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		rr.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	rec := get("/uploads/a.png")
	if rec.Code != http.StatusOK {
		t.Fatalf("png => %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q，应由白名单指定而不是交给嗅探", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if rec.Body.Len() != len(png) {
		t.Fatalf("png 内容不符: %d 字节", rec.Body.Len())
	}

	// 非位图一律 415：大小写混写的扩展名也拦得住
	for _, path := range []string{"/uploads/b.HTML", "/uploads/c.svg", "/uploads/d.html"} {
		if rec := get(path); rec.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("%s => %d, want 415: %s", path, rec.Code, rec.Body.String())
		}
	}

	// 路径穿越仍然进不了目录之外
	if rec := get("/uploads/../../etc/passwd"); rec.Code == http.StatusOK {
		t.Fatal("穿越请求不应读到目录外的文件")
	}
}
