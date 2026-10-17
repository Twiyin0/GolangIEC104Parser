// Package asdu 实现 104/101 共用的 ASDU 信息对象解析。
// ioaLen 参数化信息对象地址长度：104 为 3 字节，101 为 2 字节（[D-02]）。
// 语义移植自参考实现，保留 FIX-01~13 / D-02~06 的修复语义。
package asdu

import (
	"fmt"

	"iec104rt/internal/protocol/common"
)

// M 结果载体别名。
type M = common.M

func readIOA(r *common.Reader, ioaLen int) int {
	if ioaLen == 2 {
		return r.U16()
	}
	return r.U24()
}

func addrHex(v, ioaLen int) string { return fmt.Sprintf("0x%0*X", ioaLen*2, v) }

func strPtr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ParseTotalCall 总召唤 TI=100（[FIX-01]：先跳过 IOA 再读 QOI）。
func ParseTotalCall(ioaLen int, r *common.Reader, ti, cotRaw, addr int) (M, error) {
	if r.Left() < ioaLen+1 {
		return nil, fmt.Errorf("Total call ASDU too short")
	}
	r.Skip(ioaLen)
	qoi := r.U8()
	res := M{"type": "total_call", "cot": cotRaw, "addr": addr}
	if qoi == 20 {
		res["command"] = "total"
	} else {
		res["command"] = "group"
		res["group"] = qoi
	}
	return res, nil
}

// ParseYX 遥信 TI=1/3。
func ParseYX(ioaLen, ti int, r *common.Reader, sq, num int) (M, error) {
	results := []M{}
	isSingle := ti == 1
	dpState := func(val int) string {
		if isSingle {
			if val&0x01 != 0 {
				return "合(1)"
			}
			return "分(0)"
		}
		return common.DPState(val)
	}
	if sq == 0 {
		for i := 0; i < num; i++ {
			if r.Left() < ioaLen+1 {
				return nil, fmt.Errorf("YX item too short")
			}
			objAddr := readIOA(r, ioaLen)
			val := r.U8()
			results = append(results, M{"slot": i + 1, "addr": objAddr, "addrDec": objAddr,
				"state": dpState(val), "qds": val >> boolToInt(isSingle, 1, 2), "invalid": (val >> boolToInt(isSingle, 1, 2) & 0x10) != 0})
		}
	} else {
		if r.Left() < ioaLen {
			return nil, fmt.Errorf("YX base addr missing")
		}
		baseAddr := readIOA(r, ioaLen)
		for j := 0; j < num; j++ {
			if r.Left() < 1 {
				return nil, fmt.Errorf("YX seq item too short")
			}
			val := r.U8()
			results = append(results, M{"slot": j + 1, "addr": baseAddr + j, "addrDec": baseAddr + j, "state": dpState(val)})
		}
	}
	return M{"type": "yx", "data": results}, nil
}

func boolToInt(b bool, t, f int) int {
	if b {
		return t
	}
	return f
}

// ParseSOE 带时标遥信 TI=30/31。
func ParseSOE(ioaLen, ti int, r *common.Reader, sq, num int) (M, error) {
	results := []M{}
	isSingle := ti == 30
	dpState := func(val int) string {
		if isSingle {
			if val&0x01 != 0 {
				return "合(1)"
			}
			return "分(0)"
		}
		return common.DPState(val)
	}
	if sq == 0 {
		for i := 0; i < num; i++ {
			if r.Left() < ioaLen+8 {
				return nil, fmt.Errorf("SOE item too short")
			}
			objAddr := readIOA(r, ioaLen)
			val := r.U8()
			timeStr, err := r.CP56()
			if err != nil {
				return nil, err
			}
			r.Skip(7)
			results = append(results, M{"slot": i + 1, "addr": objAddr, "addrDec": objAddr, "state": dpState(val), "time": timeStr})
		}
	} else {
		if r.Left() < ioaLen {
			return nil, fmt.Errorf("SOE base addr missing")
		}
		baseAddr := readIOA(r, ioaLen)
		for j := 0; j < num; j++ {
			if r.Left() < 8 {
				return nil, fmt.Errorf("SOE seq item too short")
			}
			val := r.U8()
			timeStr, err := r.CP56()
			if err != nil {
				return nil, err
			}
			r.Skip(7)
			results = append(results, M{"slot": j + 1, "addr": baseAddr + j, "addrDec": baseAddr + j, "state": dpState(val), "time": timeStr})
		}
	}
	return M{"type": "soe", "data": results}, nil
}

// ParseBitstring 32 位位串 TI=5（带 CP24 时标 TI=7）。
func ParseBitstring(ioaLen, ti int, r *common.Reader, sq, num int) (M, error) {
	results := []M{}
	hasTime := ti == 7
	parseOne := func(objAddr int) (M, error) {
		value := r.U32()
		var time any
		if hasTime {
			t, err := common.ParseCP24Time2a(r.B, r.Off)
			if err != nil {
				return nil, err
			}
			time = t
			r.Skip(3)
		} else {
			time = nil
		}
		return M{"addr": objAddr, "value": value,
			"valueHex": fmt.Sprintf("0x%08X", value),
			"bits":     fmt.Sprintf("%032b", value), "time": time}, nil
	}
	if sq == 0 {
		for i := 0; i < num; i++ {
			need := ioaLen + 4
			if hasTime {
				need += 3
			}
			if r.Left() < need {
				return nil, fmt.Errorf("Bitstring item too short")
			}
			objAddr := readIOA(r, ioaLen)
			one, err := parseOne(objAddr)
			if err != nil {
				return nil, err
			}
			one["slot"] = i + 1
			results = append(results, one)
		}
	} else {
		if r.Left() < ioaLen {
			return nil, fmt.Errorf("Bitstring base addr missing")
		}
		baseAddr := readIOA(r, ioaLen)
		for j := 0; j < num; j++ {
			need := 4
			if hasTime {
				need += 3
			}
			if r.Left() < need {
				return nil, fmt.Errorf("Bitstring seq item too short")
			}
			one, err := parseOne(baseAddr + j)
			if err != nil {
				return nil, err
			}
			one["slot"] = j + 1
			results = append(results, one)
		}
	}
	return M{"type": "bitstring", "hasTime": hasTime, "data": results}, nil
}
