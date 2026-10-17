// Package iec104 实现 DL/T 634.5104-2009（IEC 60870-5-104）APDU 解析。
// 语义 1:1 移植自参考实现 104ParserClass.ts，保留 FIX-01~13 修复语义。
package iec104

import (
	"fmt"

	"iec104rt/internal/protocol/asdu"
	"iec104rt/internal/protocol/common"
)

type M = common.M

// TINames 类型标识名称表。
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
	120: "F_AF_NA(文件可用)", 121: "F_SC_NA(文件召唤)", 122: "F_DR_TA(目录)",
	123: "F_FR_NA(文件就绪)", 124: "F_SR_NA(节就绪)", 125: "F_SG_NA(段落)",
	126: "F_LS_NA(最后段/节)", 127: "F_AF_NA(确认文件)",
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

var uFrameNames = map[int]string{
	0x07: "STARTDT_ACT(启动数据传输)", 0x0B: "STARTDT_CON(启动确认)",
	0x13: "STOPDT_ACT(停止数据传输)", 0x23: "STOPDT_CON(停止确认)",
	0x43: "TESTFR_ACT(测试帧)", 0x83: "TESTFR_CON(测试确认)",
}

const ioaLen = 3 // 104 信息对象地址固定 3 字节

// ParseAPDU 解析一条完整 104 APDU（含 APCI）。
func ParseAPDU(buf []byte) M {
	if len(buf) < 6 {
		return M{"type": "error", "error": "APDU too short"}
	}
	if buf[0] != 0x68 {
		return M{"type": "error", "error": "Invalid start byte"}
	}
	length := int(buf[1])
	if len(buf) < length+2 {
		return M{"type": "error", "error": "Buffer too short for APDU"}
	}
	ctrl := buf[2:6]
	asduBuf := buf[6 : 2+length]

	// S/U 帧（非 I 帧）——附加格式识别（供状态机与列表展示使用）
	// IEC 60870-5-104：bit0=0 → I 帧；bit0=1&bit1=1 → U 帧；bit0=1&bit1=0 → S 帧
	if ctrl[0]&0x01 != 0 || len(asduBuf) == 0 {
		ctrlHex := common.HexBytes(ctrl)
		if ctrl[0]&0x03 == 0x01 {
			ns := int(ctrl[1]>>1)&0x7F | int(ctrl[2]&0x7F)<<7 // S 帧 N(R)
			return M{"type": "other", "format": "S", "ctrl": ctrlHex,
				"desc": fmt.Sprintf("S帧(监视) N(R)=%d", ns)}
		}
		name, ok := uFrameNames[int(ctrl[0])]
		if !ok {
			name = fmt.Sprintf("U帧(0x%02X)", ctrl[0])
		}
		return M{"type": "other", "format": "U", "ctrl": ctrlHex, "uName": name, "desc": name}
	}

	if len(asduBuf) < 6 {
		return M{"type": "error", "error": "ASDU too short"}
	}
	ti := int(asduBuf[0])
	vsq := int(asduBuf[1])
	cotRaw := int(asduBuf[2]) | int(asduBuf[3])<<8
	addr := int(asduBuf[4]) | int(asduBuf[5])<<8
	// [FIX-06] COT 位解析
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
	case 120:
		result, err = asdu.ParseFileAvailable(ioaLen, r, cotRaw, addr)
	case 121:
		result, err = asdu.ParseFileCall(ioaLen, r, cotRaw, addr)
	case 122:
		result, err = asdu.ParseFileDirectory(ioaLen, r, cotRaw, addr, num)
	case 123:
		result, err = asdu.ParseFileReady(ioaLen, r, cotRaw, addr)
	case 124:
		result, err = asdu.ParseSectionReady(ioaLen, r, cotRaw, addr)
	case 125:
		result, err = asdu.ParseSegment(ioaLen, r, cotRaw, addr)
	case 126:
		result, err = asdu.ParseLastSegment(ioaLen, r, cotRaw, addr)
	case 127:
		result, err = asdu.ParseFileAck(ioaLen, r, cotRaw, addr)
	case 210:
		result, err = asdu.ParseFileService210(3, r, cotRaw, addr) // 参考实现固定按 IOA=3 字节
	default:
		result = M{"type": "unknown", "ti": ti, "tiHex": fmt.Sprintf("0x%02X", ti),
			"cot": cotRaw, "cotDesc": cotDesc, "addr": addr}
	}
	if err != nil {
		return M{"type": "error", "error": err.Error(), "ti": ti, "cot": cotRaw}
	}
	if result == nil {
		result = M{"type": "unknown", "ti": ti, "cot": cotRaw, "cotDesc": cotDesc, "addr": addr}
	}
	result["pn"] = pn
	result["test"] = test
	result["cotDesc"] = cotDesc
	result["ti"] = ti
	result["tiName"] = TIName(ti)
	return result
}
