package exporter_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"iec104rt/internal/exporter"
	"iec104rt/internal/importer"
	"iec104rt/internal/protocol"
)

// buildFrames 构造一组典型帧（104 + 101）。
func buildFrames(t *testing.T) []protocol.Frame {
	mk := func(hex string) protocol.Frame {
		f, err := protocol.ParseHexAuto(hex)
		if err != nil {
			t.Fatalf("parse %s: %v", hex, err)
		}
		return f
	}
	frames := []protocol.Frame{
		mk("68 04 07 00 00 00"), // STARTDT_ACT
		mk("68 0E 04 00 00 00 64 01 06 00 01 00 00 00 00 14"),
		mk("68 12 04 00 00 00 0D 01 03 00 01 00 01 00 00 00 00 00 80 3F 00"),
		mk("68 12 04 00 00 00 03 02 03 00 01 00 01 00 00 02 02 00 00 03"),
		mk("10 00 01 00 01 16"), // 101 固定帧
		mk("68 0D 0D 68 73 01 00 64 01 06 00 01 00 00 00 14 F4 16"), // 101 可变帧总召唤
	}
	for i := range frames {
		frames[i].Time = "2026-10-16 15:04:05.00" + string(rune('0'+i%10))
		if frames[i].Dir == "" {
			frames[i].Dir = "M→T"
		}
	}
	return frames
}

// TestExportImportRoundTrip 导出（md/text/log）→ 导入 → 重解析闭环（需求 8/9 条）。
func TestExportImportRoundTrip(t *testing.T) {
	frames := buildFrames(t)
	dir := t.TempDir()

	// markdown 层级硬规范（需求 8.a~8.d）：# 文件名一次；##/### 每帧一组
	mdPath := filepath.Join(dir, "out.md")
	if err := exporter.ExportMarkdown(mdPath, "测试文件", frames); err != nil {
		t.Fatal(err)
	}
	md, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(md)
	if !strings.HasPrefix(s, "# 测试文件") {
		t.Fatalf("一级标题应为文件名：%.60s", s)
	}
	if strings.Count(s, "\n# ") != 0 {
		t.Fatal("一级标题出现多次")
	}
	if strings.Count(s, "\n## ") != len(frames) || strings.Count(s, "\n### ") != len(frames) {
		t.Fatalf("二/三级标题数应=%d，实际 ##=%d ###=%d",
			len(frames), strings.Count(s, "\n## "), strings.Count(s, "\n### "))
	}
	if !strings.Contains(s, "遥测") || !strings.Contains(s, "信息对象地址") {
		t.Fatal("正文表格缺失")
	}
	back, err := importer.ImportFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != len(frames) {
		t.Fatalf("md 导入条数 %d != %d", len(back), len(frames))
	}
	for i := range back {
		if back[i].Type != frames[i].Type || back[i].Proto != frames[i].Proto {
			t.Fatalf("md 往返不一致 #%d: %v/%v vs %v/%v", i,
				back[i].Type, back[i].Proto, frames[i].Type, frames[i].Proto)
		}
	}

	// text / log 闭环
	cases := []struct {
		name string
		fn   func(string, []protocol.Frame) error
	}{
		{"out.txt", exporter.ExportText},
		{"out.log", exporter.ExportLog},
	}
	for _, c := range cases {
		p := filepath.Join(dir, c.name)
		if err := c.fn(p, frames); err != nil {
			t.Fatal(err)
		}
		b, err := importer.ImportFile(p)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(b) != len(frames) {
			t.Fatalf("%s 导入条数 %d != %d", c.name, len(b), len(frames))
		}
		for i := range b {
			if b[i].Type != frames[i].Type {
				t.Fatalf("%s 往返类型不一致 #%d: %v vs %v", c.name, i, b[i].Type, frames[i].Type)
			}
		}
	}
}
