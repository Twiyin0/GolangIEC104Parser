package asdu

import (
	"fmt"

	"iec104rt/internal/protocol/common"
)

// ParseFileReady TI=123 文件传输准备就绪：IOA+NOF(2)+LOF(3)+FRQ(1)。
func ParseFileReady(ioaLen int, r *common.Reader, cotRaw, addr int) (M, error) {
	if r.Left() < ioaLen+6 {
		return nil, fmt.Errorf("F_FR_NA too short")
	}
	r.Skip(ioaLen)
	nof := r.U16()
	lof := readLOF(r)
	frq := r.U8()
	result := "拒绝(否定)"
	if frq&0x01 == 0 {
		result = "就绪(肯定)"
	}
	condition := "无条件传输"
	if frq&0x02 != 0 {
		condition = "有条件传输"
	}
	return M{"type": "file_service", "service": "F_FR_NA", "desc": "文件传输准备就绪",
		"nof": nof, "nofName": nofName(nof), "lof": lof, "size": fmt.Sprintf("%d字节", lof),
		"result": result, "condition": condition, "frq": frq, "cot": cotRaw, "addr": addr}, nil
}

// ParseSectionReady TI=124 节传输准备就绪：IOA+NOF(2)+NOS(1)+LOS(3)+SRQ(1)。
func ParseSectionReady(ioaLen int, r *common.Reader, cotRaw, addr int) (M, error) {
	if r.Left() < ioaLen+7 {
		return nil, fmt.Errorf("F_SR_NA too short")
	}
	r.Skip(ioaLen)
	nof := r.U16()
	nos := r.U8()
	los := readLOF(r)
	srq := r.U8()
	result := "拒绝(否定)"
	if srq&0x01 == 0 {
		result = "就绪(肯定)"
	}
	return M{"type": "file_service", "service": "F_SR_NA", "desc": "节传输准备就绪",
		"nof": nof, "nofName": nofName(nof), "nos": nos, "nosName": nosName(nos),
		"los": los, "size": fmt.Sprintf("%d字节", los), "result": result, "srq": srq,
		"cot": cotRaw, "addr": addr}, nil
}

// ParseSegment TI=125 段落：IOA+NOF(2)+NOS(1)+LOS(1)+DATA(LOS)。
func ParseSegment(ioaLen int, r *common.Reader, cotRaw, addr int) (M, error) {
	if r.Left() < ioaLen+4 {
		return nil, fmt.Errorf("F_SG_NA too short")
	}
	r.Skip(ioaLen)
	nof := r.U16()
	nos := r.U8()
	los := r.U8()
	data := r.Slice(r.Left())
	if len(data) > los {
		data = data[:los]
	}
	return M{"type": "file_service", "service": "F_SG_NA",
		"desc": fmt.Sprintf("文件段落(%d字节数据)", len(data)),
		"nof": nof, "nofName": nofName(nof), "nos": nos, "nosName": nosName(nos),
		"los": los, "dataLen": len(data), "dataHex": common.HexWrap(common.HexBytes(data)),
		"cot": cotRaw, "addr": addr}, nil
}

// ParseLastSegment TI=126 最后报文段/节：IOA+NOF(2)+NOS(1)+LSQ(1)+CHS(1)。
func ParseLastSegment(ioaLen int, r *common.Reader, cotRaw, addr int) (M, error) {
	if r.Left() < ioaLen+5 {
		return nil, fmt.Errorf("F_LS_NA too short")
	}
	r.Skip(ioaLen)
	nof := r.U16()
	nos := r.U8()
	lsq := r.U8()
	chs := r.U8()
	lsqMap := map[int]string{0: "最后段(继续)", 1: "节结束", 2: "文件结束", 3: "中止传输"}
	lsqDesc, ok := lsqMap[lsq]
	if !ok {
		lsqDesc = fmt.Sprintf("LSQ=%d", lsq)
	}
	return M{"type": "file_service", "service": "F_LS_NA", "desc": lsqDesc,
		"nof": nof, "nofName": nofName(nof), "nos": nos, "nosName": nosName(nos),
		"lsq": lsq, "lsqDesc": lsqDesc, "chs": chs, "chsHex": fmt.Sprintf("0x%02X", chs),
		"cot": cotRaw, "addr": addr}, nil
}

// ParseFileAck TI=127 确认文件传输：IOA+NOF(2)+NOS(1)+AFQ(1)。
func ParseFileAck(ioaLen int, r *common.Reader, cotRaw, addr int) (M, error) {
	if r.Left() < ioaLen+4 {
		return nil, fmt.Errorf("F_AF_NA(ack) too short")
	}
	r.Skip(ioaLen)
	nof := r.U16()
	nos := r.U8()
	afq := r.U8()
	codeMap := map[int]string{0: "无", 1: "肯定确认文件", 2: "否定确认文件", 3: "肯定确认节"}
	code := afq & 0x03
	reason := (afq >> 2) & 0x3F
	ackType, ok := codeMap[code]
	if !ok {
		ackType = fmt.Sprintf("code=%d", code)
	}
	reasonStr := "无错误"
	if reason != 0 {
		reasonStr = fmt.Sprintf("错误原因=%d", reason)
	}
	return M{"type": "file_service", "service": "F_AF_NA", "desc": "文件传输确认",
		"nof": nof, "nofName": nofName(nof), "nos": nos, "nosName": nosName(nos),
		"afq": afq, "ackType": ackType, "reason": reasonStr, "cot": cotRaw, "addr": addr}, nil
}
