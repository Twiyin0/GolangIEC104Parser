package logfile

import (
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestDefaultParser(t *testing.T) {
	p, err := NewParser("")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		line string
		dir  string
		msg  string
		time string
	}{
		{
			"protolog:2026/10/18-16:36:40: file:Proto.cpp ,line:461If(COM0) Tx(6) --->  10 00 01 00 01 16",
			"M→T", "10 00 01 00 01 16", "2026-10-18 16:36:40",
		},
		{
			"protolog:2026/10/18-16:36:40: file:Proto.cpp ,line:353If(COM0) Rx(18) <---   68 0C 0C 68 D3 01 00 64 01 0A 00 01 00 00 00 14 58 16",
			"T→M", "68 0C 0C 68 D3 01 00 64 01 0A 00 01 00 00 00 14 58 16", "2026-10-18 16:36:40",
		},
	}
	for _, c := range cases {
		rec, ok := p.Parse(c.line)
		if !ok {
			t.Fatalf("未识别: %s", c.line)
		}
		if rec.Dir != c.dir || rec.Message != c.msg || rec.Time != c.time {
			t.Fatalf("解析错误: %+v", rec)
		}
	}
	if _, ok := p.Parse("protolog:2026/10/18-16:36:40:!!!ScanLen[18]"); ok {
		t.Fatal("ScanLen 不应识别为报文")
	}
}

func TestCustomParser(t *testing.T) {
	// (?<name>) 应自动转换为 Go 的 (?P<name>)
	expr := `(?<time>\d{4}/\d{2}/\d{2}-\d{2}:\d{2}:\d{2}).*(?<dir>--->|<---)\s*(?<message>(?:[0-9A-Fa-f]{2}\s*)+)`
	p, err := NewParser(expr)
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := p.Parse("2026/10/18-16:36:40 Tx ---> 68 04 07 00 00 00")
	if !ok || rec.Message != "68 04 07 00 00 00" || rec.Dir != "M→T" {
		t.Fatalf("自定义正则解析错误: %+v ok=%v", rec, ok)
	}
	if _, err := NewParser(`(?<time>...)`); err == nil || !strings.Contains(err.Error(), "message") {
		t.Fatalf("缺少 message 命名组应报错: %v", err)
	}
}

func TestDecodeGBK(t *testing.T) {
	raw, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("终端日志"))
	if err != nil {
		t.Fatal(err)
	}
	if got := Decode(raw, "auto"); got != "终端日志" {
		t.Fatalf("GBK 解码失败: %q", got)
	}
}
