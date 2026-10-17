package capture

import (
	"testing"
	"time"

	"iec104rt/internal/protocol"
	"iec104rt/internal/protocol/common"
	"iec104rt/internal/protocol/iec104"
)


// TestParsePipeline 解析管线：抓包线程投递原始报文 → 工作协程并行解析 → 按序回调产出。
func TestParsePipeline(t *testing.T) {
	got := make(chan protocol.Frame, 4)
	e := &Engine{
		cfg:     Config{TerminalIP: "192.168.1.101", TerminalPort: 2404, ParseWorkers: 2, ParseQueueSize: 8},
		onFrame: func(f protocol.Frame) { got <- f },
		pairs:   map[string]*connState{},
		streams: map[string][]byte{},
	}
	e.startWorkers()
	defer e.stopWorkers()

	apdu, err := iec104.HexToBytes("68 04 07 00 00 00")
	if err != nil {
		t.Fatal(err)
	}
	e.feed(testPkt("192.168.1.101", 2404, "192.168.1.10", 5000, flagACK, apdu), "2026-10-17 16:00:00.000")

	select {
	case f := <-got:
		if f.Type != "other" {
			t.Fatalf("解析类型异常: %v", f.Type)
		}
		if f.Dir != "T→M" {
			t.Fatalf("方向异常: %q", f.Dir)
		}
		if f.Time != "2026-10-17 16:00:00.000" {
			t.Fatalf("捕获时间未透传: %q", f.Time)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("解析管线未产出报文（超时）")
	}
	if e.QueueLen() != 0 {
		t.Fatalf("队列应已排空，实际 %d", e.QueueLen())
	}
	if e.DroppedFrames() != 0 {
		t.Fatalf("不应有丢弃，实际 %d", e.DroppedFrames())
	}
}

// TestParsePipelineOrder 并发解析后输出顺序必须与抓包顺序一致
// （首帧解析耗时远大于其余帧，若无顺序保持器则会被后发先至）。
func TestParsePipelineOrder(t *testing.T) {
	const n = 60
	got := make(chan protocol.Frame, n)
	e := &Engine{
		cfg:     Config{TerminalIP: "192.168.1.101", TerminalPort: 2404, ParseWorkers: 8, ParseQueueSize: 256},
		onFrame: func(f protocol.Frame) { got <- f },
		pairs:   map[string]*connState{},
		streams: map[string][]byte{},
	}
	// 人为让第 1 帧解析慢 60ms，其余帧快 → 触发乱序完成
	e.testParseDelay = func(raw []byte) time.Duration {
		if len(raw) > 0 && raw[len(raw)-1] == 0x01 {
			return 60 * time.Millisecond
		}
		return 0
	}
	e.startWorkers()
	defer e.stopWorkers()

	// 构造 n 条可区分顺序的遥测帧（IOA 递增），第 1 帧末尾标记 0x01
	for i := 0; i < n; i++ {
		asdu := []byte{13, 1, 3, 0, 1, 0, byte(i & 0xFF), 0x00, 0x00, 0x00, 0x00, 0x80, 0x3F, 0x00}
		if i == 0 {
			asdu = append(asdu, 0x01)
		}
		raw := append([]byte{0x68, byte(len(asdu) + 4), 0x04, 0x00, 0x00, 0x00}, asdu...)
		e.enqueue(raw, "t", "T→M")
	}

	seen := make([]int, 0, n)
	timeout := time.After(8 * time.Second)
	for len(seen) < n {
		select {
		case f := <-got:
			data, _ := f.Parsed["data"].([]common.M)
			if len(data) == 0 {
				t.Fatalf("第 %d 条解析结果无数据: %+v", len(seen), f.Parsed)
			}
			seen = append(seen, data[0]["addr"].(int))
		case <-timeout:
			t.Fatalf("解析输出超时，仅收到 %d/%d 条", len(seen), n)
		}
	}
	for i, addr := range seen {
		if addr != i {
			t.Fatalf("输出顺序错乱：第 %d 条应为 IOA=%d，实际 %d（完整序列 %v）", i, i, addr, seen)
		}
	}
	if e.DroppedFrames() != 0 {
		t.Fatalf("队列未满不应丢弃，实际 %d", e.DroppedFrames())
	}
}

// TestParsePipelineQueueFull 队列满时丢弃并计数，且顺序保持器跳过被丢弃序号、不卡死。
func TestParsePipelineQueueFull(t *testing.T) {
	const n = 40
	got := make(chan protocol.Frame, n)
	e := &Engine{
		cfg:     Config{TerminalIP: "192.168.1.101", TerminalPort: 2404, ParseWorkers: 1, ParseQueueSize: 2},
		onFrame: func(f protocol.Frame) { got <- f },
		pairs:   map[string]*connState{},
		streams: map[string][]byte{},
	}
	// 每帧解析都慢，制造队列积压 → 触发丢弃
	e.testParseDelay = func([]byte) time.Duration { return 8 * time.Millisecond }
	e.startWorkers()
	defer e.stopWorkers()

	for i := 0; i < n; i++ {
		asdu := []byte{13, 1, 3, 0, 1, 0, byte(i & 0xFF), 0x00, 0x00, 0x00, 0x00, 0x80, 0x3F, 0x00}
		raw := append([]byte{0x68, byte(len(asdu) + 4), 0x04, 0x00, 0x00, 0x00}, asdu...)
		e.enqueue(raw, "t", "T→M")
	}
	if e.DroppedFrames() == 0 {
		t.Skip("本次未触发队列丢弃（机器较快），跳过")
	}
	// 未丢弃的报文应全部按序输出且不卡死
	want := n - int(e.DroppedFrames())
	seen := make([]int, 0, want)
	timeout := time.After(8 * time.Second)
	for len(seen) < want {
		select {
		case f := <-got:
			data, _ := f.Parsed["data"].([]common.M)
			if len(data) > 0 {
				seen = append(seen, data[0]["addr"].(int))
			}
		case <-timeout:
			t.Fatalf("顺序保持器可能卡死：收到 %d/%d 条", len(seen), want)
		}
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] <= seen[i-1] {
			t.Fatalf("丢弃后输出顺序应保持递增：%v", seen)
		}
	}
}

