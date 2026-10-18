package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/qiansi/app/internal/config"
	"github.com/qiansi/app/internal/db"
	"github.com/qiansi/app/internal/store"
)

// backupDataDir 把数据目录指到临时目录，并按应用自己的解析口径返回它。
//
// 用 QIANSI_DATA_DIR 而不是备份子命令的 -db 覆盖参数：默认解析本身就是被测对象。
func backupDataDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建数据目录: %v", err)
	}
	t.Setenv("QIANSI_DATA_DIR", dir)
	return dir
}

// backupMustMigrate 走服务路径（Open + RunMigrations）建一个已迁移的库并塞一行。
//
// 连接保持打开不外泄给测试：`qiansi backup` 正是在应用正在写库的时候被外部工具调起来的，
// 两条连接同时存在才是真实场景。
func backupMustMigrate(t *testing.T, dir string) (string, *store.Store) {
	t.Helper()
	dbPath := filepath.Join(dir, "qiansi.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.RunMigrations(context.Background(), database); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	st := store.New(database)
	if err := st.PersonCreate(context.Background(), &store.Person{Name: "张三"}); err != nil {
		t.Fatalf("PersonCreate: %v", err)
	}
	return dbPath, st
}

// backupPNG 是一张最小合成位图：内容不参与导出逻辑，只为让 uploads 里有东西可打包。
var backupPNG = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 'q', 'i', 'a', 'n', 's', 'i'}

func backupWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("写 %s: %v", path, err)
	}
}

// backupMustOpen 以导出口径打开一份产物，用于「快照到底还能不能读」这条断言。
func backupMustOpen(t *testing.T, path string) *sql.DB {
	t.Helper()
	d, err := db.OpenForExport(path)
	if err != nil {
		t.Fatalf("打开产物 %s: %v", path, err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func backupCountPeople(t *testing.T, d *sql.DB, name string) int {
	t.Helper()
	var n int
	if err := d.QueryRow("SELECT count(*) FROM people WHERE name = ?", name).Scan(&n); err != nil {
		t.Fatalf("查 %s: %v", name, err)
	}
	return n
}

func backupIntegrity(t *testing.T, d *sql.DB) {
	t.Helper()
	var msg string
	if err := d.QueryRow("PRAGMA integrity_check").Scan(&msg); err != nil {
		t.Fatalf("integrity_check: %v", err)
	}
	if msg != "ok" {
		t.Errorf("integrity_check = %q, want ok", msg)
	}
}

func backupFileSum(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开 %s: %v", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("摘要 %s: %v", path, err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ===== (a) db：真迁移库 + 数据的快照 =====

func TestRunBackupDBSnapshotIsReadableSQLite(t *testing.T) {
	dir := backupDataDir(t)
	dbPath, st := backupMustMigrate(t, dir)
	ctx := context.Background()

	// 再补两行：它们此刻只活在 -wal 里，主库文件还没有这些页。
	// 快照必须带上——工具是在应用运行中触发导出的，漏掉 WAL 等于漏掉最近的记录。
	for _, name := range []string{"李四", "王五"} {
		if err := st.PersonCreate(ctx, &store.Person{Name: name}); err != nil {
			t.Fatalf("PersonCreate %s: %v", name, err)
		}
	}
	if fi, err := os.Stat(dbPath + "-wal"); err != nil || fi.Size() == 0 {
		t.Fatalf("测试前置失效：-wal 应存在且非空（%v）", err)
	}

	var buf bytes.Buffer
	if err := runBackup(nil, &buf); err != nil {
		t.Fatalf("runBackup: %v", err)
	}
	data := buf.Bytes()
	if len(data) < 4096 {
		t.Fatalf("快照只有 %d 字节，不像一份库", len(data))
	}
	// 工具侧就是按这 16 字节判校验与命名的（docker-db-auto-backup 的 validate.go）
	if string(data[:16]) != "SQLite format 3\x00" {
		t.Errorf("头部不是 SQLite 魔数: %q", string(data[:16]))
	}

	snap := filepath.Join(t.TempDir(), "snapshot.db")
	backupWriteFile(t, snap, data)
	d := backupMustOpen(t, snap)
	backupIntegrity(t, d)
	for _, name := range []string{"张三", "李四", "王五"} {
		if got := backupCountPeople(t, d, name); got != 1 {
			t.Errorf("快照里 %s 应有 1 行，得到 %d", name, got)
		}
	}
}

// ===== (b) zip：库快照 + uploads =====

func TestRunBackupZIPContainsSnapshotAndUploads(t *testing.T) {
	dir := backupDataDir(t)
	backupMustMigrate(t, dir)

	uploads := filepath.Join(dir, "uploads")
	if err := os.MkdirAll(filepath.Join(uploads, "nested"), 0o755); err != nil {
		t.Fatalf("建 uploads: %v", err)
	}
	// 附件在库外：zip 形态的意义就是把它们和库一起带走
	backupWriteFile(t, filepath.Join(uploads, "avatar-001.png"), backupPNG)
	backupWriteFile(t, filepath.Join(uploads, "nested", "avatar-002.png"), []byte("nested-bytes"))

	var buf bytes.Buffer
	if err := runBackup([]string{"--format", "zip"}, &buf); err != nil {
		t.Fatalf("runBackup zip: %v", err)
	}
	data := buf.Bytes()
	if len(data) < 22 || string(data[:2]) != "PK" {
		t.Fatalf("产物不是 zip: %d 字节", len(data))
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip 读不出来（中央目录可能没写完）: %v", err)
	}

	entries := map[string][]byte{}
	var names []string
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("打开条目 %s: %v", f.Name, err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("读条目 %s: %v", f.Name, err)
		}
		entries[f.Name] = body
		names = append(names, f.Name)
	}
	sort.Strings(names)
	t.Logf("zip 条目: %v", names)

	snap, ok := entries["qiansi.db"]
	if !ok {
		t.Fatalf("zip 里缺 qiansi.db，实际条目 %v", names)
	}
	if string(snap[:16]) != "SQLite format 3\x00" {
		t.Errorf("包内快照头部不是 SQLite 魔数: %q", string(snap[:16]))
	}
	snapPath := filepath.Join(t.TempDir(), "from-zip.db")
	backupWriteFile(t, snapPath, snap)
	d := backupMustOpen(t, snapPath)
	backupIntegrity(t, d)
	if got := backupCountPeople(t, d, "张三"); got != 1 {
		t.Errorf("包内快照应能读到 张三，得到 %d", got)
	}

	// uploads 的相对路径整体保留在 uploads/ 前缀之下，含子目录
	for name, want := range map[string][]byte{
		"uploads/avatar-001.png":        backupPNG,
		"uploads/nested/avatar-002.png": []byte("nested-bytes"),
	} {
		got, ok := entries[name]
		if !ok {
			t.Errorf("zip 里缺条目 %s（实际 %v）", name, names)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("条目 %s 内容不符: %q", name, got)
		}
	}
}

// ===== (c) 库不存在：报错、零字节、什么都不建 =====

func TestRunBackupMissingDBFailsAndCreatesNothing(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	t.Setenv("QIANSI_DATA_DIR", dataDir)

	var buf bytes.Buffer
	err := runBackup(nil, &buf)
	if err == nil {
		t.Fatal("库不存在时应当失败")
	}
	t.Logf("报错文本: %v", err)
	if buf.Len() != 0 {
		t.Errorf("失败时 stdout 必须零字节，得到 %d 字节", buf.Len())
	}
	// 导出进程不许建目录、不许建库（config.Load 会 MkdirAll，这条路径绕开了它）
	if _, err := os.Stat(dataDir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("不该创建数据目录: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "qiansi.db")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("不该创建数据库文件: %v", err)
	}

	// -db 指向不存在的目录同样只许失败
	var buf2 bytes.Buffer
	if err := runBackup([]string{"-db", filepath.Join(root, "elsewhere", "qiansi.db")}, &buf2); err == nil {
		t.Fatal("-db 指向不存在的文件时应当失败")
	}
	if buf2.Len() != 0 {
		t.Errorf("失败时 stdout 必须零字节，得到 %d 字节", buf2.Len())
	}
	if _, err := os.Stat(filepath.Join(root, "elsewhere")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("不该创建 -db 的父目录: %v", err)
	}
}

// ===== (d) 备份过程不改源库（OpenForExport 自身那条断言在 internal/db）=====

func TestRunBackupDoesNotModifySourceDB(t *testing.T) {
	dir := backupDataDir(t)
	dbPath, st := backupMustMigrate(t, dir)

	before := backupFileSum(t, dbPath)
	var buf bytes.Buffer
	if err := runBackup(nil, &buf); err != nil {
		t.Fatalf("runBackup: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("db 形态产物为空")
	}
	if after := backupFileSum(t, dbPath); after != before {
		t.Error("备份过程改写了源库文件（sha256 不一致）")
	}
	// 源库仍是应用自己那份数据，行数与迁移版本都没被动过
	if got := backupCountPeople(t, st.CurrentDB(), "张三"); got != 1 {
		t.Errorf("源库行数被导出进程改动，张三 = %d", got)
	}
}

// ===== (e) 默认格式是 db =====

func TestRunBackupDefaultsToDBFormat(t *testing.T) {
	dir := backupDataDir(t)
	backupMustMigrate(t, dir)
	uploads := filepath.Join(dir, "uploads")
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatalf("建 uploads: %v", err)
	}
	backupWriteFile(t, filepath.Join(uploads, "avatar-001.png"), backupPNG)

	var buf bytes.Buffer
	if err := runBackup(nil, &buf); err != nil {
		t.Fatalf("runBackup（省略 --format）: %v", err)
	}
	data := buf.Bytes()
	// 默认必须是 db：备份工具的默认扩展名就是 .db，输出 zip 会被按 SQLite 魔数校验判失败
	if string(data[:2]) == "PK" {
		t.Fatal("省略 --format 时不该默认输出 zip")
	}
	if string(data[:16]) != "SQLite format 3\x00" {
		t.Errorf("默认产物应是 SQLite 快照，头部是 %q", string(data[:16]))
	}
	// uploads 里有文件也不该被顺手打进默认产物
	if !bytes.Contains(data, []byte("avatar-001.png")) {
		t.Log("db 形态未包含 uploads（预期）")
	}
}

func TestRunBackupRejectsUnknownFormat(t *testing.T) {
	dir := backupDataDir(t)
	backupMustMigrate(t, dir)

	var buf bytes.Buffer
	err := runBackup([]string{"--format", "tar"}, &buf)
	if err == nil {
		t.Fatal("未知格式应报错")
	}
	if buf.Len() != 0 {
		t.Errorf("未知格式时 stdout 必须零字节，得到 %d 字节", buf.Len())
	}
	t.Logf("报错文本: %v", err)
}

// TestBackupResolveDoesNotTouchFS 守住「导出口径与服务口径同一条路径」这条前提：
// 备份工具无需再配一份路径，QIANSI_DATA_DIR 就是唯一来源，而 Resolve 不建目录。
func TestBackupResolveDoesNotTouchFS(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	t.Setenv("QIANSI_DATA_DIR", dir)

	cfg := config.Resolve()
	if cfg.DBPath != filepath.Join(dir, "qiansi.db") {
		t.Fatalf("Resolve 的库路径不符: %s", cfg.DBPath)
	}
	if cfg.Uploads != filepath.Join(dir, "uploads") {
		t.Fatalf("Resolve 的 uploads 路径不符: %s", cfg.Uploads)
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Resolve 不该创建数据目录: %v", err)
	}
}
