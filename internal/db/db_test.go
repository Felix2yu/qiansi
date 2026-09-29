package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDBOpenCreatesFileAndAppliesPragmas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "open.db")
	d, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	if err := d.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	var journal string
	if err := d.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if journal != "wal" {
		t.Errorf("journal_mode = %q, want wal", journal)
	}
	var fk int
	if err := d.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("query foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}

	// 写入数据后关闭，再次 Open 同一文件应保留数据（幂等，不清库）。
	if _, err := d.Exec("CREATE TABLE t(x INTEGER)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := d.Exec("INSERT INTO t(x) VALUES(42)"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	d.Close()

	d2, err := Open(path)
	if err != nil {
		t.Fatalf("Open second time: %v", err)
	}
	defer d2.Close()
	var v int
	if err := d2.QueryRow("SELECT x FROM t").Scan(&v); err != nil {
		t.Fatalf("re-read after reopen: %v", err)
	}
	if v != 42 {
		t.Errorf("persisted value = %d, want 42", v)
	}
}

func TestDBOpenInvalidPath(t *testing.T) {
	// 目录不存在 → sqlite 打开失败（Ping 报错）
	if _, err := Open(filepath.Join(t.TempDir(), "missing", "sub", "x.db")); err == nil {
		t.Fatal("expected error for missing parent directory")
	}
}

func TestDBExtractVersion(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"prefixed", "migrations/001_schema.sql", 1},
		{"bare numeric prefix", "007_x.sql", 7},
		{"multi digit", "migrations/012_abc.sql", 12},
		{"non numeric", "migrations/abc_def.sql", 0},
		{"digits later ignored", "migrations/xyz_001.sql", 0},
		{"no underscore", "migrations/003.sql", 3},
		{"empty", "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractVersion(c.in); got != c.want {
				t.Errorf("extractVersion(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

func TestDBGetVersionEmptyDB(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()
	// 尚无 schema_version 表 → getVersion 吞掉错误返回 0
	if v := getVersion(context.Background(), d); v != 0 {
		t.Errorf("getVersion on fresh db = %d, want 0", v)
	}
}

func TestDBRunMigrations(t *testing.T) {
	ctx := context.Background()
	d, err := Open(filepath.Join(t.TempDir(), "mig.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	if err := RunMigrations(ctx, d); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	// schema_version 应记录全部迁移版本
	rows, err := d.QueryContext(ctx, "SELECT version FROM schema_version ORDER BY version")
	if err != nil {
		t.Fatalf("query schema_version: %v", err)
	}
	var versions []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			t.Fatalf("scan: %v", err)
		}
		versions = append(versions, v)
	}
	rows.Close() // 连接池上限为 1：必须释放后再发起其它查询
	if len(versions) == 0 {
		t.Fatal("no schema_version rows recorded")
	}
	for i := 1; i < len(versions); i++ {
		if versions[i] <= versions[i-1] {
			t.Fatalf("versions not strictly increasing: %v", versions)
		}
	}
	if got := getVersion(ctx, d); got != versions[len(versions)-1] {
		t.Errorf("getVersion = %d, want max %d", got, versions[len(versions)-1])
	}

	// 核心表应存在
	var count int
	if err := d.QueryRowContext(ctx, "SELECT COUNT(*) FROM people").Scan(&count); err != nil {
		t.Errorf("people table missing after migrations: %v", err)
	}

	// 幂等：第二次运行不重复插入版本，也不报错
	if err := RunMigrations(ctx, d); err != nil {
		t.Fatalf("RunMigrations second run: %v", err)
	}
	if err := d.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_version").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != len(versions) {
		t.Errorf("schema_version rows after re-run = %d, want %d", count, len(versions))
	}
}
