package common

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

// ── TLV / PI ────────────────────────────────────────────────────────────

var tlvTagNames = map[int]string{
	1: "布尔", 43: "小整形", 32: "无符号小整形",
	33: "短整形", 45: "无符号短整形",
	2: "整形", 35: "无符号整形",
	36: "长整形", 37: "无符号长整形",
	38: "单精度浮点", 39: "双精度浮点",
	4: "字符串/八位位串",
}

// ParseTLVValue 解析 TLV 值（附录D）。长度不足时按 0 容错（与参考实现一致）。
func ParseTLVValue(tag int, data []byte) M {
	t, ok := tlvTagNames[tag]
	if !ok {
		t = "未知"
	}
	unknown := M{"tag": t, "value": HexBytes(data)}
	switch t {
	case "布尔":
		return M{"tag": t, "value": len(data) > 0 && data[0] != 0}
	case "小整形":
		return M{"tag": t, "value": int(int8(byteAt(data, 0)))}
	case "无符号小整形":
		return M{"tag": t, "value": int(byteAt(data, 0))}
	case "短整形":
		v := 0
		if len(data) >= 2 {
			v = int(int16(binary.LittleEndian.Uint16(data)))
		}
		return M{"tag": t, "value": v}
	case "无符号短整形":
		v := 0
		if len(data) >= 2 {
			v = int(binary.LittleEndian.Uint16(data))
		}
		return M{"tag": t, "value": v}
	case "整形":
		v := 0
		if len(data) >= 4 {
			v = int(int32(binary.LittleEndian.Uint32(data)))
		}
		return M{"tag": t, "value": v}
	case "无符号整形":
		var v uint32
		if len(data) >= 4 {
			v = binary.LittleEndian.Uint32(data)
		}
		return M{"tag": t, "value": v}
	case "长整形":
		var v int64
		if len(data) >= 8 {
			v = int64(binary.LittleEndian.Uint64(data))
		}
		return M{"tag": t, "value": v}
	case "无符号长整形":
		var v uint64
		if len(data) >= 8 {
			v = binary.LittleEndian.Uint64(data)
		}
		return M{"tag": t, "value": v}
	case "单精度浮点":
		var v float64
		if len(data) >= 4 {
			v = float64(math.Float32frombits(binary.LittleEndian.Uint32(data)))
		}
		return M{"tag": t, "value": v}
	case "双精度浮点":
		var v float64
		if len(data) >= 8 {
			v = math.Float64frombits(binary.LittleEndian.Uint64(data))
		}
		return M{"tag": t, "value": v}
	case "字符串/八位位串":
		s := asciiString(data)
		if i := strings.IndexByte(s, 0); i >= 0 {
			return M{"tag": "字符串", "value": s[:i]}
		}
		return M{"tag": "八位位串", "value": HexBytes(data)}
	default:
		return unknown
	}
}

func byteAt(b []byte, i int) byte {
	if i < len(b) {
		return b[i]
	}
	return 0
}

// ParsePI 解析参数特征标识 PI（7.9.5）。
func ParsePI(pi int) M {
	return M{
		"raw":     fmt.Sprintf("0x%02X", pi),
		"cont":    pi&0x01 != 0,
		"cr":      pi&0x40 != 0,
		"se":      pi&0x80 != 0,
		"contDesc": boolStr(pi&0x01 != 0, "有后续", "无后续"),
		"crDesc":  boolStr(pi&0x40 != 0, "取消预置", "不取消预置"),
		"seDesc":   boolStr(pi&0x80 != 0, "预置", "固化"),
	}
}

func boolStr(b bool, t, f string) string {
	if b {
		return t
	}
	return f
}

func joinNL(parts []string) string { return strings.Join(parts, "\n") }
