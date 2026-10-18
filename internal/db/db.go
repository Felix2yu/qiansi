package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "modernc.org/sqlite"
)

func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=auto_vacuum(INCREMENTAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // 单写连接，避免 SQLite busy
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	if err := db.PingContext(context.Background()); err != nil {
		return nil, err
	}
	log.Printf("[db] opened %s", path)
	return db, nil
}

// OpenForExport 以「只导出」的方式打开数据库：不建目录、不跑迁移、不写任何业务数据。
//
// 与 Open 有三点不同，都源于同一个前提——导出进程是在应用运行时被外部备份工具
// 调起的（docker exec `qiansi backup`），它只读数据，不拥有这个库：
//
//   - 文件不存在直接报错。那是路径配错，不是「该新建一个空库」；SQLite 驱动会
//     顺手把空库建出来，而工具会把这份空快照当成一次成功备份存进档案。
//   - DSN 不带 journal_mode / foreign_keys / auto_vacuum。这些 pragma 是写操作，
//     会改库头，把「导出」变成一次 schema 层面的改动。
//   - 不建父目录（Open 依赖调用方 config.Load 已建好，导出路径刻意绕开它）。
//
// WAL 里已提交、尚未合并进主库的数据照常读得到：SQLite 自己会读 -wal。
func OpenForExport(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("数据文件不可用: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// 顺序读，一条连接足够；多开连接只会多份 -shm 句柄。
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
