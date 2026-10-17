package asdu

import (
	"fmt"

	"iec104rt/internal/protocol/common"
)

// ParseYC 遥测 TI=9/11/13（[FIX-03]：TI=9 归一化值除以 32767 得工程值）。
func ParseYC(ioaLen, ti int, r *common.Reader, sq, num int) (M, error) {
	results := []M{}
	isFloat := ti == 13
	isNorm := ti == 9
	valueSize := 2
	if isFloat {
		valueSize = 4
	}
	readValue := func() any {
		if isFloat {
			return r.F32() // 自身推进 4 字节
		}
		raw := r.I16() // 自身推进 2 字节
		if isNorm {
			return M{"raw": raw, "eng": round6(float64(raw) / 32767.0)}
		}
		return raw
	}
	typeLabel := "SVA(标度化)"
	switch {
	case isFloat:
		typeLabel = "float(短浮点)"
	case isNorm:
		typeLabel = "NVA(归一化)"
	}
	if sq == 0 {
		for i := 0; i < num; i++ {
			if r.Left() < ioaLen+valueSize+1 {
				return nil, fmt.Errorf("YC item too short")
			}
			objAddr := readIOA(r, ioaLen)
			val := readValue()
			qds := r.U8()
			results = append(results, M{"slot": i + 1, "addr": objAddr, "value": val, "valueType": typeLabel, "qds": qds})
		}
	} else {
		if r.Left() < ioaLen {
			return nil, fmt.Errorf("YC base addr missing")
		}
		baseAddr := readIOA(r, ioaLen)
		for j := 0; j < num; j++ {
			if r.Left() < valueSize+1 {
				return nil, fmt.Errorf("YC seq item too short")
			}
			val := readValue()
			qds := r.U8()
			results = append(results, M{"slot": j + 1, "addr": baseAddr + j, "value": val, "valueType": typeLabel, "qds": qds})
		}
	}
	return M{"type": "yc", "data": results}, nil
}

// round6 等价 JS 的 toFixed(6)（四舍五入到 6 位小数）。
func round6(f float64) float64 {
	if f < 0 {
		return float64(int64(f*1e6-0.5)) / 1e6
	}
	return float64(int64(f*1e6+0.5)) / 1e6
}

// ParseInitEnd 初始化结束 TI=70（[FIX-08]）。
func ParseInitEnd(ioaLen int, r *common.Reader, cotRaw, addr int) (M, error) {
	if r.Left() < ioaLen+1 {
		return M{"type": "init_end", "cot": cotRaw, "addr": addr, "coi": nil}, nil
	}
	r.Skip(ioaLen)
	coi := r.U8()
	coiMap := map[int]string{0: "当地电源合上", 1: "当地手动复位", 2: "远方复位"}
	coiDesc, ok := coiMap[coi]
	if !ok {
		coiDesc = fmt.Sprintf("COI=%d", coi)
	}
	return M{"type": "init_end", "cot": cotRaw, "addr": addr, "coi": coi, "coiDesc": coiDesc}, nil
}

// ParseEnergyCall 电能量召唤命令 TI=101（[FIX-09]）。
func ParseEnergyCall(ioaLen int, r *common.Reader, cotRaw, addr int) (M, error) {
	if r.Left() < ioaLen+1 {
		return M{"type": "energy_call", "cot": cotRaw, "addr": addr, "qcc": nil}, nil
	}
	r.Skip(ioaLen)
	qcc := r.U8()
	qccDesc := fmt.Sprintf("QCC=%d", qcc)
	if qcc == 5 {
		qccDesc = "总请求电能量"
	}
	return M{"type": "energy_call", "cot": cotRaw, "addr": addr, "qcc": qcc, "qccDesc": qccDesc}, nil
}

// ParseClockSync 时钟同步/读取 TI=103（[FIX-10]）。
func ParseClockSync(ioaLen int, r *common.Reader, cotRaw, addr int) (M, error) {
	if r.Left() < ioaLen {
		return M{"type": "clock_sync", "cot": cotRaw, "addr": addr, "time": nil}, nil
	}
	r.Skip(ioaLen)
	var time any
	if r.Left() >= 7 {
		t, err := r.CP56()
		if err == nil {
			time = t
		}
	}
	cause := cotRaw & 0x3F
	desc := fmt.Sprintf("COT=%d", cotRaw)
	switch cause {
	case 6:
		desc = "时钟同步命令"
	case 7:
		desc = "时钟同步确认"
	case 5:
		desc = "时钟读取"
	}
	return M{"type": "clock_sync", "cot": cotRaw, "addr": addr, "time": time, "desc": desc}, nil
}

// ParseEnergy 累计量（电能量）TI=206/207（[FIX-11][FIX-12]）。
func ParseEnergy(ioaLen, ti int, r *common.Reader, sq, num int) (M, error) {
	results := []M{}
	hasTime := ti == 207
	itemSize := 5
	if hasTime {
		itemSize = 12
	}
	if sq == 0 {
		for i := 0; i < num; i++ {
			if r.Left() < ioaLen+itemSize {
				break
			}
			objAddr := readIOA(r, ioaLen)
			value := r.F32()
			qds := r.U8()
			var time any
			if hasTime && r.Left() >= 7 {
				t, _ := r.CP56()
				time = t
				r.Skip(7)
			}
			results = append(results, M{"slot": i + 1, "addr": objAddr, "value": value, "qds": qds, "time": time})
		}
	} else {
		if r.Left() < ioaLen {
			return nil, fmt.Errorf("Energy base addr missing")
		}
		baseAddr := readIOA(r, ioaLen)
		for j := 0; j < num; j++ {
			if r.Left() < itemSize {
				break
			}
			value := r.F32()
			qds := r.U8()
			var time any
			if hasTime && r.Left() >= 7 {
				t, _ := r.CP56()
				time = t
				r.Skip(7)
			}
			results = append(results, M{"slot": j + 1, "addr": baseAddr + j, "value": value, "qds": qds, "time": time})
		}
	}
	return M{"type": "energy", "hasTime": hasTime, "data": results}, nil
}
