// Package main — 文件服务（TI=210 私有文件服务 / TI=125 段落）内容组包：
// 按文件 ID / 文件号归并分段数据，供 ParseFile 解析（msg/xml）。
package main

import (
	"fmt"
	"sort"
	"strings"

	"iec104rt/internal/protocol"
	"iec104rt/internal/protocol/common"
	"iec104rt/internal/protocol/iec104"
)

// fileAcc 文件组包累加器。
type fileAcc struct {
	Key        string
	Name       string
	Size       int
	Segments   map[int][]byte
	Bytes      int // 已收分段长度之和（用于列表/兼容）
	MaxEnd     int // 分段偏移 + 长度 的最大值（按规约 segmentNo 是文件内容偏移）
	Complete   bool
	KindHint   string // 记录类型提示（soe/fault/event/...）
	FormatHint string // 由文件名后缀推断的格式（xml/msg）
	LastTime   string
}

// FileInfo 前端文件列表项。
type FileInfo struct {
	Key        string `json:"key"`
	Name       string `json:"name"`
	Size       int    `json:"size"`
	Bytes      int    `json:"bytes"`
	Segments   int    `json:"segments"`
	Complete   bool   `json:"complete"`
	KindHint   string `json:"kindHint"`
	FormatHint string `json:"formatHint"`
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

		// op=2 目录响应：预登记文件名/大小/格式（文件名后缀决定 xml/msg）
		if op == 2 {
			if entries, ok := p["entries"].([]common.M); ok {
				for _, e := range entries {
					n, _ := e["fileName"].(string)
					if n == "" {
						continue
					}
					acc := a.fileAccOf("210:name:" + n)
					acc.Name = n
					if s := asInt(e["fileSize"]); s > 0 {
						acc.Size = s
					}
					if f := formatFromName(n); f != "" {
						acc.FormatHint = f
					}
					if k := kindFromName(n); k != "" {
						acc.KindHint = k
					}
				}
			}
			return
		}
		if op < 3 { // 目录召唤（op=1）等：不对应单个文件
			return
		}

		// 规范 7.6.3：op=4/8（激活确认）带文件名；op=5/6/9/10 只带 fileId，不带文件名。
		// 文件传输同一时刻只传一个文件，用"当前文件"补全后续数据/确认帧的文件名。
		if name != "" && (op == 3 || op == 4 || op == 7 || op == 8) {
			a.currentFile210 = name
			if op == 3 {
				a.currentFileSize210 = 0
			}
			if size > 0 {
				a.currentFileSize210 = size
			}
		}
		if name == "" {
			if op == 5 || op == 6 || op == 9 || op == 10 {
				name = a.currentFile210
				if size == 0 {
					size = a.currentFileSize210
				}
				if name != "" {
					// 注入到解析结果：列表折叠、详情解析都按文件名归并（fileId 在规范中恒为 0）
					p["fileName"] = name
					if size > 0 {
						p["fileSize"] = size
					}
				}
			}
			if name == "" && fileID > 0 {
				name = a.fileNames[fileID]
			}
		}
		if name != "" && fileID > 0 {
			a.fileNames[fileID] = name
		}
		key := fileKey210(fileID, name)
		if key == "" {
			return
		}
		// 读文件激活（op=3）表示开始一次新的读取：清空同名文件的旧分段与解析缓存，
		// 避免同一文件被再次读取时沿用上一次的内容/结果。
		if op == 3 {
			if old := a.files[key]; old != nil {
				old.Segments = map[int][]byte{}
				old.Bytes = 0
				old.MaxEnd = 0
				old.Complete = false
			}
			prefix := key + "|"
			for k := range a.fileCache {
				if strings.HasPrefix(k, prefix) {
					delete(a.fileCache, k)
				}
			}
		}
		acc := a.fileAccOf(key)
		if name != "" {
			acc.Name = name
			if f := formatFromName(name); f != "" {
				acc.FormatHint = f
			}
			if k := kindFromName(name); k != "" {
				acc.KindHint = k
			}
		}
		if size > 0 {
			acc.Size = size
		}
		switch op {
		case 5: // 读文件数据传输：追加分段
			seg := asInt(p["segmentNo"])
			if hexStr, ok := p["dataHex"].(string); ok && hexStr != "" {
				if b, err := iec104.HexToBytes(hexStr); err == nil && len(b) > 0 {
					if _, dup := acc.Segments[seg]; !dup {
						acc.Segments[seg] = b
						acc.Bytes += len(b)
						if end := seg + len(b); end > acc.MaxEnd {
							acc.MaxEnd = end
						}
					}
				}
			}
			// 规范 7.6.3.2 c) 的"后续标志"：0=无后续（最后一段）
			if v, ok := p["contFlag"]; ok && asInt(v) == 0 && acc.MaxEnd > 0 {
				acc.Complete = true
			}
		case 6, 10: // 数据传输确认：resultCode=0（无后续）→ 传输结束
			if asInt(p["resultCode"]) == 0 && acc.MaxEnd > 0 {
				acc.Complete = true
			}
		}
		// 已知文件大小时，按偏移覆盖到文件末尾即完成（比累加长度更可靠）
		if acc.Size > 0 && acc.MaxEnd >= acc.Size {
			acc.Complete = true
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
// TI=210 优先按文件名归并（目录/激活/传输帧同名文件共用一份组包），无文件名时按 fileId。
func (a *App) fileKeyOf(p map[string]any) string {
	svc, _ := p["service"].(string)
	switch svc {
	case "F_FR_NA_1":
		op := asInt(p["op"])
		// 目录召唤/目录响应（op=1/2）不对应单个文件，不挂文件解析面板
		if op < 3 {
			return ""
		}
		id := asInt(p["fileId"])
		name, _ := p["fileName"].(string)
		if name == "" {
			a.muFile.Lock()
			if id > 0 {
				name = a.fileNames[id]
			}
			if name == "" && (op == 5 || op == 6 || op == 9 || op == 10) {
				name = a.currentFile210
			}
			a.muFile.Unlock()
		}
		return fileKey210(id, name)
	case "F_SG_NA", "F_LS_NA", "F_SR_NA":
		if nof := asInt(p["nof"]); nof > 0 {
			return "125:" + fmt.Sprint(nof)
		}
	}
	return ""
}

// fileKey210 TI=210 文件组包 key：有文件名按名归并；否则按 fileId；
// 规范中 fileId 恒为 0，抓包从传输中途开始时用 "210:0" 聚合。
func fileKey210(id int, name string) string {
	if name != "" {
		return "210:name:" + name
	}
	if id > 0 {
		return "210:" + fmt.Sprint(id)
	}
	return "210:0"
}

// formatFromName 由文件名后缀判断文件格式（.xml → xml，.msg → msg）。
func formatFromName(name string) string {
	l := strings.ToLower(strings.TrimSpace(name))
	l = strings.TrimRight(l, ". ") // 兼容定长文件名尾部 NUL/空格被转成 '.' 的情况
	switch {
	case strings.HasSuffix(l, ".xml"):
		return "xml"
	case strings.HasSuffix(l, ".msg"):
		return "msg"
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
			Bytes: acc.Bytes, Segments: len(acc.Segments), Complete: acc.Complete,
			KindHint: acc.KindHint, FormatHint: acc.FormatHint})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// fileSnapshot 文件组包快照：内容 + 文件名/格式提示 + 传输进度/完成状态。
type fileSnapshot struct {
	Data       []byte
	Name       string
	KindHint   string
	FormatHint string
	Size       int
	Bytes      int
	Complete   bool
}

// snapshotFile 按数据段偏移组装文件内容（segmentNo 是文件内容偏移指针），
// 并计算实际覆盖字节数（重叠分段只算一次），供进度/长度校验使用。
func (a *App) snapshotFile(key string) (fileSnapshot, error) {
	a.muFile.Lock()
	defer a.muFile.Unlock()
	acc := a.files[key]
	if acc == nil {
		return fileSnapshot{}, fmt.Errorf("未找到文件：%s", key)
	}
	offsets := make([]int, 0, len(acc.Segments))
	for n := range acc.Segments {
		offsets = append(offsets, n)
	}
	sort.Ints(offsets)

	total := acc.MaxEnd
	if acc.Size > 0 && total > acc.Size {
		total = acc.Size // 声明大小之外的多余数据不参与解析
	}
	if total > 128<<20 {
		return fileSnapshot{}, fmt.Errorf("文件过大（%d 字节），已拒绝在内存中组装", total)
	}
	buf := make([]byte, total)
	type ival struct{ s, e int }
	ivals := make([]ival, 0, len(offsets))
	for _, off := range offsets {
		if off < 0 || off >= total {
			continue
		}
		b := acc.Segments[off]
		s, e := off, off+len(b)
		if e > total {
			e = total
		}
		if s < e {
			copy(buf[s:e], b[:e-off])
			ivals = append(ivals, ival{s, e})
		}
	}
	covered := 0
	if len(ivals) > 0 {
		sort.Slice(ivals, func(i, j int) bool { return ivals[i].s < ivals[j].s })
		curS, curE := ivals[0].s, ivals[0].e
		for _, iv := range ivals[1:] {
			if iv.s > curE {
				covered += curE - curS
				curS, curE = iv.s, iv.e
			} else if iv.e > curE {
				curE = iv.e
			}
		}
		covered += curE - curS
	}
	return fileSnapshot{
		Data: buf, Name: acc.Name, KindHint: acc.KindHint, FormatHint: acc.FormatHint,
		Size: acc.Size, Bytes: covered, Complete: acc.Complete,
	}, nil
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
	case "extreme":
		return "极值记录"
	case "fixed":
		return "定点记录"
	case "wave":
		return "波形文件"
	case "flowrev":
		return "潮流反向冻结值"
	}
	return "未知记录"
}
