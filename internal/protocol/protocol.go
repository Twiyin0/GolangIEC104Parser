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
			// 只要出现 101 帧头，就按 101 处理；长度不足由 CheckFrame 判定。
			// 不能因为 len==L+2（104 的截断长度）就误判成 104。
			return "101"
		}
		if len(buf) >= 2 {
			return "104"
		}
	}
	return ""
}

// FrameCheck 报文长度完整性检查（用于静态/日志导入时判断是否少数据）。
type FrameCheck struct {
	Protocol string `json:"protocol"`
	Expected int    `json:"expected"`
	Actual   int    `json:"actual"`
	Complete bool   `json:"complete"`
	Reason   string `json:"reason"`
}

// CheckFrame 通过帧头/L 判断报文是否完整：
//   - 101 固定帧：10 C A1 A2 CS 16，固定 6 字节；
//   - 101 可变帧：68 L L 68 … CS 16，总长 L+6；
//   - 104 APDU：68 LL …，总长 LL+2。
//
// 101 帧头优先于 104，避免 101 截断帧（len==L+2）被误判为 104。
func CheckFrame(buf []byte) FrameCheck {
	if len(buf) == 0 {
		return FrameCheck{Complete: false, Reason: "空报文"}
	}
	if buf[0] == 0x10 {
		exp := 6
		if len(buf) < exp {
			return FrameCheck{Protocol: "101", Expected: exp, Actual: len(buf), Complete: false,
				Reason: fmt.Sprintf("101 固定帧不完整：需要 %d 字节，实际 %d 字节", exp, len(buf))}
		}
		if buf[exp-1] != 0x16 {
			return FrameCheck{Protocol: "101", Expected: exp, Actual: len(buf), Complete: false,
				Reason: "101 固定帧结束符不是 16H"}
		}
		return FrameCheck{Protocol: "101", Expected: exp, Actual: len(buf), Complete: true}
	}
	if buf[0] == 0x68 {
		if len(buf) >= 4 && buf[1] == buf[2] && buf[3] == 0x68 {
			exp := int(buf[1]) + 6
			if len(buf) < exp {
				return FrameCheck{Protocol: "101", Expected: exp, Actual: len(buf), Complete: false,
					Reason: fmt.Sprintf("101 可变帧不完整：L=%d(0x%02X)，需要 %d 字节，实际 %d 字节",
						buf[1], buf[1], exp, len(buf))}
			}
			if buf[exp-1] != 0x16 {
				return FrameCheck{Protocol: "101", Expected: exp, Actual: len(buf), Complete: false,
					Reason: "101 可变帧结束符不是 16H（可能被截断）"}
			}
			return FrameCheck{Protocol: "101", Expected: exp, Actual: len(buf), Complete: true}
		}
		if len(buf) < 2 {
			return FrameCheck{Protocol: "104", Expected: 0, Actual: len(buf), Complete: false, Reason: "104 长度字节缺失"}
		}
		exp := int(buf[1]) + 2
		if len(buf) < exp {
			return FrameCheck{Protocol: "104", Expected: exp, Actual: len(buf), Complete: false,
				Reason: fmt.Sprintf("104 APDU 不完整：L=%d，需要 %d 字节，实际 %d 字节", buf[1], exp, len(buf))}
		}
		return FrameCheck{Protocol: "104", Expected: exp, Actual: len(buf), Complete: true}
	}
	return FrameCheck{Complete: false, Reason: fmt.Sprintf("无法识别的帧起始字节 0x%02X", buf[0])}
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
