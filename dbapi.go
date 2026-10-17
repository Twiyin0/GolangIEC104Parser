// Package main — 点表（dbSelect）绑定与点名注入（Edit → dbSelect，需求新增）。
package main

import (
	"fmt"
	"strconv"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"iec104rt/internal/pointdb"
	"iec104rt/internal/protocol"
	"iec104rt/internal/protocol/common"
)

// DbInfo 点表状态（前端 DbBar 展示）。
type DbInfo struct {
	Ok         bool     `json:"ok"`
	Detail     string   `json:"detail"`
	TableNames []string `json:"tableNames"`
	CurTable   string   `json:"curTable"`
	ParamTable string   `json:"paramTable"` // "" 无 | "gw" 国网 | "nw" 南网
	HasGWParam bool     `json:"hasGwParam"`
	HasNWParam bool     `json:"hasNwParam"`
}

func (a *App) dbSnapshot() DbInfo {
	a.muPoint.Lock()
	defer a.muPoint.Unlock()
	if a.pointDB == nil {
		return DbInfo{TableNames: []string{}}
	}
	return DbInfo{Ok: true, Detail: a.pointDB.Detail,
		TableNames: a.pointDB.TableNames, CurTable: a.pointDB.CurTable,
		ParamTable: a.pointDB.ParamTable,
		HasGWParam: a.pointDB.HasGWParam, HasNWParam: a.pointDB.HasNWParam}
}

// DbSelectDialog Edit→dbSelect：选择工程点表数据库（.db/.sqlite/.sqlite3）。
func (a *App) DbSelectDialog() (DbInfo, error) {
	if a.ctx == nil {
		return DbInfo{}, fmt.Errorf("应用未就绪")
	}
	path, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "选择工程项目点表数据库（SQLite .db）",
		Filters: []wruntime.FileFilter{
			{DisplayName: "点表数据库 (*.db;*.sqlite;*.sqlite3)", Pattern: "*.db;*.sqlite;*.sqlite3"},
		},
	})
	if err != nil || path == "" {
		return a.dbSnapshot(), err
	}
	return a.LoadPointDb(path)
}

// LoadPointDb 加载点表数据库。
func (a *App) LoadPointDb(path string) (DbInfo, error) {
	d, err := pointdb.Load(path)
	if err != nil {
		a.muPoint.Lock()
		a.pointDB = nil
		a.muPoint.Unlock()
		return a.dbSnapshot(), err
	}
	a.muPoint.Lock()
	a.pointDB = d
	a.muPoint.Unlock()
	// 应用转发点表基值（遥信 0x0000/遥测 0x4000/遥控 0x6000/电能量 0x6400）与定值表选择
	a.mu.Lock()
	pc := a.cfg.Parse
	a.mu.Unlock()
	d.SetBases(pc.YXTransferBase, pc.YCTransferBase, pc.YKTransferBase, pc.ENTransferBase)
	d.SetParamTable(pc.ParamTable)
	a.bumpDBRev() // 点表变更 → 文件解析缓存失效（点名随之刷新）
	a.emit("db", a.dbSnapshot())
	return a.dbSnapshot(), nil
}

// DbSetTable 切换点表模式（""=全部模式；具体表名=切换模式）。
func (a *App) DbSetTable(name string) (DbInfo, error) {
	a.muPoint.Lock()
	defer a.muPoint.Unlock()
	if a.pointDB == nil {
		return DbInfo{TableNames: []string{}}, fmt.Errorf("未加载点表")
	}
	a.pointDB.SetBases(a.cfg.Parse.YXTransferBase, a.cfg.Parse.YCTransferBase,
		a.cfg.Parse.YKTransferBase, a.cfg.Parse.ENTransferBase) // 应用最新基址配置
	a.pointDB.RebuildMaps(name)
	info := DbInfo{Ok: true, Detail: a.pointDB.Detail,
		TableNames: a.pointDB.TableNames, CurTable: a.pointDB.CurTable,
		ParamTable: a.pointDB.ParamTable,
		HasGWParam: a.pointDB.HasGWParam, HasNWParam: a.pointDB.HasNWParam}
	a.bumpDBRev()
	a.emit("db", info)
	return info, nil
}

// DbSetParamTable 选择定值表（"" 无 | "gw" 国网 ProjectGWParamPoint | "nw" 南网 ProjectNWParamPoint）。
func (a *App) DbSetParamTable(kind string) (DbInfo, error) {
	a.muPoint.Lock()
	d := a.pointDB
	if d == nil {
		a.muPoint.Unlock()
		return DbInfo{TableNames: []string{}}, fmt.Errorf("未加载点表")
	}
	d.SetParamTable(kind)
	a.muPoint.Unlock()
	a.mu.Lock()
	a.cfg.Parse.ParamTable = d.ParamTable
	cfg := a.cfg
	a.mu.Unlock()
	a.saveConfigQuiet(cfg) // 选择持久化：避免重启后定值表回到"无"（参数类型/比例整列为空）
	info := DbInfo{Ok: true, Detail: d.Detail,
		TableNames: d.TableNames, CurTable: d.CurTable,
		ParamTable: d.ParamTable,
		HasGWParam: d.HasGWParam, HasNWParam: d.HasNWParam}
	a.bumpDBRev()
	a.emit("db", info)
	return info, nil
}

// DbClear 移除点表。
func (a *App) DbClear() DbInfo {
	a.muPoint.Lock()
	a.pointDB = nil
	a.muPoint.Unlock()
	a.bumpDBRev()
	a.emit("db", a.dbSnapshot())
	return a.dbSnapshot()
}

// GetDbInfo 当前点表状态。
func (a *App) GetDbInfo() DbInfo { return a.dbSnapshot() }

// ── 点名注入 ──────────────────────────────────────────────────────────

// ptNameFor 查询点名（101 协议未命中时按 ProjectParaTransferPoint 转发近似处理）。
func (a *App) ptNameFor(typ string, value int, proto string) string {
	a.muPoint.Lock()
	d := a.pointDB
	a.muPoint.Unlock()
	if d == nil {
		return ""
	}
	if n := d.PtName(typ, value); n != "" {
		return n
	}
	if proto == "101" {
		return d.PtName("para", value) // 101：经参数转发表映射（近似 follow101Transfer）
	}
	return ""
}

// ptParamInfo 取定值表（国网/南网）中该地址的定值点信息（仅当前选定表，用于点名）。
func (a *App) ptParamInfo(addr int) (pointdb.ParaPoint, bool) {
	a.muPoint.Lock()
	d := a.pointDB
	a.muPoint.Unlock()
	if d == nil {
		return pointdb.ParaPoint{}, false
	}
	return d.ParamPoint(addr)
}

// ptParamExtra 取定值补充信息（比例/参数类型/十六进制地址）：
// 选定定值表优先；"定值表=无"时按 参数转发表(含 Rate/ParaType) → 国网 → 南网 兜底补齐，
// 避免未选择定值表时"参数类型/比例"整列为空。
func (a *App) ptParamExtra(addr int) (pointdb.ParaPoint, bool) {
	a.muPoint.Lock()
	d := a.pointDB
	a.muPoint.Unlock()
	if d == nil {
		return pointdb.ParaPoint{}, false
	}
	return d.ParamExtra(addr)
}

func addrHexToInt(s string) int {
	s = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "0x")
	v, err := strconv.ParseInt(s, 16, 64)
	if err != nil || v < 0 {
		return -1
	}
	return int(v)
}

// enrichFrame 把点名/参数名注入解析结果（点击渲染/导出/静态解析共用）。
// 同时按配置写入地址显示形式 addrShow（十六进制/十进制，可在 Connection 配置）。
func (a *App) enrichFrame(f *protocol.Frame) {
	if f == nil || f.Parsed == nil {
		return
	}
	a.mu.Lock()
	af := a.cfg.Parse.AddrFormat
	a.mu.Unlock()
	putName := func(item map[string]any, name string) {
		if name != "" {
			item["name"] = name
		}
	}
	switch f.Type {
	case "yx", "soe":
		for _, item := range itemsOf(f.Parsed) {
			if addr, ok := item["addr"].(int); ok {
				putName(item, a.ptNameFor("yx", addr, f.Proto))
				setAddrShow(item, addr, af.YX, f.Proto)
			}
		}
	case "yc":
		for _, item := range itemsOf(f.Parsed) {
			if addr, ok := item["addr"].(int); ok {
				putName(item, a.ptNameFor("yc", addr, f.Proto))
				setAddrShow(item, addr, af.YC, f.Proto)
			}
		}
	case "control":
		for _, item := range itemsOf(f.Parsed) {
			if addrStr, ok := item["addr"].(string); ok {
				n := addrHexToInt(addrStr)
				putName(item, a.ptNameFor("yk", n, f.Proto))
				setAddrShow(item, n, af.YK, f.Proto)
			}
		}
	case "energy":
		for _, item := range itemsOf(f.Parsed) {
			if addr, ok := item["addr"].(int); ok {
				putName(item, a.ptNameFor("en", addr, f.Proto))
				setAddrShow(item, addr, af.EN, f.Proto)
			}
		}
	case "param":
		for _, item := range itemsOf(f.Parsed) {
			addr, ok := item["addr"].(string)
			if !ok {
				continue
			}
			n := addrHexToInt(addr)
			putName(item, a.ptNameFor("para", n, f.Proto))
			// 定值补充信息：比例 Rate、参数类型 ParaType、十六进制地址 StrEncodeName
			// （选定定值表优先；未选定时按转发表/国网/南网兜底补齐，避免整列为"—"）
			if p, ok := a.ptParamExtra(n); ok {
				if p.Rate != "" {
					item["rate"] = p.Rate
				}
				if p.ParaType != "" {
					item["paraType"] = p.ParaType
				}
				if p.StrEncodeName != "" {
					item["addrHex"] = p.StrEncodeName
				}
			}
			if af.Para == "dec" {
				item["addrShow"] = n
			} else if v, ok := item["addrHex"].(string); ok && v != "" {
				item["addrShow"] = common.NormHexShow(v) // 定值表 StrEncodeName（高位 0 不补位）
			} else {
				item["addrShow"] = fmt.Sprintf("0x%X", n) // 高位为 0 不补位
			}
		}
	case "fault":
		if yx, ok := f.Parsed["yx"].([]common.M); ok {
			for _, item := range yx {
				if pt, ok := item["point"].(int); ok {
					putName(item, a.ptNameFor("yx", pt, f.Proto))
					setAddrShow(item, pt, af.YX, f.Proto)
				}
			}
		}
		if yc, ok := f.Parsed["yc"].([]common.M); ok {
			for _, item := range yc {
				if addr, ok := item["addr"].(int); ok {
					putName(item, a.ptNameFor("yc", addr, f.Proto))
					setAddrShow(item, addr, af.YC, f.Proto)
				}
			}
		}
	}
}

// setAddrShow 按配置写入地址显示形式：hex → 0x 十六进制（高位为 0 不补位，如 0x006401 → 0x6401）；dec → 十进制。
func setAddrShow(item map[string]any, addr int, format string, proto string) {
	if format == "dec" {
		item["addrShow"] = addr
		return
	}
	item["addrShow"] = fmt.Sprintf("0x%X", addr)
}

// itemsOf 取解析结果中的信息对象列表（注意：解析器产出类型为 []common.M）。
func itemsOf(p map[string]any) []common.M {
	if d, ok := p["data"].([]common.M); ok {
		return d
	}
	return nil
}

func (a *App) enrichFrames(fs []protocol.Frame) {
	for i := range fs {
		a.enrichFrame(&fs[i])
	}
}
