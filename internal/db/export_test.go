package db

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// exportLegacyDB 造一个「schema 落后于当前迁移」的库：只有 schema_version 表，
// 没有 people 等任何业务表——正是老数据文件升级前的样子。
// 用它验证导出路径不会顺手把迁移跑了。
func exportLegacyDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "old.db")
	d, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("造旧库: %v", err)
	}
	if _, err := d.Exec("CREATE TABLE schema_version(version INTEGER)"); err != nil {
		d.Close()
		t.Fatalf("造旧库: %v", err)
	}
	if _, err := d.Exec("INSERT INTO schema_version(version) VALUES(1)"); err != nil {
		d.Close()
		t.Fatalf("造旧库: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("关闭造库连接: %v", err)
	}
	return path
}

func exportFileSum(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "!读取失败: " + err.Error()
	}
	sum := sha256.Sum256(data)
	return string(sum[:])
}

// TestOpenForExportLeavesSourceUntouched 锁住导出打开方式的两条底线：
// 不迁移（旧库的表结构原样）、不写库头（连 journal_mode 都不改）。
//
// 导出进程是在应用运行时被外部工具调起来的，它只读数据；一旦它把 schema 改了，
// 就等于「跑一次备份把应用升级了」——出问题还查不到是谁动的手。
func TestOpenForExportLeavesSourceUntouched(t *testing.T) {
	path := exportLegacyDB(t)
	before := exportFileSum(path)

	d, err := OpenForExport(path)
	if err != nil {
		t.Fatalf("OpenForExport: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	var ver int
	if err := d.QueryRow("SELECT version FROM schema_version").Scan(&ver); err != nil {
		t.Fatalf("读取原表: %v", err)
	}
	if ver != 1 {
		t.Errorf("原表内容应原样可读，得到 %d", ver)
	}

	// 迁移不该被触发：people 表仍然不存在
	if _, err := d.Exec("SELECT 1 FROM people"); err == nil {
		t.Error("导出打开时不应把迁移跑完（people 表凭空出现了）")
	}

	// journal_mode 保持 delete：OpenForExport 的 DSN 里没有 journal_mode pragma，
	// 导出连接不该把目标库切成 WAL（那是改库头）。
	var journal string
	if err := d.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatalf("查 journal_mode: %v", err)
	}
	if journal != "delete" {
		t.Errorf("journal_mode 被改成了 %q，导出连接不该动库头", journal)
	}
	d.Close()

	if after := exportFileSum(path); after != before {
		t.Error("数据文件被导出打开过程改写过（sha256 不一致）")
	}
}

// TestOpenForExportMissingFile 库不存在时报错，而且一个字节都不留下：
// SQLite 驱动默认可写打开会顺手建出空库，导出进程交出空文件比失败更糟
// ——备份工具会把它当成一次成功备份归档。
func TestOpenForExportMissingFileCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nope", "qiansi.db")

	d, err := OpenForExport(path)
	if err == nil {
		d.Close()
		t.Fatal("库文件不存在时应报错")
	}
	if _, err := os.Stat(filepath.Join(dir, "nope")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("不该创建缺失的目录，得到 %v", err)
	}
}
