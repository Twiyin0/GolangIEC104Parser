package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"iec104rt/internal/capture/wpcap"
	"iec104rt/internal/exporter"
	"iec104rt/internal/importer"
	"iec104rt/internal/protocol"
	"iec104rt/internal/protocol/iec104"
)

// ── 报文查询 ──────────────────────────────────────────────────────────

// GetFrames 增量拉取：ID 大于 fromID 的帧（列表渲染用）。
func (a *App) GetFrames(fromID int64, limit int) []protocol.Frame {
	return a.sess.SnapshotFrom(fromID, limit)
}

// GetFrame 按 ID 取完整解析结果（点击报文立即渲染；注入点表点名/参数名；
// 文件服务帧附带文件内容解析结果：自动识别 msg/xml 与记录类型）。
// 若报文不在当前会话缓冲（已清空/已被环形上限淘汰）则返回 Found=false。
func (a *App) GetFrame(id int64) protocol.Frame {
	f, ok := a.sess.Get(id)
	if !ok {
		return protocol.Frame{}
	}
	a.enrichFrame(&f)
	if key := a.fileKeyOf(f.Parsed); key != "" {
		f.Parsed["fileKey"] = key
		res, _ := a.ParseFile(key, "")
		f.Parsed["fileParse"] = res
	}
	return f
}

// ClearSession 清空本次监听缓冲。
func (a *App) ClearSession() { a.sess.Clear() }

// ── 导出（File → Export）────────────────────────────────────────────

func (a *App) saveDialog(defaultName, filterExt string) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("应用未就绪")
	}
	p := exporter.DefaultPath(filterExt)
	base := filepath.Base(p)
	return wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:           "导出文件",
		DefaultFilename: strings.TrimSuffix(base, filepath.Ext(base)) + "_copy." + filterExt,
		Filters: []wruntime.FileFilter{
			{DisplayName: strings.ToUpper(filterExt) + " 文件 (*." + filterExt + ")", Pattern: "*." + filterExt},
		},
	})
}

// ExportDialog 统一导出入口（File → Export）。
//
//	kind   : md | txt | log
//	source : session（实时缓冲）| import（导入回放）| static（静态解析，frames 由前端回传）
//	frames : source=static 时前端回传的报文
//	ids    : 非空时仅导出这些 ID 的报文（"仅导出筛选结果"）
//
// 导出前统一按**当前点表**注入点名/参数名（含当前表模式）。
func (a *App) ExportDialog(kind string, source string, frames []protocol.Frame, ids []int64) (string, error) {
	var all []protocol.Frame
	switch source {
	case "import":
		a.muImported.Lock()
		all = append(all, a.imported...)
		a.muImported.Unlock()
	case "static":
		all = append(all, frames...)
	default:
		all = a.sess.All()
	}
	if len(all) == 0 {
		return "", fmt.Errorf("没有可导出的报文")
	}
	if len(ids) > 0 { // 仅导出筛选结果
		want := make(map[int64]bool, len(ids))
		for _, id := range ids {
			want[id] = true
		}
		picked := make([]protocol.Frame, 0, len(ids))
		for _, f := range all {
			if want[f.ID] {
				picked = append(picked, f)
			}
		}
		if len(picked) == 0 {
			return "", fmt.Errorf("筛选结果为空，无可导出报文")
		}
		all = picked
	}
	a.enrichFrames(all) // 使用当前点表渲染点名/参数名

	path, err := a.saveDialog(kind, exportExt(kind))
	if err != nil || path == "" {
		return "", err
	}
	switch kind {
	case "txt":
		return path, exporter.ExportText(path, all)
	case "log":
		return path, exporter.ExportLog(path, all)
	default:
		title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		return path, exporter.ExportMarkdown(path, title, all)
	}
}

func exportExt(kind string) string {
	switch kind {
	case "txt":
		return "txt"
	case "log":
		return "log"
	default:
		return "md"
	}
}

// ExportMarkdownDialog 导出 markdown（实时缓冲全量；保留兼容）。
func (a *App) ExportMarkdownDialog() (string, error) {
	return a.ExportDialog("md", "session", nil, nil)
}

// ExportTextDialog 导出 text（实时缓冲全量；保留兼容）。
func (a *App) ExportTextDialog() (string, error) {
	return a.ExportDialog("txt", "session", nil, nil)
}

// ExportLogDialog 导出 log（实时缓冲全量；保留兼容）。
func (a *App) ExportLogDialog() (string, error) {
	return a.ExportDialog("log", "session", nil, nil)
}

// ── 导入（File → Import）────────────────────────────────────────────

// ImportFileDialog 导入 md/text/log，重新解析渲染（不写入实时缓冲；保存副本供筛选导出）。
func (a *App) ImportFileDialog() ([]protocol.Frame, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("应用未就绪")
	}
	path, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "导入文件（md / text / log）",
		Filters: []wruntime.FileFilter{
			{DisplayName: "解析文件 (*.md;*.txt;*.log)", Pattern: "*.md;*.txt;*.log"},
		},
	})
	if err != nil || path == "" {
		return nil, err
	}
	frames, err := importer.ImportFile(path)
	if err != nil {
		return nil, err
	}
	for i := range frames { // 分配 ID（供列表点击渲染与筛选后导出）
		frames[i].ID = int64(i + 1)
	}
	a.enrichFrames(frames)
	a.muImported.Lock()
	a.imported = frames // 保存副本（供"仅导出筛选结果"）
	a.muImported.Unlock()
	return frames, nil
}

// ── 静态解析（Edit → Mode → Static Parser）─────────────────────────

// ParseStatic 静态模式：逐行提取 HEX，自适应 101/104 切帧解析。
func (a *App) ParseStatic(text string) ([]protocol.Frame, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("请输入报文（HEX，支持多行）")
	}
	now := time.Now().Format("2006-01-02 15:04:05.000")
	var out []protocol.Frame
	var bad []string
	lineNo := 0
	for _, line := range strings.Split(text, "\n") {
		lineNo++
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		buf, err := iec104.HexToBytes(line)
		if err != nil || len(buf) == 0 {
			continue
		}
		// 先按 L 判断是否少数据：101 可变帧需 L+6 字节，101 固定帧需 6 字节。
		if chk := protocol.CheckFrame(buf); !chk.Complete {
			if len(bad) < 8 {
				bad = append(bad, fmt.Sprintf("第%d行：%s", lineNo, chk.Reason))
			}
			continue
		}
		// 优先按帧头/校验自适应；长度恰好为 104 时也要让 101 完整帧胜出
		// （101 可变帧 68 L L 68 … CS 16，按 104 切会截断成 68 L … 而被误判为 104）。
		got := protocol.ParseBest(buf)
		if len(got) == 0 {
			continue
		}
		for i := range got {
			got[i].Time = now
			got[i].ID = int64(len(out) + 1)
		}
		out = append(out, got...)
	}
	if len(out) == 0 {
		if len(bad) > 0 {
			return nil, fmt.Errorf("报文不完整：%s", strings.Join(bad, "；"))
		}
		return nil, fmt.Errorf("未找到可解析的报文（请检查 HEX 格式）")
	}
	if len(bad) > 0 {
		msg := "已跳过不完整帧：" + strings.Join(bad, "；")
		out = append(out, protocol.Frame{
			ID: int64(len(out) + 1), Time: now, Type: "error", Category: "错误", Summary: msg,
			Parsed: map[string]any{"type": "error", "error": msg},
		})
	}
	a.enrichFrames(out)
	a.setStaticFrames(out)
	return out, nil
}

// setStaticFrames 保存最近一次静态/log 文本解析结果，供点表变更后重新注入点名。
func (a *App) setStaticFrames(frames []protocol.Frame) {
	a.muStatic.Lock()
	a.staticFrames = append([]protocol.Frame(nil), frames...)
	a.muStatic.Unlock()
}

// ── 其他工具 ──────────────────────────────────────────────────────────

// DetectMasterPortManually 手动扫描主站端口（本机 TCP 连接表）。
func (a *App) DetectMasterPortManually() (int, error) {
	a.mu.Lock()
	cfg := a.cfg.Connection
	a.mu.Unlock()
	p, err := wpcap.FindLocalPortTo(cfg.TerminalIP, cfg.TerminalPort)
	if err == nil {
		a.muMaster.Lock()
		a.masterPort = p
		a.muMaster.Unlock()
		a.emit("master", map[string]any{"port": p})
	}
	return p, err
}
