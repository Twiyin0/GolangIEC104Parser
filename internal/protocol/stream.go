package protocol

// Extract104 从字节流按 APCI(68 LL) 切出 APDU（含再同步），返回帧与剩余。
func Extract104(stream []byte) ([][]byte, []byte) {
	var frames [][]byte
	i := 0
	for i < len(stream) {
		if stream[i] != 0x68 {
			i++ // 再同步：跳过错位字节
			continue
		}
		if i+1 >= len(stream) {
			break // 长度字节不完整
		}
		total := int(stream[i+1]) + 2
		if total < 3 || total > 255 {
			i++ // 非法长度，再同步
			continue
		}
		if i+total > len(stream) {
			break // 帧不完整，等待后续字节
		}
		frames = append(frames, stream[i:i+total])
		i += total
	}
	return frames, stream[i:]
}

// Extract101 从字节流按 FT1.2 边界切帧（含再同步），返回帧与剩余。
func Extract101(stream []byte) ([][]byte, []byte) {
	var frames [][]byte
	i := 0
	for i < len(stream) {
		switch stream[i] {
		case 0x10: // 固定帧 10 C A1 A2 CS 16（6 字节）
			if i+6 > len(stream) {
				return frames, stream[i:]
			}
			if stream[i+5] == 0x16 {
				frames = append(frames, stream[i:i+6])
				i += 6
			} else {
				i++
			}
		case 0x68: // 可变帧 68 L L 68 … CS 16
			if i+4 > len(stream) {
				return frames, stream[i:]
			}
			if stream[i+1] != stream[i+2] || stream[i+3] != 0x68 {
				i++
				continue
			}
			total := int(stream[i+1]) + 6
			if total < 9 || total > 261 {
				i++
				continue
			}
			if i+total > len(stream) {
				return frames, stream[i:]
			}
			if stream[i+total-1] == 0x16 {
				frames = append(frames, stream[i:i+total])
				i += total
			} else {
				i++
			}
		default:
			i++
		}
	}
	return frames, nil
}

// ExtractAuto 从字节流自适应切帧：优先识别 101 FT1.2（固定帧 10 … 16、
// 可变帧 68 L L 68 … CS 16，且校验通过），否则按 104 APCI(68 LL) 切分。
// 用于网口 TCP 流：有些现场把 101 帧透传到 TCP，不能一律按 104 截断。
func ExtractAuto(stream []byte) ([][]byte, []byte) {
	var frames [][]byte
	i := 0
	for i < len(stream) {
		if stream[i] == 0x10 { // 101 固定帧
			if i+6 > len(stream) {
				return frames, stream[i:]
			}
			if stream[i+5] == 0x16 {
				frames = append(frames, stream[i:i+6])
				i += 6
				continue
			}
			i++
			continue
		}
		if stream[i] != 0x68 {
			i++
			continue
		}
		// 101 可变帧特征：68 L L 68 …，用长度 + 结尾 16 + 校验确认
		if i+4 <= len(stream) && stream[i+1] == stream[i+2] && stream[i+3] == 0x68 {
			total := int(stream[i+1]) + 6
			if total >= 9 && total <= 261 {
				if i+total > len(stream) {
					return frames, stream[i:] // 等待完整 101 帧
				}
				if stream[i+total-1] == 0x16 && ft12ChecksumOK(stream[i:i+total]) {
					frames = append(frames, stream[i:i+total])
					i += total
					continue
				}
			}
		}
		// 否则按 104 APDU
		if i+1 >= len(stream) {
			break
		}
		total := int(stream[i+1]) + 2
		if total < 3 || total > 255 {
			i++
			continue
		}
		if i+total > len(stream) {
			break
		}
		frames = append(frames, stream[i:i+total])
		i += total
	}
	return frames, stream[i:]
}

// ft12ChecksumOK 校验 FT1.2 可变帧：C..ASDU 逐字节累加 == CS。
func ft12ChecksumOK(frame []byte) bool {
	if len(frame) < 6 {
		return false
	}
	sum := 0
	for i := 4; i < len(frame)-2; i++ {
		sum = (sum + int(frame[i])) & 0xFF
	}
	return sum == int(frame[len(frame)-2])
}

// ParseBest 自适应选择协议解析：优先按 Detect 结果，其次比较两种切帧数量，
// 最后退回 ParseAuto。用于静态解析/导入等“一行完整报文”场景。
func ParseBest(buf []byte) []Frame {
	if len(buf) == 0 {
		return nil
	}
	if chk := CheckFrame(buf); !chk.Complete {
		return nil // 少数据/截断帧不猜测协议，避免误判为 104
	}
	switch Detect(buf) {
	case "101":
		if fs := ParseStream("101", buf); len(fs) > 0 {
			return fs
		}
		// 101 帧不完整时仍按 101 解析（保留旧行为，避免误落到 104）
		if f, err := ParseAuto(buf); err == nil {
			return []Frame{f}
		}
	case "104":
		if fs := ParseStream("104", buf); len(fs) > 0 {
			return fs
		}
		if f, err := ParseAuto(buf); err == nil {
			return []Frame{f}
		}
	}
	f104 := ParseStream("104", buf)
	f101 := ParseStream("101", buf)
	if len(f101) > len(f104) {
		return f101
	}
	if len(f104) > 0 {
		return f104
	}
	if f, err := ParseAuto(buf); err == nil {
		return []Frame{f}
	}
	return nil
}

// ParseStream 按协议切帧并逐条解析（实时抓包/串口/导入共用管线）。
func ParseStream(proto string, stream []byte) []Frame {
	var raws [][]byte
	switch proto {
	case "101":
		raws, _ = Extract101(stream)
	default:
		raws, _ = Extract104(stream)
	}
	out := make([]Frame, 0, len(raws))
	for _, raw := range raws {
		f, err := ParseAuto(raw)
		if err != nil {
			continue
		}
		out = append(out, f)
	}
	return out
}
