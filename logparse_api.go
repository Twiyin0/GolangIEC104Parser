package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/text/encoding/simplifiedchinese"

	"iec104rt/internal/protocol"
	"iec104rt/internal/protocol/iec104"
)

// LogParseResult 静态模式 log 文件/文本解析结果。
type LogParseResult struct {
	FileName  string           `json:"fileName"`
	Lines     int              `json:"lines"`
	HexLines  int              `json:"hexLines"`
	Frames    []protocol.Frame `json:"frames"`
	Ignored   int              `json:"ignored"`
	Truncated int              `json:"truncated"`
	Warnings  []string         `json:"warnings,omitempty"`
	Note      string           `json:"note"`
}

// logMsgRe 匹配终端日志中的 101/104 报文行：
//
//	protolog:2026/10/18-16:36:40: ...If(COM0) Tx(6) --->  10 00 01 00 01 16
//	protolog:2026/10/18-16:36:40: ...If(COM0) Rx(18) <---   68 0C ... 16
//
// 只取方向与末尾连续 HEX；前缀里的时间、文件、行号不参与报文解析。
var logMsgRe = regexp.MustCompile(`(?i)\b(Tx|Rx)\s*\(\s*\d+\s*\)\s*(?:-+>|<-+)\s*((?:[0-9A-Fa-f]{2})(?:\s+[0-9A-Fa-f]{2})*)`)

// logTimeRe 提取日志前缀中的终端时间，支持 2026/10/18-16:36:40[.mmm]。
var logTimeRe = regexp.MustCompile(`(\d{4})[/-](\d{2})[/-](\d{2})[-_ T](\d{2}):(\d{2}):(\d{2})(?:[.,](\d{1,6}))?`)

// ParseLogText 解析粘贴的 log 文本（静态模式“解析 Log 文本”）。
func (a *App) ParseLogText(text string) (LogParseResult, error) {
	if len(text) > 128<<20 {
		return LogParseResult{}, fmt.Errorf("log 文本过大（>128MB），请先拆分")
	}
	return a.parseLogText(text, ""), nil
}

// ParseLogFile 解析指定 log 文件（供测试/内部调用）。
func (a *App) ParseLogFile(path string) (LogParseResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return LogParseResult{}, fmt.Errorf("读取 log 文件失败: %v", err)
	}
	if len(data) > 128<<20 {
		return LogParseResult{}, fmt.Errorf("log 文件过大（>128MB），请先拆分")
	}
	return a.parseLogText(decodeLogBytes(data), filepath.Base(path)), nil
}

// ParseLogFileDialog 静态模式：选择 log 文件并解析。
func (a *App) ParseLogFileDialog() (LogParseResult, error) {
	if a.ctx == nil {
		return LogParseResult{}, fmt.Errorf("应用未就绪")
	}
	path, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "选择 log 文件（自动解析 101/104 报文）",
		Filters: []wruntime.FileFilter{
			{DisplayName: "日志文件 (*.log;*.txt;*.out;*.dat)", Pattern: "*.log;*.txt;*.out;*.dat"},
			{DisplayName: "所有文件 (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil {
		return LogParseResult{}, err
	}
	if path == "" {
		return LogParseResult{Note: "已取消选择"}, nil
	}
	return a.ParseLogFile(path)
}

func (a *App) parseLogText(text, fileName string) LogParseResult {
	res := LogParseResult{FileName: fileName}
	// 去掉 UTF-8 BOM（逐行解析前统一处理）
	text = strings.TrimPrefix(text, "\uFEFF")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	res.Lines = len(lines)
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		m := logMsgRe.FindStringSubmatch(line)
		if m == nil {
			res.Ignored++
			continue
		}
		res.HexLines++
		hexStr := strings.Join(strings.Fields(m[2]), " ")
		buf, err := iec104.HexToBytes(hexStr)
		if err != nil || len(buf) == 0 {
			res.Ignored++
			continue
		}
		// 先用 L 判断是否少数据：101 可变帧需 L+6 字节；不完整帧不猜测协议。
		if chk := protocol.CheckFrame(buf); !chk.Complete {
			res.Truncated++
			res.Ignored++
			if len(res.Warnings) < 3 {
				res.Warnings = append(res.Warnings, chk.Reason)
			}
			continue
		}
		frames := protocol.ParseBest(buf)
		if len(frames) == 0 {
			res.Ignored++
			continue
		}
		ts := logTime(line)
		dir := "M→T"
		if strings.EqualFold(m[1], "Rx") {
			dir = "T→M"
		}
		for i := range frames {
			frames[i].Time = ts
			frames[i].Dir = dir
			frames[i].ID = int64(len(res.Frames) + 1)
			res.Frames = append(res.Frames, frames[i])
		}
	}
	res.Note = fmt.Sprintf("共 %d 行，识别报文行 %d，解析帧 %d，忽略 %d 行",
		res.Lines, res.HexLines, len(res.Frames), res.Ignored)
	if res.Truncated > 0 {
		res.Note += fmt.Sprintf("；其中不完整 %d 行", res.Truncated)
		if len(res.Warnings) > 0 {
			res.Note += "（" + res.Warnings[0] + "）"
		}
	}
	a.enrichFrames(res.Frames)
	a.setStaticFrames(res.Frames)
	return res
}

// logTime 从日志前缀提取终端时间并统一为 2026-10-18 16:36:40[.mmm]。
func logTime(line string) string {
	m := logTimeRe.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	ts := m[1] + "-" + m[2] + "-" + m[3] + " " + m[4] + ":" + m[5] + ":" + m[6]
	if m[7] != "" {
		ts += "." + m[7]
	}
	return ts
}

// decodeLogBytes 日志编码兼容：UTF-8 优先，失败回退 GB18030（现场 log 常见 GBK）。
func decodeLogBytes(data []byte) string {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(data) {
		return string(data)
	}
	if out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data); err == nil {
		return string(out)
	}
	return string(data)
}
