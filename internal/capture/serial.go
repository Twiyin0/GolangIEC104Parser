package capture

import (
	"fmt"
	"time"

	"go.bug.st/serial"

	"iec104rt/internal/protocol"
)

// StartSerial 启动串口引擎。
// SerialMode: "passive"=RS-485 总线被动旁听（只收不发，零占用）；
//             "direct" =RS-232/USB 直接测试（工具作主站问答，占用链路）。
func StartSerial(cfg Config, onFrame FrameFunc, onStatus StatusFunc) (*Engine, error) {
	if cfg.Device == "" {
		return nil, fmt.Errorf("未指定串口（Setting→Connection 中选择）")
	}
	mode := &serial.Mode{
		BaudRate: orDefault(cfg.BaudRate, 9600),
		DataBits: orDefault(cfg.DataBits, 8),
		Parity:   parseParity(cfg.Parity),
		StopBits: parseStopBits(cfg.StopBits),
	}
	port, err := serial.Open(cfg.Device, mode)
	if err != nil {
		return nil, fmt.Errorf("打开串口 %s 失败: %v", cfg.Device, err)
	}
	e := &Engine{
		cfg: cfg, onFrame: onFrame, onStatus: onStatus,
		sourceKind: "serial",
		stopCh:  make(chan struct{}),
		pairs:   map[string]*connState{},
		streams: map[string][]byte{},
	}
	e.running.Store(true)
	e.status("engine", fmt.Sprintf("串口已打开: %s %d-%d%s%d 模式=%s（%s）",
		cfg.Device, mode.BaudRate, mode.DataBits, parityName(cfg.Parity), cfg.StopBits,
		cfg.SerialMode, serialModeDesc(cfg.SerialMode)))
	e.startWorkers()

	if cfg.SerialMode == "direct" {
		go e.directHandshake(port)
	}
	e.wg.Add(1)
	go e.serialLoop(port)
	e.wg.Add(1)
	go e.serialIdleWatch()
	return e, nil
}

func serialModeDesc(m string) string {
	if m == "direct" {
		return "直接测试（占用，工具作主站）"
	}
	return "485总线被动旁听（零占用，只收不发）"
}

func orDefault(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func parseParity(p string) serial.Parity {
	switch p {
	case "odd":
		return serial.OddParity
	case "even":
		return serial.EvenParity
	default:
		return serial.NoParity
	}
}

func parityName(p string) string {
	switch p {
	case "odd":
		return "O"
	case "even":
		return "E"
	default:
		return "N"
	}
}

func parseStopBits(s int) serial.StopBits {
	if s == 2 {
		return serial.TwoStopBits
	}
	return serial.OneStopBit
}

var serialLastFrame time.Time

func (e *Engine) serialLoop(port serial.Port) {
	defer e.wg.Done()
	defer port.Close()
	buf := make([]byte, 4096)
	var acc []byte
	for {
		n, err := port.Read(buf)
		if err != nil {
			select {
			case <-e.stopCh:
				return
			default:
			}
			e.status("serial", "串口读取错误: "+err.Error())
			return
		}
		if n == 0 {
			continue
		}
		serialLastFrame = time.Now()
		acc = append(acc, buf[:n]...)
		if len(acc) > 65536 {
			acc = acc[len(acc)-32768:]
		}
		raws, rest := protocol.Extract101(acc)
		acc = append([]byte(nil), rest...)
		now := time.Now().Format("2006-01-02 15:04:05.000")
		for _, raw := range raws {
			e.enqueue(raw, now, serialDirOf(raw)) // 解析交给工作协程
		}
	}
}

// serialDirOf 由 FT1.2 控制域 C 的 PRM 位快速推断方向（无需完整解析，供解析管线使用）。
func serialDirOf(raw []byte) string {
	var c int
	switch {
	case len(raw) >= 2 && raw[0] == 0x10: // 固定帧 10 C A1 A2 CS 16
		c = int(raw[1])
	case len(raw) >= 5 && raw[0] == 0x68: // 可变帧 68 L L 68 C A1 A2 …
		c = int(raw[4])
	default:
		return ""
	}
	if (c>>6)&0x01 == 1 {
		return "M→T"
	}
	return "T→M"
}

func (e *Engine) serialIdleWatch() {
	defer e.wg.Done()
	idle := false
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-e.stopCh:
			return
		case <-t.C:
			if time.Since(serialLastFrame) > 5*time.Second {
				if !idle {
					idle = true
					e.status("serial", "串口: 无数据（链路静默）")
				}
			} else {
				idle = false
			}
		}
	}
}

// serialDir 由链路层 PRM 位推断方向：PRM=1 主站→终端。
func serialDir(p map[string]any) string {
	if li, ok := p["linkInfo"].(map[string]any); ok {
		if prm, ok := li["prm"].(int); ok {
			if prm == 1 {
				return "M→T"
			}
			return "T→M"
		}
	}
	if prm, ok := p["prm"].(int); ok {
		if prm == 1 {
			return "M→T"
		}
		return "T→M"
	}
	return ""
}

// directHandshake 直连测试模式最小主站问答：复位链路→链路测试→总召唤。
func (e *Engine) directHandshake(port serial.Port) {
	link := orDefault(e.cfg.LinkAddr, 1)
	// 1) 复位远方链路（FC=0）
	e.writeSerial(port, fixedFrame(0x40, link))
	time.Sleep(300 * time.Millisecond)
	// 2) 链路测试（FC=2）
	e.writeSerial(port, fixedFrame(0x42, link))
	time.Sleep(300 * time.Millisecond)
	// 3) 总召唤（FC=3 发送/确认，FCB=0，FCV=1）
	asdu := []byte{100, 1, 6, 0, byte(link & 0xFF), byte(link >> 8), 0x00, 0x00, 20}
	e.writeSerial(port, varFrame(0x73, link, asdu))
	e.status("serial", "直接测试: 已发送 复位链路/链路测试/总召唤，等待终端响应")
}

func (e *Engine) writeSerial(port serial.Port, frame []byte) {
	if _, err := port.Write(frame); err != nil {
		e.status("serial", "串口发送失败: "+err.Error())
		return
	}
	f, err := protocol.ParseAuto(frame)
	if err == nil {
		f.Time = time.Now().Format("2006-01-02 15:04:05.000")
		f.Dir = "M→T"
		if e.onFrame != nil {
			e.onFrame(f)
		}
	}
}

// fixedFrame 组 FT1.2 固定帧：10 C A1 A2 CS 16。
func fixedFrame(c int, linkAddr int) []byte {
	cs := (c + linkAddr&0xFF + (linkAddr >> 8)) & 0xFF
	return []byte{0x10, byte(c), byte(linkAddr & 0xFF), byte(linkAddr >> 8), byte(cs), 0x16}
}

// varFrame 组 FT1.2 可变帧：68 L L 68 C A1 A2 ASDU CS 16。
func varFrame(c int, linkAddr int, asdu []byte) []byte {
	body := []byte{byte(c), byte(linkAddr & 0xFF), byte(linkAddr >> 8)}
	body = append(body, asdu...)
	l := len(body)
	cs := 0
	for _, b := range body {
		cs = (cs + int(b)) & 0xFF
	}
	out := []byte{0x68, byte(l), byte(l), 0x68}
	out = append(out, body...)
	out = append(out, byte(cs), 0x16)
	return out
}

// SerialPorts 枚举本机串口。
func SerialPorts() ([]string, error) { return serial.GetPortsList() }
