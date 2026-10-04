package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

// ===== 全量 JSON 导出 / 导入（N3）=====

// dgDump 导出再按 JSON 走一遍往返：数字必须以 json.Number 回来，
// 否则整数列会被写成 REAL，读回来就不是同一个值了。
func dgDump(t *testing.T, s *Store) *Dump {
	t.Helper()
	d, err := s.DumpAll(context.Background())
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("导出结果无法编码: %v", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var back Dump
	if err := dec.Decode(&back); err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	return &back
}

func dgTable(t *testing.T, d *Dump, name string) *DumpTable {
	t.Helper()
	for _, tab := range d.Tables {
		if tab.Name == name {
			return tab
		}
	}
	return nil
}

func dgCol(t *testing.T, tab *DumpTable, name string) int {
	t.Helper()
	for i, c := range tab.Columns {
		if c == name {
			return i
		}
	}
	t.Fatalf("表 %s 没有列 %s", tab.Name, name)
	return -1
}

// dgText 读一个可空文本列，NULL 读成空串。
func dgText(t *testing.T, s *Store, query string, args ...any) string {
	t.Helper()
	var v sql.NullString
	if err := s.DB.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatalf("查询 %s 失败: %v", query, err)
	}
	return v.String
}

func dgRestoreErr(ctx context.Context, s *Store, d *Dump) error {
	_, err := s.RestoreAll(ctx, d)
	return err
}

// dgEvent 记一场往来，giftFen>0 时带上事件自带的那笔礼金。
func dgEvent(t *testing.T, s *Store, hostID, title, date string, giftFen int) *Event {
	t.Helper()
	e := &Event{Title: title, EventDate: date,
		GiftAmountFen: giftFen, GiftDirection: "out", GiftPersonID: hostID}
	egCreate(t, s, e, []string{hostID})
	return e
}

// 导出→导入往返：id 不变，事件与账目互相引用的那两列都还在，空表也不会被漏掉。
func TestDump_RoundTripKeepsIDsAndPointers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	host := evMustPerson(t, s, "老王")
	e := dgEvent(t, s, host.ID, "老王儿子婚礼", "2026-10-01", 80000)
	txID := e.GiftTransactionID
	if txID == "" {
		t.Fatal("事件没认领上礼金")
	}

	d := dgDump(t, s)
	if dgTable(t, d, "people") == nil || dgTable(t, d, "events") == nil {
		t.Fatalf("导出缺表: %v", d.Tables)
	}
	if dgTable(t, d, "memos") == nil {
		t.Fatal("空表也要列出来，否则「没数据」和「没导出」分不清")
	}
	// schema_version 绝不能进文件：那是这台机器跑到第几号迁移的记录
	if dgTable(t, d, "schema_version") != nil {
		t.Fatal("schema_version 不该被导出")
	}

	if _, err := s.RestoreAll(ctx, d); err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if got := dgText(t, s, "SELECT gift_transaction_id FROM events WHERE id=?", e.ID); got != txID {
		t.Fatalf("往返后礼金指针变了: %q -> %q", txID, got)
	}
	if got := dgText(t, s, "SELECT event_id FROM transactions WHERE id=?", txID); got != e.ID {
		t.Fatalf("往返后账目的关联事件变了: %q -> %q", e.ID, got)
	}
	// 金额还得是整数：被写成 REAL 的话 CAST 出来是 '80000.0'
	if n := egCount(t, s, "SELECT COUNT(*) FROM transactions WHERE CAST(amount_fen AS TEXT)='80000'"); n != 1 {
		t.Fatal("礼金金额在往返后不再是整数 80000")
	}
	if n := egCount(t, s, "SELECT COUNT(*) FROM people"); n != 1 {
		t.Fatalf("往返后人数 = %d, want 1", n)
	}
}

// 导入是整库覆盖：文件里没有的人必须消失，而不是和现有数据并存。
func TestDump_RestoreReplacesEverything(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	evMustPerson(t, s, "留下")
	evMustPerson(t, s, "会被覆盖掉")
	d := dgDump(t, s)

	evMustPerson(t, s, "导入前新增的人")
	if n := egCount(t, s, "SELECT COUNT(*) FROM people"); n != 3 {
		t.Fatalf("导出后又加了人 = %d, want 3", n)
	}
	if _, err := s.RestoreAll(ctx, d); err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if n := egCount(t, s, "SELECT COUNT(*) FROM people"); n != 2 {
		t.Fatalf("导入后人数 = %d, want 2（整库覆盖）", n)
	}
	if n := egCount(t, s, "SELECT COUNT(*) FROM people WHERE name='导入前新增的人'"); n != 0 {
		t.Fatal("导入没做整库覆盖，多出一个「导入前新增的人」")
	}
}

// 空库往返不能报错，也不能凭空造出数据。
func TestDump_EmptyDatabaseRoundTrip(t *testing.T) {
	s := newTestStore(t)
	d := dgDump(t, s)
	if len(d.Tables) < 10 {
		t.Fatalf("空库也该列出全部表，只有 %d 张", len(d.Tables))
	}
	for _, tab := range d.Tables {
		if len(tab.Rows) != 0 {
			t.Fatalf("空库里 %s 有 %d 行", tab.Name, len(tab.Rows))
		}
	}
	// 空库也会覆盖每一张表，所以报告里有表名是正常的，关键是一行都没写进去。
	written, err := s.RestoreAll(context.Background(), d)
	if err != nil {
		t.Fatalf("空库导入失败: %v", err)
	}
	total := 0
	for _, n := range written {
		total += n
	}
	if total != 0 {
		t.Fatalf("空库导入写出了数据: %v", written)
	}
}

// 文件里的表名/列名会直接参与拼 SQL，所以只认本机 schema 里真有的那些。
func TestDump_RejectsUnknownTableColumnAndHeader(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	evMustPerson(t, s, "老王")

	bad := dgDump(t, s)
	bad.Tables = append(bad.Tables, &DumpTable{Name: "sqlite_master", Columns: []string{"name"}, Rows: [][]any{{"x"}}})
	if err := dgRestoreErr(ctx, s, bad); err == nil || !strings.Contains(err.Error(), "不认识的表") {
		t.Fatalf("未知表没被拒: %v", err)
	}

	bad = dgDump(t, s)
	dgTable(t, bad, "people").Columns = append(dgTable(t, bad, "people").Columns, "password_hash")
	if err := dgRestoreErr(ctx, s, bad); err == nil || !strings.Contains(err.Error(), "没有列") {
		t.Fatalf("未知列没被拒: %v", err)
	}

	bad = dgDump(t, s)
	bad.App = "weixin"
	if err := dgRestoreErr(ctx, s, bad); err == nil || !strings.Contains(err.Error(), "牵丝") {
		t.Fatalf("别家程序的文件没被拒: %v", err)
	}

	bad = dgDump(t, s)
	bad.Version = 99
	if err := dgRestoreErr(ctx, s, bad); err == nil || !strings.Contains(err.Error(), "牵丝") {
		t.Fatalf("未知版本号没被拒: %v", err)
	}

	// 被拒之后库不能被动过
	if n := egCount(t, s, "SELECT COUNT(*) FROM people"); n != 1 {
		t.Fatalf("失败的导入动了库：人数 = %d, want 1", n)
	}
}

// 坏外键必须整体回滚，不能把库清空一半。
func TestDump_BadForeignKeyRollsBackWholeImport(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	host := evMustPerson(t, s, "老王")
	guest := evMustPerson(t, s, "阿张")
	egCreate(t, s, &Event{Title: "老王儿子婚礼", EventDate: "2026-10-01"},
		[]string{host.ID, guest.ID})

	d := dgDump(t, s)
	peeps := dgTable(t, d, "people")
	nameCol := dgCol(t, peeps, "name")
	idCol := dgCol(t, peeps, "id")
	dropped := ""
	for i, row := range peeps.Rows {
		if row[nameCol] == "阿张" {
			dropped, peeps.Rows = row[idCol].(string), append(peeps.Rows[:i], peeps.Rows[i+1:]...)
		}
	}
	if dropped == "" {
		t.Fatal("导出里没有阿张")
	}
	// 事件参与人还指着被删掉的那个人
	if _, err := s.RestoreAll(ctx, d); err == nil {
		t.Fatal("外键指向不存在的记录，居然导入成功了")
	}
	if n := egCount(t, s, "SELECT COUNT(*) FROM people"); n != 2 {
		t.Fatalf("失败的导入动了库：人数 = %d, want 2", n)
	}
	if n := egCount(t, s, "SELECT COUNT(*) FROM event_participants"); n == 0 {
		t.Fatal("失败的导入把参与人弄丢了")
	}
}

// ===== 明细导出（N3）=====

func TestExportDetail_EventsGiftAndManualList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	host := evMustPerson(t, s, "老王")
	guest := evMustPerson(t, s, "阿张")
	e := dgEvent(t, s, host.ID, "老王儿子婚礼", "2026-10-01", 80000)

	// 同一场宴席上手工登记的礼单
	stMustTx(t, s, &Transaction{PersonID: guest.ID, Kind: "gift", Direction: "in",
		AmountFen: 60000, Title: "回礼", OccurredAt: "2026-10-01", Settled: true, EventID: e.ID})

	head, rows, err := s.ExportDetail(ctx, "events", ExportFilter{})
	if err != nil {
		t.Fatalf("导出往来明细失败: %v", err)
	}
	if head[0] != "日期" || head[6] != "礼金（元）" {
		t.Fatalf("表头不对: %v", head)
	}
	if len(rows) != 1 {
		t.Fatalf("往来明细行数 = %d, want 1", len(rows))
	}
	row := rows[0]
	if row[0] != "2026-10-01" || row[1] != "老王儿子婚礼" || row[4] != "老王" {
		t.Fatalf("行内容不对: %v", row)
	}
	if row[5] != "支出" || row[6] != "800.00" {
		t.Fatalf("礼金没导出来: %v", row)
	}
	// 事件自带那笔礼金不能又算进「花费」
	if row[7] != "" {
		t.Fatalf("礼金被重复计成开销: %v", row)
	}

	// 金钱明细：一场宴席两条（随出去的 800 + 收到的 600）
	_, rows, err = s.ExportDetail(ctx, "transactions", ExportFilter{EventID: e.ID})
	if err != nil {
		t.Fatalf("导出金钱明细失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("礼单明细行数 = %d, want 2: %v", len(rows), rows)
	}
	byPerson := map[string][]string{}
	for _, r := range rows {
		byPerson[r[1]] = r
	}
	if r := byPerson["老王"]; r == nil || r[3] != "支出" || r[4] != "800.00" || r[10] != "老王儿子婚礼" {
		t.Fatalf("随出去的礼金行不对: %v", r)
	}
	if r := byPerson["阿张"]; r == nil || r[2] != "礼物" || r[3] != "收入" || r[4] != "600.00" {
		t.Fatalf("手工礼单行不对: %v", r)
	}
}

func TestExportDetail_Filters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	host := evMustPerson(t, s, "老王")
	quiet := evMustPerson(t, s, "没来往的人")
	lastYear := dgEvent(t, s, host.ID, "去年的一场", "2025-03-01", 50000)
	thisYear := dgEvent(t, s, host.ID, "今年的一场", "2026-02-01", 0)

	_, rows, err := s.ExportDetail(ctx, "events", ExportFilter{Year: 2026})
	if err != nil {
		t.Fatalf("按年导出失败: %v", err)
	}
	if len(rows) != 1 || rows[0][0] != "2026-02-01" {
		t.Fatalf("按年过滤没生效: %v", rows)
	}
	_, rows, err = s.ExportDetail(ctx, "events", ExportFilter{PersonID: host.ID})
	if err != nil {
		t.Fatalf("按人导出失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("老王该有两场往来: %v", rows)
	}
	if _, rows, err = s.ExportDetail(ctx, "events", ExportFilter{PersonID: quiet.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("没人来往却导出了 %d 行: %v", len(rows), err)
	}
	if _, rows, err = s.ExportDetail(ctx, "transactions", ExportFilter{EventID: lastYear.ID}); err != nil || len(rows) != 1 {
		t.Fatalf("只导某一场往来 = %d 行: %v", len(rows), err)
	}
	if _, rows, err = s.ExportDetail(ctx, "transactions", ExportFilter{EventID: thisYear.ID}); err != nil || len(rows) != 0 {
		t.Fatalf("没有账的往来导出了 %d 行: %v", len(rows), err)
	}
}

func TestExportDetail_MemosAndUnknownType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	host := evMustPerson(t, s, "老王")
	stMustMemo(t, s, &Memo{PersonID: host.ID, Speaker: "other", Content: "他说下个月还钱", SaidAt: "2026-01-05", Status: "open"})

	head, rows, err := s.ExportDetail(ctx, "memos", ExportFilter{Year: 2026})
	if err != nil {
		t.Fatalf("导出对话失败: %v", err)
	}
	if head[0] != "时间" || len(rows) != 1 || rows[0][1] != "老王" || rows[0][2] != "对方说" {
		t.Fatalf("对话明细不对: %v / %v", head, rows)
	}
	if _, _, err := s.ExportDetail(ctx, "people", ExportFilter{}); err == nil {
		t.Fatal("未知类型该报错")
	}
	if _, rows, err := s.ExportDetail(ctx, "memos", ExportFilter{Year: 1999}); err != nil || len(rows) != 0 {
		t.Fatalf("1999 年不该有对话: %v %v", rows, err)
	}
}
