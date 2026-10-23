// Package importer 导入此前导出的 md/text/log 文件，提取 HEX 重新解析渲染。
package importer

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"iec104rt/internal/protocol"
	"iec104rt/internal/protocol/iec104"
)

var (
	timeRe = regexp.MustCompile(`(\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}(?:\.\d{1,3})?)|(\d{2}:\d{2}:\d{2}(?:\.\d{1,3})?)`)
)

// ImportFile 导入 md/text/log 文件并重新解析（自动识别格式）。
func ImportFile(path string) ([]protocol.Frame, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(data)
	var frames []protocol.Frame
	if strings.Contains(content, "\n### ") {
		frames = importMarkdown(content)
	} else {
		frames = importLines(content)
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("文件中未找到可解析的报文（HEX）：%s", path)
	}
	return frames, nil
}

// importMarkdown 解析 markdown：### 行取报文原文 HEX，## 行取时间（需求第 8/9 条闭环）。
func importMarkdown(content string) []protocol.Frame {
	var frames []protocol.Frame
	var curTime string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "## ") {
			if m := timeRe.FindString(line); m != "" {
				curTime = m
			}
			continue
		}
		if strings.HasPrefix(line, "### ") {
			for _, f := range parseHexRun(line[4:]) {
				if curTime != "" {
					f.Time = curTime
				}
				frames = append(frames, f)
			}
		}
	}
	return frames
}

// importLines 解析 text/log 行式格式：每行提取最长 HEX 字段。
func importLines(content string) []protocol.Frame {
	var frames []protocol.Frame
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		tm := timeRe.FindString(line)
		for _, f := range parseHexRun(line) {
			if tm != "" {
				f.Time = tm
			}
			frames = append(frames, f)
		}
	}
	return frames
}

// parseHexRun 从一行文本提取 HEX 字节序列（token 化：只接受纯字节对 token 的连续段，
// 天然隔离 "101"/"104" 协议前缀与日期时间等内容），切帧并逐条解析。
func parseHexRun(line string) []protocol.Frame {
	best := ""
	cur := ""
	flush := func() {
		if len(cur) > len(best) {
			best = cur
		}
		cur = ""
	}
	for _, tok := range strings.Fields(strings.ToUpper(line)) {
		switch {
		case isHexPair(tok):
			cur += tok
		case len(tok) >= 4 && len(tok)%2 == 0 && isHex(tok): // 连续无分隔写法
			flush()
			if len(tok) > len(best) {
				best = tok
			}
		default:
			flush()
		}
	}
	flush()
	if best == "" {
		return nil
	}
	buf, err := iec104.HexToBytes(best)
	if err != nil {
		return nil
	}
	var out []protocol.Frame
	// 先按 L 判断是否少数据；截断帧不猜测协议（避免 101 截断帧被当成 104）。
	if chk := protocol.CheckFrame(buf); !chk.Complete {
		return nil
	}
	// 自适应：优先 Detect（含 101 FT1.2 校验），其次比较两种切帧数量。
	out = protocol.ParseBest(buf)
	if len(out) == 0 {
		if f, err := protocol.ParseAuto(buf); err == nil {
			out = []protocol.Frame{f}
		}
	}
	now := time.Now().Format("2006-01-02 15:04:05.000")
	for i := range out {
		if out[i].Time == "" {
			out[i].Time = now
		}
	}
	return out
}

func isHexPair(tok string) bool { return len(tok) == 2 && isHex(tok) }

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9') && !(c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
