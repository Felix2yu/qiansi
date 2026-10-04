package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ===== 全量 JSON 导出 / 导入（N3）=====
//
// 备份文件（.db）只能在同版本的 SQLite 之间搬，换机器、迁到别的存储、或者只想把
// 数据交给别的程序看一眼都不方便。这里给出一份纯文本的全量快照：
// 每张表的列名 + 按列顺序的行，导入时原样写回（连 id 都不换），
// 所以「导出→导入」之后事件与账目之间的那些认领指针仍然对得上。

const (
	DumpApp     = "qiansi"
	DumpVersion = 1
)

// ErrDumpInvalid 标记「这份文件本身就不对」，和写库时的真故障区分开，
// HTTP 层据此回 400，而不是把用户导错的文件报成服务器 500。
var ErrDumpInvalid = errors.New("导入文件不可用")

type DumpTable struct {
	Name    string   `json:"table"`
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

type Dump struct {
	App        string       `json:"app"`
	Version    int          `json:"dump_version"`
	ExportedAt string       `json:"exported_at"`
	Tables     []*DumpTable `json:"tables"`
}

// dumpOrder 决定「先父后子」的写入顺序，导入时反过来清空。
// 顺序本身挡不住 events ↔ transactions 这种互相引用（下面 RestoreAll 用
// defer_foreign_keys 兜住），但它让绝大多数表在清空阶段就不会撞到子记录。
// 表在迁移里新增却没写进这里也没关系：dumpTables 会按名字补到末尾，
// 新表通常引用旧表，排在后面正好。
var dumpOrder = []string{
	"settings", "categories", "tags", "event_types", "people",
	"person_categories", "person_fields", "relationships",
	"events", "event_participants", "transactions", "repayments",
	"memos", "anniversaries", "anniversary_dismiss", "reminders",
	"attachments", "intimacy_snapshots", "notification_logs",
}

// dumpSkip 是绝不能进导出/导入的表：schema_version 记录的是这台机器跑到第几号迁移，
// 覆盖它会让下次启动以为不用迁移（或者反过来重复迁移）。
var dumpSkip = map[string]bool{"schema_version": true}

// dumpTables 返回本次要搬运的表名，按 dumpOrder 排，未登记的按名字排在后面。
func dumpTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		if !dumpSkip[n] {
			found = append(found, n)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rank := map[string]int{}
	for i, n := range dumpOrder {
		rank[n] = i
	}
	sort.SliceStable(found, func(i, j int) bool {
		ri, ok := rank[found[i]]
		if !ok {
			ri = len(dumpOrder)
		}
		rj, ok := rank[found[j]]
		if !ok {
			rj = len(dumpOrder)
		}
		if ri != rj {
			return ri < rj
		}
		return found[i] < found[j]
	})
	return found, nil
}

// DumpAll 读出全库内容。空表也照样列出来，导入时才能看出「这张表确实没数据」而不是「没导出这张表」。
func (s *Store) DumpAll(ctx context.Context) (*Dump, error) {
	names, err := dumpTables(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	d := &Dump{App: DumpApp, Version: DumpVersion, ExportedAt: nowUTC(), Tables: []*DumpTable{}}
	for _, name := range names {
		t, err := dumpOneTable(ctx, s.DB, name)
		if err != nil {
			return nil, err
		}
		d.Tables = append(d.Tables, t)
	}
	return d, nil
}

func dumpOneTable(ctx context.Context, db *sql.DB, name string) (*DumpTable, error) {
	cols, err := tableColumns(ctx, db, name)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, "SELECT * FROM "+quoteIdent(name))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	t := &DumpTable{Name: name, Columns: cols, Rows: [][]any{}}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		for i, v := range vals {
			vals[i] = dumpValue(v)
		}
		t.Rows = append(t.Rows, vals)
	}
	return t, rows.Err()
}

// dumpValue 把 driver 返回的类型收敛成 JSON 能原样带回来的几种：
// BLOB 按文本处理（本库没有二进制列，附件只存路径），其余 int64/float64/bool/string/nil 不动。
func dumpValue(v any) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}

// quoteIdent 只用于我们自己查出来的表名/列名（来自 sqlite_master，不是用户输入）。
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func tableColumns(ctx context.Context, db *sql.DB, name string) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT name FROM pragma_table_info(?)", name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// RestoreAll 用导出文件整库覆盖：先按外键反序清空，再按正序写回。
// 返回每张表实际写入的行数，供界面回显「导入了什么」。
//
// 覆盖前不做任何合并——同一份数据出现在两处会让人分不清哪条是真的，
// 调用方（设置页）负责先落一份 .db 归档快照。
func (s *Store) RestoreAll(ctx context.Context, d *Dump) (map[string]int, error) {
	if d == nil {
		return nil, fmt.Errorf("%w：内容为空", ErrDumpInvalid)
	}
	if d.App != DumpApp || d.Version != DumpVersion {
		return nil, fmt.Errorf("%w：不是牵丝 v%d 的导出文件（app=%q）", ErrDumpInvalid, DumpVersion, d.App)
	}
	names, err := dumpTables(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, n := range names {
		allowed[n] = true
	}
	seen := map[string]bool{}
	for _, t := range d.Tables {
		if t == nil {
			continue
		}
		if !allowed[t.Name] {
			return nil, fmt.Errorf("%w：文件里有本程序不认识的表 %s", ErrDumpInvalid, t.Name)
		}
		if seen[t.Name] {
			return nil, fmt.Errorf("%w：表 %s 在文件里出现了两次", ErrDumpInvalid, t.Name)
		}
		seen[t.Name] = true
		cols, err := tableColumns(ctx, s.DB, t.Name)
		if err != nil {
			return nil, err
		}
		have := map[string]bool{}
		for _, c := range cols {
			have[c] = true
		}
		for _, c := range t.Columns {
			// 列名会直接拼进 INSERT，只允许本表 schema 里真有的那些
			if !have[c] {
				return nil, fmt.Errorf("%w：表 %s 没有列 %s", ErrDumpInvalid, t.Name, c)
			}
		}
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// events.gift_transaction_id → transactions，transactions.event_id → events：
	// 这两张表互相引用，按任何顺序插都会有一半撞在外键上。
	// 把约束推迟到 COMMIT，一份自洽的导出文件就能整库原样落回来。
	if _, err := tx.ExecContext(ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		return nil, err
	}
	for i := len(names) - 1; i >= 0; i-- {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+quoteIdent(names[i])); err != nil {
			return nil, err
		}
	}
	written := map[string]int{}
	for _, t := range d.Tables {
		if t == nil || len(t.Columns) == 0 {
			continue
		}
		n, err := restoreTable(ctx, tx, t)
		if err != nil {
			return nil, err
		}
		written[t.Name] = n
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return written, nil
}

func restoreTable(ctx context.Context, tx *sql.Tx, t *DumpTable) (int, error) {
	cols := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		cols[i] = quoteIdent(c)
	}
	stmt := "INSERT INTO " + quoteIdent(t.Name) + "(" + strings.Join(cols, ",") + ") VALUES(" +
		strings.TrimSuffix(strings.Repeat("?,", len(t.Columns)), ",") + ")"
	n := 0
	for _, row := range t.Rows {
		if len(row) != len(t.Columns) {
			return n, fmt.Errorf("%w：表 %s 有一行的列数和表头不一致", ErrDumpInvalid, t.Name)
		}
		args := make([]any, len(row))
		for i, v := range row {
			args[i] = restoreValue(v)
		}
		res, err := tx.ExecContext(ctx, stmt, args...)
		if err != nil {
			return n, fmt.Errorf("写入 %s 第 %d 行失败: %w", t.Name, n+1, err)
		}
		if aff, err := res.RowsAffected(); err == nil {
			n += int(aff)
		} else {
			n++
		}
	}
	return n, nil
}

// restoreValue 把 JSON 数字还原成整数或小数：解码时用了 UseNumber，
// 所以 80000 不会像 float64 那样被 SQLite 存成 REAL，读回来变成 80000.0。
func restoreValue(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := strconv.ParseInt(x.String(), 10, 64); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(x.String(), 64); err == nil {
			return f
		}
		return x.String()
	case float64: // 只有绕开 UseNumber 才会走到这里
		if x == float64(int64(x)) {
			return int64(x)
		}
		return x
	case []byte:
		return string(x)
	}
	return v
}
