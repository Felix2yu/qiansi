package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/qiansi/app/internal/config"
	"github.com/qiansi/app/internal/store"
	"github.com/qiansi/app/internal/testdb"
)

type testServer struct {
	t     *testing.T
	Store *store.Store
	Cfg   *config.Config
	*API
}

// newTestServer 建一个带完整临时数据目录（DBPath/Uploads/Backups）的 API 实例。
// 备份、附件上传等处理器会直接落盘，DataDir 必须指向 t.TempDir()。
func newTestServer(t *testing.T) *testServer {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir: dir,
		DBPath:  filepath.Join(dir, "qiansi.db"),
		Uploads: filepath.Join(dir, "uploads"),
		Backups: filepath.Join(dir, "backups"),
	}
	ts := &testServer{t: t, Store: store.New(testdb.New(t)), Cfg: cfg}
	ts.API = New(ts.Store, cfg)
	return ts
}

func (s *testServer) do(method, path string, body any, headers ...map[string]string) *httptest.ResponseRecorder {
	s.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			s.t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for _, h := range headers {
		for k, v := range h {
			req.Header.Set(k, v)
		}
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func (s *testServer) raw(method, path string, body []byte, contentType string, headers ...map[string]string) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, h := range headers {
		for k, v := range h {
			req.Header.Set(k, v)
		}
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

// get 断言 200 并返回响应体
func (s *testServer) get(path string) map[string]any {
	s.t.Helper()
	rec := s.do(http.MethodGet, path, nil)
	if rec.Code != http.StatusOK {
		s.t.Fatalf("GET %s => %d: %s", path, rec.Code, rec.Body.String())
	}
	return decodeMap(s.t, rec)
}

func decodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return m
}
