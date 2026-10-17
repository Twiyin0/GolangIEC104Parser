package exporter

import (
	"fmt"
	"strings"

	"iec104rt/internal/protocol"
	"iec104rt/internal/protocol/common"
)

// RenderFrameMD 将一帧解析结果渲染为 markdown 正文（文本摘要 + 表格 + 符号化呈现）。
// 点表加载后（enrich 注入 name 字段），遥信/遥测/遥控/定值/电能量表格带"点名"列。
func RenderFrameMD(f protocol.Frame) string {
	p := f.Parsed
	if p == nil {
		return "（无解析结果）\n"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("**分类：%s** ｜ **%s** ｜ TI=%s ｜ %s\n\n", f.Category, f.Proto, f.TiName, f.Summary))
	for _, k := range []string{"desc", "cotDesc", "coiDesc", "qccDesc", "fcName", "uName", "size",
		"lsqDesc", "ackType", "reason", "operation", "scqDesc", "fbpHex", "subTypeName",
		"opName", "packetTypeName"} {
		if v, ok := p[k].(string); ok && v != "" {
			b.WriteString(fmt.Sprintf("- **%s**: %s\n", k, v))
		}
	}
	if v, ok := p["cotDesc"].(string); ok {
		b.WriteString(fmt.Sprintf("- **传送原因**: %s\n", v))
	}
	if v, ok := p["addr"].(int); ok {
		b.WriteString(fmt.Sprintf("- **公共地址**: %d\n", v))
	}

	// 在 at 位置插入"点名"列（紧随信息体地址/地址列之后）；
	// 任一项存在 name 字段即显示，未命中项显示 "—"（绝不显示空名）。
	dec := func(headers []string, rows [][]string, data []common.M, at int) ([]string, [][]string) {
		if len(data) == 0 {
			return headers, rows
		}
		hasAny := false
		for _, d := range data {
			if _, ok := d["name"]; ok {
				hasAny = true
				break
			}
		}
		if !hasAny {
			return headers, rows
		}
		headers = insertStr(headers, at, "点名")
		for i := range rows {
			rows[i] = insertStr(rows[i], at, strOf(data[i], "name"))
		}
		return headers, rows
	}

	switch f.Type {
	case "yx":
		data := dataSlice(p)
		hh, rr := dec([]string{"#", "信息对象地址", "状态"},
			rows(p, func(d common.M) []string { return []string{slot(d), addrStr(d), strOf(d, "state")} }), data, 2)
		renderTable(&b, "遥信", hh, rr)
	case "soe":
		data := dataSlice(p)
		hh, rr := dec([]string{"#", "信息对象地址", "状态", "时标"},
			rows(p, func(d common.M) []string { return []string{slot(d), addrStr(d), strOf(d, "state"), strOf(d, "time")} }), data, 2)
		renderTable(&b, "SOE 带时标遥信", hh, rr)
	case "yc":
		data := dataSlice(p)
		hh, rr := dec([]string{"#", "信息对象地址", "值", "类型", "品质(QDS)"},
			rows(p, func(d common.M) []string {
				return []string{slot(d), addrStr(d), valueStr(d), strOf(d, "valueType"), strOf(d, "qds")}
			}), data, 2)
		renderTable(&b, "遥测", hh, rr)
	case "control":
		data := dataSlice(p)
		hh, rr := dec([]string{"#", "地址", "状态", "操作", "QOC", "结果"},
			rows(p, func(d common.M) []string {
				return []string{slot(d), addrStr(d), strOf(d, "state"), strOf(d, "operation"),
					strOf(d, "qoc"), strOf(d, "return")}
			}), data, 2)
		renderTable(&b, "遥控", hh, rr)
	case "param":
		// 定值参数表（列序：#,地址,点名,值,参数类型,比例,操作,SN）；地址默认显示十六进制
		data := dataSlice(p)
		headers := []string{"#", "地址", "点名", "值", "参数类型", "比例", "操作", "SN"}
		paramRows := make([][]string, 0, len(data))
		for i, d := range data {
			addr := addrStr(d)
			slotStr := numStr(i + 1)
			if v, ok := d["slot"]; ok {
				slotStr = numStr(v)
			}
			paramRows = append(paramRows, []string{
				slotStr, addr, strOf(d, "name"), strOf(d, "value"),
				strOf(d, "paraType"), strOf(d, "rate"), strOf(d, "operation"), strOf(d, "sn"),
			})
		}
		renderTable(&b, "定值参数", headers, paramRows)
	case "energy":
		data := dataSlice(p)
		hh, rr := dec([]string{"#", "信息对象地址", "值", "品质", "时标"},
			rows(p, func(d common.M) []string {
				return []string{slot(d), addrStr(d), valueStr(d), strOf(d, "qds"), strOf(d, "time")}
			}), data, 2)
		renderTable(&b, "电能量(累计量)", hh, rr)
	case "bitstring":
		renderTable(&b, "位串", []string{"#", "信息对象地址", "值", "位"},
			rows(p, func(d common.M) []string {
				return []string{slot(d), addrStr(d), strOf(d, "valueHex"), strOf(d, "bits")}
			}))
	case "fault":
		if yx, ok := p["yx"].([]common.M); ok && len(yx) > 0 {
			hh, rr := dec([]string{"类型", "点号", "状态", "时标"}, mapRows(yx, func(d common.M) []string {
				return []string{strOf(d, "typeHex"), addrStr(d), strOf(d, "state"), strOf(d, "time")}
			}), yx, 2)
			renderTable(&b, "故障遥信", hh, rr)
		}
		if yc, ok := p["yc"].([]common.M); ok && len(yc) > 0 {
			hh, rr := dec([]string{"信息对象地址", "值", "类型"}, mapRows(yc, func(d common.M) []string {
				return []string{addrStr(d), numStr(d["value"]), strOf(d, "valueType")}
			}), yc, 1)
			renderTable(&b, "故障遥测", hh, rr)
		}
	case "file_service":
		if entries, ok := p["entries"].([]common.M); ok && len(entries) > 0 {
			renderTable(&b, "文件目录", []string{"文件名", "属性", "大小", "时间"},
				mapRows(entries, func(d common.M) []string {
					return []string{strOf(d, "fileName"), numStr(d["fileAttr"]), numStr(d["fileSize"]), strOf(d, "fileTime")}
				}))
		}
		if files, ok := p["files"].([]common.M); ok && len(files) > 0 {
			renderTable(&b, "文件列表", []string{"文件名", "类型", "开始时间", "结束时间"},
				mapRows(files, func(d common.M) []string {
					return []string{strOf(d, "name"), strOf(d, "fileCategory"), strOf(d, "startTime"), strOf(d, "endTime")}
				}))
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func dataSlice(p common.M) []common.M {
	if d, ok := p["data"].([]common.M); ok {
		return d
	}
	return nil
}

// insertStr 在 at 位置插入元素（越界时追加到末尾）。
func insertStr(a []string, at int, v string) []string {
	if at < 0 || at > len(a) {
		at = len(a)
	}
	out := make([]string, 0, len(a)+1)
	out = append(out, a[:at]...)
	out = append(out, v)
	out = append(out, a[at:]...)
	return out
}


func renderTable(b *strings.Builder, title string, headers []string, data [][]string) {
	if len(data) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("**%s**\n\n", title))
	b.WriteString("| " + strings.Join(headers, " | ") + " |\n")
	b.WriteString("|" + strings.Repeat(" --- |", len(headers)) + "\n")
	for _, row := range data {
		for i := range row {
			row[i] = strings.ReplaceAll(row[i], "|", "\\|")
		}
		b.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}
	b.WriteString("\n")
}

func rows(p common.M, fn func(common.M) []string) [][]string {
	data, ok := p["data"].([]common.M)
	if !ok {
		return nil
	}
	return mapRows(data, fn)
}

func mapRows(data []common.M, fn func(common.M) []string) [][]string {
	out := make([][]string, 0, len(data))
	for _, d := range data {
		out = append(out, fn(d))
	}
	return out
}

func slot(d common.M) string  { return numStr(d["slot"]) }
// AddrStrForTest 供测试与外部调用核对地址显示规则（等同 addrStr）。
func AddrStrForTest(d common.M) string { return addrStr(d) }

// addrStr 地址显示：优先使用按配置写入的 addrShow（十六进制/十进制），
// 其次 addrHex/hexAddr/pointHex，最后退回 addr 原值；十六进制统一按"高位为 0 不补位"显示。
func addrStr(d common.M) string {
	if v, ok := d["addrShow"]; ok && v != nil {
		return numStr(v)
	}
	for _, k := range []string{"addrHex", "hexAddr", "pointHex"} {
		if s, ok := d[k].(string); ok && s != "" {
			return common.NormHexShow(s)
		}
	}
	if s, ok := d["addr"].(string); ok {
		return common.NormHexShow(s)
	}
	return numStr(d["addr"])
}

func strOf(d common.M, k string) string {
	v, ok := d[k]
	if !ok || v == nil {
		return "—"
	}
	if s, ok := v.(string); ok {
		if s == "" {
			return "—"
		}
		return s
	}
	return numStr(v)
}

func valueStr(d common.M) string {
	if m, ok := d["value"].(common.M); ok { // NVA {raw, eng}
		return fmt.Sprintf("%v / 工程值=%v", m["raw"], m["eng"])
	}
	if v, ok := d["value"]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
		return numStr(v)
	}
	return "—"
}

func numStr(v any) string {
	switch n := v.(type) {
	case nil:
		return "—"
	case int:
		return fmt.Sprintf("%d", n)
	case int64:
		return fmt.Sprintf("%d", n)
	case uint32:
		return fmt.Sprintf("%d", n)
	case uint64:
		return fmt.Sprintf("%d", n)
	case float64:
		return fmt.Sprintf("%g", n)
	case bool:
		return fmt.Sprintf("%v", n)
	case string:
		return n
	default:
		return fmt.Sprintf("%v", n)
	}
}
