package iec101

import (
	"fmt"
	"strings"

	"iec104rt/internal/protocol/asdu"
	"iec104rt/internal/protocol/common"
	"iec104rt/internal/protocol/iec104"
)

// ParseASdu 解析 ASDU（IOA=2 字节，[D-02]），附加链路信息。
func ParseASdu(asduBuf []byte, linkAddr int, ctrl M) M {
	if len(asduBuf) < 6 {
		return M{"type": "error", "error": "ASDU too short", "linkInfo": linkInfo(linkAddr, ctrl), "protocol": "101"}
	}
	ti := int(asduBuf[0])
	vsq := int(asduBuf[1])
	cotRaw := int(asduBuf[2]) | int(asduBuf[3])<<8
	addr := int(asduBuf[4]) | int(asduBuf[5])<<8
	cot := cotRaw & 0x3F
	pn := (cotRaw >> 6) & 0x01
	test := (cotRaw >> 7) & 0x01
	cotDesc := common.COTName(cot)
	sq := vsq >> 7
	num := vsq & 0x7F

	r := common.NewReader(asduBuf, 6)
	var result M
	var err error
	switch ti {
	case 1, 3:
		result, err = asdu.ParseYX(ioaLen, ti, r, sq, num)
	case 5, 7:
		result, err = asdu.ParseBitstring(ioaLen, ti, r, sq, num)
	case 9, 11, 13:
		result, err = asdu.ParseYC(ioaLen, ti, r, sq, num)
	case 30, 31:
		result, err = asdu.ParseSOE(ioaLen, ti, r, sq, num)
	case 42:
		result = asdu.ParseFault(ioaLen, r, cotRaw, addr)
	case 45, 46:
		result, err = asdu.ParseRemoteControl(ioaLen, ti, r, cotRaw, sq, num, pn != 0)
	case 70:
		result, err = asdu.ParseInitEnd(ioaLen, r, cotRaw, addr)
	case 100:
		result, err = asdu.ParseTotalCall(ioaLen, r, ti, cotRaw, addr)
	case 101:
		result, err = asdu.ParseEnergyCall(ioaLen, r, cotRaw, addr)
	case 103:
		result, err = asdu.ParseClockSync(ioaLen, r, cotRaw, addr)
	case 104:
		result, err = asdu.ParseTestCmd(ioaLen, r, cotRaw, addr)
	case 105:
		result, err = asdu.ParseResetProcess(ioaLen, r, cotRaw, addr)
	case 200, 201, 202, 203:
		result, err = asdu.ParseParamCommand(ioaLen, ti, r, cotRaw, sq, num)
	case 206, 207:
		result, err = asdu.ParseEnergy(ioaLen, ti, r, sq, num)
	case 210:
		result, err = asdu.ParseFileService210(3, r, cotRaw, addr) // 结构同104（D-05）
	default:
		result = M{"type": "unknown", "ti": ti, "tiHex": fmt.Sprintf("0x%02X", ti),
			"cot": cotRaw, "cotDesc": cotDesc, "addr": addr}
	}
	if err != nil {
		return M{"type": "error", "error": err.Error(), "ti": ti, "cot": cotRaw,
			"linkInfo": linkInfo(linkAddr, ctrl), "protocol": "101"}
	}
	if result == nil {
		result = M{"type": "unknown", "ti": ti, "cot": cotRaw, "cotDesc": cotDesc, "addr": addr}
	}
	result["pn"] = pn
	result["test"] = test
	result["cotDesc"] = cotDesc
	result["ti"] = ti
	result["tiName"] = TIName(ti)
	result["linkInfo"] = linkInfo(linkAddr, ctrl)
	result["protocol"] = "101"
	return result
}

func linkInfo(linkAddr int, ctrl M) M {
	if ctrl == nil {
		return M{"linkAddr": linkAddr}
	}
	info := M{"linkAddr": linkAddr}
	for _, k := range []string{"prm", "fcName", "direction", "acd", "dfc", "fc", "fcb", "fcv"} {
		if v, ok := ctrl[k]; ok {
			info[k] = v
		}
	}
	return info
}

// ParseFrame101 解析一帧完整 101 报文（含链路层），返回完整解析结果。
func ParseFrame101(buf []byte) M {
	frame := ParseFrame(buf)
	if frame.FrameType == "error" {
		return M{"frameType": "error", "error": frame.Err, "protocol": "101"}
	}
	// 固定帧：无 ASDU，仅链路层信息
	if frame.FrameType == "fixed" {
		out := M{"type": "link_frame", "protocol": "101", "frameType": "fixed",
			"linkAddr": frame.LinkAddr, "ctrl": frame.Ctrl, "csValid": frame.CSValid}
		for _, k := range []string{"fcName", "prm", "acd", "dfc", "fc", "direction", "fcb", "fcv"} {
			if v, ok := frame.Ctrl[k]; ok {
				out[k] = v
			}
		}
		out["tiName"] = "链路帧(FT1.2固定帧)"
		return out
	}
	// 可变帧无 ASDU
	if len(frame.AsduBuf) == 0 {
		out := M{"type": "link_frame", "protocol": "101", "frameType": "variable_noasdu",
			"linkAddr": frame.LinkAddr, "ctrl": frame.Ctrl, "csValid": frame.CSValid,
			"tiName": "链路帧(FT1.2可变帧)"}
		return out
	}
	asduResult := ParseASdu(frame.AsduBuf, frame.LinkAddr, frame.Ctrl)
	asduResult["frameType"] = "variable"
	asduResult["csValid"] = frame.CSValid
	asduResult["protocol"] = "101"
	return asduResult
}

// ParseBuffer 在字节流中顺序解析多帧 101 报文（FT1.2 帧边界扫描）。
func ParseBuffer(buf []byte) []M {
	var out []M
	offset := 0
	for offset < len(buf) {
		chunk := buf[offset:]
		frame := ParseFrame(chunk)
		if frame.FrameType == "error" {
			out = append(out, M{"frameType": "error", "error": frame.Err, "protocol": "101"})
			break
		}
		if frame.FrameLength <= 0 || frame.FrameLength > len(chunk) {
			out = append(out, M{"type": "error", "error": "Invalid 101 frame length", "protocol": "101"})
			break
		}
		out = append(out, ParseFrame101(chunk[:frame.FrameLength]))
		offset += frame.FrameLength
	}
	return out
}

// ParseLines 按行解析（每行一条或多帧 FT1.2）。
func ParseLines(hex string) []M {
	items := strings.FieldsFunc(hex, func(r rune) bool { return r == '\n' || r == '\r' })
	var out []M
	for _, it := range items {
		if strings.TrimSpace(it) == "" {
			continue
		}
		b, err := iec104.HexToBytes(it)
		if err != nil {
			continue
		}
		out = append(out, ParseBuffer(b)...)
	}
	return out
}
