package config

import (
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Addr     string
	DataDir  string
	DBPath   string
	Uploads  string
	Backups  string
	PublicUI bool
	// Token 非空时，/api 与 /uploads 需要 Bearer 鉴权（默认关闭以保持单机开箱即用）
	Token string
	// CORSOrigins 为允许跨域的来源；空表示只接受同源（浏览器同源请求不带 Origin）
	CORSOrigins []string
}

func Load() *Config {
	cfg := &Config{
		Addr:    getenv("QIANSI_ADDR", ":8080"),
		DataDir: getenv("QIANSI_DATA_DIR", "data"),
		Token:   getenv("QIANSI_TOKEN", ""),
	}
	cfg.CORSOrigins = splitList(getenv("QIANSI_CORS_ORIGINS", ""))
	cfg.DBPath = filepath.Join(cfg.DataDir, "qiansi.db")
	cfg.Uploads = filepath.Join(cfg.DataDir, "uploads")
	cfg.Backups = filepath.Join(cfg.DataDir, "backups")
	_ = os.MkdirAll(cfg.DataDir, 0o755)
	_ = os.MkdirAll(cfg.Uploads, 0o755)
	_ = os.MkdirAll(cfg.Backups, 0o755)
	return cfg
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// splitList 解析逗号分隔的白名单；空输入返回 nil（交由 cors 视为不允许跨域）。
func splitList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
