package pointdb

import "strings"

// SetYCBase 设置遥测转发点表基址并重建映射（默认 0x4000）。
func (d *DB) SetYCBase(base int) {
	if base <= 0 {
		base = TransferBaseYC
	}
	d.YCBase = base
	d.RebuildMaps(d.CurTable)
}

// RebuildMaps 切换点表模式（tableName="" 为全部模式；具体表名为切换模式）。
// 转发点表规则：选定具体表（TableName 非 NULL 且非"全点表"）时，
// 信息体地址 = 基值 + SwitchPoint（遥信 0x0000 / 遥测 0x4000 / 遥控 0x6000 / 电能量 0x6400），
// 点名为 SwitchName。
func (d *DB) RebuildMaps(tableName string) {
	d.CurTable = strings.TrimSpace(tableName)
	if d.BaseYC == 0 {
		d.BaseYC = BaseYC
	}
	d.YCBase = d.BaseYC
	d.allYc = buildAllMap(d.rawYc)
	d.allYx = buildAllMap(d.rawYx)
	d.allYk = buildAllMap(d.rawYk)
	d.allEn = buildAllMap(d.rawEn)
	d.switchYx = buildSwitchMap(d.rawYx, d.CurTable, d.BaseYX) // 遥信：0x0000 + SwitchPoint
	d.switchYc = buildSwitchMap(d.rawYc, d.CurTable, d.BaseYC) // 遥测：0x4000 + SwitchPoint
	d.switchYk = buildSwitchMap(d.rawYk, d.CurTable, d.BaseYK) // 遥控：0x6000 + SwitchPoint
	d.switchEn = buildSwitchMap(d.rawEn, d.CurTable, d.BaseEN) // 电能量：0x6400 + SwitchPoint
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

// PtName 查询点名（typ: yx/yc/yk/en/para）。查不到返回 ""。
func (d *DB) PtName(typ string, value int) string {
	if d == nil || value < 0 {
		return ""
	}
	useAll := d.CurTable == "" || d.CurTable == FullTableName
	switch typ {
	case "yx":
		if useAll {
			return d.allYx[value]
		}
		if n, ok := d.switchYx[value]; ok && n != "" {
			return n
		}
		return d.allYx[value]
	case "yc":
		if useAll {
			return d.allYc[value]
		}
		if n, ok := d.switchYc[value]; ok && n != "" {
			return n
		}
		return d.allYc[value]
	case "yk":
		if useAll {
			return d.allYk[value]
		}
		if n, ok := d.switchYk[value]; ok && n != "" {
			return n
		}
		return d.allYk[value]
	case "en":
		if useAll {
			return d.allEn[value]
		}
		if n, ok := d.switchEn[value]; ok && n != "" {
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
