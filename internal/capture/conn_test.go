package capture

import (
	"testing"
	"time"
)

func newTestEngine() *Engine {
	return &Engine{
		cfg:   Config{TerminalIP: "192.168.1.101", TerminalPort: 2404},
		pairs: map[string]*connState{},
	}
}

func testPkt(srcIP string, srcPort int, dstIP string, dstPort int, flags int, payload []byte) *TCPPacket {
	return &TCPPacket{
		Key:     ConnKey{SrcIP: srcIP, SrcPort: srcPort, DstIP: dstIP, DstPort: dstPort},
		Flags:   flags,
		Payload: payload,
	}
}

// TestConnStatusMidStream 监听中途接入（看不到 SYN，只见数据流）应判定为已连接。
func TestConnStatusMidStream(t *testing.T) {
	e := newTestEngine()
	if got := e.ConnStatus(); got != "未连接" {
		t.Fatalf("初始应未连接: %q", got)
	}
	// 终端 → 主站：APDU 数据帧（S/I 帧都在 TCP 载荷中）
	e.updateConn(testPkt("192.168.1.101", 2404, "192.168.1.10", 5000, flagACK,
		[]byte{0x68, 0x04, 0x01, 0x00, 0x01, 0x00}))
	if got := e.ConnStatus(); got != "已连接" {
		t.Fatalf("观测到数据流应判定已连接: %q", got)
	}
	// FIN → 未连接
	e.updateConn(testPkt("192.168.1.10", 5000, "192.168.1.101", 2404, flagFIN|flagACK, nil))
	if got := e.ConnStatus(); got != "未连接" {
		t.Fatalf("FIN 后应未连接: %q", got)
	}
	// 断开后又有数据流（新连接中途接入）→ 已连接
	e.updateConn(testPkt("192.168.1.101", 2404, "192.168.1.10", 5000, flagACK, []byte{0x68, 0x04, 0x43, 0x00, 0x00, 0x00}))
	if got := e.ConnStatus(); got != "已连接" {
		t.Fatalf("断开后重新观测到数据流应已连接: %q", got)
	}
}

// TestConnStatusHandshake 完整三次握手路径。
func TestConnStatusHandshake(t *testing.T) {
	e := newTestEngine()
	e.updateConn(testPkt("192.168.1.10", 5000, "192.168.1.101", 2404, flagSYN, nil))
	if got := e.ConnStatus(); got != "未连接" {
		t.Fatalf("仅 SYN 时应未连接: %q", got)
	}
	e.updateConn(testPkt("192.168.1.101", 2404, "192.168.1.10", 5000, flagSYN|flagACK, nil))
	e.updateConn(testPkt("192.168.1.10", 5000, "192.168.1.101", 2404, flagACK, nil))
	if got := e.ConnStatus(); got != "已连接" {
		t.Fatalf("握手完成应已连接: %q", got)
	}
	// RST → 未连接
	e.updateConn(testPkt("192.168.1.101", 2404, "192.168.1.10", 5000, flagRST|flagACK, nil))
	if got := e.ConnStatus(); got != "未连接" {
		t.Fatalf("RST 后应未连接: %q", got)
	}
}

// TestConnStatusIdleTimeout 空闲超过阈值视为未连接。
func TestConnStatusIdleTimeout(t *testing.T) {
	e := newTestEngine()
	e.updateConn(testPkt("192.168.1.101", 2404, "192.168.1.10", 5000, flagACK, []byte{1, 2, 3}))
	if got := e.ConnStatus(); got != "已连接" {
		t.Fatalf("应已连接: %q", got)
	}
	e.mu.Lock()
	for _, st := range e.pairs {
		st.lastSeen = time.Now().Add(-connIdleTimeout - time.Second)
	}
	e.mu.Unlock()
	if got := e.ConnStatus(); got != "未连接" {
		t.Fatalf("空闲超时应未连接: %q", got)
	}
}

// TestMasterPortHint SYN 发起方即主站（端口识别）。
func TestMasterPortHint(t *testing.T) {
	e := newTestEngine()
	got := 0
	e.onMaster = func(port int) { got = port }
	e.updateConn(testPkt("192.168.1.10", 50123, "192.168.1.101", 2404, flagSYN, nil))
	if got != 50123 {
		t.Fatalf("主站端口识别失败: %d", got)
	}
}
