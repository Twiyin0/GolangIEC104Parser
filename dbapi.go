// Package main — 点表（dbSelect）绑定与点名注入（Edit → dbSelect，需求新增）。
package main

import (
	"fmt"
	"strconv"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"iec104rt/internal/config"
	"iec104rt/internal/pointdb"
	"iec104rt/internal/protocol"
	"iec104rt/internal/protocol/common"
)

// DbInfo 点表状态（前端 DbBar 展示）。
type DbInfo struct {
	Ok          bool     `json:"ok"`
	Detail      string   `json:"detail"`
	TableNames  []string `json:"tableNames"`
	CurTable    string   `json:"curTable"` // 兼容：最近一次选择
	CurTable101 string   `json:"curTable101"`
	CurTable104 string   `json:"curTable104"`
	ParamTable  string   `json:"paramTable"` // "" 无 | "gw" 国网 | "nw" 南网
	HasGWParam  bool     `json:"hasGwParam"`
	HasNWParam  bool     `json:"hasNwParam"`
}

func dbInfoOf(d *pointdb.DB) DbInfo {
	if d == nil {
		return DbInfo{TableNames: []string{}}
	}
	return DbInfo{Ok: true, Detail: d.Detail,
		TableNames: d.TableNames, CurTable: d.CurTable,
		CurTable101: d.CurTable101, CurTable104: d.CurTable104,
		ParamTable: d.ParamTable,
		HasGWParam: d.HasGWParam, HasNWParam: d.HasNWParam}
}

func (a *App) dbSnapshot() DbInfo {
	a.muPoint.Lock()
	defer a.muPoint.Unlock()
	return dbInfoOf(a.pointDB)
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
	// 应用转发点表基值（101/104 各自独立）与定值表选择
	a.mu.Lock()
	pc := a.cfg.Parse
	a.mu.Unlock()
	a.applyBases(d, pc)
	d.SetParamTable(pc.ParamTable)
	a.bumpDBRev() // 点表变更 → 文件解析缓存失效（点名随之刷新）
	a.emit("db", a.dbSnapshot())
	return a.dbSnapshot(), nil
}

// applyBases 把 101/104 各自的转发基址下发到点表库。
func (a *App) applyBases(d *pointdb.DB, pc config.ParseCfg) {
	b101 := pc.BasesFor("101")
	b104 := pc.BasesFor("104")
	d.SetBasesFor("101", b101.YX, b101.YC, b101.YK, b101.EN)
	d.SetBasesFor("104", b104.YX, b104.YC, b104.YK, b104.EN)
}

// DbSetTable 兼容旧接口：同时切换 101/104 点表模式（""=全部模式；具体表名=切换模式）。
func (a *App) DbSetTable(name string) (DbInfo, error) {
	a.muPoint.Lock()
	defer a.muPoint.Unlock()
	if a.pointDB == nil {
		return DbInfo{TableNames: []string{}}, fmt.Errorf("未加载点表")
	}
	a.applyBases(a.pointDB, a.cfg.Parse) // 应用最新基址配置（101/104 各自独立）
	a.pointDB.RebuildMaps(name)
	info := dbInfoOf(a.pointDB)
	a.bumpDBRev()
	a.emit("db", info)
	return info, nil
}

// DbSetTableFor 按协议（"101"/"104"）切换点表模式：两个协议互不影响。
func (a *App) DbSetTableFor(proto, name string) (DbInfo, error) {
	a.muPoint.Lock()
	defer a.muPoint.Unlock()
	if a.pointDB == nil {
		return DbInfo{TableNames: []string{}}, fmt.Errorf("未加载点表")
	}
	a.applyBases(a.pointDB, a.cfg.Parse)
	a.pointDB.RebuildMapsFor(proto, name)
	info := dbInfoOf(a.pointDB)
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
	info := dbInfoOf(d)
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

// ptNameFor 查询点名：101/104 使用各自选定的点表模式；
// 101 未命中时再按 ProjectParaTransferPoint 转发近似处理。
func (a *App) ptNameFor(typ string, value int, proto string) string {
	a.muPoint.Lock()
	d := a.pointDB
	a.muPoint.Unlock()
	if d == nil {
		return ""
	}
	if n := d.PtNameFor(proto, typ, value); n != "" {
		return n
	}
	if strings.Contains(proto, "101") {
		return d.PtNameFor(proto, "para", value) // 101：经参数转发表映射（近似 follow101Transfer）
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
		} else {
			delete(item, "name") // 点表切换/移除后清除旧点名，避免残留
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
			delete(item, "rate")
			delete(item, "paraType")
			delete(item, "addrHex")
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

// EnrichStaticFrames 点表变更后重新为静态/log 文本解析结果注入点名。
func (a *App) EnrichStaticFrames() []protocol.Frame {
	a.muStatic.Lock()
	fs := append([]protocol.Frame(nil), a.staticFrames...)
	a.muStatic.Unlock()
	a.enrichFrames(fs)
	a.muStatic.Lock()
	a.staticFrames = append([]protocol.Frame(nil), fs...)
	a.muStatic.Unlock()
	return fs
}

// EnrichImportedFrames 点表变更后重新为导入回放报文注入点名。
func (a *App) EnrichImportedFrames() []protocol.Frame {
	a.muImported.Lock()
	fs := append([]protocol.Frame(nil), a.imported...)
	a.muImported.Unlock()
	a.enrichFrames(fs)
	a.muImported.Lock()
	a.imported = append([]protocol.Frame(nil), fs...)
	a.muImported.Unlock()
	return fs
}

// EnrichSessionFrames 点表变更后重新为实时/日志会话缓冲注入点名，并返回全量快照。
func (a *App) EnrichSessionFrames() []protocol.Frame {
	a.sess.EnrichAll(func(frames []protocol.Frame) {
		for i := range frames {
			a.enrichFrame(&frames[i])
		}
	})
	return a.sess.All()
}
