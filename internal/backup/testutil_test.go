package backup

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// newTestDB 建一个带 t 表和一行数据的临时 SQLite，返回连接与文件路径。
// 参数与生产 db.Open 的关键项一致（WAL + 单写连接），避免测试与运行时快照行为有差异。
func newTestDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := t.TempDir() + "/src.db"
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE t(a INTEGER)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Exec("INSERT INTO t VALUES(1)"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	return db, path
}
