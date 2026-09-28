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
	})
}

// snapshotDB 生成一份一致性快照到 dest。
// 优先用 VACUUM INTO（原子且不受 WAL 写入干扰）；不可用时退回 checkpoint + 文件拷贝。
func (a *API) snapshotDB(ctx context.Context, dest string) error {
	if _, err := a.Store.DB.ExecContext(ctx, "VACUUM INTO ?", dest); err == nil {
		return nil
	}
	_, _ = a.Store.DB.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	src, err := os.Open(a.Cfg.DBPath)
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, src); err != nil {
		return err
	}
	return out.Sync()
}

func (a *API) backupExport(w http.ResponseWriter, r *http.Request) {
	tmp := filepath.Join(a.Cfg.Backups, "export-"+time.Now().Format("20060102150405")+".db")
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
	if err := os.MkdirAll(a.Cfg.Backups, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(a.Cfg.Backups, "qiansi-"+time.Now().Format("20060102-150405")+".db")
	if err := a.snapshotDB(ctx, dest); err != nil {
		return "", err
	}
	return dest, nil
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
	if err := a.Store.DB.Close(); err != nil {
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
	a.Store.DB = fresh
	log.Printf("[backup] restored from %s (%d people)", header.Filename, cnt)
	writeJSON(w, 200, map[string]any{"ok": true, "people": cnt})
}

// DailyBackupTick 供调度器每日调用一次，最多保留最近 7 份归档快照。
func (a *API) DailyBackupTick(ctx context.Context) {
	a.dailyBackupIfNeeded(ctx, 7)
}

func (a *API) dailyBackupIfNeeded(ctx context.Context, keep int) {
	if keep <= 0 {
		keep = 7
	}
	if _, err := a.archiveSnapshot(ctx); err != nil {
		log.Printf("[backup] snapshot failed: %v", err)
		return
	}
	entries, err := os.ReadDir(a.Cfg.Backups)
	if err != nil {
		return
	}
	names := []string{}
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "qiansi-") && strings.HasSuffix(e.Name(), ".db") {
			names = append(names, e.Name())
		}
	}
	if len(names) <= keep {
		return
	}
	sort.Strings(names) // 文件名含时间戳，字典序即时间序
	for _, n := range names[:len(names)-keep] {
		_ = os.Remove(filepath.Join(a.Cfg.Backups, n))
	}
}
