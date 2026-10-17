package asdu

import (
	"fmt"

	"iec104rt/internal/protocol/common"
)

// ParseTestCmd 测试命令 TI=104（[D-04]，101 特有）。
func ParseTestCmd(ioaLen int, r *common.Reader, cotRaw, addr int) (M, error) {
	if r.Left() < ioaLen+2 {
		return M{"type": "test_cmd", "cot": cotRaw, "addr": addr, "fbp": nil}, nil
	}
	r.Skip(ioaLen)
	fbp := r.U16()
	cause := cotRaw & 0x3F
	desc := fmt.Sprintf("COT=%d", cotRaw)
	switch cause {
	case 6:
		desc = "测试命令"
	case 7:
		desc = "测试命令确认"
	}
	return M{"type": "test_cmd", "cot": cotRaw, "addr": addr, "fbp": fbp,
		"fbpHex": fmt.Sprintf("0x%04X", fbp), "fbpValid": fbp == 0x55AA, "desc": desc}, nil
}

// ParseResetProcess 复位进程命令 TI=105（[D-04]，101 特有）。
func ParseResetProcess(ioaLen int, r *common.Reader, cotRaw, addr int) (M, error) {
	if r.Left() < ioaLen+1 {
		return M{"type": "reset_process", "cot": cotRaw, "addr": addr, "qrp": nil}, nil
	}
	r.Skip(ioaLen)
	qrp := r.U8()
	cause := cotRaw & 0x3F
	desc := fmt.Sprintf("COT=%d", cotRaw)
	switch cause {
	case 6:
		desc = "复位进程命令"
	case 7:
		desc = "复位进程确认"
	}
	qrpDesc := fmt.Sprintf("QRP=%d", qrp)
	if qrp == 1 {
		qrpDesc = "进程的总复位"
	}
	return M{"type": "reset_process", "cot": cotRaw, "addr": addr, "qrp": qrp, "qrpDesc": qrpDesc, "desc": desc}, nil
}

// ParseRemoteControl 遥控 TI=45/46（[FIX-07]：QOC 解析、拼写修正、P/N 否定标注）。
func ParseRemoteControl(ioaLen, ti int, r *common.Reader, cotRaw, sq, num int, pn bool) (M, error) {
	results := []M{}
	isSingle := ti == 45
	pointType := "double(双点)"
	if isSingle {
		pointType = "single(单点)"
	}
	cause := cotRaw & 0x3F
	parseOne := func(objAddr, rawByte int) M {
		se := (rawByte >> 7) & 0x01
		qoc := (rawByte >> 2) & 0x1F
		var cmdState string
		if isSingle {
			if rawByte&0x01 != 0 {
				cmdState = "控合"
			} else {
				cmdState = "控分"
			}
		} else {
			switch rawByte & 0x03 {
			case 1:
				cmdState = "控分"
			case 2:
				cmdState = "控合"
			default:
				cmdState = "不确定"
			}
		}
		var operation string
		switch cause {
		case 6:
			if se != 0 {
				operation = "选择"
			} else {
				operation = "执行"
			}
		case 7:
			if se != 0 {
				operation = "选择确认"
			} else {
				operation = "执行确认"
			}
		case 8:
			operation = "撤销"
		case 9:
			operation = "撤销确认"
		case 10:
			operation = "执行结束"
		default:
			operation = fmt.Sprintf("COT=%d", cotRaw)
		}
		if pn {
			operation += "(否定)"
		}
		ret := ""
		if (cause == 7 || cause == 9 || cause == 10) && !pn {
			ret = "success"
		} else if (cause >= 44 && cause <= 50) || pn {
			ret = "failed"
		}
		return M{"addr": addrHex(objAddr, ioaLen), "state": cmdState, "operation": operation,
			"qoc": common.QOCName(qoc), "return": ret, "pointType": pointType}
	}
	if sq == 0 {
		for i := 0; i < num; i++ {
			if r.Left() < ioaLen+1 {
				return nil, fmt.Errorf("Control item too short")
			}
			objAddr := readIOA(r, ioaLen)
			one := parseOne(objAddr, r.U8())
			one["slot"] = i + 1
			results = append(results, one)
		}
	} else {
		if r.Left() < ioaLen {
			return nil, fmt.Errorf("Control base addr missing")
		}
		baseAddr := readIOA(r, ioaLen)
		for j := 0; j < num; j++ {
			if r.Left() < 1 {
				return nil, fmt.Errorf("Control seq item too short")
			}
			one := parseOne(baseAddr+j, r.U8())
			one["slot"] = j + 1
			results = append(results, one)
		}
	}
	return M{"type": "control", "data": results}, nil
}
