// Package common 提供 101/104 协议解析共用工具：
// 常量表（COT/QOC/双点状态/星期）、CP56Time2a/CP24Time2a 时标解析。
//
// 语义 1:1 移植自 example/VUE104Parser/src_parsers，保留其全部修复语义
//（FIX-01~13、D-01~06），差异处以规约 PDF 为准并注明。
package common

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// M 解析结果的通用载体（与参考项目 TS 版 frame 对象字段一一对应，可直接 JSON 序列化）。
type M map[string]any

// ── 常量表 ──────────────────────────────────────────────────────────────

var dowNames = [...]string{"", "一", "二", "三", "四", "五", "六", "日"}

var dpStates = map[int]string{0: "中间态", 1: "分", 2: "合", 3: "故障"}

var qocNames = map[int]string{0: "无附加定义", 1: "短脉冲", 2: "长脉冲", 3: "持续输出"}

var cotNames = map[int]string{
	1: "per/cyc", 2: "back", 3: "spont", 4: "init", 5: "req",
	6: "act", 7: "actcon", 8: "deact", 9: "deactcon", 10: "actterm",
	13: "file", 20: "introgen", 37: "reqcogen",
	44: "未知TI", 45: "未知COT", 46: "未知公共地址", 47: "未知IOA",
	48: "软压板状态错", 49: "时间戳错", 50: "数字签名错",
}

// DPState 双点状态文字（FIX-04）。0/1/2/3 → 中间态/分/合/故障。
func DPState(v int) string {
	if s, ok := dpStates[v&0x03]; ok {
		return s
	}
	return fmt.Sprintf("%d", v&0x03)
}

// QOCName 命令限定词文字。
func QOCName(v int) string {
	if s, ok := qocNames[v]; ok {
		return s
	}
	return fmt.Sprintf("QOC=%d", v)
}

// COTName 传送原因文字（带兜底）。
func COTName(v int) string {
	if s, ok := cotNames[v]; ok {
		return s
	}
	return fmt.Sprintf("COT=%d", v)
}

// ── 时标解析 ────────────────────────────────────────────────────────────

// ParseCP56Time2a 解析 7 字节二进制时标（FIX-05：含 DOW 星期位）。
func ParseCP56Time2a(b []byte, off int) (string, error) {
	if len(b) < off+7 {
		return "", fmt.Errorf("Insufficient buffer for CP56Time2a")
	}
	msRaw := int(binary.LittleEndian.Uint16(b[off:]))
	minB, hourB, dayB, monB, yearB := b[off+2], b[off+3], b[off+4], b[off+5], b[off+6]
	ms := msRaw % 1000
	sec := msRaw / 1000
	min := int(minB & 0x3F)
	hour := int(hourB & 0x1F)
	dow := int((hourB >> 5) & 0x07)
	day := int(dayB & 0x1F)
	month := int(monB & 0x0F)
	year := int(yearB&0x7F) + 2000
	dowStr := ""
	if dow >= 1 && dow <= 7 {
		dowStr = "(周" + dowNames[dow] + ")"
	}
	return fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d.%03d%s",
		year, month, day, hour, min, sec, ms, dowStr), nil
}

// ParseCP24Time2a 解析 3 字节二进制时标（分:秒.毫秒）。
func ParseCP24Time2a(b []byte, off int) (string, error) {
	if len(b) < off+3 {
		return "", fmt.Errorf("Insufficient buffer for CP24Time2a")
	}
	msRaw := int(binary.LittleEndian.Uint16(b[off:]))
	min := int(b[off+2] & 0x3F)
	return fmt.Sprintf("%02d:%02d.%03d", min, msRaw/1000, msRaw%1000), nil
}

// ── 工具函数 ────────────────────────────────────────────────────────────

// Sum8 模 256 求和（FT1.2 帧校验 CS）。
func Sum8(b []byte) int {
	s := 0
	for _, v := range b {
		s = (s + int(v)) & 0xFF
	}
	return s
}

func asciiString(b []byte) string {
	var sb []byte
	for _, c := range b {
		if c >= 0x20 && c < 0x7F {
			sb = append(sb, c)
		} else {
			sb = append(sb, '.')
		}
	}
	return string(sb)
}

func hexEncode(b []byte) string {
	const hexd = "0123456789ABCDEF"
	sb := make([]byte, 0, len(b)*3)
	for i, v := range b {
		if i > 0 {
			sb = append(sb, ' ')
		}
		sb = append(sb, hexd[v>>4], hexd[v&0x0F])
	}
	return string(sb)
}

// HexBytes 将字节序列渲染为空格分隔的大写 HEX。
func HexBytes(b []byte) string { return hexEncode(b) }

// NormHexShow 规范化十六进制显示：信息体地址为 3 字节，高位为 0 时不显示高位
//（0x006401 → 0x6401、0x004001 → 0x4001、0x000001 → 0x1）；非 "0x+十六进制" 形式原样返回。
func NormHexShow(s string) string {
	t := strings.TrimSpace(s)
	if len(t) < 3 || t[0] != '0' || (t[1] != 'x' && t[1] != 'X') {
		return s
	}
	digits := t[2:]
	for i := 0; i < len(digits); i++ {
		c := digits[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return s
		}
	}
	d := strings.TrimLeft(digits, "0")
	if d == "" {
		d = "0"
	}
	return "0x" + strings.ToUpper(d)
}

// HexWrap 每 64 字符换行（TI=125 文件段落数据展示，与参考实现一致）。
func HexWrap(s string) string {
	if len(s) <= 64 {
		return s
	}
	var parts []string
	for i := 0; i < len(s); i += 64 {
		end := i + 64
		if end > len(s) {
			end = len(s)
		}
		parts = append(parts, s[i:end])
	}
	return joinNL(parts)
}
