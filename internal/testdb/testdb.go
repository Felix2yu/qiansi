// Package testdb 仅供各包的测试建库：临时文件 SQLite + 全量迁移。
// 迁移文件直接读 internal/db/migrations（运行时读目录，与生产共用同一份来源），
// 新增迁移无需回来改这里。
package testdb

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// New 返回一个已完成迁移的 *sql.DB，生命周期跟随 t。
// 用文件而非 ":memory:"：迁移与后续查询可能落在连接池的不同连接上，
// 私有内存库是 per-connection 的，会表现为「表不存在」。
func New(t *testing.T) *sql.DB {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "test.db") +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	migrate(t, db)
	return db
}

func migrate(t *testing.T, db *sql.DB) {
	t.Helper()
	dir := migrationsDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations dir %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			t.Fatalf("read %s: %v", n, err)
		}
		if _, err := db.Exec(string(data)); err != nil {
			t.Fatalf("exec migration %s: %v", n, err)
		}
		// 版本号要按生产 RunMigrations 的方式登记：备份恢复末尾会再跑一次迁移，
		// 库里没登记就会把已应用的迁移重放成 duplicate column / table already exists。
		if ver, err := strconv.Atoi(strings.SplitN(n, "_", 2)[0]); err != nil {
			t.Fatalf("parse version from %s: %v", n, err)
		} else if _, err := db.Exec("INSERT INTO schema_version(version) VALUES(?)", ver); err != nil {
			t.Fatalf("record version %d: %v", ver, err)
		}
	}
}

// migrationsDir 从当前测试包目录逐级上溯寻找 internal/db/migrations，
// 这样从仓库根或任意位置执行 go test 都能定位到。
func migrationsDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "internal", "db", "migrations")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("internal/db/migrations not found above %s", wd)
		}
	}
}
