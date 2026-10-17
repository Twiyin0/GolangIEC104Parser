package asdu

import (
	"fmt"

	"iec104rt/internal/protocol/common"
)

// ParseFault 故障事件 TI=42（[FIX-02]：移除错误的保留字节跳过，遥测偏移正确）。
func ParseFault(ioaLen int, r *common.Reader, cotRaw, addr int) M {
	result := M{"type": "fault", "cot": cotRaw, "addr": addr, "yx": []M{}, "yc": []M{}}
	if r.Left() < 1 {
		return result
	}
	yxCount := r.U8()
	// 遥信：类型(1)+点号(2)+值(1)+时标(7) = 11字节/条
	for i := 0; i < yxCount; i++ {
		if r.Left() < 11 {
			break
		}
		yxType := r.U8()
		yxPoint := r.U16()
		yxVal := r.U8()
		yxTime, err := r.CP56()
		if err != nil {
			break
		}
		r.Skip(7)
		var state string
		switch yxType {
		case 1, 0x1e:
			if yxVal&0x01 != 0 {
				state = "合(1)"
			} else {
				state = "分(0)"
			}
		case 3, 0x1f:
			state = common.DPState(yxVal)
		default:
			state = fmt.Sprintf("0x%02X", yxVal)
		}
		result["yx"] = append(result["yx"].([]M), M{"type": yxType, "typeHex": fmt.Sprintf("0x%02X", yxType),
			"point": yxPoint, "pointHex": fmt.Sprintf("0x%04X", yxPoint),
			"state": state, "time": yxTime, "stateHex": fmt.Sprintf("0x%02X", yxVal)})
	}
	// 遥信结束后跳过1字节保留/扩展字段
	if r.Left() < 3 {
		return result
	}
	r.Skip(1)
	ycCount := r.U8()
	ycType := r.U8()
	result["ycCount"] = ycCount
	result["ycType"] = ycType
	result["ycTypeHex"] = fmt.Sprintf("0x%02X", ycType)
	valueSize := 4
	isNorm := false
	if ycType == 9 || ycType == 11 {
		valueSize = 2
		isNorm = ycType == 9
	}
	valueTypeMap := map[int]string{0x0d: "float", 0x09: "NVA(归一化)", 0x0b: "SVA(标度化)"}
	valueType, ok := valueTypeMap[ycType]
	if !ok {
		valueType = fmt.Sprintf("type=0x%02X", ycType)
	}
	for i := 0; i < ycCount; i++ {
		if r.Left() < ioaLen+valueSize {
			break
		}
		ycAddr := readIOA(r, ioaLen)
		var eng float64
		var raw any
		if valueSize == 4 {
			eng = r.F32()
		} else {
			rawV := r.I16()
			raw = rawV
			if isNorm {
				eng = round6(float64(rawV) / 32767.0)
			} else {
				eng = float64(rawV)
			}
		}
		result["yc"] = append(result["yc"].([]M), M{"addr": ycAddr, "hexAddr": addrHex(ycAddr, ioaLen),
			"value": eng, "rawValue": raw, "valueType": valueType})
	}
	result["description"] = fmt.Sprintf("故障事件: %d个遥信, %d个遥测",
		len(result["yx"].([]M)), len(result["yc"].([]M)))
	return result
}
