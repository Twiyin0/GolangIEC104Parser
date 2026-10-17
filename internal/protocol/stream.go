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
