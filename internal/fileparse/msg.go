package fileparse

import (
	"strings"
)

// parseMSG 解析文本（MSG）格式：分隔符（, ; \t 空格）+ 可选表头；亦支持 key=value 行。
func parseMSG(text string) Result {
	clean := strings.NewReplacer("\x00", "", "\r\n", "\n", "\r", "\n").Replace(text)
	lines := splitNonEmpty(clean)
	if len(lines) == 0 {
		return Result{Error: "MSG 解析失败：文件内容为空"}
	}

	delim, delimName := detectDelimiter(lines)
	if delim == "" {
		if kv := parseKeyValue(lines); len(kv.Rows) > 0 {
			return kv
		}
		return Result{Error: "MSG 解析失败：未识别到分隔符（支持 , ; Tab 空格）或 key=value 结构"}
	}

	// 仅保留含分隔符的行（跳过标题行等）
	rows0 := make([]string, 0, len(lines))
	for _, ln := range lines {
		if strings.Contains(ln, delim) {
			rows0 = append(rows0, ln)
		}
	}
	if len(rows0) == 0 {
		return Result{Error: "MSG 解析失败：未找到含分隔符的数据行"}
	}
	header := []string(nil)
	body := rows0
	if looksLikeHeader(rows0, delim) {
		header = splitFields(rows0[0], delim)
		body = rows0[1:]
	}
	rows := make([]Record, 0, len(body))
	maxCols := len(header)
	for _, ln := range body {
		fields := splitFields(ln, delim)
		if len(fields) > maxCols {
			maxCols = len(fields)
		}
		row := Record{}
		for i, v := range fields {
			row[colName(header, i)] = strings.TrimSpace(v)
		}
		rows = append(rows, row)
	}
	cols := header
	if cols == nil {
		cols = make([]string, 0, maxCols)
		for i := 0; i < maxCols; i++ {
			cols = append(cols, colName(nil, i))
		}
	}
	note := "分隔符 " + delimName
	if header != nil {
		note += "，含表头"
	} else {
		note += "，无表头（按列序号）"
	}
	return Result{Columns: cols, Rows: rows, Note: note}
}

func colName(header []string, i int) string {
	if i < len(header) {
		if h := strings.TrimSpace(header[i]); h != "" {
			return h
		}
	}
	return "列" + itoa(i+1)
}

func splitNonEmpty(s string) []string {
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, ln := range raw {
		if strings.TrimSpace(ln) != "" {
			out = append(out, ln)
		}
	}
	return out
}

// detectDelimiter 选取"出现行数最多且每行列数一致"的分隔符（不要求首行含分隔符，容忍标题行）。
func detectDelimiter(lines []string) (string, string) {
	cands := []struct{ d, name string }{
		{",", "逗号"}, {"\t", "Tab"}, {";", "分号"}, {"|", "竖线"}, {" ", "空格"},
	}
	n := len(lines)
	if n > 30 {
		n = 30
	}
	best, bestName, bestScore := "", "", 0
	for _, c := range cands {
		counts := map[int]int{}
		maxCount := 0
		for i := 0; i < n; i++ {
			k := strings.Count(lines[i], c.d)
			if k > 0 {
				counts[k]++
				if k > maxCount {
					maxCount = k
				}
			}
		}
		if maxCount == 0 {
			continue
		}
		bestFreq := 0
		bestCount := 0
		for k, freq := range counts {
			if freq > bestFreq || (freq == bestFreq && k > bestCount) {
				bestFreq, bestCount = freq, k
			}
		}
		score := bestCount * bestFreq
		if score > bestScore {
			best, bestName, bestScore = c.d, c.name, score
		}
	}
	return best, bestName
}

// looksLikeHeader 判断首行（已含分隔符）是否为表头：非纯数字、且字段数与次行一致。
func looksLikeHeader(lines []string, delim string) bool {
	if len(lines) < 2 {
		return false
	}
	h := splitFields(lines[0], delim)
	if len(h) < 2 {
		return false
	}
	numeric := 0
	for _, f := range h {
		if isNumeric(strings.TrimSpace(f)) {
			numeric++
		}
	}
	if numeric == len(h) {
		return false // 首行全数字 → 数据行
	}
	second := splitFields(lines[1], delim)
	return len(second) == len(h)
}

func splitFields(line, delim string) []string {
	parts := strings.Split(line, delim)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, p)
	}
	return out
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && c != '.' && c != '-' && c != '+' && c != ':' {
			return false
		}
	}
	return true
}

// parseKeyValue 解析 "key=value"（或 "key:value"）文本 → 合成一条记录（如配置/单记录文件）。
func parseKeyValue(lines []string) Result {
	row := Record{}
	cols := []string{}
	for _, ln := range lines {
		sep := "="
		if !strings.Contains(ln, "=") && strings.Contains(ln, ":") {
			sep = ":"
		}
		if !strings.Contains(ln, sep) {
			return Result{}
		}
		parts := strings.SplitN(ln, sep, 2)
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		if k == "" {
			continue
		}
		row[k] = v
		if !containsStr(cols, k) {
			cols = append(cols, k)
		}
	}
	if len(row) == 0 {
		return Result{}
	}
	return Result{Columns: cols, Rows: []Record{row}, Note: "key=value 结构（1 条记录）"}
}
