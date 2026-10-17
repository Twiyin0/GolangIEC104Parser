package asdu

import (
	"fmt"

	"iec104rt/internal/protocol/common"
)

// ParseParamCommand 定值参数读写 TI=200~203（[FIX-13] 读响应结构修正：PI 在首位统一读取）。
func ParseParamCommand(ioaLen, ti int, r *common.Reader, cotRaw, sq, num int) (M, error) {
	results := []M{}
	cause := cotRaw & 0x3F
	// push 追加结果并自动编号（渲染/导出显示序号，避免 undefined）
	push := func(m M) {
		m["slot"] = len(results) + 1
		results = append(results, m)
	}

	// TI=200 切换定值区
	if ti == 200 {
		if r.Left() < ioaLen+2 {
			return nil, fmt.Errorf("Missing object address or SN (TI=200)")
		}
		r.Skip(ioaLen)
		sn := r.U16()
		opMap := map[int]string{6: "切换请求", 7: "切换确认", 8: "切换撤销", 9: "切换撤销确认"}
		op, ok := opMap[cause]
		if !ok {
			op = fmt.Sprintf("COT=%d", cotRaw)
		}
		push(M{"type": "Para_switch", "operation": op, "sn": sn})
		return M{"type": "param", "data": results}, nil
	}

	// TI=201 读定值区号
	if ti == 201 {
		if r.Left() < ioaLen {
			return nil, fmt.Errorf("Missing IOA (TI=201)")
		}
		objAddr := readIOA(r, ioaLen)
		if cause == 7 && r.Left() >= 6 {
			current := r.U16()
			min := r.U16()
			max := r.U16()
			push(M{"type": "SNNum", "operation": "读区号响应", "current": current, "min": min, "max": max})
		} else {
			opMap := map[int]string{6: "读区号请求", 8: "读区号撤销", 9: "读区号撤销确认"}
			op, ok := opMap[cause]
			if !ok {
				op = fmt.Sprintf("COT=%d", cotRaw)
			}
			push(M{"type": "SNNum", "operation": op, "addr": addrHex(objAddr, ioaLen)})
		}
		return M{"type": "param", "data": results}, nil
	}

	// TI=202 读参数和定值
	if ti == 202 {
		return parseParamRead202(ioaLen, r, cotRaw, num, &results)
	}

	// TI=203 写参数和定值
	if ti == 203 {
		return parseParamWrite203(ioaLen, r, cotRaw, num, &results)
	}
	return M{"type": "param", "data": results}, nil
}

func parseParamRead202(ioaLen int, r *common.Reader, cotRaw, num int, out *[]M) (M, error) {
	push := func(m M) { // 自动编号
		m["slot"] = len(*out) + 1
		*out = append(*out, m)
	}
	cause := cotRaw & 0x3F
	if r.Left() < 2 {
		return nil, fmt.Errorf("Missing SN (TI=202)")
	}
	sn := r.U16()
	switch cause {
	case 6: // 读请求：SN(2) + N个地址
		addrs := []string{}
		for i := 0; i < num; i++ {
			if r.Left() < ioaLen {
				break
			}
			addrs = append(addrs, addrHex(readIOA(r, ioaLen), ioaLen))
		}
		push(M{"type": "Para_read", "operation": "读请求", "sn": sn, "addrs": addrs})
	case 7: // [FIX-13] 读响应：SN(2) + PI(1) + [地址+Tag+Len+Value]×N
		if r.Left() < 1 {
			push(M{"type": "Para_read", "operation": "读响应(数据不足)", "sn": sn})
			return M{"type": "param", "data": *out}, nil
		}
		pi := r.U8()
		piFlags := common.ParsePI(pi)
		isFirst := true
		pushed := 0
		for r.Left() >= 5 {
			a := readIOA(r, ioaLen)
			tag := r.U8()
			length := r.U8()
			if r.Left() < length {
				break
			}
			vBuf := r.Slice(length)
			parsed := common.ParseTLVValue(tag, vBuf)
			item := M{"type": "Para_read", "operation": "读响应", "sn": sn,
				"addr": addrHex(a, ioaLen), "tag": parsed["tag"], "value": parsed["value"]}
			if isFirst {
				item["pi"] = pi
				item["piFlags"] = piFlags
			}
			push(item)
			pushed++
			isFirst = false
		}
		if pushed == 0 {
			push(M{"type": "Para_read", "operation": "读响应(空)", "sn": sn, "pi": pi, "piFlags": piFlags})
		}
	default:
		opMap := map[int]string{8: "读撤销", 9: "读撤销确认"}
		op, ok := opMap[cause]
		if !ok {
			op = fmt.Sprintf("COT=%d", cotRaw)
		}
		push(M{"type": "Para_read", "operation": op, "sn": sn})
	}
	return M{"type": "param", "data": *out}, nil
}

func parseParamWrite203(ioaLen int, r *common.Reader, cotRaw, num int, out *[]M) (M, error) {
	push := func(m M) { // 自动编号
		m["slot"] = len(*out) + 1
		*out = append(*out, m)
	}
	cause := cotRaw & 0x3F
	if r.Left() < 2 {
		return nil, fmt.Errorf("Missing SN (TI=203)")
	}
	sn := r.U16()
	switch cause {
	case 6: // 写预置：SN(2) + PI(1) + N×[地址+Tag+Len+Value]
		if r.Left() < 1 {
			push(M{"type": "Para_write", "operation": "写预置(PI缺失)", "sn": sn})
			break
		}
		pi := r.U8()
		piFlags := common.ParsePI(pi)
		for i := 0; i < num; i++ {
			if r.Left() < 5 {
				break
			}
			a := readIOA(r, ioaLen)
			tag := r.U8()
			length := r.U8()
			n := length
			if r.Left() < n {
				n = r.Left()
			}
			vBuf := r.Slice(n)
			parsed := common.ParseTLVValue(tag, vBuf)
			push(M{"type": "Para_write", "operation": "写预置", "sn": sn, "pi": pi, "piFlags": piFlags,
				"addr": addrHex(a, ioaLen), "tag": parsed["tag"], "value": parsed["value"]})
		}
	case 7: // 写确认（可带回显）
		var pi any
		var piFlags any
		if r.Left() >= 1 {
			pv := r.U8()
			pi = pv
			piFlags = common.ParsePI(pv)
		}
		values := []M{}
		for r.Left() >= 5 {
			a := readIOA(r, ioaLen)
			tag := r.U8()
			length := r.U8()
			if r.Left() < length {
				break
			}
			vBuf := r.Slice(length)
			parsed := common.ParseTLVValue(tag, vBuf)
			values = append(values, M{"addr": addrHex(a, ioaLen), "tag": parsed["tag"], "value": parsed["value"]})
		}
		operation := "写确认"
		if len(values) > 0 {
			operation = "写确认(带回显)"
		}
		push(M{"type": "Para_write", "operation": operation, "sn": sn, "pi": pi, "piFlags": piFlags, "values": values})
	default:
		opMap := map[int]string{8: "撤销", 9: "撤销确认", 10: "激活终止"}
		op, ok := opMap[cause]
		if !ok {
			op = fmt.Sprintf("COT=%d", cotRaw)
		}
		var pi any
		var piFlags any
		if r.Left() >= 1 {
			pv := r.U8()
			pi = pv
			piFlags = common.ParsePI(pv)
		}
		push(M{"type": "Para_write", "operation": op, "sn": sn, "pi": pi, "piFlags": piFlags})
	}
	return M{"type": "param", "data": *out}, nil
}