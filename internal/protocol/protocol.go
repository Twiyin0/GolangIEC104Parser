// Package protocol 提供顶层解析入口：101/104 自适应识别、字节流切帧、
// 摘要与分类（供列表展示与导出复用）。
package protocol

import (
	"fmt"

	"iec104rt/internal/protocol/common"
	"iec104rt/internal/protocol/iec101"
	"iec104rt/internal/protocol/iec104"
)

// Frame 一条报文的完整解析记录（可直接 JSON 序列化给前端）。
type Frame struct {
	ID       int64          `json:"id"`
	Time     string         `json:"time"` // 捕获时间（工具收到报文的系统时间）
	Dir      string         `json:"dir"`  // "M→T" / "T→M" / ""
	Proto    string         `json:"proto"`
	Hex      string         `json:"hex"`
	Type     string         `json:"type"`
	Ti       int            `json:"ti"`
	TiName   string         `json:"tiName"`
	Summary  string         `json:"summary"`
	Category string         `json:"category"`
	Parsed   map[string]any `json:"parsed"`
}

// Detect 101/104 自适应识别（帧头 + 长度 + 结构 + 校验特征多级判别）。
func Detect(buf []byte) string {
	if len(buf) == 0 {
		return ""
	}
	if buf[0] == 0x10 { // FT1.2 固定帧
		return "101"
	}
	if buf[0] == 0x68 {
		// 101 可变帧特征：68 L L 68 …
		if len(buf) >= 4 && buf[1] == buf[2] && buf[3] == 0x68 {
			if len(buf) == int(buf[1])+2 { // 长度恰好等于 104 APDU 长度时更可能是 104
				return "104"
			}
			total := int(buf[1]) + 6
			if len(buf) >= total { // 完整帧用 FT1.2 校验确认
				csCalc := 0
				for i := 4; i < total-2; i++ {
					csCalc = (csCalc + int(buf[i])) & 0xFF
				}
				if csCalc == int(buf[total-2]) {
					return "101"
				}
			}
			return "101"
		}
		if len(buf) >= 2 {
			return "104"
		}
	}
	return ""
}

// ParseAuto 自适应解析一条完整报文。
func ParseAuto(buf []byte) (Frame, error) {
	proto := Detect(buf)
	f := Frame{Hex: common.HexBytes(buf), Proto: proto}
	switch proto {
	case "104":
		f.Parsed = iec104.ParseAPDU(buf)
	case "101":
		f.Parsed = iec101.ParseFrame101(buf)
	default:
		return f, fmt.Errorf("无法识别的报文格式（起始字节 0x%02X）", buf[0])
	}
	Enrich(&f)
	return f, nil
}

// ParseHexAuto 解析一行 HEX（自动识别协议）。
func ParseHexAuto(hex string) (Frame, error) {
	b, err := iec104.HexToBytes(hex)
	if err != nil {
		return Frame{}, err
	}
	return ParseAuto(b)
}

// Enrich 填充 Frame 的 Type/Ti/TiName/Summary/Category。
func Enrich(f *Frame) {
	p := f.Parsed
	if p == nil {
		return
	}
	if v, ok := p["type"].(string); ok {
		f.Type = v
	}
	if v, ok := p["ti"].(int); ok {
		f.Ti = v
	}
	if v, ok := p["tiName"].(string); ok {
		f.TiName = v
	} else if f.Ti != 0 {
		f.TiName = fmt.Sprintf("TI=%d", f.Ti)
	}
	if f.Proto == "" {
		f.Proto, _ = p["protocol"].(string)
	}
	f.Category, f.Summary = classify(p, f.Proto)
}
