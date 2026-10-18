package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/qiansi/app/internal/api"
	"github.com/qiansi/app/internal/backup"
	bsched "github.com/qiansi/app/internal/backup/scheduler"
	"github.com/qiansi/app/internal/config"
	"github.com/qiansi/app/internal/db"
	"github.com/qiansi/app/internal/notify"
	"github.com/qiansi/app/internal/store"
)

func main() {
	// 外部备份工具（docker-db-auto-backup）经 docker exec 调用 `qiansi backup`，
	// 把一致性快照写到 stdout。必须拦在最前面——早于 flag 解析、config 加载、存储初始化、
	// 迁移、后台调度器与 HTTP 服务：导出进程只做导出这一件事。
	// 尤其不能走 config.Load，它会 MkdirAll：库不存在时导出要报错，而不是先建目录再产一个空库。
	// stdout 只承载产物字节，日志一律留在 stderr（Go 的 log 默认就写 stderr）。
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		if err := runBackup(os.Args[2:], os.Stdout); err != nil {
			log.Fatalf("备份导出失败: %v", err)
		}
		return
	}

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

// runBackup 实现 `qiansi backup`：把一份备份写到 w（由调用方给 os.Stdout）。
//
// db（默认）= SQLite 自身的一致性快照（VACUUM INTO），产物只有数据库；
// zip = 快照 + uploads 目录，牵丝的图片存在库外，只有 zip 形态是「一份产物还原全部」。
// 默认取 db 是为了对上备份工具的默认扩展名：改用 zip 必须同时告诉它 BACKUP_SQLITE_EXT=zip，
// 否则工具会拿 SQLite 魔数去校验 zip 产物，这一轮备份判失败（不静默存坏备份）。
//
// 库路径沿用应用自己的解析（QIANSI_DATA_DIR，默认 data/qiansi.db）；
// -db 只是「导出别处的库」留的口子，uploads 目录按它的父目录一起算。
func runBackup(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("qiansi backup", flag.ContinueOnError)
	fs.SetOutput(os.Stderr) // 用法错误与 -h 只能进 stderr，stdout 留给产物
	dbPath := fs.String("db", "", "SQLite 数据文件路径（默认按 QIANSI_DATA_DIR 解析）")
	format := fs.String("format", "db", "导出格式：db（SQLite 一致性快照）或 zip（快照 + uploads）")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil // -h 已自行打印到 stderr，不算失败
		}
		return err
	}

	cfg := config.Resolve()
	if *dbPath != "" {
		cfg.DBPath = *dbPath
		cfg.Uploads = filepath.Join(filepath.Dir(*dbPath), "uploads")
	}

	// 只读式打开：不建目录、不跑迁移。库打不开就直接失败，交出一份空备份比不备份更糟。
	database, err := db.OpenForExport(cfg.DBPath)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx := context.Background()
	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "db":
		return backup.WriteSnapshot(ctx, database, cfg.DBPath, w)
	case "zip":
		return backup.WriteZIP(ctx, database, cfg.DBPath, cfg.Uploads, w)
	default:
		return fmt.Errorf("未知格式 %q（可选 db / zip）", *format)
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
