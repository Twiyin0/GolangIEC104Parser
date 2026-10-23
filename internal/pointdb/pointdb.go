// Package pointdb 工程项目点表数据库（SQLite .db）：
// 与参考项目 VUE104Parser 的 db.ts 语义一致——
// 表 ProjectYcPoint / ProjectYxPoint / ProjectYkPoint / ProjectEnergyPoint /
// ProjectParaTransferPoint；全点表模式（普通表名 > 无表名 > "全点表" 优先级）
// 与具体表模式（SwitchPoint/SwitchName 优先、全表兜底）切换。
package pointdb

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动（无 cgo）
)

// FullTableName 全点表标识（与参考项目一致）。
const FullTableName = "全点表"

// 转发点表地址基值：选定具体点表（TableName 非 NULL 且非"全点表"且 == 用户选定表）时，
// 信息体地址 = 基值 + SwitchPoint，点名为 SwitchName。
const (
	BaseYX = 0x0000 // 遥信
	BaseYC = 0x4000 // 遥测
	BaseYK = 0x6000 // 遥控
	BaseEN = 0x6400 // 电能量
)

// TransferBaseYC 兼容旧名：遥测转发基址（= BaseYC）。
const TransferBaseYC = BaseYC

// 定值表类型（国网 / 南网）。
const (
	ParamTableNone = ""   // 不使用定值表
	ParamTableGW   = "gw" // 国网 ProjectGWParamPoint
	ParamTableNW   = "nw" // 南网 ProjectNWParamPoint
)

// ParaPoint 定值点（ProjectGWParamPoint / ProjectNWParamPoint）。
// 定值信息体地址 = Addr（十进制），名称 = Name。
type ParaPoint struct {
	Addr          int    `json:"addr"`
	Name          string `json:"name"`
	Rate          string `json:"rate"`          // 比例
	ParaType      string `json:"paraType"`      // 参数类型
	StrEncodeName string `json:"strEncodeName"` // 十六进制地址
}

// Row 点表行（Yc/Yx/Yk/Energy 通用）。
type Row struct {
	Addr        int
	BoardPoint  int
	Name        string
	SwitchPoint int
	SwitchName  string
	TableName   string // "" 表示 NULL（无表名）
}

// ParaRow 参数转发表行（Rate/ParaType 仅在库中存在这些列时读到，否则为空）。
type ParaRow struct {
	UpAddr    int
	Name      string
	Rate      string
	ParaType  string
	TableName string
}

// DB 点表数据库与派生映射。
type DB struct {
	FilePath   string
	Detail     string
	TableNames []string
	CurTable   string // 兼容旧字段：最近一次选择的表（"" = 全部模式）
	// 101 / 104 可独立选择点表模式（"" 或 "全点表" = 全部模式）
	CurTable101 string
	CurTable104 string

	// 转发点表基值（选定具体表时生效）：104 用 Base*，101 用 Base*101
	BaseYX, BaseYC, BaseYK, BaseEN int
	BaseYX101, BaseYC101           int
	BaseYK101, BaseEN101           int
	YCBase                         int // 兼容旧字段（= BaseYC）

	// 定值表（国网/南网）与当前选择
	ParamTable string
	HasGWParam bool
	HasNWParam bool
	paramGW    []ParaPoint
	paramNW    []ParaPoint
	paramMap   map[int]ParaPoint // 当前定值表：Addr → 定值点
	mapGW      map[int]ParaPoint // 国网全表映射（"无"时兜底补齐 参数类型/比例）
	mapNW      map[int]ParaPoint // 南网全表映射
	paraExtra  map[int]ParaPoint // 参数转发表中的 比例/参数类型（若库中存在这些列）

	rawYc, rawYx, rawYk, rawEn []Row
	rawPara                    []ParaRow

	// 全点表/转发表共享映射（与协议无关）
	allYx, allYc, allYk, allEn map[int]string
	paraMap                    map[int]string
	// 协议独立的“选定具体表”切换映射
	sw101, sw104 switchMaps
}

// Load 打开并加载点表数据库（缺表按空处理，容错与参考实现一致）。
func Load(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开点表失败: %v", err)
	}
	defer db.Close()
	d := &DB{FilePath: path, BaseYX: BaseYX, BaseYC: BaseYC, BaseYK: BaseYK, BaseEN: BaseEN,
		BaseYX101: BaseYX, BaseYC101: BaseYC, BaseYK101: BaseYK, BaseEN101: BaseEN,
		YCBase: BaseYC, ParamTable: ParamTableNone}
	d.rawYc = readRows(db, "SELECT Addr,BoardPoint,Name,SwitchPoint,SwitchName,TableName FROM ProjectYcPoint ORDER BY Addr")
	d.rawYx = readRows(db, "SELECT Addr,BoardPoint,Name,SwitchPoint,SwitchName,TableName FROM ProjectYxPoint ORDER BY Addr")
	d.rawYk = readRows(db, "SELECT Addr,BoardPoint,Name,SwitchPoint,SwitchName,TableName FROM ProjectYkPoint ORDER BY Addr")
	d.rawEn = readRows(db, "SELECT Addr, NULL AS BoardPoint, Name, SwitchPoint, SwitchName, TableName FROM ProjectEnergyPoint ORDER BY Addr")
	d.rawPara = readPara(db)
	// 定值表：国网 ProjectGWParamPoint / 南网 ProjectNWParamPoint（缺表容错）
	d.paramGW = readParaPoints(db, "SELECT Addr,Name,Rate,ParaType,StrEncodeName FROM ProjectGWParamPoint")
	d.paramNW = readParaPoints(db, "SELECT Addr,Name,Rate,ParaType,StrEncodeName FROM ProjectNWParamPoint")
	d.HasGWParam = len(d.paramGW) > 0
	d.HasNWParam = len(d.paramNW) > 0
	if len(d.rawYc)+len(d.rawYx)+len(d.rawYk)+len(d.rawEn)+len(d.rawPara)+len(d.paramGW)+len(d.paramNW) == 0 {
		return nil, fmt.Errorf("点表数据库为空（未找到 ProjectYcPoint/ProjectYxPoint/ProjectYkPoint/ProjectEnergyPoint/ProjectParaTransferPoint/ProjectGWParamPoint/ProjectNWParamPoint 表）")
	}
	names := map[string]bool{}
	for _, rows := range [][]Row{d.rawYc, d.rawYx, d.rawYk, d.rawEn} {
		for _, r := range rows {
			if r.TableName != "" {
				names[r.TableName] = true
			}
		}
	}
	d.TableNames = make([]string, 0, len(names))
	for n := range names {
		d.TableNames = append(d.TableNames, n)
	}
	sort.Slice(d.TableNames, func(i, j int) bool { return d.TableNames[i] < d.TableNames[j] })
	d.RebuildMaps("")
	d.Detail = fmt.Sprintf("%s  YC %d | YX %d | YK %d | EN %d | PARA %d",
		filepath.Base(path), len(d.rawYc), len(d.rawYx), len(d.rawYk), len(d.rawEn), len(d.rawPara))
	return d, nil
}

// readParaPoints 读取定值表（国网/南网）。
func readParaPoints(db *sql.DB, query string) []ParaPoint {
	rows, err := db.Query(query)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []ParaPoint
	for rows.Next() {
		var addr sql.NullInt64
		var name, rate, paraType, strEncode sql.NullString
		if err := rows.Scan(&addr, &name, &rate, &paraType, &strEncode); err != nil {
			continue
		}
		if !addr.Valid || addr.Int64 < 0 {
			continue
		}
		p := ParaPoint{Addr: int(addr.Int64), Name: normName(name.String),
			Rate: strings.TrimSpace(rate.String), ParaType: strings.TrimSpace(paraType.String),
			StrEncodeName: strings.TrimSpace(strEncode.String)}
		if p.Name == "" && p.StrEncodeName == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// SetBases 设置转发点表基值（0 表示使用默认）。同时设置 101/104，兼容旧接口。
func (d *DB) SetBases(yx, yc, yk, en int) {
	d.SetBasesFor("104", yx, yc, yk, en)
	d.setBases101(yx, yc, yk, en)
}

// SetBasesFor 设置指定协议（"101"/"104"）的转发基址；另一个协议保持不变。
func (d *DB) SetBasesFor(proto string, yx, yc, yk, en int) {
	if normProto(proto) == "101" {
		d.setBases101(yx, yc, yk, en)
		d.rebuildSwitches()
		return
	}
	if yx > 0 {
		d.BaseYX = yx
	}
	if yc > 0 {
		d.BaseYC = yc
	}
	if yk > 0 {
		d.BaseYK = yk
	}
	if en > 0 {
		d.BaseEN = en
	}
	d.YCBase = d.BaseYC
	d.rebuildSwitches()
}

func (d *DB) setBases101(yx, yc, yk, en int) {
	if yx > 0 {
		d.BaseYX101 = yx
	}
	if yc > 0 {
		d.BaseYC101 = yc
	}
	if yk > 0 {
		d.BaseYK101 = yk
	}
	if en > 0 {
		d.BaseEN101 = en
	}
}

// SetParamTable 选择定值表（"" 无 | "gw" 国网 | "nw" 南网）。
func (d *DB) SetParamTable(kind string) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case ParamTableGW:
		if !d.HasGWParam {
			kind = ParamTableNone
		}
	case ParamTableNW:
		if !d.HasNWParam {
			kind = ParamTableNone
		}
	default:
		kind = ParamTableNone
	}
	d.ParamTable = kind
	d.rebuildParamMap()
}

func (d *DB) rebuildParamMap() {
	var src []ParaPoint
	switch d.ParamTable {
	case ParamTableGW:
		src = d.paramGW
	case ParamTableNW:
		src = d.paramNW
	}
	m := map[int]ParaPoint{}
	for _, p := range src {
		m[p.Addr] = p
	}
	d.paramMap = m
	// 各表独立映射与转发表补充信息：用于"定值表=无"时兜底补齐 参数类型/比例
	d.mapGW = mapParaPoints(d.paramGW)
	d.mapNW = mapParaPoints(d.paramNW)
	d.paraExtra = map[int]ParaPoint{}
	for _, r := range d.rawPara {
		if r.UpAddr < 0 || (r.Rate == "" && r.ParaType == "") {
			continue
		}
		d.paraExtra[r.UpAddr] = ParaPoint{Addr: r.UpAddr, Name: r.Name, Rate: r.Rate, ParaType: r.ParaType}
	}
}

func mapParaPoints(src []ParaPoint) map[int]ParaPoint {
	m := map[int]ParaPoint{}
	for _, p := range src {
		m[p.Addr] = p
	}
	return m
}

// ParamPoint 按定值信息体地址（十进制）取定值点信息（仅当前选定定值表）。
func (d *DB) ParamPoint(addr int) (ParaPoint, bool) {
	if d == nil || d.paramMap == nil {
		return ParaPoint{}, false
	}
	p, ok := d.paramMap[addr]
	return p, ok
}

// ParamExtra 取定值**补充信息**（比例/参数类型/十六进制地址），用于填充"参数类型/比例"列：
//  1. 当前选定定值表（国网/南网）命中即用；
//  2. 未选定定值表（"无"）时依次兜底：参数转发表中若含 Rate/ParaType → 国网 → 南网；
//
// 未选定时的兜底只用于补齐展示信息，不改变点名来源（点名仍按 选定表 → 转发表）。
func (d *DB) ParamExtra(addr int) (ParaPoint, bool) {
	if d == nil || addr < 0 {
		return ParaPoint{}, false
	}
	if p, ok := d.paramMap[addr]; ok && paraHasExtra(p) {
		return p, true
	}
	if d.ParamTable != ParamTableNone {
		return ParaPoint{}, false // 用户明确选定定值表：不跨表取补充信息
	}
	if p, ok := d.paraExtra[addr]; ok && (p.Rate != "" || p.ParaType != "") {
		return p, true
	}
	for _, m := range []map[int]ParaPoint{d.mapGW, d.mapNW} {
		if p, ok := m[addr]; ok && paraHasExtra(p) {
			return p, true
		}
	}
	return ParaPoint{}, false
}

// paraHasExtra 该定值点是否带有可用于展示的补充信息。
func paraHasExtra(p ParaPoint) bool {
	return p.Rate != "" || p.ParaType != "" || p.StrEncodeName != ""
}

func readRows(db *sql.DB, query string) []Row {
	rows, err := db.Query(query)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		var addr, board, sw sql.NullInt64
		var name, swName, tableName sql.NullString
		if err := rows.Scan(&addr, &board, &name, &sw, &swName, &tableName); err != nil {
			continue
		}
		// NULL 值处理：Addr/SwitchPoint 为 NULL → 置 -1 哨兵（后续 <0 检查跳过），
		// 防止 NULL 落到 0 号地址；Name 空白或字面 "NULL" 视为无名称（绝不显示空点名）。
		r.Addr = nullInt(addr)
		r.BoardPoint = nullInt(board)
		r.Name = normName(name.String)
		r.SwitchPoint = nullInt(sw)
		r.SwitchName = normName(swName.String)
		r.TableName = strings.TrimSpace(tableName.String)
		out = append(out, r)
	}
	return out
}

// nullInt NULL → -1 哨兵（无效地址），否则数值。
func nullInt(v sql.NullInt64) int {
	if !v.Valid {
		return -1
	}
	return int(v.Int64)
}

// normName 名称规范化：NULL/空串/空白/字面 "null"/"NULL" → ""（无名称）。
func normName(s string) string {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "", "null":
		return ""
	}
	return s
}

// readPara 读取参数转发表 ProjectParaTransferPoint：
// 优先读取含 Rate/ParaType 的扩展列（部分工程库带比例/参数类型），列不存在时回退基础三列。
func readPara(db *sql.DB) []ParaRow {
	if rows := readParaQuery(db, "SELECT UpAddr,Name,Rate,ParaType,TableName FROM ProjectParaTransferPoint"); rows != nil {
		return rows
	}
	return readParaQuery(db, "SELECT UpAddr,Name,TableName FROM ProjectParaTransferPoint")
}

func readParaQuery(db *sql.DB, query string) []ParaRow {
	rows, err := db.Query(query)
	if err != nil {
		return nil
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil
	}
	hasExtra := len(cols) >= 5
	out := make([]ParaRow, 0, 16)
	for rows.Next() {
		var up sql.NullInt64
		var name, tableName sql.NullString
		var rate, paraType sql.NullString
		if hasExtra {
			if err := rows.Scan(&up, &name, &rate, &paraType, &tableName); err != nil {
				continue
			}
		} else {
			if err := rows.Scan(&up, &name, &tableName); err != nil {
				continue
			}
		}
		out = append(out, ParaRow{UpAddr: nullInt(up), Name: normName(name.String),
			Rate: strings.TrimSpace(rate.String), ParaType: strings.TrimSpace(paraType.String),
			TableName: strings.TrimSpace(tableName.String)})
	}
	return out
}
