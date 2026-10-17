// Package main — 文件服务（TI=210 私有文件服务 / TI=125 段落）内容组包：
// 按文件 ID / 文件号归并分段数据，供 ParseFile 解析（msg/xml）。
package main

import (
	"fmt"
	"sort"
	"strings"

	"iec104rt/internal/protocol"
	"iec104rt/internal/protocol/iec104"
)

// fileAcc 文件组包累加器。
type fileAcc struct {
	Key      string
	Name     string
	Size     int
	Segments map[int][]byte
	Bytes    int
	Complete bool
	KindHint string
	LastTime string
}

// FileInfo 前端文件列表项。
type FileInfo struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Size     int    `json:"size"`
	Bytes    int    `json:"bytes"`
	Segments int    `json:"segments"`
	Complete bool   `json:"complete"`
	KindHint string `json:"kindHint"`
}

func (a *App) fileAccOf(key string) *fileAcc {
	acc := a.files[key]
	if acc == nil {
		acc = &fileAcc{Key: key, Segments: map[int][]byte{}}
		a.files[key] = acc
	}
	return acc
}

// collectFileFrame 采集文件服务帧内容（报文入缓冲前调用）。
func (a *App) collectFileFrame(f *protocol.Frame) {
	p := f.Parsed
	if p == nil || f.Type != "file_service" {
		return
	}
	svc, _ := p["service"].(string)
	a.muFile.Lock()
	defer a.muFile.Unlock()
	switch svc {
	case "F_FR_NA_1": // TI=210 私有文件服务
		op := asInt(p["op"])
		fileID := asInt(p["fileId"])
		name, _ := p["fileName"].(string)
		size := asInt(p["fileSize"])
		key := ""
		if fileID > 0 {
			key = "210:" + fmt.Sprint(fileID)
		} else if name != "" {
			key = "210:name:" + name
		}
		if key == "" {
			return
		}
		acc := a.fileAccOf(key)
		if name != "" {
			acc.Name = name
			acc.KindHint = kindFromName(name)
		}
		if size > 0 {
			acc.Size = size
		}
		if op == 5 { // 读文件数据传输：追加分段数据
			seg := asInt(p["segmentNo"])
			if hexStr, ok := p["dataHex"].(string); ok && hexStr != "" {
				if b, err := iec104.HexToBytes(hexStr); err == nil && len(b) > 0 {
					if _, dup := acc.Segments[seg]; !dup {
						acc.Segments[seg] = b
						acc.Bytes += len(b)
					}
				}
			}
		}
		acc.LastTime = f.Time
	case "F_SG_NA": // TI=125 文件段落
		nof := asInt(p["nof"])
		nos := asInt(p["nos"])
		key := "125:" + fmt.Sprint(nof)
		acc := a.fileAccOf(key)
		if acc.Name == "" {
			acc.Name = fmt.Sprintf("文件#%d", nof)
			acc.KindHint = "event"
		}
		if hexStr, ok := p["dataHex"].(string); ok && hexStr != "" {
			if b, err := iec104.HexToBytes(hexStr); err == nil && len(b) > 0 {
				if _, dup := acc.Segments[nos]; !dup {
					acc.Segments[nos] = b
					acc.Bytes += len(b)
				}
			}
		}
		acc.LastTime = f.Time
	case "F_LS_NA": // 最后段/节 → 标记完成
		nof := asInt(p["nof"])
		if acc := a.files["125:"+fmt.Sprint(nof)]; acc != nil {
			acc.Complete = true
		}
	}
}

// fileKeyOf 取报文对应的文件 key（详情渲染时用于展示文件解析结果）。
func (a *App) fileKeyOf(p map[string]any) string {
	svc, _ := p["service"].(string)
	switch svc {
	case "F_FR_NA_1":
		if id := asInt(p["fileId"]); id > 0 {
			return "210:" + fmt.Sprint(id)
		}
		if name, _ := p["fileName"].(string); name != "" {
			return "210:name:" + name
		}
	case "F_SG_NA", "F_LS_NA", "F_SR_NA":
		if nof := asInt(p["nof"]); nof > 0 {
			return "125:" + fmt.Sprint(nof)
		}
	}
	return ""
}

// GetFiles 已组包文件列表。
func (a *App) GetFiles() []FileInfo {
	a.muFile.Lock()
	defer a.muFile.Unlock()
	out := make([]FileInfo, 0, len(a.files))
	for _, acc := range a.files {
		out = append(out, FileInfo{Key: acc.Key, Name: acc.Name, Size: acc.Size,
			Bytes: acc.Bytes, Segments: len(acc.Segments), Complete: acc.Complete, KindHint: acc.KindHint})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// fileContent 拼接文件内容（按分段号排序）。
func (a *App) fileContent(key string) ([]byte, string, string, error) {
	a.muFile.Lock()
	defer a.muFile.Unlock()
	acc := a.files[key]
	if acc == nil {
		return nil, "", "", fmt.Errorf("未找到文件：%s", key)
	}
	segs := make([]int, 0, len(acc.Segments))
	for n := range acc.Segments {
		segs = append(segs, n)
	}
	sort.Ints(segs)
	var buf []byte
	for _, n := range segs {
		buf = append(buf, acc.Segments[n]...)
	}
	if len(buf) == 0 {
		return nil, acc.Name, acc.KindHint, fmt.Errorf("文件尚无内容（等待数据传输帧）")
	}
	return buf, acc.Name, acc.KindHint, nil
}

// kindFromName 由文件名/目录名推断记录类型（HISTORY/SOE、COMTRADE 等）。
func kindFromName(name string) string {
	u := strings.ToUpper(name)
	switch {
	case strings.Contains(u, "SOE"):
		return "soe"
	case strings.Contains(u, "COMTRADE"), strings.Contains(u, "FAULT"),
		strings.Contains(u, "故障"), strings.Contains(u, "录波"):
		return "fault"
	case strings.Contains(u, "CTRL"), strings.Contains(u, "遥控"):
		return "control"
	case strings.Contains(u, "EXTREME"), strings.Contains(u, "极值"),
		strings.Contains(u, "FIXED"), strings.Contains(u, "定点"),
		strings.Contains(u, "LOG"), strings.Contains(u, "日志"), strings.Contains(u, "记录"):
		return "event"
	}
	return ""
}

func kindDisplay(kind string) string {
	switch kind {
	case "soe":
		return "SOE记录"
	case "fault":
		return "故障事件"
	case "control":
		return "遥控记录"
	case "event":
		return "事件记录"
	}
	return "未知记录"
}
