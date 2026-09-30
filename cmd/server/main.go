package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/qiansi/app/internal/api"
	"github.com/qiansi/app/internal/config"
	"github.com/qiansi/app/internal/db"
	"github.com/qiansi/app/internal/notify"
	"github.com/qiansi/app/internal/store"
)

func main() {
	cfg := config.Load()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer database.Close()

	if err := db.RunMigrations(ctx, database); err != nil {
		log.Fatalf("migrations: %v", err)
	}
	st := store.New(database)

	a := api.New(st, cfg)

	// uploads static
	a.Router.Get("/uploads/*", http.StripPrefix("/uploads/", safeFileServer(cfg.Uploads)).ServeHTTP)

	// SPA static files (expect ./web/dist or ./web to contain built SPA, or placeholder)
	a.Router.Get("/*", spaHandler(cfg))

	// notifications scheduler（顺带每日归档一份数据库快照）
	go notify.RunScheduler(ctx, st, a.DailyBackupTick)

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      a,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listen on %s", cfg.Addr)
		errCh <- srv.ListenAndServe()
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		log.Printf("server stopped: %v", err)
	case s := <-sig:
		log.Printf("got %s, shutting down", s)
		cancel()
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShutdown()
		_ = srv.Shutdown(shutdownCtx)
	}
}

func safeFileServer(root string) http.Handler {
	fs := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// path.Clean + no absolute
		p := filepath.Clean(r.URL.Path)
		if filepath.IsAbs(p) {
			http.Error(w, "bad path", 400)
			return
		}
		r.URL.Path = p
		fs.ServeHTTP(w, r)
	})
}

func spaHandler(cfg *config.Config) http.HandlerFunc {
	// Look for built SPA in this order: web/dist, web, QIANSI_WEB_DIR
	candidates := []string{"web/dist", "web"}
	if v := os.Getenv("QIANSI_WEB_DIR"); v != "" {
		candidates = append([]string{v}, candidates...)
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "index.html")); err == nil {
			log.Printf("[web] serving SPA from %s", c)
			fs := http.Dir(c)
			fileServer := http.FileServer(fs)
			indexFile := filepath.Join(c, "index.html")
			return func(w http.ResponseWriter, r *http.Request) {
				path := r.URL.Path
				f, err := fs.Open(path)
				if err == nil {
					f.Close()
					// /assets/ 下的产物文件名带内容 hash，改版即换名，可以放心让浏览器永久缓存；
					// 其余（含 index.html）必须每次校验，否则前端更新发不出去。
					if strings.HasPrefix(path, "/assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					} else {
						w.Header().Set("Cache-Control", "no-cache")
					}
					fileServer.ServeHTTP(w, r)
					return
				}
				// fallback to index.html for SPA routes
				w.Header().Set("Cache-Control", "no-cache")
				http.ServeFile(w, r, indexFile)
			}
		}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>牵丝</title></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;max-width:560px;margin:8vh auto;padding:0 24px;color:#333">
<h1 style="margin-bottom:8px">牵丝</h1>
<p style="color:#666">后端 API 已启动，前端 SPA 尚未构建。</p>
<p><code>cd web &amp;&amp; pnpm install &amp;&amp; pnpm run build</code> 然后重启即可。</p>
<p>当前后端已在 <a href="/api/v1/health">/api/v1/health</a> 提供服务。</p>
</body></html>`))
	}
}
