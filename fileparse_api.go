package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"iec104rt/internal/fileparse"
)

// ParseFile 解析已组包的文件内容（forced="" 自动识别；"msg"/"xml" 用户指定）。
// 未传输完成且未手动指定格式时返回"等待传输完成"占位结果；完成后按文件名后缀（.xml/.msg）
// 或内容自动解析。记录中的点号/地址用当前点表解析点名（SOE记录、故障事件必需）。
func (a *App) ParseFile(key string, forced string) (fileparse.Result, error) {
	snap, err := a.snapshotFile(key)
	if err != nil {
		return fileparse.Result{Error: err.Error()}, err
	}
	name := snap.Name
	if name == "" {
		name = strings.TrimPrefix(key, "210:name:")
	}
	if ext := strings.ToLower(filepath.Ext(name)); ext == ".cfg" || ext == ".dat" {
		return a.parseComtradeFile(key, snap)
	}
	// TI=125 段落没有文件大小/结束标志时，收到内容即按现有数据解析（兼容旧行为）
	if !snap.Complete && strings.HasPrefix(key, "125:") && len(snap.Data) > 0 {
		snap.Complete = true
	}
	userForced := strings.ToLower(strings.TrimSpace(forced))
	hint := strings.ToLower(strings.TrimSpace(snap.FormatHint))
	effective := userForced
	if effective == "" {
		effective = hint // 文件名后缀优先（soe.xml → xml，xxx.msg → msg）
	}

	// 未完成且用户没有手动指定格式：已知文件大小时等待收满；大小未知时先按当前内容解析，
	// 后续分段/结束确认到达后轮询会继续更新（避免厂家不发结束帧时永远不解析）。
	if !snap.Complete && userForced == "" && strings.HasPrefix(key, "210:") &&
		(snap.Size > 0 || snap.Bytes == 0) {
		res := fileparse.Result{
			Format: effective, FileName: snap.Name,
			Bytes: snap.Bytes, Size: snap.Size, Complete: false,
			Kind: snap.KindHint, KindName: kindDisplay(snap.KindHint),
			Note: "等待文件传输完成",
		}
		switch {
		case snap.Bytes == 0:
			res.Error = "文件尚无内容（等待数据传输帧）"
		case snap.Size > 0:
			res.Error = fmt.Sprintf("文件传输未完成（已收 %d/%d 字节），完成后将自动解析", snap.Bytes, snap.Size)
		default:
			res.Error = fmt.Sprintf("文件传输未完成（已收 %d 字节），等待结束确认后自动解析", snap.Bytes)
		}
		return res, nil
	}

	if len(snap.Data) == 0 {
		return fileparse.Result{
			Format: effective, FileName: snap.Name, Bytes: 0, Size: snap.Size,
			Kind: snap.KindHint, KindName: kindDisplay(snap.KindHint),
			Error: "文件尚无内容（等待数据传输帧）",
		}, nil
	}

	cacheKey := fmt.Sprintf("%s|%s|%d", key, effective, a.dbRev)
	if snap.Complete {
		a.muFile.Lock()
		if r, ok := a.fileCache[cacheKey]; ok {
			a.muFile.Unlock()
			return r, nil
		}
		a.muFile.Unlock()
	}

	res := fileparse.Parse(snap.Data, effective, a.fileNameResolver)
	res.FileName = snap.Name
	res.Bytes = snap.Bytes
	res.Size = snap.Size
	res.Complete = snap.Complete
	if !snap.Complete && strings.TrimSpace(res.Error) != "" {
		// 大小未知时可能先解析到半截内容：不把解析器的半包错误当成最终失败
		res.Error = "传输未完成，当前内容暂无法解析；完成后将自动重试"
	}
	if snap.Complete && snap.Size > 0 && snap.Bytes < snap.Size {
		msg := fmt.Sprintf("传输已结束，但文件长度不符：声明 %d 字节，实际覆盖 %d 字节（可能丢帧/分段缺失）", snap.Size, snap.Bytes)
		if res.Error == "" {
			res.Error = msg
		} else {
			res.Error = msg + "；" + res.Error
		}
	}
	if (res.Kind == "" || res.Kind == "unknown") && snap.KindHint != "" {
		res.Kind = snap.KindHint
		res.KindName = kindDisplay(snap.KindHint)
	}
	if res.Format == "" && effective == "" {
		res.Error = strings.TrimSpace(res.Error + "；未能根据文件名/内容识别格式，请手动选择 MSG 或 XML 格式")
	}
	prefix := fmt.Sprintf("文件 %s（%d 字节）", key, len(snap.Data))
	if snap.Name != "" {
		prefix = fmt.Sprintf("文件 %s（%d 字节）", snap.Name, len(snap.Data))
	}
	if !snap.Complete {
		if snap.Size > 0 {
			prefix += fmt.Sprintf("，传输未完成（已收 %d/%d 字节）", snap.Bytes, snap.Size)
		} else {
			prefix += fmt.Sprintf("，传输未完成（已收 %d 字节）", snap.Bytes)
		}
	}
	if res.Note != "" {
		res.Note = prefix + "，" + res.Note
	} else {
		res.Note = prefix
	}
	if snap.Complete {
		a.muFile.Lock()
		a.fileCache[cacheKey] = res
		a.muFile.Unlock()
	}
	return res, nil
}

// parseComtradeFile 处理 .cfg/.dat：自动配对同名文件，两者都传输完成后绘制波形。
func (a *App) parseComtradeFile(key string, snap fileSnapshot) (fileparse.Result, error) {
	name := snap.Name
	if name == "" {
		name = strings.TrimPrefix(key, "210:name:")
	}
	ext := strings.ToLower(filepath.Ext(name))
	base := strings.TrimSuffix(name, filepath.Ext(name))

	var cfgSnap, datSnap *fileSnapshot
	if ext == ".cfg" {
		cfgSnap = &snap
		if s, ok := a.fileSnapshotByName(base + ".dat"); ok {
			datSnap = &s
		}
	} else {
		datSnap = &snap
		if s, ok := a.fileSnapshotByName(base + ".cfg"); ok {
			cfgSnap = &s
		}
	}
	if cfgSnap == nil {
		return fileparse.Result{
			Kind: "wave", KindName: "波形文件", Format: "dat", FileName: name,
			Bytes: snap.Bytes, Size: snap.Size, Complete: false,
			Error: "未找到同名 .cfg 配置文件，无法还原通道名称、变比和采样率",
		}, nil
	}
	_, res, err := fileparse.ParseComtradeCFG(cfgSnap.Data)
	res.FileName = name
	res.Bytes = cfgSnap.Bytes
	res.Size = cfgSnap.Size
	if err != nil {
		res.Complete = cfgSnap.Complete
		return res, nil
	}
	if datSnap == nil {
		res.Complete = false
		res.Error = "等待同名 .dat 数据文件（传输完成后自动绘制波形）"
		return res, nil
	}
	if !cfgSnap.Complete || !datSnap.Complete {
		res.Complete = false
		res.Bytes = cfgSnap.Bytes + datSnap.Bytes
		res.Size = cfgSnap.Size + datSnap.Size
		res.Error = fmt.Sprintf("等待 .cfg/.dat 传输完成（cfg %d/%d 字节，dat %d/%d 字节）",
			cfgSnap.Bytes, cfgSnap.Size, datSnap.Bytes, datSnap.Size)
		return res, nil
	}
	full, err := fileparse.ParseComtrade(cfgSnap.Data, datSnap.Data)
	full.FileName = name
	full.Bytes = datSnap.Bytes
	full.Size = datSnap.Size
	full.Complete = true
	if err != nil {
		full.Error = err.Error()
	}
	return full, nil
}

func (a *App) fileSnapshotByName(name string) (fileSnapshot, bool) {
	key := a.findFileKey(name)
	if key == "" {
		return fileSnapshot{}, false
	}
	s, err := a.snapshotFile(key)
	if err != nil {
		return fileSnapshot{}, false
	}
	return s, true
}

func (a *App) findFileKey(name string) string {
	a.muFile.Lock()
	defer a.muFile.Unlock()
	if _, ok := a.files["210:name:"+name]; ok {
		return "210:name:" + name
	}
	base := filepath.Base(name)
	for k, acc := range a.files {
		if acc.Name != "" && (strings.EqualFold(acc.Name, name) || strings.EqualFold(filepath.Base(acc.Name), base)) {
			return k
		}
	}
	return ""
}

// fileNameResolver 点表点名解析（按记录类型选表；未命中时依次兜底）。
func (a *App) fileNameResolver(kind string, addr int) string {
	order := []string{"yx", "yc", "yk", "en"}
	switch kind {
	case "control":
		order = []string{"yk", "yx", "yc", "en"}
	case "event":
		order = []string{"yx", "yc", "en", "yk"}
	}
	for _, t := range order {
		if n := a.ptNameFor(t, addr, ""); n != "" {
			return n
		}
	}
	return ""
}

// bumpDBRev 点表变更 → 文件解析缓存失效（下次解析使用新点表点名）。
func (a *App) bumpDBRev() {
	a.muFile.Lock()
	a.dbRev++
	a.fileCache = map[string]fileparse.Result{}
	a.muFile.Unlock()
}

// asInt 兼容 map 中的数值/字符串。
func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case uint32:
		return int(n)
	case uint64:
		return int(n)
	case float64:
		return int(n)
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return 0
		}
		iv := 0
		for _, c := range s {
			if c < '0' || c > '9' {
				return 0
			}
			iv = iv*10 + int(c-'0')
		}
		return iv
	}
	return 0
}
