package db

import (
	"context"
	"path/filepath"
	"testing"
)

// TestMigration010SettledByKind 把存量数据里「未结清的礼物/花销」理顺。
//
// 只有借还才有未结清这回事，但历史上礼物和花销也能勾着 settled=0，
// 于是被算进首页的「我借出（未还）」，建议里冒出「某人待还 ¥600」。
// 统计口径已经按 kind 过滤，这条迁移负责让存量数据与新口径一致。
func TestMigration010SettledByKind(t *testing.T) {
	ctx := context.Background()
	d, err := Open(filepath.Join(t.TempDir(), "settled010.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	applyThrough(t, d, 9)
	const seed = `INSERT INTO transactions(id,person_id,kind,direction,amount_fen,title,occurred_at,settled,settled_at,created_at) VALUES
		('g1','p1','gift','out',60000,'乔迁随礼','2026-01-01',0,NULL,'2026-01-01'),
		('e1','p1','expense','out',3000,'打车','2026-01-02',0,NULL,'2026-01-02'),
		('l1','p1','loan','out',50000,'周转','2026-01-03',0,NULL,'2026-01-03'),
		('l2','p1','loan','out',20000,'已还清','2026-01-04',1,'2026-02-01','2026-01-04'),
		('o1','p1','other','in',80000,'收下的回礼','2026-01-05',0,'2026-01-05','2026-01-05')`
	if _, err := d.ExecContext(ctx, `INSERT INTO people(id,name,created_at,updated_at) VALUES('p1','张三','t','t')`); err != nil {
		t.Fatalf("seed person: %v", err)
	}
	if _, err := d.ExecContext(ctx, seed); err != nil {
		t.Fatalf("seed transactions: %v", err)
	}

	if err := RunMigrations(ctx, d); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	if v := getVersion(ctx, d); v < 10 {
		t.Fatalf("schema_version = %d，期望至少 010", v)
	}

	for id, want := range map[string]int{"g1": 1, "e1": 1, "o1": 1, "l1": 0, "l2": 1} {
		var got int
		if err := d.QueryRowContext(ctx, `SELECT settled FROM transactions WHERE id=?`, id).Scan(&got); err != nil {
			t.Fatalf("读 %s: %v", id, err)
		}
		if got != want {
			t.Errorf("%s settled = %d, 期望 %d", id, got, want)
		}
	}
	// 结清时间不伪造：原本没有的留空，原本有的保持不变
	for id, want := range map[string]string{"g1": "", "e1": "", "o1": "2026-01-05", "l2": "2026-02-01"} {
		var got string
		if err := d.QueryRowContext(ctx,
			`SELECT COALESCE(settled_at,'') FROM transactions WHERE id=?`, id).Scan(&got); err != nil {
			t.Fatalf("读 %s 的 settled_at: %v", id, err)
		}
		if got != want {
			t.Errorf("%s settled_at = %q, 期望 %q", id, got, want)
		}
	}
}
