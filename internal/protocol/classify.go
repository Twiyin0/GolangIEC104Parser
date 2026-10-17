package protocol

import (
	"fmt"
	"strings"

	"iec104rt/internal/protocol/common"
)

func classify(p common.M, proto string) (cat, summary string) {
	t, _ := p["type"].(string)
	switch t {
	case "yx":
		return "遥信", fmt.Sprintf("遥信 %d 点", dataLen(p))
	case "soe":
		return "遥信(SOE)", fmt.Sprintf("SOE 带时标遥信 %d 点", dataLen(p))
	case "yc":
		return "遥测", fmt.Sprintf("遥测 %d 点", dataLen(p))
	case "bitstring":
		return "其他", fmt.Sprintf("位串 %d 点", dataLen(p))
	case "control":
		return "遥控", controlSummary(p)
	case "param":
		return "定值", paramSummary(p)
	case "energy":
		return "电能量", fmt.Sprintf("电能量(累计量) %d 点", dataLen(p))
	case "fault":
		if d, ok := p["description"].(string); ok {
			return "事件", d
		}
		return "事件", "故障事件"
	case "total_call":
		if c, _ := p["command"].(string); c == "total" {
			return "命令", "总召唤"
		}
		if g, ok := p["group"].(int); ok {
			return "命令", fmt.Sprintf("分组召唤(组=%d)", g)
		}
		return "命令", "召唤命令"
	case "energy_call":
		return "命令", "电能量召唤: " + strOr(p, "qccDesc", "")
	case "clock_sync":
		return "命令", strOr(p, "desc", "时钟同步")
	case "init_end":
		return "命令", "初始化结束: " + strOr(p, "coiDesc", "")
	case "test_cmd":
		return "命令", strOr(p, "desc", "测试命令")
	case "reset_process":
		return "命令", strOr(p, "desc", "复位进程")
	case "file_service":
		return "文件", strOr(p, "desc", "文件服务")
	case "link_frame":
		return "链路", strOr(p, "fcName", "101 链路帧")
	case "other":
		return "链路", strOr(p, "desc", "链路帧")
	case "unknown":
		return "其他", strOr(p, "tiName", "未知类型")
	case "error":
		return "错误", "解析错误: " + strOr(p, "error", "")
	}
	return "其他", t
}

func dataLen(p common.M) int {
	if d, ok := p["data"].([]common.M); ok {
		return len(d)
	}
	return 0
}

func strOr(p common.M, k, def string) string {
	if v, ok := p[k].(string); ok && v != "" {
		return v
	}
	return def
}

func controlSummary(p common.M) string {
	d, ok := p["data"].([]common.M)
	if !ok || len(d) == 0 {
		return "遥控"
	}
	first := d[0]
	parts := []string{strOr(first, "addr", "")}
	if op, ok := first["operation"].(string); ok {
		parts = append(parts, op)
	}
	if st, ok := first["state"].(string); ok {
		parts = append(parts, st)
	}
	if len(d) > 1 {
		parts = append(parts, fmt.Sprintf("等%d点", len(d)))
	}
	return strings.Join(parts, " ")
}

func paramSummary(p common.M) string {
	d, ok := p["data"].([]common.M)
	if !ok || len(d) == 0 {
		return "定值操作"
	}
	first := d[0]
	op, _ := first["operation"].(string)
	if sn, ok := first["sn"].(int); ok {
		return fmt.Sprintf("%s SN=%d", op, sn)
	}
	return op
}
