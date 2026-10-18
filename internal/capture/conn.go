package capture

import (
	"fmt"
	"time"

	"iec104rt/internal/protocol"
)

// match 过滤：主站↔终端 双向，可选按主站 IP 过滤。
func (e *Engine) match(p *TCPPacket) bool {
	tIP, tPort := e.cfg.TerminalIP, e.cfg.TerminalPort
	mIP := e.cfg.MasterIP
	fwd := p.Key.SrcIP == tIP && p.Key.SrcPort == tPort
	rev := p.Key.DstIP == tIP && p.Key.DstPort == tPort
	if !fwd && !rev {
		return false
	}
	if mIP != "" && fwd && p.Key.DstIP != mIP {
		return false
	}
	if mIP != "" && rev && p.Key.SrcIP != mIP {
		return false
	}
	return true
}

// updateConn 连接状态机：以**实际观测到的报文**推断链路状态
// （支持监听中途接入：看不到握手也能凭数据帧判定已连接）。
func (e *Engine) updateConn(p *TCPPacket) {
	pair := p.Key.Pair()
	e.mu.Lock()
	st, ok := e.pairs[pair]
	if !ok {
		st = &connState{}
		e.pairs[pair] = st
	}
	prev := st.tcpState
	st.lastSeen = time.Now()
	masterHint := -1
	switch {
	case p.IsSyn():
		// SYN 发起方即主站（终端为服务端）
		if p.Key.SrcIP != e.cfg.TerminalIP {
			masterHint = p.Key.SrcPort
		}
		st.tcpState = "syn"
	case p.IsRst() || p.IsFin():
		st.tcpState = "down"
	case len(p.Payload) > 0:
		st.tcpState = "up" // 数据流（I/S 帧均在 TCP 载荷中）→ 链路已建立
	case p.Flags&flagACK != 0 && st.tcpState == "syn":
		st.tcpState = "up" // 握手完成
	}
	needMaster := false
	if masterHint >= 0 && !e.masterSet {
		e.masterSet = true
		needMaster = true
	}
	e.mu.Unlock()
	if needMaster && e.onMaster != nil {
		e.onMaster(masterHint)
	}
	if st.tcpState != prev {
		switch st.tcpState {
		case "syn":
			e.status("tcp", "TCP: 握手发起(SYN) → 链路建立中")
		case "up":
			if prev == "" || prev == "down" {
				e.status("tcp", "TCP: 链路已连接（观测到数据流）")
			} else {
				e.status("tcp", "TCP: 已连接（三次握手完成）")
			}
		case "down":
			if p.IsRst() {
				e.status("tcp", "TCP: 复位断开(RST)")
			} else {
				e.status("tcp", "TCP: 断开(FIN)")
			}
		}
	}
}

// feed 流重组 + APDU 切分 + 解析 + 应用层状态（STARTDT/STOPDT/TESTFR）。
func (e *Engine) feed(p *TCPPacket, capTime string) {
	dir := "M→T"
	if p.Key.SrcIP == e.cfg.TerminalIP && p.Key.SrcPort == e.cfg.TerminalPort {
		dir = "T→M"
	}
	key := fmt.Sprintf("%s:%d>%s:%d", p.Key.SrcIP, p.Key.SrcPort, p.Key.DstIP, p.Key.DstPort)
	e.mu.Lock()
	buf := append(e.streams[key], p.Payload...)
	if len(buf) > 262144 { // 缓冲上限，防错位数据堆积
		buf = buf[len(buf)-131072:]
	}
	raws, rest := protocol.ExtractAuto(buf)
	e.streams[key] = rest
	e.mu.Unlock()

	for _, raw := range raws {
		// 仅 104 帧用控制字节更新 STARTDT/STOPDT 状态；101 帧（透传到 TCP）跳过。
		if len(raw) >= 6 && protocol.Detect(raw) == "104" {
			e.updateApp(raw[2]) // 应用层状态：仅读一个字节，开销极小
		}
		e.enqueue(raw, capTime, dir) // 解析交给工作协程（不阻塞抓包循环）
	}
}

func (e *Engine) updateApp(ctrl byte) {
	switch ctrl {
	case 0x07:
		e.setApp("started", "104: STARTDT_ACT 启动传输请求")
	case 0x0B:
		e.setApp("started", "104: STARTDT_CON 启动确认（链路已启动）")
	case 0x13:
		e.setApp("stopped", "104: STOPDT_ACT 停止传输")
	case 0x23:
		e.setApp("stopped", "104: STOPDT_CON 停止确认")
	case 0x43:
		e.status("app", "104: TESTFR_ACT 测试帧")
	case 0x83:
		e.status("app", "104: TESTFR_CON 测试确认")
	}
}

func (e *Engine) setApp(state, text string) {
	e.mu.Lock()
	changed := false
	for _, st := range e.pairs {
		if st.appState != state {
			st.appState = state
			changed = true
		}
	}
	e.mu.Unlock()
	if changed {
		e.status("app", text)
	}
}
