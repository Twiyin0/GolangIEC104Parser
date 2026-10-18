package pointdb

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// buildTestDB 用 modernc/sqlite 动态创建与参考项目同结构的点表数据库。
func buildTestDB(t *testing.T) string {
	path := filepath.Join(t.TempDir(), "point.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	schema := []string{
		`CREATE TABLE ProjectYcPoint (Addr INTEGER, BoardPoint INTEGER, Name TEXT, SwitchPoint INTEGER, SwitchName TEXT, TableName TEXT)`,
		`CREATE TABLE ProjectYxPoint (Addr INTEGER, BoardPoint INTEGER, Name TEXT, SwitchPoint INTEGER, SwitchName TEXT, TableName TEXT)`,
		`CREATE TABLE ProjectYkPoint (Addr INTEGER, BoardPoint INTEGER, Name TEXT, SwitchPoint INTEGER, SwitchName TEXT, TableName TEXT)`,
		`CREATE TABLE ProjectEnergyPoint (Addr INTEGER, Name TEXT, SwitchPoint INTEGER, SwitchName TEXT, TableName TEXT)`,
		`CREATE TABLE ProjectParaTransferPoint (UpAddr INTEGER, Name TEXT, TableName TEXT)`,
		`CREATE TABLE ProjectGWParamPoint (Addr INTEGER, Name TEXT, Rate TEXT, ParaType TEXT, StrEncodeName TEXT)`,
		`CREATE TABLE ProjectNWParamPoint (Addr INTEGER, Name TEXT, Rate TEXT, ParaType TEXT, StrEncodeName TEXT)`,
	}
	for _, s := range schema {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	ycRows := [][]any{
		{101, 1, "10kV进线A相电压", 201, "I段10kV进线电压", "1#主变"},
		{101, 1, "全点表电压名", nil, nil, "全点表"},
		{102, 2, "无表名电流", nil, nil, nil},
		// NULL 容错场景：全点表名称为 NULL（不得覆盖 1#主变 的名称）
		{103, 3, nil, nil, nil, "全点表"},
		{103, 3, "103常规电压名", nil, nil, "1#主变"},
		// NULL Addr（不得落到 0 号地址）
		{nil, 9, "孤儿点", nil, nil, "1#主变"},
		// 字面 "NULL" 字符串名称（视为无名称）
		{104, 4, "NULL", nil, nil, "1#主变"},
		// 合法 0 号地址
		{0, 0, "零号遥测点", nil, nil, "1#主变"},
	}
	yxRows := [][]any{
		{101, 1, "断路器合位", 201, "I段断路器合位", "1#主变"},
		{101, 1, "全点表遥信名", nil, nil, "全点表"},
	}
	ykRows := [][]any{
		{101, 1, "遥控合闸", 201, "I段遥控合闸", "1#主变"},
		// SwitchPoint 为 NULL 的切换行（不得落到 0 号切换地址）
		{202, 2, "遥控分闸", nil, "I段遥控分闸", "1#主变"},
	}
	enRows := [][]any{{101, "正向有功电能", 201, "I段正向有功", "1#主变"}}
	paraRows := [][]any{
		{101, "保护定值1区电流定值", "1#主变"},
		// UpAddr 为 NULL（不得落到 0 号参数地址）
		{nil, "坏参数名", "1#主变"},
	}
	insert := func(table string, rows [][]any, n int) {
		for _, r := range rows {
			q := "INSERT INTO " + table + " VALUES (" + placeholders(n) + ")"
			if _, err := db.Exec(q, r...); err != nil {
				t.Fatal(table, err)
			}
		}
	}
	insert("ProjectYcPoint", ycRows, 6)
	insert("ProjectYxPoint", yxRows, 6)
	insert("ProjectYkPoint", ykRows, 6)
	insert("ProjectEnergyPoint", enRows, 5)
	insert("ProjectParaTransferPoint", paraRows, 3)
	// 定值表：国网/南网（同一 Addr 名称不同，用于区分选择）
	gwRows := [][]any{
		{101, "国网-过流I段定值", "0.01", "短整形", "0x0065"},
		{102, "国网-零序定值", "0.1", "无符号短整形", "0x0066"},
	}
	nwRows := [][]any{
		{101, "南网-过流I段定值", "0.02", "整形", "0x65"},
	}
	insert("ProjectGWParamPoint", gwRows, 5)
	insert("ProjectNWParamPoint", nwRows, 5)
	return path
}

func placeholders(n int) string {
	s := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			s += ","
		}
		s += "?"
	}
	return s
}

func TestPointDB(t *testing.T) {
	path := buildTestDB(t)
	d, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.TableNames) != 2 || d.TableNames[0] != "1#主变" || d.TableNames[1] != "全点表" {
		t.Fatalf("tableNames wrong: %v", d.TableNames)
	}
	// 全部模式（默认）：优先级 普通表名 > 无表名 > 全点表（覆盖）
	if got := d.PtName("yc", 101); got != "全点表电压名" {
		t.Fatalf("all-mode yc: %q", got)
	}
	if got := d.PtName("yc", 102); got != "无表名电流" {
		t.Fatalf("null-table yc: %q", got)
	}
	if got := d.PtName("yx", 101); got != "全点表遥信名" {
		t.Fatalf("all-mode yx: %q", got)
	}
	if got := d.PtName("yk", 101); got != "遥控合闸" {
		t.Fatalf("all-mode yk: %q", got)
	}
	if got := d.PtName("en", 101); got != "正向有功电能" {
		t.Fatalf("all-mode en: %q", got)
	}
	if got := d.PtName("para", 101); got != "保护定值1区电流定值" {
		t.Fatalf("para name: %q", got)
	}
	if got := d.PtName("yc", 999); got != "" {
		t.Fatalf("missing addr should be empty: %q", got)
	}
	// 定值表：未选择时回落到参数转发表（ProjectParaTransferPoint）
	if got := d.PtName("para", 101); got != "保护定值1区电流定值" {
		t.Fatalf("para(no table) = %q", got)
	}
	// 选择国网定值表：定值信息体地址 = Addr（十进制），名称 = Name
	if !d.HasGWParam || !d.HasNWParam {
		t.Fatalf("应识别到国网/南网定值表: gw=%v nw=%v", d.HasGWParam, d.HasNWParam)
	}
	d.SetParamTable(ParamTableGW)
	if got := d.PtName("para", 101); got != "国网-过流I段定值" {
		t.Fatalf("国网定值表点名 = %q", got)
	}
	if p, ok := d.ParamPoint(101); !ok || p.Rate != "0.01" || p.ParaType != "短整形" || p.StrEncodeName != "0x0065" {
		t.Fatalf("国网定值点信息异常: %+v ok=%v", p, ok)
	}
	// 切换南网定值表
	d.SetParamTable(ParamTableNW)
	if got := d.PtName("para", 101); got != "南网-过流I段定值" {
		t.Fatalf("南网定值表点名 = %q", got)
	}
	if got := d.PtName("para", 102); got != "保护定值1区电流定值" {
		// 南网表无 102 → 回落参数转发表（102 也不在转发表 → 应为空）
		if got != "" {
			t.Fatalf("南网未命中应回落转发表（102 应为空）: %q", got)
		}
	}
	d.SetParamTable(ParamTableNone)
	// NULL 容错：全点表名称为 NULL → 不得覆盖常规表名，也绝不返回空名占位
	if got := d.PtName("yc", 103); got != "103常规电压名" {
		t.Fatalf("NULL-name full-table row must not override: %q", got)
	}
	// NULL Addr → 不得落到 0 号地址；0 号地址应由真实行命中
	if got := d.PtName("yc", 0); got != "零号遥测点" {
		t.Fatalf("addr 0 must hit real row, not NULL-addr orphan: %q", got)
	}
	// 字面 "NULL" 名称视为无名称
	if got := d.PtName("yc", 104); got != "" {
		t.Fatalf("literal NULL name must be treated empty: %q", got)
	}
	// 具体表模式：SwitchPoint 为 NULL 的切换行不得落到 0 号切换地址
	d.RebuildMaps("1#主变")
	if got := d.PtName("yk", 0); got != "" {
		t.Fatalf("NULL SwitchPoint must not hit switch addr 0: %q", got)
	}
	if got := d.PtName("para", 0); got != "" {
		t.Fatalf("NULL UpAddr must not hit para addr 0: %q", got)
	}
	// 遥测转发规则：选定具体表时 信息体地址 = 0x4000 + SwitchPoint，点名为 SwitchName
	if got := d.PtName("yc", BaseYC+201); got != "I段10kV进线电压" {
		t.Fatalf("switch-mode yc(0x4000+201) = %q（应为 I段10kV进线电压）", got)
	}
	if got := d.PtName("yc", 201); got != "" {
		t.Fatalf("遥测转发不应按裸 SwitchPoint 命中（201 → %q）", got)
	}
	// 遥信 0x0000 + SwitchPoint；遥控 0x6000 + SwitchPoint；电能量 0x6400 + SwitchPoint
	if got := d.PtName("yx", BaseYX+201); got != "I段断路器合位" {
		t.Fatalf("switch-mode yx(0x0000+201): %q", got)
	}
	if got := d.PtName("yk", BaseYK+201); got != "I段遥控合闸" {
		t.Fatalf("switch-mode yk(0x6000+201): %q", got)
	}
	if got := d.PtName("en", BaseEN+201); got != "I段正向有功" {
		t.Fatalf("switch-mode en(0x6400+201): %q", got)
	}
	if got := d.PtName("yc", 101); got != "全点表电压名" { // 无切换项 → 全表兜底
		t.Fatalf("switch-mode fallback yc(101): %q", got)
	}
}

func TestLoadInvalid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "empty.db")
	os.WriteFile(p, []byte("not a sqlite file"), 0o644)
	if _, err := Load(p); err == nil {
		t.Fatal("expect error for invalid db")
	}
}

// 101/104 可各自选择不同点表模式：一个选具体转发表、另一个保持全点表（反之亦可）。
func TestPointDBPerProtocol(t *testing.T) {
	d, err := Load(buildTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	// 101 选 1#主变（转发），104 保持全部
	d.SetTable("101", "1#主变")
	d.SetTable("104", "")
	if got := d.PtNameFor("101", "yc", BaseYC+201); got != "I段10kV进线电压" {
		t.Fatalf("101 转发表遥测 = %q", got)
	}
	if got := d.PtNameFor("101", "yc", 101); got != "全点表电压名" {
		t.Fatalf("101 转发表未命中应回落全表 = %q", got)
	}
	if got := d.PtNameFor("104", "yc", 101); got != "全点表电压名" {
		t.Fatalf("104 全部模式遥测 = %q", got)
	}
	if got := d.PtNameFor("104", "yc", BaseYC+201); got != "" {
		t.Fatalf("104 全部模式不应命中 0x4000+201 转发地址 = %q", got)
	}
	// 反过来：104 选转发表，101 全部
	d.SetTable("104", "1#主变")
	d.SetTable("101", "")
	if got := d.PtNameFor("104", "yc", BaseYC+201); got != "I段10kV进线电压" {
		t.Fatalf("104 转发表遥测 = %q", got)
	}
	if got := d.PtNameFor("101", "yc", BaseYC+201); got != "" {
		t.Fatalf("101 全部模式不应命中转发地址 = %q", got)
	}
	if d.CurTable101 != "" || d.CurTable104 != "1#主变" {
		t.Fatalf("协议选择状态错误: 101=%q 104=%q", d.CurTable101, d.CurTable104)
	}
}
