package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSnapshotStreamsReadableSQLite(t *testing.T) {
	database, dbPath := newTestDB(t)

	var buf bytes.Buffer
	if err := WriteSnapshot(context.Background(), database, dbPath, &buf); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}
	if string(buf.Bytes()[:16]) != "SQLite format 3\x00" {
		t.Errorf("头部不是 SQLite 魔数: %q", string(buf.Bytes()[:16]))
	}
	// 临时目录由 defer 清掉：数据目录里不该多出中间产物
	if _, err := os.Stat(dbPath + "-snapshot.db"); err == nil {
		t.Error("快照不该落在数据目录里")
	}
}

// TestWriteZIPWithoutUploadsDir 一个还没上传过图片的库是正常的：
// uploads 不存在时 zip 仍要成功，包里只有快照。
func TestWriteZIPWithoutUploadsDir(t *testing.T) {
	database, dbPath := newTestDB(t)
	missing := filepath.Join(t.TempDir(), "uploads")

	var buf bytes.Buffer
	if err := WriteZIP(context.Background(), database, dbPath, missing, &buf); err != nil {
		t.Fatalf("uploads 目录不存在时 zip 应照常成功: %v", err)
	}
	names := zipNames(t, buf.Bytes())
	if len(names) != 1 || names[0] != ZipDBEntry {
		t.Errorf("只应有快照一个条目，得到 %v", names)
	}
}

// TestWriteZIPSkipsNonRegular 符号链接一类非常规文件不进包：
// 备份里塞一条指向数据目录之外的链接，回档时就是一次越界写。
func TestWriteZIPSkipsNonRegularFiles(t *testing.T) {
	database, dbPath := newTestDB(t)
	uploads := t.TempDir()
	real := filepath.Join(uploads, "real.png")
	if err := os.WriteFile(real, []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(uploads, "link.png")); err != nil {
		t.Skipf("该环境不支持符号链接: %v", err)
	}

	var buf bytes.Buffer
	if err := WriteZIP(context.Background(), database, dbPath, uploads, &buf); err != nil {
		t.Fatalf("WriteZIP: %v", err)
	}
	names := zipNames(t, buf.Bytes())
	want := map[string]bool{ZipDBEntry: true, ZipUploadsDir + "real.png": true}
	for _, n := range names {
		if !want[n] {
			t.Errorf("条目 %q 不该出现在包里（非常规文件应被跳过）", n)
		}
		delete(want, n)
	}
	for n := range want {
		t.Errorf("缺少条目 %q", n)
	}
}

func zipNames(t *testing.T, data []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip 读不出来（中央目录可能没写完）: %v", err)
	}
	names := make([]string, 0, len(zr.File))
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
		if f.Name == ZipDBEntry && string(body[:16]) != "SQLite format 3\x00" {
			t.Errorf("包内快照头部不是 SQLite 魔数")
		}
		names = append(names, f.Name)
	}
	return names
}
