package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/qiansi/app/internal/lunar"
)

func TestMigration019RepairsLunarBirthdayAnniversaries(t *testing.T) {
	ctx := context.Background()
	d, err := Open(filepath.Join(t.TempDir(), "lunar_birthday.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	applyThrough(t, d, 18)
	_, err = d.ExecContext(ctx, `INSERT INTO people(id,name,birthday,birthday_is_lunar,created_at,updated_at) VALUES
('lunar','童瑶','1994-10-26',1,'t','t'),
('solar','阳历生日','1994-10-26',0,'t','t')`)
	if err != nil {
		t.Fatalf("seed people: %v", err)
	}
	_, err = d.ExecContext(ctx, `INSERT INTO anniversaries(id,person_id,title,date,is_lunar,repeat_yearly,remind_days,created_at,source) VALUES
('a-lunar','lunar','童瑶的生日','1994-10-26',1,1,'7,3,1,0','t','birthday'),
('a-solar','solar','阳历生日','1994-10-26',0,1,'7,3,1,0','t','birthday'),
('a-manual',NULL,'手动农历纪念日','1994-10-26',1,1,'7,3,1,0','t','')`)
	if err != nil {
		t.Fatalf("seed anniversaries: %v", err)
	}

	if err := RunMigrations(ctx, d); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	if got := getVersion(ctx, d); got < 19 {
		t.Fatalf("schema_version = %d, want at least 19", got)
	}
	for id, want := range map[string]string{
		"a-lunar":  "1994-09-22",
		"a-solar":  "1994-10-26",
		"a-manual": "1994-10-26",
	} {
		var got string
		if err := d.QueryRowContext(ctx, "SELECT date FROM anniversaries WHERE id=?", id).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		if got != want {
			t.Errorf("%s date = %q, want %q", id, got, want)
		}
	}

	if err := RunMigrations(ctx, d); err != nil {
		t.Fatalf("second RunMigrations: %v", err)
	}
	var got string
	if err := d.QueryRowContext(ctx, "SELECT date FROM anniversaries WHERE id='a-lunar'").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "1994-09-22" {
		t.Fatalf("重复迁移不能再次换算, got %q", got)
	}
	next := lunar.NextOccurrence(lunar.ParseDate(got), true, lunar.YMD{Year: 2026, Month: 1, Day: 1})
	if next != (lunar.YMD{Year: 2026, Month: 10, Day: 31}) {
		t.Fatalf("修复后的 2026 年生日应为 10-31, got %s", next)
	}
}
