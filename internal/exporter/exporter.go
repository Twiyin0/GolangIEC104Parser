// Package exporter 导出：markdown（层级硬规范 # 文件名 / ## 时间 / ### 报文原文 / 正文=解析结果）、
// text、log 三种格式。HEX 均完整可被 importer 回收。
package exporter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"iec104rt/internal/protocol"
)

// ExportMarkdown 导出 markdown（需求第 8 条硬性格式规范）。
func ExportMarkdown(path, title string, frames []protocol.Frame) error {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s\n\n", title)) // 一级=文件名（仅一次）
	if len(frames) == 0 {
		b.WriteString("> 本次监听没有报文\n")
	}
	for i := range frames {
		f := frames[i]
		b.WriteString(fmt.Sprintf("## %s\n\n", timeText(f)))                       // 二级=时间
		b.WriteString(fmt.Sprintf("### %s\n\n", rawLine(f)))                        // 三级=报文原文
		b.WriteString(RenderFrameMD(f))                                             // 正文=解析结果
		b.WriteString("\n")
	}
	return writeFile(path, []byte(b.String()))
}

// ExportText 导出 text：时间 TAB 方向 TAB 协议TAB HEX TAB 摘要。
func ExportText(path string, frames []protocol.Frame) error {
	var b strings.Builder
	for i := range frames {
		f := frames[i]
		b.WriteString(fmt.Sprintf("%s\t%s\t%s\t%s\t%s\n", timeText(f), f.Dir, f.Proto, f.Hex, f.Summary))
	}
	return writeFile(path, []byte(b.String()))
}

// ExportLog 导出 log：[时间] [方向] [协议] HEX -> 摘要。
func ExportLog(path string, frames []protocol.Frame) error {
	var b strings.Builder
	for i := range frames {
		f := frames[i]
		b.WriteString(fmt.Sprintf("[%s] [%s] [%s] %s -> %s\n", timeText(f), f.Dir, f.Proto, f.Hex, f.Summary))
	}
	return writeFile(path, []byte(b.String()))
}

// DefaultPath 默认导出路径：exe 同目录 export\ 前缀。
func DefaultPath(ext string) string {
	dir := "export"
	if exe, err := os.Executable(); err == nil {
		dir = filepath.Join(filepath.Dir(exe), "export")
	}
	name := fmt.Sprintf("104parser_%s.%s", time.Now().Format("20060102_150405"), ext)
	return filepath.Join(dir, name)
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func timeText(f protocol.Frame) string {
	if f.Time != "" {
		return f.Time
	}
	return time.Now().Format("2006-01-02 15:04:05.000")
}

func rawLine(f protocol.Frame) string {
	return fmt.Sprintf("[%s] [%s] %s", f.Proto, f.Dir, f.Hex)
}
