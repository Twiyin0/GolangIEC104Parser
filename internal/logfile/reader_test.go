package logfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReaderIncremental(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.log")
	if err := os.WriteFile(path, []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, _ := NewReader(path, "auto")
	lines, err := r.ReadNew(true)
	if err != nil || len(lines) != 2 || lines[0] != "line1" || lines[1] != "line2" {
		t.Fatalf("首次读取错误: %v %#v", err, lines)
	}
	// 重复读取不应重复返回
	lines, _ = r.ReadNew(true)
	if len(lines) != 0 {
		t.Fatalf("重复读取产生重复行: %#v", lines)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("line3\n")
	_ = f.Close()
	lines, _ = r.ReadNew(true)
	if len(lines) != 1 || lines[0] != "line3" {
		t.Fatalf("增量读取错误: %#v", lines)
	}
}

func TestReaderTailAndRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.log")
	// 足够长的历史，验证从尾部启动会丢弃第一段半行
	if err := os.WriteFile(path, []byte("old-1\nold-2\nnew-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, _ := NewReader(path, "auto")
	if err := r.StartFromTail(6); err != nil {
		t.Fatal(err)
	}
	lines, _ := r.ReadNew(true)
	if len(lines) != 1 || lines[0] != "new-1" {
		t.Fatalf("tail 读取错误: %#v", lines)
	}
	// 模拟截断/轮转：文件变短后从 0 重读
	if err := os.WriteFile(path, []byte("rotated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, _ = r.ReadNew(true)
	if len(lines) != 1 || lines[0] != "rotated" {
		t.Fatalf("轮转读取错误: %#v", lines)
	}
}

func TestReaderFinalPartialLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.log")
	if err := os.WriteFile(path, []byte("no-newline"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, _ := NewReader(path, "auto")
	lines, _ := r.ReadNew(true)
	if len(lines) != 1 || lines[0] != "no-newline" {
		t.Fatalf("末尾半行读取错误: %#v", lines)
	}
}
