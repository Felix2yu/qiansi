// Package backup 负责数据库快照的底层能力：一致性快照、归档命名、旧档清理。
//
// 抽成独立包是为了让 HTTP 处理器与定时调度器共用同一份实现：
// 调度器不能依赖 api，否则会形成 api -> scheduler -> api 的循环。
package backup

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Stamp 是归档文件名里的时间戳，字典序即时间序，清理时可直接按名字排序。
const Stamp = "20060102-150405"

// Snapshot 生成一份一致性快照到 dest。
//
// 优先用 VACUUM INTO（原子、不受 WAL 写入干扰）；不可用时退回 checkpoint + 文件拷贝。
//
// 降级不是静默的：VACUUM INTO 失败（含「目标文件已存在」）会写日志，
// 因为拷贝兜底用 os.Create 会覆盖已存在的 dest —— 降级发生时必须留下痕迹。
func Snapshot(ctx context.Context, db *sql.DB, dbPath, dest string) error {
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", dest); err == nil {
		return nil
	} else {
		log.Printf("[backup] VACUUM INTO 失败（%v），退回 checkpoint + 文件拷贝", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return err
	}
	src, err := os.Open(dbPath)
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

// Archive 在 dir 下留一份带时间戳的归档快照，返回落盘路径。
// 同名文件（同一秒内重复触发）已存在时追加序号，避免 VACUUM INTO 因目标存在而失败。
func Archive(ctx context.Context, db *sql.DB, dbPath, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := "qiansi-" + time.Now().Format(Stamp)
	dest := filepath.Join(dir, base+".db")
	for i := 1; i < 100; i++ {
		if _, err := os.Stat(dest); err != nil {
			break
		}
		dest = filepath.Join(dir, fmt.Sprintf("%s-%d.db", base, i))
	}
	if err := Snapshot(ctx, db, dbPath, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// Prune 只清理自动归档产生的前缀文件，保留最近 keep 份。
// beforeRestore 前缀的文件是恢复前的保险，不在清理范围内。
func Prune(dir string, keep int) error {
	if keep <= 0 {
		keep = 7
	}
	names, err := ArchiveNames(dir)
	if err != nil {
		return err
	}
	if len(names) <= keep {
		return nil
	}
	sort.Strings(names)
	for _, n := range names[:len(names)-keep] {
		if err := os.Remove(filepath.Join(dir, n)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// ArchiveNames 列出 dir 下的自动归档文件名（不含目录、不含恢复前保险）。
func ArchiveNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		if !strings.HasPrefix(e.Name(), "qiansi-") {
			continue
		}
		names = append(names, e.Name())
	}
	return names, nil
}
