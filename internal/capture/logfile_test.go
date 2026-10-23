package capture

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"iec104rt/internal/protocol"
)

func test101VarFrame() []byte {
	body := []byte{0xF3, 0x01, 0x00, 0x0D, 0x01, 0x14, 0x00, 0x01, 0x00, 0x00, 0x80, 0x3F}
	cs := 0
	for _, b := range body {
		cs = (cs + int(b)) & 0xFF
	}
	frame := append([]byte{0x68, byte(len(body)), byte(len(body)), 0x68}, body...)
	return append(frame, byte(cs), 0x16)
}

func waitFrame(t *testing.T, ch <-chan protocol.Frame) protocol.Frame {
	t.Helper()
	select {
	case f := <-ch:
		return f
	case <-time.After(3 * time.Second):
		t.Fatal("等待日志帧超时")
		return protocol.Frame{}
	}
}

func TestStartLogStaticAndUpdate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	line104 := "protolog:2026/10/18-16:36:40: file:Proto.cpp ,line:461If(COM0) Tx(6) --->  68 04 07 00 00 00\n"
	line101 := "protolog:2026/10/18-16:36:40: file:Proto.cpp ,line:353If(COM0) Rx(18) <---   " +
		hexString(test101VarFrame()) + "\n"
	if err := os.WriteFile(path, []byte(line104+line101), 0o644); err != nil {
		t.Fatal(err)
	}
	ch := make(chan protocol.Frame, 16)
	cfg := Config{LogProtocol: "local", LogPath: path, LogWatchMode: "static", LogEncoding: "auto", LogTailBytes: 65536}
	e, err := StartLog(cfg, func(f protocol.Frame) { ch <- f }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	f1 := waitFrame(t, ch)
	f2 := waitFrame(t, ch)
	if f1.Proto != "104" || f2.Proto != "101" || f2.Ti != 13 {
		t.Fatalf("静态日志解析错误: %+v %+v", f1, f2)
	}
	if f2.Dir != "T→M" || f2.Time != "2026-10-18 16:36:40" {
		t.Fatalf("时间/方向错误: %+v", f2)
	}
	// 追加后 Update 只读增量，不重复
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("protolog:2026/10/18-16:36:41: Tx(6) ---> 68 04 43 00 00 00\n")
	_ = f.Close()
	if err := e.UpdateLog(); err != nil {
		t.Fatal(err)
	}
	if f3 := waitFrame(t, ch); f3.Proto != "104" {
		t.Fatalf("更新日志解析错误: %+v", f3)
	}
	select {
	case f := <-ch:
		t.Fatalf("重复读取产生重复帧: %+v", f)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestStartLogDynamic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	ch := make(chan protocol.Frame, 16)
	cfg := Config{LogProtocol: "local", LogPath: path, LogWatchMode: "dynamic", LogEncoding: "auto", LogTailBytes: 0}
	e, err := StartLog(cfg, func(f protocol.Frame) { ch <- f }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("protolog:2026/10/18-16:36:40: Rx(6) <--- 68 04 83 00 00 00\n")
	_ = f.Close()
	if got := waitFrame(t, ch); got.Proto != "104" {
		t.Fatalf("动态日志解析错误: %+v", got)
	}
	st := e.LogStats()
	if st.Lines != 1 || st.Frames != 1 {
		t.Fatalf("动态统计错误: %+v", st)
	}
}

func hexString(b []byte) string {
	const hex = "0123456789ABCDEF"
	out := make([]byte, 0, len(b)*3)
	for i, v := range b {
		if i > 0 {
			out = append(out, ' ')
		}
		out = append(out, hex[v>>4], hex[v&0x0F])
	}
	return string(out)
}
