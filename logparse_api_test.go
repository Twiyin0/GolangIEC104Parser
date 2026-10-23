package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"

	"iec104rt/internal/protocol/common"
)

// build101VarFrame 构造一帧 101 可变帧（2 字节链路地址，TI=13，1 个短浮点遥测）。
func build101VarFrame() []byte {
	body := []byte{0xF3, 0x01, 0x00, 0x0D, 0x01, 0x14, 0x00, 0x01, 0x00, 0x00, 0x80, 0x3F}
	cs := 0
	for _, b := range body {
		cs = (cs + int(b)) & 0xFF
	}
	frame := append([]byte{0x68, byte(len(body)), byte(len(body)), 0x68}, body...)
	return append(frame, byte(cs), 0x16)
}

func TestParseLogText101And104(t *testing.T) {
	v101 := common.HexBytes(build101VarFrame())
	logText := "protolog:2026/10/18-16:36:40: file:Proto.cpp ,line:461If(COM0) Tx(6) --->  68 04 07 00 00 00\n" +
		"protolog:2026/10/18-16:36:40: file:Proto.cpp ,line:353If(COM0) Rx(18) <---   " + v101 + "\n" +
		"protolog:2026/10/18-16:36:40:!!!ScanLen[18]\n" +
		"protolog:2026/10/18-16:36:40: file:Proto.cpp ,line:461If(COM0) Tx(6) --->  10 00 01 00 01 16\n" +
		"SOCK1log:2026/10/18-16:36:41: file:Proto.cpp ,line:461If(SOCK1) Tx(6) --->  " + v101 + "\n"

	a := NewApp()
	res, err := a.ParseLogText(logText)
	if err != nil {
		t.Fatal(err)
	}
	if res.HexLines != 4 || len(res.Frames) != 4 || res.Ignored != 1 {
		t.Fatalf("统计错误: hexLines=%d frames=%d ignored=%d note=%s",
			res.HexLines, len(res.Frames), res.Ignored, res.Note)
	}
	wantProto := []string{"104", "101", "101", "101"}
	wantDir := []string{"M→T", "T→M", "M→T", "M→T"}
	for i := range res.Frames {
		if res.Frames[i].Proto != wantProto[i] {
			t.Fatalf("#%d 协议=%q，期望 %q", i+1, res.Frames[i].Proto, wantProto[i])
		}
		if res.Frames[i].Dir != wantDir[i] {
			t.Fatalf("#%d 方向=%q，期望 %q", i+1, res.Frames[i].Dir, wantDir[i])
		}
		if res.Frames[i].ID != int64(i+1) {
			t.Fatalf("#%d ID=%d", i+1, res.Frames[i].ID)
		}
	}
	if res.Frames[0].Time != "2026-10-18 16:36:40" || res.Frames[3].Time != "2026-10-18 16:36:41" {
		t.Fatalf("日志时间未提取: %q / %q", res.Frames[0].Time, res.Frames[3].Time)
	}
	if res.Frames[1].Ti != 13 || res.Frames[3].Ti != 13 {
		t.Fatalf("101 可变帧 TI 应为 13，实际 #2=%d #4=%d", res.Frames[1].Ti, res.Frames[3].Ti)
	}
}

// 日志里的 101 帧被截断时，按 L 判定不完整并跳过，不能误当 104。
func TestParseLogTruncated101(t *testing.T) {
	full := build101VarFrame()
	trunc := full[:len(full)-4]
	logText := "protolog:2026/10/18-16:36:40: If(COM0) Rx(18) <--- " + common.HexBytes(trunc) + "\n"
	a := NewApp()
	res, err := a.ParseLogText(logText)
	if err != nil {
		t.Fatal(err)
	}
	if res.Truncated != 1 || len(res.Frames) != 0 || res.Ignored != 1 {
		t.Fatalf("截断 101 处理错误: %+v", res)
	}
	if !strings.Contains(res.Note, "不完整") {
		t.Fatalf("统计提示缺少不完整信息: %s", res.Note)
	}
}

func TestParseLogFileGBK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "terminal.log")
	// 中文调试行 + 104 U 帧（GBK）
	text := "终端日志：开始\nprotolog:2026/10/18-16:36:40: 调试 Tx(6) --->  68 04 07 00 00 00\n"
	enc, err := simplifiedGBK(text)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, enc, 0o644); err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	res, err := a.ParseLogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if res.FileName != "terminal.log" || len(res.Frames) != 1 || res.Frames[0].Proto != "104" {
		t.Fatalf("GBK log 解析失败: %+v", res)
	}
}

// simplifiedGBK 测试辅助：用 GB18030 编码，验证 GBK 日志回退。
func simplifiedGBK(s string) ([]byte, error) {
	return simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(s))
}
