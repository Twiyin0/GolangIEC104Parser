package capture

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"iec104rt/internal/capture/wpcap"
	"iec104rt/internal/protocol"
)

// Config 抓包引擎配置（Connection 面板）。
type Config struct {
	Mode         string // "net" 网口旁听(104) | "serial" 串口(101)
	Device       string // 网卡 \Device\NPF_{GUID}；串口 "COM3"
	TerminalIP   string
	TerminalPort int
	MasterIP     string // "" = 任意
	MasterPort   int    // 0 = 自动识别
	Promisc      bool

	SerialMode string // "passive"(485总线旁听) | "direct"(232直接测试)
	BaudRate   int
	DataBits   int
	Parity     string
	StopBits   int
	LinkAddr   int // 101 链路地址（direct 模式问答用）

	ParseWorkers   int // 解析并发数（0=默认 4，上限 10）
	ParseQueueSize int // 解析队列容量（0=默认 4096）
}

// StatusFunc 状态回调（level: tcp/app/serial/engine）。
type StatusFunc func(level, text string)

// FrameFunc 报文回调（已解析、已定向、已带捕获时间）。
type FrameFunc func(f protocol.Frame)

// MasterFunc 主站端口识别回调。
type MasterFunc func(port int)

// Engine 抓包引擎（网口或串口后端）。
type Engine struct {
	cfg      Config
	onFrame  FrameFunc
	onStatus StatusFunc
	onMaster MasterFunc
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	running  atomic.Bool

	mu        sync.Mutex
	pairs     map[string]*connState
	streams   map[string][]byte
	masterSet bool

	// 解析管线（多线程并行 + 有界队列 + 顺序保持）
	parseCh       chan rawFrame
	parseResCh    chan parsedResult
	workers       int
	seqCounter    atomic.Uint64
	nextEmit      uint64
	pending       map[uint64]parsedResult
	skipped       map[uint64]bool
	skipMu        sync.Mutex
	droppedFrames atomic.Int64
	parseWG       sync.WaitGroup // 解析工作协程
	orderWG       sync.WaitGroup // 顺序保持器
	testParseDelay func([]byte) time.Duration // 仅测试用：模拟解析耗时差异
}

type connState struct {
	tcpState string // down / syn / up
	appState string // "" / started / stopped
	lastSeen time.Time
}

// StartNet 启动网口旁听引擎（只旁听、不占用终端端口，红线见 Goal.md §4-1）。
func StartNet(cfg Config, onFrame FrameFunc, onStatus StatusFunc, onMaster MasterFunc) (*Engine, error) {
	if cfg.TerminalIP == "" || cfg.TerminalPort <= 0 {
		return nil, fmt.Errorf("请先在 Setting→Connection 中配置终端 IP 与端口")
	}
	if !wpcap.Available() {
		return nil, fmt.Errorf("Npcap 不可用：请安装 Npcap，并以管理员权限运行本程序（静态解析与导入功能不受影响）")
	}
	if cfg.Device == "" {
		return nil, fmt.Errorf("未指定抓包网卡（Setting→Connection 中选择）")
	}
	handle, err := wpcap.OpenLive(cfg.Device, 65535, cfg.Promisc, 100)
	if err != nil {
		return nil, err
	}
	e := &Engine{
		cfg: cfg, onFrame: onFrame, onStatus: onStatus, onMaster: onMaster,
		stopCh:  make(chan struct{}),
		pairs:   map[string]*connState{},
		streams: map[string][]byte{},
	}
	e.running.Store(true)
	e.status("engine", fmt.Sprintf("网口旁听已启动: %s (终端 %s:%d)", cfg.Device, cfg.TerminalIP, cfg.TerminalPort))
	e.startWorkers()
	// 主站端口扫描（需求第 7 条）：本机 TCP 连接表
	go func() {
		if p, err := wpcap.FindLocalPortTo(cfg.TerminalIP, cfg.TerminalPort); err == nil && onMaster != nil {
			e.mu.Lock()
			e.masterSet = true
			e.mu.Unlock()
			onMaster(p)
		}
	}()
	e.wg.Add(1)
	go e.netLoop(handle)
	return e, nil
}

func (e *Engine) status(level, text string) {
	if e.onStatus != nil {
		e.onStatus(level, text)
	}
}

// Running 引擎是否运行中。
func (e *Engine) Running() bool { return e.running.Load() }

// Stop 停止引擎并释放资源（先停抓包循环，再排空解析队列）。
func (e *Engine) Stop() {
	e.stopOnce.Do(func() { close(e.stopCh) })
	e.wg.Wait()
	e.stopWorkers()
	e.running.Store(false)
	e.status("engine", "监听已停止")
}

// ConnStatus 终端连接状态（状态灯）：仅链路级"已连接 / 未连接"。
// 判定依据：观测到 TCP 数据流/握手即视为已连接（支持监听中途接入的场景——
// 此时看不到 SYN，但能看到数据帧），空闲超过 idleTimeout 视为未连接；
// 应用层细节（STARTDT/STOPDT/TESTFR）通过状态栏事件展示。
func (e *Engine) ConnStatus() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	for _, st := range e.pairs {
		if st.tcpState == "up" && now.Sub(st.lastSeen) < connIdleTimeout {
			return "已连接"
		}
	}
	return "未连接"
}

// connIdleTimeout 无任何报文超过该时长则视为链路断开/静默（104 心跳 TESTFR 约 20s）。
const connIdleTimeout = 30 * time.Second

func (e *Engine) netLoop(handle *wpcap.Handle) {
	defer e.wg.Done()
	defer handle.Close()
	for {
		select {
		case <-e.stopCh:
			return
		default:
		}
		data, sec, usec, ret, err := handle.NextEx()
		if ret == 0 { // 超时，循环检查停止
			continue
		}
		if ret != 1 {
			if err != nil {
				e.status("engine", "抓包错误: "+err.Error())
			}
			return
		}
		pkt := ParseEthernet(data)
		if pkt == nil {
			continue
		}
		if !e.match(pkt) {
			continue
		}
		capTime := time.Unix(sec, usec*1000).Format("2006-01-02 15:04:05.000")
		e.updateConn(pkt)
		if len(pkt.Payload) > 0 {
			e.feed(pkt, capTime)
		}
	}
}

// Devices 网卡列表（带 IPv4，供 Connection 选择）。
func Devices() ([]wpcap.Device, error) { return wpcap.FindAllDevs() }

// PcapVersion Npcap 版本信息。
func PcapVersion() string { return wpcap.LibVersion() }
