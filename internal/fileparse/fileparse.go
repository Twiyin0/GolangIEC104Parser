// Package fileparse 解析文件服务报文中传输的工程文件（msg / xml 格式）：
//   - 自动识别格式（优先 XML，其次 MSG），识别失败时可由用户指定强制格式；
//   - 判定记录类型：SOE 记录 / 故障事件 / 遥控记录 / 事件记录；
//   - 记录中的点号/地址字段通过点表（pointdb）解析点名（SOE、故障事件必需）。
package fileparse

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// Record 一条记录（列名 → 值）。
type Record = map[string]any

// NameResolver 点名解析回调：kind=soe/fault/control/event，addr=点号/地址。
type NameResolver func(kind string, addr int) string

// Result 文件解析结果。
type Result struct {
	Format   string     `json:"format"`   // xml | msg | cfg | dat
	Kind     string     `json:"kind"`     // soe | fault | control | event | extreme | fixed | flowrev | wave | unknown
	KindName string     `json:"kindName"` // 中文记录类型
	Columns  []string   `json:"columns"`
	Rows     []Record   `json:"rows"`
	Note     string     `json:"note"`  // 识别/统计说明
	Error    string     `json:"error"` // 自动解析失败原因（提示用户手动选择格式）
	HasNames bool       `json:"hasNames"`
	Chart    *WaveChart `json:"chart,omitempty"` // COMTRADE 波形

	// 文件传输状态（用于详情展示与"等待传输完成后再解析"）
	FileName string `json:"fileName"`
	Bytes    int    `json:"bytes"`
	Size     int    `json:"size"`
	Complete bool   `json:"complete"`
}

var kindNames = map[string]string{
	"soe":     "SOE记录",
	"fault":   "故障事件",
	"control": "遥控记录",
	"event":   "事件记录",
	"extreme": "极值记录",
	"fixed":   "定点记录",
	"wave":    "波形文件",
	"flowrev": "潮流反向冻结值",
	"unknown": "未知记录",
}

// Parse 解析文件内容；forced 为空表示自动识别（"xml"/"msg" 为强制格式）。
func Parse(data []byte, forced string, resolve NameResolver) Result {
	text := decodeText(data)
	res := Result{}
	switch strings.ToLower(strings.TrimSpace(forced)) {
	case "xml":
		res = parseXML(text, resolve)
		res.Format = "xml"
	case "msg":
		res = parseMSG(text)
		res.Format = "msg"
	default:
		res = autoParse(text, resolve)
	}
	if res.Format == "" {
		res.Format = strings.ToLower(strings.TrimSpace(forced))
	}
	if res.Kind == "" {
		res.Kind = classify(text, res.Columns)
	}
	res.KindName = kindNames[res.Kind]
	if res.KindName == "" {
		res.KindName = kindNames["unknown"]
	}
	applyNames(&res, resolve)
	normalizeFileColumns(&res)
	translateOperations(&res)
	if len(res.Rows) == 0 && res.Error == "" {
		res.Error = "未能从文件中解析出记录（可尝试手动指定 MSG 或 XML 格式）"
	}
	if res.Note == "" {
		res.Note = "共 " + itoa(len(res.Rows)) + " 条记录"
	} else {
		res.Note += "，共 " + itoa(len(res.Rows)) + " 条记录"
	}
	return res
}

// autoParse 自动识别：先 XML（以 < 开头或含 XML 声明），否则按 MSG 文本。
func autoParse(text string, resolve NameResolver) Result {
	trimmed := strings.TrimSpace(text)
	looksXML := strings.HasPrefix(trimmed, "<?xml") || strings.HasPrefix(trimmed, "<")
	if looksXML {
		r := parseXML(text, resolve)
		if len(r.Rows) > 0 || r.Chart != nil {
			r.Format = "xml"
			r.Note = joinNote("自动识别为 XML 格式", r.Note)
			return r
		}
	}
	r := parseMSG(text)
	if len(r.Rows) > 0 {
		r.Format = "msg"
		r.Note = joinNote("自动识别为 MSG（文本/分隔符）格式", r.Note)
		return r
	}
	if looksXML {
		r.Format = "xml"
		r.Error = r.Error + "（XML 解析未得到记录）"
	}
	return r
}

// joinNote 合并说明（保留解析器给出的分隔符/记录元素等细节）。
func joinNote(prefix, note string) string {
	if note == "" {
		return prefix
	}
	return prefix + "，" + note
}

// classify 依据关键字判定记录类型（优先级：SOE > 故障 > 遥控 > 事件）。
func classify(text string, columns []string) string {
	hay := strings.ToUpper(text)
	if len(hay) > 20000 {
		hay = hay[:20000]
	}
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(hay, strings.ToUpper(w)) {
				return true
			}
		}
		return false
	}
	switch {
	case has("SOE"):
		return "soe"
	case has("故障", "FAULT"):
		return "fault"
	case has("遥控", "YK", "CTRL", "CONTROL", "遥调"):
		return "control"
	case has("事件", "EVENT", "历史记录", "操作记录"):
		return "event"
	}
	return "unknown"
}

// applyNames 用点表解析记录中的点号/地址 → 点名。
func applyNames(res *Result, resolve NameResolver) {
	if resolve == nil || len(res.Rows) == 0 {
		return
	}
	// 定点记录已在“遥测名称”列完成点名解析，行里的“地址”是十六进制展示值，
	// 不能再按十进制地址二次解析，否则会追加多余的“点名”列或解析到错误点。
	if res.Kind == "fixed" {
		return
	}
	kind := res.Kind
	if kind == "" || kind == "unknown" {
		kind = "yx"
	}
	for _, row := range res.Rows {
		key, addr, ok := findAddr(row)
		if !ok {
			continue
		}
		if name := resolve(kind, addr); name != "" {
			row["点名"] = name
			if key != "" {
				row[key+"_name"] = name
			}
			res.HasNames = true
		}
	}
	if res.HasNames && !containsStr(res.Columns, "点名") {
		res.Columns = append(res.Columns, "点名")
	}
}

// findAddr 在记录中查找点号/地址字段（返回列名与数值）。
func findAddr(row Record) (string, int, bool) {
	for k, v := range row {
		if !isAddrKey(k) {
			continue
		}
		if n, ok := toInt(v); ok {
			return k, n, true
		}
	}
	return "", 0, false
}

func isAddrKey(k string) bool {
	lk := strings.ToLower(strings.TrimSpace(k))
	switch lk {
	case "addr", "address", "point", "pointaddr", "pointno", "pointid", "ioa",
		"infoobjaddr", "objaddr", "点号", "地址", "信息体地址", "点地址", "点位", "点表地址":
		return true
	}
	return strings.HasSuffix(lk, "addr") || strings.HasSuffix(lk, "point") || strings.HasSuffix(lk, "ioa")
}

// toInt 数值解析：0x 前缀 → 十六进制；纯数字 → 十进制；含 a-f 字母 → 十六进制。
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return 0, false
		}
		hex := false
		if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
			hex = true
			s = s[2:]
		}
		v := 0
		for _, c := range s {
			var d int
			switch {
			case c >= '0' && c <= '9':
				d = int(c - '0')
			case (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'):
				d = int(strings.ToLower(string(c))[0]-'a') + 10
				hex = true // 出现字母 → 按十六进制解析
			default:
				return 0, false
			}
			if hex {
				v = v*16 + d
			} else {
				v = v*10 + d
			}
		}
		return v, true
	}
	return 0, false
}

// decodeText 字节 → 文本（UTF-8 / GBK 简化处理：非法字节按字节映射，保留可读 ASCII）。
func decodeText(data []byte) string {
	if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF { // UTF-16BE BOM
		return decodeUTF16(data[2:], true)
	}
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE { // UTF-16LE BOM
		return decodeUTF16(data[2:], false)
	}
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	// 国内工程文件常见 GBK/GB18030 编码：非法 UTF-8 时按 GB18030 解码（含 GBK 子集）
	if !utf8.Valid(data) {
		if s, _, err := transform.String(simplifiedchinese.GB18030.NewDecoder(), string(data)); err == nil {
			return s
		}
	}
	return string(data)
}

func decodeUTF16(b []byte, bigEndian bool) string {
	var sb strings.Builder
	for i := 0; i+1 < len(b); i += 2 {
		var u uint16
		if bigEndian {
			u = uint16(b[i])<<8 | uint16(b[i+1])
		} else {
			u = uint16(b[i+1])<<8 | uint16(b[i])
		}
		sb.WriteRune(rune(u))
	}
	return sb.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// normalizeFileColumns 统一历史文件表头顺序（即使走的是通用 MSG/XML 解析）。
func normalizeFileColumns(res *Result) {
	var order []string
	switch res.Kind {
	case "soe":
		order = []string{"信息体地址", "点名", "值", "时间"}
	case "control":
		order = []string{"信息体地址", "点名", "操作", "命令状态", "时间"}
		// 通用表格可能把命令状态列命名为“命令/值”，统一成“命令状态”
		for _, row := range res.Rows {
			if _, ok := row["命令状态"]; !ok {
				if v, ok := row["命令"]; ok {
					row["命令状态"] = v
					delete(row, "命令")
				} else if v, ok := row["值"]; ok {
					row["命令状态"] = v
					delete(row, "值")
				}
			}
		}
		for i, c := range res.Columns {
			if c == "命令" || c == "值" {
				res.Columns[i] = "命令状态"
			}
		}
	default:
		return
	}
	have := map[string]bool{}
	for _, c := range res.Columns {
		have[c] = true
	}
	out := make([]string, 0, len(res.Columns))
	used := map[string]bool{}
	for _, c := range order {
		if have[c] {
			out = append(out, c)
			used[c] = true
		}
	}
	for _, c := range res.Columns {
		if !used[c] {
			out = append(out, c)
		}
	}
	res.Columns = out
}
func translateOperations(res *Result) {
	for _, row := range res.Rows {
		if v, ok := row["操作"].(string); ok {
			row["操作"] = operationCN(v)
		}
	}
}

// cnKey 把文件中的常见英文标识转成中文表头，避免详情出现 ioa/tm/val 等原始字段名。
func cnKey(k string) string {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "ioa", "infoobjaddr", "objaddr", "address", "addr":
		return "信息体地址"
	case "tm", "time", "timestamp":
		return "时间"
	case "val", "value":
		return "值"
	case "cmd", "operation", "op":
		return "操作"
	case "max":
		return "最大值"
	case "min":
		return "最小值"
	case "max_tm", "maxtm":
		return "最大值时间"
	case "min_tm", "mintm":
		return "最小值时间"
	case "type":
		return "类型"
	case "unit", "uu":
		return "单位"
	case "name", "ch_id":
		return "名称"
	case "sect":
		return "节号"
	}
	return k
}
