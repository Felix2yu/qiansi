package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigration009IntroducedBy 引荐人列要可空、可自引用，
// 删掉引荐人后由 ON DELETE SET NULL 自动断开。
func TestMigration009IntroducedBy(t *testing.T) {
	ctx := context.Background()
	d, err := Open(filepath.Join(t.TempDir(), "intro009.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	applyThrough(t, d, 8)
	if _, err := d.ExecContext(ctx,
		`INSERT INTO people(id,name,created_at,updated_at) VALUES('a','张三','t','t'),('b','李四','t','t')`); err != nil {
		t.Fatalf("seed people: %v", err)
	}
	// 迁移前该列不存在
	if err := d.QueryRowContext(ctx, `SELECT introduced_by_person_id FROM people WHERE id='a'`).Scan(nil); err == nil {
		t.Fatal("009 之前不应有 introduced_by_person_id 列")
	}

	if err := RunMigrations(ctx, d); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	// 老数据迁移后该列应为 NULL
	var intro sql.NullString
	if err := d.QueryRowContext(ctx, `SELECT introduced_by_person_id FROM people WHERE id='a'`).Scan(&intro); err != nil {
		t.Fatalf("读引荐人列: %v", err)
	}
	if intro.Valid {
		t.Fatalf("老数据引荐人应为 NULL，得到 %q", intro.String)
	}

	// 指向另一个人
	if _, err := d.ExecContext(ctx, `UPDATE people SET introduced_by_person_id='b' WHERE id='a'`); err != nil {
		t.Fatalf("设置引荐人: %v", err)
	}
	// 外键仍生效：不能指向不存在的人
	if _, err := d.ExecContext(ctx, `UPDATE people SET introduced_by_person_id='ghost' WHERE id='a'`); err == nil {
		t.Fatal("外键没生效：引荐人能指向不存在的人")
	}

	// 删掉引荐人 → 自动置空，人物仍在
	if _, err := d.ExecContext(ctx, `DELETE FROM people WHERE id='b'`); err != nil {
		t.Fatalf("删除引荐人: %v", err)
	}
	var n int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM people WHERE id='a'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("被引荐人不应被级联删除: %v / %d", err, n)
	}
	if err := d.QueryRowContext(ctx, `SELECT introduced_by_person_id FROM people WHERE id='a'`).Scan(&intro); err != nil {
		t.Fatalf("删除后读引荐人列: %v", err)
	}
	if intro.Valid {
		t.Fatalf("删掉引荐人后应 SET NULL，得到 %q", intro.String)
	}

	// 只断言「至少跑到 009」：后面再加迁移文件时不必回来改这里（与 008 的写法一致）
	if got := getVersion(ctx, d); got < 9 {
		t.Fatalf("schema_version = %d，期望至少 009", got)
	}
}
