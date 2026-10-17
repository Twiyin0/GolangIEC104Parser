package asdu

import (
	"fmt"

	"iec104rt/internal/protocol/common"
)

// ── 标准文件服务 TI=120~127 ─────────────────────────────────────────────

func nofName(nof int) string {
	m := map[int]string{1: "录波文件", 2: "故障事件记录", 3: "遥控操作记录", 4: "定值修改记录", 5: "运行日志"}
	if s, ok := m[nof]; ok {
		return s
	}
	return fmt.Sprintf("文件#%d", nof)
}

func nosName(nos int) string {
	if nos == 0 {
		return "整体(无节)"
	}
	return fmt.Sprintf("第%d节", nos)
}

func readLOF(r *common.Reader) int {
	return int(r.U8()) | int(r.U8())<<8 | int(r.U8())<<16
}

// ParseFileAvailable TI=120 文件可用：IOA+NOF(2)+LOF(3)+SRQ(1)。
func ParseFileAvailable(ioaLen int, r *common.Reader, cotRaw, addr int) (M, error) {
	if r.Left() < ioaLen+6 {
		return nil, fmt.Errorf("F_AF_NA too short")
	}
	r.Skip(ioaLen)
	nof := r.U16()
	lof := readLOF(r)
	srq := r.U8()
	status := "目录项"
	if srq&0x01 != 0 {
		status = "可用"
	}
	return M{"type": "file_service", "service": "F_AF_NA", "desc": "文件可用通知",
		"nof": nof, "nofName": nofName(nof), "lof": lof, "size": fmt.Sprintf("%d字节", lof),
		"status": status, "srq": srq, "cot": cotRaw, "addr": addr}, nil
}

// ParseFileCall TI=121 文件召唤/目录请求：IOA+NOF(2)+NOS(1)+SCQ(1)。
func ParseFileCall(ioaLen int, r *common.Reader, cotRaw, addr int) (M, error) {
	if r.Left() < ioaLen+4 {
		return nil, fmt.Errorf("F_SC_NA too short")
	}
	r.Skip(ioaLen)
	nof := r.U16()
	nos := r.U8()
	scq := r.U8()
	scqMap := map[int]string{0x00: "无请求", 0x01: "选择文件", 0x02: "请求文件", 0x03: "撤销选择", 0x05: "请求目录"}
	scqDesc, ok := scqMap[scq]
	if !ok {
		scqDesc = fmt.Sprintf("SCQ=0x%02X", scq)
	}
	desc := "文件召唤"
	if scq == 0x05 {
		desc = "目录查询请求"
	}
	return M{"type": "file_service", "service": "F_SC_NA", "desc": desc,
		"nof": nof, "nofName": nofName(nof), "nos": nos, "nosName": nosName(nos),
		"scq": scq, "scqDesc": scqDesc, "cot": cotRaw, "addr": addr}, nil
}

// ParseFileDirectory TI=122 目录：IOA+[NOF(2)+LOF(3)+CP56(7)+SRQ(1)]×N。
func ParseFileDirectory(ioaLen int, r *common.Reader, cotRaw, addr, num int) (M, error) {
	if r.Left() < ioaLen {
		return nil, fmt.Errorf("F_DR_TA too short")
	}
	r.Skip(ioaLen)
	entries := []M{}
	count := num
	if count <= 0 {
		count = 255
	}
	for i := 0; i < count; i++ {
		if r.Left() < 13 {
			break
		}
		nof := r.U16()
		lof := readLOF(r)
		timeStr, err := r.CP56()
		if err != nil {
			break
		}
		r.Skip(7)
		srq := r.U8()
		status := "不可用"
		if srq&0x01 != 0 {
			status = "可用"
		}
		entries = append(entries, M{"nof": nof, "nofName": nofName(nof), "lof": lof,
			"size": fmt.Sprintf("%d字节", lof), "lastModified": timeStr, "status": status, "srq": srq})
	}
	return M{"type": "file_service", "service": "F_DR_TA",
		"desc": fmt.Sprintf("目录响应(%d个文件)", len(entries)), "entries": entries,
		"cot": cotRaw, "addr": addr}, nil
}
