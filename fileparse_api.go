package main

import (
	"fmt"
	"strings"

	"iec104rt/internal/fileparse"
)

// ParseFile 解析已组包的文件内容（forced="" 自动识别；"msg"/"xml" 用户指定）。
// 记录中的点号/地址用当前点表解析点名（SOE记录、故障事件必需）。
func (a *App) ParseFile(key string, forced string) (fileparse.Result, error) {
	data, name, hint, err := a.fileContent(key)
	if err != nil {
		return fileparse.Result{Error: err.Error()}, err
	}
	a.muFile.Lock()
	cacheKey := fmt.Sprintf("%s|%s|%d", key, strings.ToLower(strings.TrimSpace(forced)), a.dbRev)
	if r, ok := a.fileCache[cacheKey]; ok {
		a.muFile.Unlock()
		return r, nil
	}
	a.muFile.Unlock()

	res := fileparse.Parse(data, forced, a.fileNameResolver)
	if (res.Kind == "" || res.Kind == "unknown") && hint != "" {
		res.Kind = hint
		res.KindName = kindDisplay(hint)
	}
	if res.Format == "" && strings.TrimSpace(forced) == "" {
		res.Error = strings.TrimSpace(res.Error + "；自动识别失败，请手动选择 MSG 或 XML 格式")
	}
	prefix := fmt.Sprintf("文件 %s（%d 字节）", key, len(data))
	if name != "" {
		prefix = fmt.Sprintf("文件 %s（%d 字节）", name, len(data))
	}
	if res.Note != "" {
		res.Note = prefix + "，" + res.Note
	} else {
		res.Note = prefix
	}
	a.muFile.Lock()
	a.fileCache[cacheKey] = res
	a.muFile.Unlock()
	return res, nil
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
