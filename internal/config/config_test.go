package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestConfigLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("QIANSI_ADDR", "")
	t.Setenv("QIANSI_DATA_DIR", "")
	t.Setenv("QIANSI_TOKEN", "")
	t.Setenv("QIANSI_CORS_ORIGINS", "")

	cfg := Load()
	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", cfg.Addr)
	}
	if cfg.DataDir != "data" {
		t.Errorf("DataDir = %q, want data", cfg.DataDir)
	}
	if cfg.DBPath != filepath.Join("data", "qiansi.db") {
		t.Errorf("DBPath = %q", cfg.DBPath)
	}
	if cfg.Uploads != filepath.Join("data", "uploads") {
		t.Errorf("Uploads = %q", cfg.Uploads)
	}
	if cfg.Backups != filepath.Join("data", "backups") {
		t.Errorf("Backups = %q", cfg.Backups)
	}
	if cfg.Token != "" {
		t.Errorf("Token = %q, want empty", cfg.Token)
	}
	if cfg.CORSOrigins != nil {
		t.Errorf("CORSOrigins = %v, want nil", cfg.CORSOrigins)
	}
	// Load 应当把三个目录都建出来
	for _, d := range []string{cfg.DataDir, cfg.Uploads, cfg.Backups} {
		if st, err := os.Stat(filepath.Join(dir, d)); err != nil || !st.IsDir() {
			t.Errorf("dir %s not created: %v", d, err)
		}
	}
}

func TestConfigLoadFromEnv(t *testing.T) {
	base := t.TempDir()
	dataDir := filepath.Join(base, "mydata")
	t.Setenv("QIANSI_ADDR", ":9999")
	t.Setenv("QIANSI_DATA_DIR", dataDir)
	t.Setenv("QIANSI_TOKEN", "s3cret")
	t.Setenv("QIANSI_CORS_ORIGINS", "https://a.example, https://b.example ,")

	cfg := Load()
	if cfg.Addr != ":9999" {
		t.Errorf("Addr = %q, want :9999", cfg.Addr)
	}
	if cfg.Token != "s3cret" {
		t.Errorf("Token = %q, want s3cret", cfg.Token)
	}
	wantOrigins := []string{"https://a.example", "https://b.example"}
	if !reflect.DeepEqual(cfg.CORSOrigins, wantOrigins) {
		t.Errorf("CORSOrigins = %v, want %v", cfg.CORSOrigins, wantOrigins)
	}
	if cfg.DBPath != filepath.Join(dataDir, "qiansi.db") {
		t.Errorf("DBPath = %q", cfg.DBPath)
	}
	if cfg.Uploads != filepath.Join(dataDir, "uploads") {
		t.Errorf("Uploads = %q", cfg.Uploads)
	}
	if cfg.Backups != filepath.Join(dataDir, "backups") {
		t.Errorf("Backups = %q", cfg.Backups)
	}
	for _, d := range []string{dataDir, cfg.Uploads, cfg.Backups} {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			t.Errorf("dir %s not created: %v", d, err)
		}
	}
}

func TestConfigGetenv(t *testing.T) {
	const key = "QIANSI_TEST_GETENV_KEY"
	if got := getenv(key, "fallback"); got != "fallback" {
		t.Errorf("unset: getenv = %q, want fallback", got)
	}
	t.Setenv(key, "value")
	if got := getenv(key, "fallback"); got != "value" {
		t.Errorf("set: getenv = %q, want value", got)
	}
	// 空字符串视为未设置
	t.Setenv(key, "")
	if got := getenv(key, "fallback"); got != "fallback" {
		t.Errorf("empty: getenv = %q, want fallback", got)
	}
}

func TestConfigSplitList(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"whitespace only", "   ", nil},
		{"tabs newlines", "\t\n ", nil},
		{"single", "a", []string{"a"}},
		{"multiple", "a,b,c", []string{"a", "b", "c"}},
		{"spaces around", " a , b ", []string{"a", "b"}},
		{"trailing comma", "a,b,", []string{"a", "b"}},
		{"leading and empty middle", ",a,,b,", []string{"a", "b"}},
		{"only commas", ",,", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := splitList(c.in)
			if len(got) == 0 && len(c.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("splitList(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
