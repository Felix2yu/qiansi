package db

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// applyThrough 按 RunMigrations 的方式把迁移跑到 maxVer，用来复现升级前的库结构。
func applyThrough(t *testing.T, d *sql.DB, maxVer int) {
	t.Helper()
	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(files)
	for _, f := range files {
		ver := extractVersion(f)
		if ver > maxVer {
			continue
		}
		data, err := migrationsFS.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if _, err := d.Exec(string(data)); err != nil {
			t.Fatalf("exec %s: %v", f, err)
		}
		if _, err := d.Exec("INSERT INTO schema_version(version) VALUES(?)", ver); err != nil {
			t.Fatalf("record version %d: %v", ver, err)
		}
	}
}

// TestMigration008KeepsRelationshipEdges 老库的唯一键是一对一人一条边，
// 迁移到 UNIQUE(from,to,type) 时既不能丢边，也不能把外键约束迁没了。
func TestMigration008KeepsRelationshipEdges(t *testing.T) {
	ctx := context.Background()
	d, err := Open(filepath.Join(t.TempDir(), "rel008.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	applyThrough(t, d, 7)
	if _, err := d.ExecContext(ctx,
		`INSERT INTO people(id,name,created_at,updated_at) VALUES('a','张三','t','t'),('b','李四','t','t')`); err != nil {
		t.Fatalf("seed people: %v", err)
	}
	seed := `INSERT INTO relationships(id,from_person_id,to_person_id,type,remark,created_at)
	         VALUES('r1','a','b','同事','同组','2026-01-01')`
	if _, err := d.ExecContext(ctx, seed); err != nil {
		t.Fatalf("seed relationship: %v", err)
	}
	// 先确认这确实是旧结构：同一对人再加一个类型应撞唯一键
	if _, err := d.ExecContext(ctx, `INSERT INTO relationships(id,from_person_id,to_person_id,type,created_at)
	                                 VALUES('r2','a','b','好友','2026-01-02')`); err == nil {
		t.Fatal("旧唯一键下同一对人竟能并存两条边")
	} else if !strings.Contains(err.Error(), "UNIQUE constraint failed: relationships") {
		t.Fatalf("旧库报错不该是撞唯一键: %v", err)
	}

	if err := RunMigrations(ctx, d); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	var v int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM relationships`).Scan(&v); err != nil {
		t.Fatalf("count relationships: %v", err)
	}
	if v != 1 {
		t.Fatalf("迁移后边数 = %d，期望原边完整保留", v)
	}
	var typ, remark, createdAt string
	if err := d.QueryRowContext(ctx,
		`SELECT type, remark, created_at FROM relationships WHERE id='r1'`).Scan(&typ, &remark, &createdAt); err != nil {
		t.Fatalf("查原边: %v", err)
	}
	if typ != "同事" || remark != "同组" || createdAt != "2026-01-01" {
		t.Fatalf("原边内容被改动: %q / %q / %q", typ, remark, createdAt)
	}

	// 新唯一键：不同类型可并存，完全重复仍拒绝
	if _, err := d.ExecContext(ctx, `INSERT INTO relationships(id,from_person_id,to_person_id,type,created_at)
	                                 VALUES('r2','a','b','好友','2026-01-02')`); err != nil {
		t.Fatalf("同人不同类型应可并存: %v", err)
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO relationships(id,from_person_id,to_person_id,type,created_at)
	                                 VALUES('r3','a','b','好友','2026-01-03')`); err == nil {
		t.Fatal("完全重复的边应撞唯一键")
	} else if !strings.Contains(err.Error(), "UNIQUE constraint failed: relationships") {
		t.Fatalf("重复边报错 = %v", err)
	}
	// 反向是另一条有向边，同样允许
	if _, err := d.ExecContext(ctx, `INSERT INTO relationships(id,from_person_id,to_person_id,type,created_at)
	                                 VALUES('r4','b','a','好友','2026-01-04')`); err != nil {
		t.Fatalf("反向边应可并存: %v", err)
	}

	// 迁移重建过表，外键与两侧索引都得还在
	if _, err := d.ExecContext(ctx, `INSERT INTO relationships(id,from_person_id,to_person_id,type,created_at)
	                                 VALUES('r5','a','ghost','同事','2026-01-05')`); err == nil {
		t.Fatal("外键没生效：能挂到不存在的人身上")
	}
	for _, name := range []string{"idx_relationships_from", "idx_relationships_to"} {
		if err := d.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&v); err != nil {
			t.Fatalf("查索引 %s: %v", name, err)
		}
		if v != 1 {
			t.Fatalf("迁移后少了索引 %s", name)
		}
	}

	if got := getVersion(ctx, d); got < 8 {
		t.Fatalf("schema_version = %d，期望至少跑到 008", got)
	}
}
