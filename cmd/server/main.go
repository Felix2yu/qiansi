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
	bsched "github.com/qiansi/app/internal/backup/scheduler"
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

	// 自动备份调度器需要 store/cfg，与 HTTP 层共用同一份配置对象。
	// 不传 *sql.DB：恢复备份会换掉 Store 里的句柄，常驻调度器每次执行时现取才不会抱着旧库。
	backupRunner := bsched.New(st, cfg)

	// uploads static
	a.Router.Get("/uploads/*", http.StripPrefix("/uploads/", safeFileServer(cfg.Uploads)).ServeHTTP)

	// SPA static files (expect ./web/dist or ./web to contain built SPA, or placeholder)
	a.Router.Get("/*", spaHandler(cfg))

	// notifications scheduler（每日亲密快照 + 每日摘要推送）
	go notify.RunScheduler(ctx, st)

	// 自动备份调度器：周期/时间点由用户在设置页配置，默认每日 04:00。
	// 与 notify 分开跑 —— 两者的节奏（30 分钟 vs 30 秒）与配置来源都不同。
	go backupRunner.Run(ctx)

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

// uploads 只放位图。早前落盘的扩展名取自客户端文件名，目录里可能已经有 .html/.svg；
// 同源回显这些文件等于给自己上存储型 XSS，所以按扩展名再收一道口，其余一律不下发。
var uploadContentTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
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
		ct, ok := uploadContentTypes[strings.ToLower(filepath.Ext(p))]
		if !ok {
			http.Error(w, "unsupported file type", http.StatusUnsupportedMediaType)
			return
		}
		// 预设 Content-Type：FileServer 见它非空就不再自行嗅探；nosniff 挡住 polyglot 被当页面解析
		w.Header().Set("Content-Type", ct)
		w.Header().Set("X-Content-Type-Options", "nosniff")
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
					// Go 的 mime 表不认识 .webmanifest，会当成 text/plain 发出去，
					// 而 Safari 只接受 application/manifest+json，否则直接忽略清单。
					if strings.HasSuffix(path, ".webmanifest") {
						w.Header().Set("Content-Type", "application/manifest+json")
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
