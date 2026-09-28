package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	Addr     string
	DataDir  string
	DBPath   string
	Uploads  string
	Backups  string
	PublicUI bool
}

func Load() *Config {
	cfg := &Config{
		Addr:    getenv("QIANSI_ADDR", ":8080"),
		DataDir: getenv("QIANSI_DATA_DIR", "data"),
	}
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
