package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

// 本文件是给导出子命令 `qiansi backup` 用的产物写法。
//
// 与备份调度器那条路径的区别只在「产物交给谁」：调度器把归档落在 data/backups/，
// 而外部备份工具（docker-db-auto-backup）经 docker exec 调起本进程，把 stdout 当产物，
// 所以这里必须走临时目录 + 流式输出，不能在数据目录里留任何中间文件。
// 快照本身仍然复用 Snapshot（VACUUM INTO 优先，失败退回 checkpoint + 拷贝），
// 不另写第二份实现——一致性只该有一处定义。

// 压缩包内部的固定布局。回档时按同样的名字把两样东西放回原处，改这里要同步改 README。
const (
	// ZipDBEntry 是放在包根的库快照文件名。
	ZipDBEntry = "qiansi.db"
	// ZipUploadsDir 是附件在包内的目录前缀（含结尾斜杠，方便直接拼接相对路径）。
	ZipUploadsDir = "uploads/"
)

// WriteSnapshot 把一份一致性快照流式写到 w。
//
// 快照先落临时目录再 io.Copy 出去：VACUUM INTO 要求目标不存在，而 stdout 不能倒回去重写。
// defer 删临时目录，调用方读到一半断开也不会留下半成品。
func WriteSnapshot(ctx context.Context, db *sql.DB, dbPath string, w io.Writer) error {
	dir, err := os.MkdirTemp("", "qiansi-snapshot-")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(dir)

	target := filepath.Join(dir, "snapshot.db")
	if err := Snapshot(ctx, db, dbPath, target); err != nil {
		return err
	}
	return streamFile(target, w)
}

// WriteZIP 把「库快照 + uploads 目录」打成 zip 流式写到 w。
//
// 图片存在库外（data/uploads/），只备份 .db 会丢附件、只拷 uploads 会丢账本，
// 所以 zip 形态才是「一份产物能还原全部」。zip.Writer 本身就是流式的：
// 逐个文件拷进去，整包不在内存里攒。
func WriteZIP(ctx context.Context, db *sql.DB, dbPath, uploadsDir string, w io.Writer) error {
	dir, err := os.MkdirTemp("", "qiansi-zip-")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(dir)

	snap := filepath.Join(dir, "snapshot.db")
	if err := Snapshot(ctx, db, dbPath, snap); err != nil {
		return err
	}

	zw := zip.NewWriter(w)
	if err := zipOne(zw, ZipDBEntry, snap); err != nil {
		_ = zw.Close() // 尽力写完中央目录，让半截包在解包时还能报出前缀
		return err
	}
	count, err := zipDir(zw, uploadsDir)
	if err != nil {
		_ = zw.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("完成压缩包失败: %w", err)
	}
	log.Printf("[backup] zip 完成：库快照 + %d 个 uploads 文件", count)
	return nil
}

// zipOne 把磁盘上的一个文件作为指定条目写进压缩包。
func zipOne(zw *zip.Writer, name, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %w", name, err)
	}
	defer f.Close()
	hw, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("创建压缩包条目 %s 失败: %w", name, err)
	}
	if _, err := io.Copy(hw, f); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", name, err)
	}
	return nil
}

// zipDir 把 dir 下的普通文件按相对路径写进 uploads/ 前缀之下，返回打包个数。
//
// 目录不存在返回 0 而不是报错——一个还没上传过图片的库是正常的。
// 非常规文件（符号链接、套接字之类）跳过：备份包里塞一条指向数据目录之外的链接，
// 回档时就成了越界写。
func zipDir(zw *zip.Writer, dir string) (int, error) {
	count := 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			log.Printf("[backup] 跳过 uploads 里的非常规文件 %s", p)
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		// 包内路径必须用正斜杠：zip 规范如此，filepath 的分隔符在别的平台上读不出来
		if err := zipOne(zw, ZipUploadsDir+filepath.ToSlash(rel), p); err != nil {
			return err
		}
		count++
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	return count, err
}

// streamFile 把整个文件拷进 w。
func streamFile(path string, w io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("读取快照失败: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(w, f); err != nil {
		return fmt.Errorf("写出快照失败: %w", err)
	}
	return nil
}
