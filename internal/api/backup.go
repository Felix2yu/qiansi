package api

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/qiansi/app/internal/backup"
	bsched "github.com/qiansi/app/internal/backup/scheduler"
	"github.com/qiansi/app/internal/db"
)

// registerBackup 提供数据库快照导出、恢复与归档备份。
//
// 之前设置页的「导出数据库」按钮指向 /api/v1/backup/export，而该路由并不存在，
// 请求会被 SPA 的 /* 兜底命中，用户下载到的是 index.html —— 以为备份了，其实没有。
func (a *API) registerBackup(r chi.Router) {
	r.Route("/api/v1/backup", func(r chi.Router) {
		r.Get("/export", a.backupExport)
		r.Post("/restore", a.backupRestore)
		r.Get("/list", a.backupList)
		r.Post("/snapshot", a.backupSnapshot)
		// 自动备份：配置读写 + 状态查询 + 立即执行
		r.Get("/auto", a.backupAutoGet)
		r.Put("/auto", a.backupAutoSet)
		r.Post("/auto/run", a.backupAutoRun)
	})
}

// backupRunner 构造自动备份调度器。API 每次请求现构造：
// Runner 只持有 store/cfg 两个指针，无内部可变状态（并发控制靠自身的 mutex），
// 这样避免在 New() 里多一个字段依赖，也便于测试直接构造。
func (a *API) backupRunner() *bsched.Runner {
	return bsched.New(a.Store, a.Cfg)
}

// snapshotDB 生成一份一致性快照到 dest（委托给 backup 包，与调度器共用实现）。
func (a *API) snapshotDB(ctx context.Context, dest string) error {
	return backup.Snapshot(ctx, a.Store.CurrentDB(), a.Cfg.DBPath, dest)
}

func (a *API) backupExport(w http.ResponseWriter, r *http.Request) {
	// 临时文件放 DataDir 而不是 Backups：后者是归档目录，混入导出的中间产物
	// 会让「归档列表」出现非归档文件（虽不匹配 qiansi- 前缀而不被裁剪，但语义上不该同处一地）。
	tmp := filepath.Join(a.Cfg.DataDir, "export-"+time.Now().Format("20060102150405")+".db")
	if err := a.snapshotDB(r.Context(), tmp); err != nil {
		writeErr(w, 500, "生成快照失败: "+err.Error())
		return
	}
	defer os.Remove(tmp)
	f, err := os.Open(tmp)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer f.Close()
	name := "qiansi-" + time.Now().Format("20060102-150405") + ".db"
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeContent(w, r, name, time.Now(), f)
}

// backupSnapshot 在数据目录的 backups/ 下留一份归档快照。
func (a *API) backupSnapshot(w http.ResponseWriter, r *http.Request) {
	path, err := a.archiveSnapshot(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"path": path})
}

func (a *API) archiveSnapshot(ctx context.Context) (string, error) {
	return backup.Archive(ctx, a.Store.CurrentDB(), a.Cfg.DBPath, a.Cfg.Backups)
}

func (a *API) backupList(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(a.Cfg.Backups)
	if err != nil {
		writeJSON(w, 200, []map[string]any{})
		return
	}
	type item struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		Time string `json:"time"`
	}
	list := []item{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		list = append(list, item{Name: e.Name(), Size: info.Size(), Time: info.ModTime().Format("2006-01-02 15:04")})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name > list[j].Name })
	if len(list) > 20 {
		list = list[:20]
	}
	writeJSON(w, 200, list)
}

// backupRestore 用上传的数据库文件替换当前库。
//
// 流程：校验是 SQLite → 归档当前库 → 关闭连接 → 替换 → 重新打开并跑迁移 → 换掉 Store.DB。
// 恢复是破坏性操作，必须先把现有库另存，失败时可人工找回。
func (a *API) backupRestore(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeErr(w, 400, "上传解析失败: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "file required")
		return
	}
	defer file.Close()

	head := make([]byte, 16)
	n, _ := io.ReadFull(file, head)
	if n < 16 || string(head[:15]) != "SQLite format 3" {
		writeErr(w, 400, "不是有效的 SQLite 数据库文件")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeErr(w, 500, err.Error())
		return
	}

	// 1) 落盘到临时文件
	tmp := filepath.Join(a.Cfg.DataDir, "restore-"+time.Now().Format("20060102150405")+".db")
	out, err := os.Create(tmp)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		os.Remove(tmp)
		writeErr(w, 500, err.Error())
		return
	}
	out.Close()

	// 2) 用临时库做一次健全性检查（能打开、能查 people 表）
	probe, err := db.Open(tmp)
	if err != nil {
		os.Remove(tmp)
		writeErr(w, 400, "数据库无法打开: "+err.Error())
		return
	}
	var cnt int
	if err := probe.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM people").Scan(&cnt); err != nil {
		probe.Close()
		os.Remove(tmp)
		writeErr(w, 400, "数据库结构不符合预期: "+err.Error())
		return
	}
	probe.Close()

	// 3) 归档当前库，便于回滚
	if _, err := os.Stat(a.Cfg.DBPath); err == nil {
		keep := filepath.Join(a.Cfg.Backups, "before-restore-"+time.Now().Format("20060102-150405")+".db")
		_ = a.snapshotDB(r.Context(), keep)
	}

	// 4) 关闭连接并替换
	if err := a.Store.CurrentDB().Close(); err != nil {
		log.Printf("[backup] close db: %v", err)
	}
	if err := os.Rename(tmp, a.Cfg.DBPath); err != nil {
		writeErr(w, 500, "替换数据库失败: "+err.Error())
		return
	}
	// WAL 边车文件属于旧库，必须一并清除
	_ = os.Remove(a.Cfg.DBPath + "-wal")
	_ = os.Remove(a.Cfg.DBPath + "-shm")

	// 5) 重新打开并补齐迁移
	fresh, err := db.Open(a.Cfg.DBPath)
	if err != nil {
		writeErr(w, 500, "重新打开数据库失败: "+err.Error())
		return
	}
	if err := db.RunMigrations(r.Context(), fresh); err != nil {
		writeErr(w, 500, "迁移失败: "+err.Error())
		return
	}
	// 换句柄必须走 UseDB：常驻的自动备份调度器每次执行时现取，
	// 直接改字段会让它继续握着上面刚 Close 掉的旧库。
	a.Store.UseDB(fresh)
	log.Printf("[backup] restored from %s (%d people)", header.Filename, cnt)
	writeJSON(w, 200, map[string]any{"ok": true, "people": cnt})
}

// ===== 自动备份 =====

// backupAutoGet 返回自动备份配置与执行状态（含下次执行时间）。
func (a *API) backupAutoGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.backupRunner().Status(r.Context(), time.Now()))
}

// backupAutoSet 保存自动备份配置。
//
// 频率用路径语义之外的显式字段传输，非法值由 Schedule.Normalize 兜底为每日，
// 因此这里不需要额外的 400 分支 —— 前端拿到的是归一化后的结果。
func (a *API) backupAutoSet(w http.ResponseWriter, r *http.Request) {
	var s backup.Schedule
	if err := decode(r, &s); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	runner := a.backupRunner()
	if err := runner.SaveSchedule(r.Context(), s); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// 配置变了，旧的排期时刻已失效，让下一次 tick 按新配置重算
	writeJSON(w, 200, runner.Status(r.Context(), time.Now()))
}

// backupAutoRun 立即执行一次备份并回传最新状态。失败时返回 500 与错误文本。
func (a *API) backupAutoRun(w http.ResponseWriter, r *http.Request) {
	runner := a.backupRunner()
	path, err := runner.RunNow(r.Context(), time.Now())
	if err != nil {
		writeErr(w, 500, "备份失败: "+err.Error())
		return
	}
	st := runner.Status(r.Context(), time.Now())
	writeJSON(w, 200, map[string]any{"path": path, "status": st})
}
