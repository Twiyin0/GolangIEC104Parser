package pointdb

import "strings"

// switchMaps 某一协议、某一“选定具体表”下的转发映射。
type switchMaps struct {
	yx, yc, yk, en map[int]string
}

// SetYCBase 设置遥测转发点表基址并重建映射（默认 0x4000）。
func (d *DB) SetYCBase(base int) {
	if base <= 0 {
		base = TransferBaseYC
	}
	d.YCBase = base
	d.BaseYC = base
	d.rebuildSwitches()
}

// RebuildMaps 兼容旧接口：同时把 101/104 两个协议切换到同一表模式
// （tableName="" 或 "全点表" 为全部模式；具体表名为切换模式）。
func (d *DB) RebuildMaps(tableName string) {
	name := strings.TrimSpace(tableName)
	d.CurTable = name
	d.CurTable101 = name
	d.CurTable104 = name
	d.rebuildShared()
	d.rebuildSwitches()
}

// RebuildMapsFor 只切换指定协议（"101"/"104"）的点表模式，另一个协议保持不变。
func (d *DB) RebuildMapsFor(proto, tableName string) {
	name := strings.TrimSpace(tableName)
	if normProto(proto) == "101" {
		d.CurTable101 = name
	} else {
		d.CurTable104 = name
	}
	d.CurTable = name // 兼容旧字段：记录最近一次选择
	if d.allYc == nil {
		d.rebuildShared()
	}
	d.rebuildSwitches()
}

// SetTable 同 RebuildMapsFor，语义化别名。
func (d *DB) SetTable(proto, tableName string) { d.RebuildMapsFor(proto, tableName) }

// rebuildShared 重建与协议无关的全点表映射（普通表名 > 无表名 > 全点表）。
func (d *DB) rebuildShared() {
	if d.BaseYC == 0 {
		d.BaseYC = BaseYC
	}
	d.YCBase = d.BaseYC
	d.allYc = buildAllMap(d.rawYc)
	d.allYx = buildAllMap(d.rawYx)
	d.allYk = buildAllMap(d.rawYk)
	d.allEn = buildAllMap(d.rawEn)
	if d.paraMap == nil {
		m := map[int]string{}
		for _, p := range d.rawPara {
			if p.UpAddr >= 0 && p.Name != "" {
				m[p.UpAddr] = p.Name
			}
		}
		d.paraMap = m
	}
	if d.paramMap == nil {
		d.rebuildParamMap()
	}
}

// rebuildSwitches 按 101/104 各自当前选定表重建切换映射。
func (d *DB) rebuildSwitches() {
	d.sw101 = d.buildSwitchFor(d.CurTable101)
	d.sw104 = d.buildSwitchFor(d.CurTable104)
}

func (d *DB) buildSwitchFor(table string) switchMaps {
	return switchMaps{
		yx: buildSwitchMap(d.rawYx, table, d.BaseYX), // 遥信：0x0000 + SwitchPoint
		yc: buildSwitchMap(d.rawYc, table, d.BaseYC), // 遥测：0x4000 + SwitchPoint
		yk: buildSwitchMap(d.rawYk, table, d.BaseYK), // 遥控：0x6000 + SwitchPoint
		en: buildSwitchMap(d.rawEn, table, d.BaseEN), // 电能量：0x6400 + SwitchPoint
	}
}

// normProto 归一化协议标识：含 "101" 视为 101，其余（含空串）按 104 处理。
func normProto(proto string) string {
	if strings.Contains(proto, "101") {
		return "101"
	}
	return "104"
}

// buildAllMap 全点表映射：普通表名 > 无表名 > "全点表"（后者覆盖，与 db.ts 一致）。
func buildAllMap(rows []Row) map[int]string {
	fallback := map[int]string{}
	nullTable := map[int]string{}
	fullTable := map[int]string{}
	for _, r := range rows {
		if r.Addr < 0 || r.Name == "" {
			continue
		}
		switch {
		case r.TableName == FullTableName:
			fullTable[r.Addr] = r.Name
		case r.TableName == "":
			nullTable[r.Addr] = r.Name
		default:
			if _, ok := fallback[r.Addr]; !ok {
				fallback[r.Addr] = r.Name
			}
		}
	}
	m := map[int]string{}
	for k, v := range fallback {
		m[k] = v
	}
	for k, v := range nullTable {
		m[k] = v
	}
	for k, v := range fullTable {
		m[k] = v
	}
	return m
}

// buildSwitchMap 切换表映射：仅"用户选定具体表"（非空且非"全点表"）生效；
// 键 = base + SwitchPoint（遥测 base=0x4000，其余类型 base=0）。
func buildSwitchMap(rows []Row, active string, base int) map[int]string {
	m := map[int]string{}
	if active == "" || active == FullTableName {
		return m
	}
	for _, r := range rows {
		if r.TableName != active { // 仅选定表；NULL/全点表已由上一条件排除
			continue
		}
		if r.SwitchPoint < 0 || r.SwitchName == "" {
			continue
		}
		m[base+r.SwitchPoint] = r.SwitchName
	}
	return m
}

// PtNameFor 按协议查询点名（typ: yx/yc/yk/en/para；proto: "101"/"104"）。
// 101 与 104 各自使用独立选择的点表模式；查不到返回 ""。
func (d *DB) PtNameFor(proto, typ string, value int) string {
	if d == nil || value < 0 {
		return ""
	}
	sm, cur := d.sw104, d.CurTable104
	if normProto(proto) == "101" {
		sm, cur = d.sw101, d.CurTable101
	}
	useAll := cur == "" || cur == FullTableName
	switch typ {
	case "yx":
		if useAll {
			return d.allYx[value]
		}
		if n, ok := sm.yx[value]; ok && n != "" {
			return n
		}
		return d.allYx[value]
	case "yc":
		if useAll {
			return d.allYc[value]
		}
		if n, ok := sm.yc[value]; ok && n != "" {
			return n
		}
		return d.allYc[value]
	case "yk":
		if useAll {
			return d.allYk[value]
		}
		if n, ok := sm.yk[value]; ok && n != "" {
			return n
		}
		return d.allYk[value]
	case "en":
		if useAll {
			return d.allEn[value]
		}
		if n, ok := sm.en[value]; ok && n != "" {
			return n
		}
		return d.allEn[value]
	case "para":
		// 定值：优先按当前选定定值表（国网/南网）的 Addr（十进制）匹配
		if p, ok := d.ParamPoint(value); ok && p.Name != "" {
			return p.Name
		}
		return d.paraMap[value] // 兼容参数转发表（UpAddr）
	}
	return ""
}

// PtName 兼容旧接口：默认按 104 协议查询。
func (d *DB) PtName(typ string, value int) string { return d.PtNameFor("104", typ, value) }
