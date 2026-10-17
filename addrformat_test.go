package main

import (
	"testing"

	"iec104rt/internal/exporter"
	"iec104rt/internal/protocol/common"
)

// TestEnrichAddrFormat 地址显示进制：遥测/遥控/电能量/定值默认十六进制，遥信默认十进制；
// 十六进制按"高位为 0 不补位"显示（0x006401 → 0x6401）。
func TestEnrichAddrFormat(t *testing.T) {
	a := NewApp()
	// 遥测 IOA = 0x004001 → 显示 0x4001
	ycFrame := "68 12 04 00 00 00 0D 01 03 00 01 00 01 40 00 00 00 80 3F 00"
	yxFrame := "68 12 04 00 00 00 03 02 03 00 01 00 01 00 00 02 02 00 00 03" // 遥信 IOA=1

	f := parseOne(t, ycFrame)
	a.enrichFrame(&f)
	if got := dataOf(t, f)[0]["addrShow"]; got != "0x4001" {
		t.Fatalf("遥测地址默认应十六进制且不补高位: %v", got)
	}
	f = parseOne(t, yxFrame)
	a.enrichFrame(&f)
	if got := dataOf(t, f)[0]["addrShow"]; got != 1 {
		t.Fatalf("遥信地址默认应十进制: %v", got)
	}

	// 配置切换：遥测→十进制、遥信→十六进制
	cfg := a.GetConfig()
	cfg.Parse.AddrFormat.YC = "dec"
	cfg.Parse.AddrFormat.YX = "hex"
	a.UpdateConfig(cfg)
	f = parseOne(t, ycFrame)
	a.enrichFrame(&f)
	if got := dataOf(t, f)[0]["addrShow"]; got != 16385 {
		t.Fatalf("遥测地址切换为十进制后: %v", got)
	}
	f = parseOne(t, yxFrame)
	a.enrichFrame(&f)
	if got := dataOf(t, f)[0]["addrShow"]; got != "0x1" {
		t.Fatalf("遥信地址切换为十六进制后（高位 0 不补位）: %v", got)
	}
}

// TestAddrShowHexUnpadded 地址十六进制显示：3 字节地址高位为 0 时不显示（101/104 同一规则）。
func TestAddrShowHexUnpadded(t *testing.T) {
	cases := []struct {
		addr int
		want string
	}{
		{0x000001, "0x1"},
		{0x004001, "0x4001"},
		{0x006401, "0x6401"},
		{0x0000FF, "0xFF"},
		{0x00FFFF, "0xFFFF"},
	}
	for _, proto := range []string{"104", "101"} {
		for _, c := range cases {
			item := map[string]any{}
			setAddrShow(item, c.addr, "hex", proto)
			if item["addrShow"] != c.want {
				t.Fatalf("%s hex 地址显示: 0x%X → %v，期望 %s", proto, c.addr, item["addrShow"], c.want)
			}
			item = map[string]any{}
			setAddrShow(item, c.addr, "dec", proto)
			if item["addrShow"] != c.addr {
				t.Fatalf("%s dec 地址显示: %v，期望 %d", proto, item["addrShow"], c.addr)
			}
		}
	}
}

// TestNormHexShow 十六进制显示规范化：信息体地址 3 字节，高位为 0 不显示。
func TestNormHexShow(t *testing.T) {
	cases := map[string]string{
		"0x006401": "0x6401",
		"0x004001": "0x4001",
		"0x000001": "0x1",
		"0x000000": "0x0",
		"0X00AB":   "0xAB",
		"0xFFFF":   "0xFFFF",
		"无地址":      "无地址",
		"0xZZ":     "0xZZ",
		"":         "",
	}
	for in, want := range cases {
		if got := common.NormHexShow(in); got != want {
			t.Fatalf("NormHexShow(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestExportAddrStrUnpadded 导出地址显示：旧字段回退路径同样不补高位 0。
func TestExportAddrStrUnpadded(t *testing.T) {
	cases := []struct {
		item common.M
		want string
	}{
		{common.M{"addrShow": "0x6401"}, "0x6401"},
		{common.M{"addrHex": "0x006401"}, "0x6401"},
		{common.M{"hexAddr": "0x004001"}, "0x4001"},
		{common.M{"pointHex": "0x000001"}, "0x1"},
		{common.M{"addr": "0x006402"}, "0x6402"},
		{common.M{"addr": 25603}, "25603"},
	}
	for _, c := range cases {
		if got := exporter.AddrStrForTest(c.item); got != c.want {
			t.Fatalf("导出地址显示 %v = %q，期望 %q", c.item, got, c.want)
		}
	}
}

