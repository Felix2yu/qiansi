package db

import (
	"context"
	"database/sql"
	"embed"
	"io/fs"
	"log"
	"sort"
	"strings"

	"github.com/qiansi/app/internal/lunar"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func RunMigrations(ctx context.Context, db *sql.DB) error {
	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)

	current := getVersion(ctx, db)
	for _, f := range files {
		ver := extractVersion(f)
		if ver <= current {
			continue
		}
		data, err := migrationsFS.ReadFile(f)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, string(data)); err != nil {
			log.Printf("[migrate] FAIL %s: %v", f, err)
			return err
		}
		if ver == 19 {
			if err := repairLunarBirthdayAnniversaries(ctx, db); err != nil {
				log.Printf("[migrate] FAIL %s: %v", f, err)
				return err
			}
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO schema_version(version) VALUES(?)", ver); err != nil {
			return err
		}
		log.Printf("[migrate] applied %s (version %d)", f, ver)
	}
	return nil
}

func getVersion(ctx context.Context, db *sql.DB) int {
	var v int
	row := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM schema_version")
	_ = row.Scan(&v)
	return v
}

func extractVersion(name string) int {
	// migrations/001_schema.sql -> 1
	name = strings.TrimPrefix(name, "migrations/")
	name = strings.SplitN(name, "_", 2)[0]
	n := 0
	for _, c := range name {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

func repairLunarBirthdayAnniversaries(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT a.id,p.birthday
FROM anniversaries a JOIN people p ON p.id=a.person_id
WHERE a.source='birthday' AND p.birthday_is_lunar<>0 AND COALESCE(p.birthday,'')<>''`)
	if err != nil {
		return err
	}
	type row struct{ id, birthday string }
	var pending []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.birthday); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, r := range pending {
		solar := lunar.ParseDate(r.birthday)
		lunarDate, ok := lunar.SolarToLunarDate(solar)
		if !ok {
			continue
		}
		if _, err := db.ExecContext(ctx,
			"UPDATE anniversaries SET date=? WHERE id=? AND source='birthday'", lunarDate.String(), r.id); err != nil {
			return err
		}
	}
	return nil
}
