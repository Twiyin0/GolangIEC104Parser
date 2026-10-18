package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"iec104rt/internal/protocol"
	"iec104rt/internal/protocol/common"
)

// buildPointDBForTest 创建最小点表库（遥测/遥信/遥控/参数转发表各一行）。
func buildPointDBForTest(t *testing.T) string {
	path := filepath.Join(t.TempDir(), "point.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE ProjectYcPoint (Addr INTEGER, BoardPoint INTEGER, Name TEXT, SwitchPoint INTEGER, SwitchName TEXT, TableName TEXT)`,
		`CREATE TABLE ProjectYxPoint (Addr INTEGER, BoardPoint INTEGER, Name TEXT, SwitchPoint INTEGER, SwitchName TEXT, TableName TEXT)`,
		`CREATE TABLE ProjectYkPoint (Addr INTEGER, BoardPoint INTEGER, Name TEXT, SwitchPoint INTEGER, SwitchName TEXT, TableName TEXT)`,
		`CREATE TABLE ProjectEnergyPoint (Addr INTEGER, Name TEXT, SwitchPoint INTEGER, SwitchName TEXT, TableName TEXT)`,
		`CREATE TABLE ProjectParaTransferPoint (UpAddr INTEGER, Name TEXT, TableName TEXT)`,
		`CREATE TABLE ProjectGWParamPoint (Addr INTEGER, Name TEXT, Rate TEXT, ParaType TEXT, StrEncodeName TEXT)`,
		`CREATE TABLE ProjectNWParamPoint (Addr INTEGER, Name TEXT, Rate TEXT, ParaType TEXT, StrEncodeName TEXT)`,
		`INSERT INTO ProjectYcPoint VALUES (1, 1, '10kV进线A相电压', 201, 'I段10kV进线电压', '1#主变')`,
		`INSERT INTO ProjectYxPoint VALUES (1, 1, '断路器合位', 201, 'I段断路器合位', '1#主变')`,
		`INSERT INTO ProjectYkPoint VALUES (1, 1, '遥控合闸', 201, 'I段遥控合闸', '1#主变')`,
		`INSERT INTO ProjectEnergyPoint VALUES (1, '正向有功电能', 201, 'I段正向有功', '1#主变')`,
		`INSERT INTO ProjectParaTransferPoint VALUES (101, '保护定值1区电流定值', '1#主变')`,
		`INSERT INTO ProjectGWParamPoint VALUES (101, '国网-过流I段定值', '0.01', '短整形', '0x0065')`,
		`INSERT INTO ProjectNWParamPoint VALUES (101, '南网-过流I段定值', '0.02', '整形', '0x65')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func parseOne(t *testing.T, hex string) protocol.Frame {
	t.Helper()
	f, err := protocol.ParseHexAuto(hex)
	if err != nil {
		t.Fatalf("parse %s: %v", hex, err)
	}
	return f
}

func dataOf(t *testing.T, f protocol.Frame) []common.M {
	t.Helper()
	d, ok := f.Parsed["data"].([]common.M)
	if !ok {
		t.Fatalf("data 类型断言失败（type=%v, %T）", f.Type, f.Parsed["data"])
	}
	return d
}

// TestEnrichPointNames 点表加载后，解析结果必须注入点名/参数名（回归：类型断言 bug）。
func TestEnrichPointNames(t *testing.T) {
	a := NewApp()
	if _, err := a.LoadPointDb(buildPointDBForTest(t)); err != nil {
		t.Fatal(err)
	}

	// 遥测 TI=13 addr=1
	f := parseOne(t, "68 12 04 00 00 00 0D 01 03 00 01 00 01 00 00 00 00 80 3F 00")
	a.enrichFrame(&f)
	if got := dataOf(t, f)[0]["name"]; got != "10kV进线A相电压" {
		t.Fatalf("yc name = %v", got)
	}

	// 遥信 TI=3 addr=1/2（addr=2 无点表 → 不注入，避免空名）
	f = parseOne(t, "68 12 04 00 00 00 03 02 03 00 01 00 01 00 00 02 02 00 00 03")
	a.enrichFrame(&f)
	yx := dataOf(t, f)
	if yx[0]["name"] != "断路器合位" {
		t.Fatalf("yx[0] name = %v", yx[0]["name"])
	}
	if _, exists := yx[1]["name"]; exists {
		t.Fatalf("未命中点不应注入 name（不得显示空点名）: %v", yx[1])
	}

	// 遥控 TI=45 addr=0x000001
	f = parseOne(t, "68 0E 04 00 00 00 2D 01 06 00 01 00 01 00 00 81")
	a.enrichFrame(&f)
	if got := dataOf(t, f)[0]["name"]; got != "遥控合闸" {
		t.Fatalf("yk name = %v", got)
	}

	// 定值参数 TI=203(0xCB) 写预置 addr=0x000065(=101)
	f = parseOne(t, "68 14 04 00 00 00 CB 01 06 00 01 00 01 00 80 65 00 00 2B 02 0A 00")
	a.enrichFrame(&f)
	if got := dataOf(t, f)[0]["name"]; got != "保护定值1区电流定值" {
		t.Fatalf("para name = %v", got)
	}

	// 定值表（国网）：定值信息体地址 = Addr（十进制）→ 名称/比例/参数类型/十六进制地址
	if _, err := a.DbSetParamTable("gw"); err != nil {
		t.Fatal(err)
	}
	f = parseOne(t, "68 14 04 00 00 00 CB 01 06 00 01 00 01 00 80 65 00 00 2B 02 0A 00")
	a.enrichFrame(&f)
	item := dataOf(t, f)[0]
	if item["name"] != "国网-过流I段定值" {
		t.Fatalf("国网定值表点名 = %v", item["name"])
	}
	if item["rate"] != "0.01" || item["paraType"] != "短整形" || item["addrHex"] != "0x0065" {
		t.Fatalf("定值附加信息注入失败: %#v", item)
	}
	// 切南网定值表 → 名称随之变化
	if _, err := a.DbSetParamTable("nw"); err != nil {
		t.Fatal(err)
	}
	f = parseOne(t, "68 14 04 00 00 00 CB 01 06 00 01 00 01 00 80 65 00 00 2B 02 0A 00")
	a.enrichFrame(&f)
	if got := dataOf(t, f)[0]["name"]; got != "南网-过流I段定值" {
		t.Fatalf("南网定值表点名 = %v", got)
	}

	// 故障事件 TI=42：遥信点号 1 注入点名
	f = parseOne(t, "68 20 04 00 00 00 2A 01 03 00 01 00 01 01 01 00 01 80 2B 0C 22 0A 06 50 1A 01 0D 01 00 00 00 00 80 3F")
	a.enrichFrame(&f)
	fyx, ok := f.Parsed["yx"].([]common.M)
	if !ok || len(fyx) == 0 {
		t.Fatalf("fault yx missing: %#v", f.Parsed)
	}
	if fyx[0]["name"] != "断路器合位" {
		t.Fatalf("fault yx name = %v", fyx[0]["name"])
	}

	// 具体表模式：切换后点名随 SwitchPoint 生效
	if _, err := a.DbSetTable("1#主变"); err != nil {
		t.Fatal(err)
	}
	// 遥测转发点表规则：信息体地址 = 0x4000 + SwitchPoint(201) = 16585(0x40C9)
	f2 := parseOne(t, "68 12 04 00 00 00 0D 01 03 00 01 00 C9 40 00 00 00 80 3F 00")
	a.enrichFrame(&f2)
	if got := dataOf(t, f2)[0]["name"]; got != "I段10kV进线电压" {
		t.Fatalf("遥测转发点表点名解析失败（0x4000+201）: %v", got)
	}
	// 裸 SwitchPoint 地址不应命中转发表
	f3 := parseOne(t, "68 12 04 00 00 00 0D 01 03 00 01 00 C9 00 00 00 00 80 3F 00")
	a.enrichFrame(&f3)
	if got := dataOf(t, f3)[0]["name"]; got != nil {
		t.Fatalf("裸 SwitchPoint 不应命中遥测转发表: %v", got)
	}
}

// TestEnrichWithoutDB 未加载点表时不注入 name（前端不显示点名列）。
func TestEnrichWithoutDB(t *testing.T) {
	a := NewApp()
	f := parseOne(t, "68 12 04 00 00 00 0D 01 03 00 01 00 01 00 00 00 00 80 3F 00")
	a.enrichFrame(&f)
	if _, exists := dataOf(t, f)[0]["name"]; exists {
		t.Fatal("未加载点表时不应注入 name")
	}
}

// TestParseStatic101VariableFrame 静态解析必须把 101 可变帧识别为 101，
// 不能因为“按 104 切也能切出一帧”而误判为 104。
func TestParseStatic101VariableFrame(t *testing.T) {
	// 68 L L 68 F3 01 00 | ASDU(TI=13,VSQ=1,COT=20,CA=0,IOA=1,float 1.0) | CS 16
	// 101 链路地址默认 2 字节，所以 A=01 00。
	body := []byte{0xF3, 0x01, 0x00, 0x0D, 0x01, 0x14, 0x00, 0x01, 0x00, 0x00, 0x80, 0x3F}
	cs := 0
	for _, b := range body {
		cs = (cs + int(b)) & 0xFF
	}
	frame := append([]byte{0x68, byte(len(body)), byte(len(body)), 0x68}, body...)
	frame = append(frame, byte(cs), 0x16)

	a := NewApp()
	fs, err := a.ParseStatic(common.HexBytes(frame))
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Proto != "101" || fs[0].Ti != 13 {
		t.Fatalf("静态解析应识别为 101/TI13，实际: %+v", fs)
	}
}

// TestPointTablePerProtocol 101/104 可以各自选择不同点表模式，互不影响。
func TestPointTablePerProtocol(t *testing.T) {
	a := NewApp()
	if _, err := a.LoadPointDb(buildPointDBForTest(t)); err != nil {
		t.Fatal(err)
	}
	// 101 选具体转发表；104 保持全部
	if _, err := a.DbSetTableFor("101", "1#主变"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DbSetTableFor("104", ""); err != nil {
		t.Fatal(err)
	}
	if got := a.ptNameFor("yc", 0x40C9, "101"); got != "I段10kV进线电压" {
		t.Fatalf("101 转发表点名 = %q", got)
	}
	if got := a.ptNameFor("yc", 1, "104"); got != "10kV进线A相电压" {
		t.Fatalf("104 全部模式点名 = %q", got)
	}
	if got := a.ptNameFor("yc", 0x40C9, "104"); got != "" {
		t.Fatalf("104 全部模式不应命中转发地址 = %q", got)
	}
	// 反过来：104 选转发表；101 全部
	if _, err := a.DbSetTableFor("104", "1#主变"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DbSetTableFor("101", ""); err != nil {
		t.Fatal(err)
	}
	if got := a.ptNameFor("yc", 0x40C9, "104"); got != "I段10kV进线电压" {
		t.Fatalf("104 转发表点名 = %q", got)
	}
	if got := a.ptNameFor("yc", 1, "101"); got != "10kV进线A相电压" {
		t.Fatalf("101 全部模式点名 = %q", got)
	}
	info := a.dbSnapshot()
	if info.CurTable101 != "" || info.CurTable104 != "1#主变" {
		t.Fatalf("DbInfo 协议选择状态错误: 101=%q 104=%q", info.CurTable101, info.CurTable104)
	}
}
