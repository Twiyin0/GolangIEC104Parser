// Package iec101 实现 DL/T 634.5101-2002（IEC 60870-5-101，FT1.2）解析。
// 语义 1:1 移植自参考实现 101ParserClass.ts，保留 D-01~06 差异语义：
// IOA=2 字节、链路层 FT1.2 剥离、链路功能码解析、TI=104/105。
package iec101

import (
	"fmt"

	"iec104rt/internal/protocol/common"
)

type M = common.M

// 链路功能码（非平衡传输）
var fcNameDown = map[int]string{
	0: "复位远方链路", 1: "复位用户进程", 2: "链路测试", 3: "发送/确认用户数据",
	4: "发送/无回答用户数据", 9: "请求链路状态", 10: "请求1级用户数据", 11: "请求2级用户数据",
}

var fcNameUp = map[int]string{
	0: "认可(ACK)", 1: "否定认可(NACK)", 8: "以用户数据响应", 9: "无所请求的用户数据",
	11: "链路状态/访问请求",
}

// TINames 101 类型标识名称表。
var TINames = map[int]string{
	1: "M_SP_NA_1(单点遥信)", 3: "M_DP_NA_1(双点遥信)",
	5: "M_ST_NA_1(步位置信息)", 7: "M_BO_TA_1(带时标位串)",
	9: "M_ME_NA_1(归一化遥测)", 11: "M_ME_NB_1(标度化遥测)", 13: "M_ME_NC_1(短浮点遥测)",
	30: "M_SP_TB_1(带时标单点遥信)", 31: "M_DP_TB_1(带时标双点遥信)",
	42: "M_EV_TA_1(故障事件信息)",
	45: "C_SC_NA_1(单点遥控)", 46: "C_DC_NA_1(双点遥控)",
	70: "M_EI_NA_1(初始化结束)",
	100: "C_IC_NA_1(总召唤)", 101: "C_CI_NA_1(电能量召唤)",
	103: "C_CS_NA_1(时钟同步)", 104: "C_TS_NA_1(测试命令)", 105: "C_RP_NA_1(复位进程)",
	200: "C_SW_NA(切换定值区)", 201: "C_SN_NA(读定值区号)",
	202: "C_PR_NA(读参数定值)", 203: "C_PW_NA(写参数定值)",
	206: "M_IT_NA(累计量)", 207: "M_IT_TB(带时标累计量)",
	210: "F_FR_NA_1(私有文件服务)",
}

// TIName 取类型名称。
func TIName(ti int) string {
	if s, ok := TINames[ti]; ok {
		return s
	}
	return fmt.Sprintf("TI=%d", ti)
}

const ioaLen = 2 // [D-02] 101 信息对象地址 2 字节

// ParseControlByte 解析链路控制域 C（[D-03]）。
func ParseControlByte(c int) M {
	prm := (c >> 6) & 0x01
	dir := (c >> 7) & 0x01 // 平衡模式下有效
	fc := c & 0x0F
	result := M{"raw": fmt.Sprintf("0x%02X", c), "prm": prm, "dir": dir, "fc": fc}
	if prm == 1 {
		fcName, ok := fcNameDown[fc]
		if !ok {
			fcName = fmt.Sprintf("FC=%d", fc)
		}
		dirStr := "↓下行"
		if dir != 0 {
			dirStr = "↑上行(平衡)"
		}
		result["fcb"] = (c >> 5) & 0x01
		result["fcv"] = (c >> 4) & 0x01
		result["fcName"] = fcName
		result["direction"] = dirStr
	} else {
		fcName, ok := fcNameUp[fc]
		if !ok {
			fcName = fmt.Sprintf("FC=%d", fc)
		}
		dirStr := "↑上行"
		if dir != 0 {
			dirStr = "↓下行(平衡)"
		}
		result["acd"] = (c >> 5) & 0x01
		result["dfc"] = (c >> 4) & 0x01
		result["fcName"] = fcName
		result["direction"] = dirStr
	}
	return result
}

// Frame 链路层帧解析结果。
type Frame struct {
	FrameType  string // fixed / variable / error
	Err        string
	Ctrl       M
	LinkAddr   int
	CS         int
	CSCalc     int
	CSValid    bool
	AsduBuf    []byte
	FrameLength int
}

// ParseFrame 识别 FT1.2 帧类型并剥离链路层（[D-01]）。
func ParseFrame(buf []byte) Frame {
	if len(buf) < 1 {
		return Frame{FrameType: "error", Err: "Empty buffer"}
	}
	// 固定帧长格式：10H C A1 A2 CS 16H（6 字节）
	if buf[0] == 0x10 {
		if len(buf) < 6 {
			return Frame{FrameType: "error", Err: "固定帧长度不足6字节"}
		}
		if buf[len(buf)-1] != 0x16 {
			return Frame{FrameType: "error", Err: "固定帧结束字符不是16H"}
		}
		c := int(buf[1])
		linkAddr := int(buf[2]) | int(buf[3])<<8
		cs := int(buf[4])
		csCalc := (c + int(buf[2]) + int(buf[3])) & 0xFF
		return Frame{FrameType: "fixed", Ctrl: ParseControlByte(c), LinkAddr: linkAddr,
			CS: cs, CSCalc: csCalc, CSValid: cs == csCalc, FrameLength: 6}
	}
	// 可变帧长格式：68H L L 68H C A1 A2 [ASDU] CS 16H
	if buf[0] == 0x68 {
		if len(buf) < 6 {
			return Frame{FrameType: "error", Err: "可变帧最小长度不足"}
		}
		l1 := int(buf[1])
		l2 := int(buf[2])
		if l1 != l2 {
			return Frame{FrameType: "error", Err: fmt.Sprintf("可变帧两个L不一致: L1=%d L2=%d", l1, l2)}
		}
		if buf[3] != 0x68 {
			return Frame{FrameType: "error", Err: "可变帧第4字节不是68H"}
		}
		totalLen := l1 + 6
		if len(buf) < totalLen {
			return Frame{FrameType: "error", Err: fmt.Sprintf("可变帧实际长度%d不足，需要%d", len(buf), totalLen)}
		}
		if buf[totalLen-1] != 0x16 {
			return Frame{FrameType: "error", Err: "可变帧结束字符不是16H"}
		}
		c := int(buf[4])
		linkAddr := int(buf[5]) | int(buf[6])<<8 // [D-03] A：2字节链路地址
		cs := int(buf[totalLen-2])
		csCalc := 0
		for i := 4; i < totalLen-2; i++ {
			csCalc = (csCalc + int(buf[i])) & 0xFF
		}
		return Frame{FrameType: "variable", Ctrl: ParseControlByte(c), LinkAddr: linkAddr,
			CS: cs, CSCalc: csCalc, CSValid: cs == csCalc,
			AsduBuf: buf[7 : totalLen-2], FrameLength: totalLen}
	}
	return Frame{FrameType: "error", Err: fmt.Sprintf("未知帧起始字节: 0x%02X", buf[0])}
}
