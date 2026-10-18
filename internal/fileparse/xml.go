package fileparse

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// xmlNode 通用 XML 节点（容忍未知结构，按"重复出现的元素=记录"提取）。
type xmlNode struct {
	Name     string
	Attrs    []kvPair
	Text     string
	Children []*xmlNode
	Depth    int
}

type kvPair struct{ K, V string }

// parseXML 通用 XML 解析：优先按接口规范识别 SOE/CO/EXV/FIXPT 等历史文件，
// 其余结构再走"重复元素=记录"的通用提取。
func parseXML(text string, resolve NameResolver) Result {
	dec := xml.NewDecoder(strings.NewReader(text))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity
	// 内容已由 decodeText 统一转为 UTF-8；忽略声明中的 GBK/GB2312 等编码名
	dec.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) { return input, nil }

	var root *xmlNode
	var stack []*xmlNode
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xmlNode{Name: t.Name.Local, Depth: len(stack)}
			for _, a := range t.Attr {
				n.Attrs = append(n.Attrs, kvPair{K: a.Name.Local, V: strings.TrimSpace(a.Value)})
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, n)
			} else if root == nil {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(t)
			}
		}
	}
	if root == nil {
		return Result{Error: "XML 解析失败：未找到根元素"}
	}

	// 先按规范识别历史数据文件（Header fileType=SOE/CO/EXV/FIXPT/...），
	// 避免通用"重复元素"算法把 DataAttr 与 DataRec 混成错位的表。
	if ft := xmlFileType(root); ft != "" {
		if res, ok := parseKnownXML(root, ft, resolve); ok {
			return res
		}
	}

	recs := pickRecordNodes(root)
	if len(recs) == 0 {
		return Result{Error: "XML 解析失败：未找到可识别的记录元素"}
	}
	res := Result{Note: "记录元素 <" + recs[0].Name + ">"}
	for _, n := range recs {
		row := Record{}
		for _, a := range n.Attrs {
			row[cnKey(a.K)] = a.V
		}
		for _, c := range n.Children {
			v := strings.TrimSpace(c.Text)
			if v == "" && len(c.Attrs) > 0 {
				parts := make([]string, 0, len(c.Attrs))
				for _, a := range c.Attrs {
					parts = append(parts, cnKey(a.K)+"="+a.V)
				}
				v = strings.Join(parts, " ")
			}
			row[cnKey(c.Name)] = v
		}
		if len(row) == 0 {
			if v := strings.TrimSpace(n.Text); v != "" {
				row["值"] = v
			}
		}
		if len(row) > 0 {
			res.Rows = append(res.Rows, row)
			for k := range row {
				if !containsStr(res.Columns, k) {
					res.Columns = append(res.Columns, k)
				}
			}
		}
	}
	return res
}

// pickRecordNodes 选取记录元素：出现次数最多的（同数量时取层级更深者）。
func pickRecordNodes(root *xmlNode) []*xmlNode {
	counts := map[string][]*xmlNode{}
	var walk func(n *xmlNode)
	walk = func(n *xmlNode) {
		for _, c := range n.Children {
			if len(c.Children) > 0 || len(c.Attrs) > 0 {
				counts[c.Name] = append(counts[c.Name], c)
			}
			walk(c)
		}
	}
	walk(root)
	best := ""
	bestLen := 0
	bestDepth := -1
	for name, nodes := range counts {
		if len(nodes) > bestLen || (len(nodes) == bestLen && nodes[0].Depth > bestDepth) {
			best, bestLen, bestDepth = name, len(nodes), nodes[0].Depth
		}
	}
	if best == "" {
		return nil
	}
	return counts[best]
}

// ── 规范历史数据文件（B.3.2）专用解析 ─────────────────────────────

func xmlAttr(n *xmlNode, key string) string {
	for _, a := range n.Attrs {
		if strings.EqualFold(a.K, key) {
			return strings.TrimSpace(a.V)
		}
	}
	return ""
}

func xmlChildren(n *xmlNode, name string) []*xmlNode {
	out := make([]*xmlNode, 0, len(n.Children))
	for _, c := range n.Children {
		if strings.EqualFold(c.Name, name) {
			out = append(out, c)
		}
	}
	return out
}

func xmlFileType(root *xmlNode) string {
	for _, h := range xmlChildren(root, "Header") {
		if ft := xmlAttr(h, "fileType"); ft != "" {
			return strings.ToUpper(ft)
		}
	}
	return ""
}

func parseKnownXML(root *xmlNode, fileType string, resolve NameResolver) (Result, bool) {
	switch fileType {
	case "SOE":
		return parseSOEXML(root, resolve)
	case "CO":
		return parseCOXML(root, resolve)
	case "EXV":
		return parseEXVXML(root, resolve)
	case "FIXPT":
		return parseFIXPTXML(root, resolve)
	case "FLOWREV":
		return parseFlowRevXML(root, resolve)
	}
	return Result{}, false
}

func pointName(resolve NameResolver, kind, ioa string) (string, bool) {
	if resolve == nil || ioa == "" {
		return "", false
	}
	n, ok := toInt(ioa)
	if !ok {
		return "", false
	}
	if s := resolve(kind, n); s != "" {
		return s, true
	}
	return "", false
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func parseSOEXML(root *xmlNode, resolve NameResolver) (Result, bool) {
	var rows []Record
	hasNames := false
	for _, dr := range xmlChildren(root, "DataRec") {
		for _, d := range xmlChildren(dr, "DI") {
			ioa := xmlAttr(d, "ioa")
			name, ok := pointName(resolve, "soe", ioa)
			if ok {
				hasNames = true
			}
			rows = append(rows, Record{
				"信息体地址": ioa, "点名": orDash(name), "值": xmlAttr(d, "val"), "时间": formatFileTime(xmlAttr(d, "tm")),
			})
		}
	}
	if len(rows) == 0 {
		return Result{}, false
	}
	return Result{Kind: "soe", KindName: "SOE记录", HasNames: hasNames,
		Columns: []string{"信息体地址", "点名", "值", "时间"}, Rows: rows,
		Note: "SOE 记录（XML）"}, true
}

func parseCOXML(root *xmlNode, resolve NameResolver) (Result, bool) {
	var rows []Record
	hasNames := false
	for _, dr := range xmlChildren(root, "DataRec") {
		for _, d := range xmlChildren(dr, "DI") {
			ioa := xmlAttr(d, "ioa")
			name, ok := pointName(resolve, "control", ioa)
			if ok {
				hasNames = true
			}
			rows = append(rows, Record{
				"信息体地址": ioa, "点名": orDash(name),
				"操作": operationCN(xmlAttr(d, "cmd")), "命令状态": xmlAttr(d, "val"),
				"时间": formatFileTime(xmlAttr(d, "tm")),
			})
		}
	}
	if len(rows) == 0 {
		return Result{}, false
	}
	return Result{Kind: "control", KindName: "遥控记录", HasNames: hasNames,
		Columns: []string{"信息体地址", "点名", "操作", "命令状态", "时间"}, Rows: rows,
		Note: "遥控操作记录（XML）"}, true
}

func parseEXVXML(root *xmlNode, resolve NameResolver) (Result, bool) {
	attrs := []*xmlNode{}
	for _, da := range xmlChildren(root, "DataAttr") {
		attrs = append(attrs, xmlChildren(da, "DI")...)
	}
	var recDIs []*xmlNode
	if recs := xmlChildren(root, "DataRec"); len(recs) > 0 {
		recDIs = xmlChildren(recs[0], "DI")
	}
	n := len(attrs)
	if len(recDIs) > n {
		n = len(recDIs)
	}
	if n == 0 {
		return Result{}, false
	}
	rows := make([]Record, 0, n)
	hasNames := false
	for i := 0; i < n; i++ {
		ioa, unit := "", ""
		if i < len(attrs) {
			ioa = xmlAttr(attrs[i], "ioa")
			unit = xmlAttr(attrs[i], "unit")
		}
		name, ok := pointName(resolve, "yc", ioa)
		if ok {
			hasNames = true
		}
		row := Record{"信息体地址": ioa, "点名": orDash(name), "单位": unit,
			"最大值": "", "最大值时间": "", "最小值": "", "最小值时间": ""}
		if i < len(recDIs) {
			d := recDIs[i]
			row["最大值"] = xmlAttr(d, "max")
			row["最大值时间"] = formatFileTime(xmlAttr(d, "max_tm"))
			row["最小值"] = xmlAttr(d, "min")
			row["最小值时间"] = formatFileTime(xmlAttr(d, "min_tm"))
		}
		if ioa == "" {
			row["信息体地址"] = "点" + itoa(i+1)
		}
		rows = append(rows, row)
	}
	return Result{Kind: "extreme", KindName: "极值记录", HasNames: hasNames,
		Columns: []string{"信息体地址", "点名", "单位", "最大值", "最大值时间", "最小值", "最小值时间"},
		Rows:    rows, Note: "极值记录（XML）"}, true
}

func parseFIXPTXML(root *xmlNode, resolve NameResolver) (Result, bool) {
	attrs := []*xmlNode{}
	dataNum, sectNum, interval := "", "", ""
	for _, da := range xmlChildren(root, "DataAttr") {
		attrs = append(attrs, xmlChildren(da, "DI")...)
		if dataNum == "" {
			dataNum = xmlAttr(da, "dataNum")
		}
		if sectNum == "" {
			sectNum = xmlAttr(da, "sectNum")
		}
		if interval == "" {
			interval = xmlAttr(da, "interval")
		}
	}
	recs := xmlChildren(root, "DataRec")
	if len(recs) == 0 || len(attrs) == 0 {
		return Result{}, false
	}
	hasNames := false
	rows := make([]Record, 0, len(recs)*len(attrs))
	for ri, dr := range recs {
		sect := xmlAttr(dr, "sect")
		if sect == "" {
			sect = itoa(ri + 1)
		}
		tm := formatFileTime(xmlAttr(dr, "tm"))
		vals := xmlChildren(dr, "DI")
		for j, attr := range attrs {
			ioa := xmlAttr(attr, "ioa")
			name, ok := pointName(resolve, "yc", ioa)
			if ok {
				hasNames = true
			}
			val := ""
			if j < len(vals) {
				val = xmlAttr(vals[j], "val")
			}
			rows = append(rows, Record{
				"节数": sect, "地址": addrHexText(ioa), "遥测名称": orDash(name),
				"遥测值": val, "时间": tm,
			})
		}
	}
	note := "定点记录（XML）"
	if dataNum != "" || sectNum != "" {
		note += "，点数 " + dataNum + "，节数 " + sectNum
	}
	if interval != "" {
		note += "，间隔 " + interval
	}
	note += "；共 " + itoa(len(rows)) + " 条"
	return Result{Kind: "fixed", KindName: "定点记录", HasNames: hasNames,
		Columns: []string{"节数", "地址", "遥测名称", "遥测值", "时间"}, Rows: rows, Note: note}, true
}

// addrHexText 文件里的信息体地址按十六进制显示（与现场工具一致，如 16385 → 4001）。
func addrHexText(ioa string) string {
	if n, ok := toInt(ioa); ok {
		return fmt.Sprintf("%x", n)
	}
	if strings.TrimSpace(ioa) == "" {
		return "—"
	}
	return ioa
}

// formatFileTime 将历史文件里的 YYMMDD_HHMMSS[_mmm] 转成 YYYY-MM-DD HH:MM:SS[.mmm]。
func formatFileTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "-") && strings.Contains(s, ":") {
		return strings.ReplaceAll(s, "_", " ")
	}
	core, ms := s, ""
	if i := strings.LastIndex(s, "_"); i > 0 && len(s)-i-1 == 3 && allDigits(s[i+1:]) {
		core, ms = s[:i], "."+s[i+1:]
	}
	parts := strings.Split(core, "_")
	if len(parts) >= 2 && len(parts[0]) == 6 && len(parts[1]) == 6 &&
		allDigits(parts[0]) && allDigits(parts[1]) {
		return "20" + parts[0][0:2] + "-" + parts[0][2:4] + "-" + parts[0][4:6] + " " +
			parts[1][0:2] + ":" + parts[1][2:4] + ":" + parts[1][4:6] + ms
	}
	if len(core) == 12 && allDigits(core) {
		return "20" + core[0:2] + "-" + core[2:4] + "-" + core[4:6] + " " +
			core[6:8] + ":" + core[8:10] + ":" + core[10:12] + ms
	}
	if len(core) == 14 && allDigits(core) {
		return core[0:4] + "-" + core[4:6] + "-" + core[6:8] + " " +
			core[8:10] + ":" + core[10:12] + ":" + core[12:14] + ms
	}
	return s
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func parseFlowRevXML(root *xmlNode, resolve NameResolver) (Result, bool) {
	var rows []Record
	hasNames := false
	for _, dr := range xmlChildren(root, "DataRec") {
		for _, d := range xmlChildren(dr, "DI") {
			ioa := xmlAttr(d, "ioa")
			name, ok := pointName(resolve, "yc", ioa)
			if ok {
				hasNames = true
			}
			rows = append(rows, Record{"信息体地址": ioa, "点名": orDash(name),
				"值": xmlAttr(d, "val"), "时间": formatFileTime(xmlAttr(d, "tm"))})
		}
	}
	if len(rows) == 0 {
		return Result{}, false
	}
	return Result{Kind: "flowrev", KindName: "潮流反向冻结值", HasNames: hasNames,
		Columns: []string{"信息体地址", "点名", "值", "时间"}, Rows: rows,
		Note: "潮流反向电能量冻结值（XML）"}, true
}

func operationCN(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "select", "选择", "sbo_select":
		return "选择"
	case "oper", "operate", "execute", "执行":
		return "执行"
	case "cancel", "撤销":
		return "撤销"
	case "select_confirm", "selectconfirm", "选择确认":
		return "选择确认"
	case "oper_confirm", "operconfirm", "execute_confirm", "执行确认":
		return "执行确认"
	case "cancel_confirm", "cancelconfirm", "撤销确认":
		return "撤销确认"
	}
	if s == "" {
		return "—"
	}
	return s
}
