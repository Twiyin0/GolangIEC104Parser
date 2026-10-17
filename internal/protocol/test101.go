package protocol

import (
	"testing"

	"iec104rt/internal/protocol/common"
	"iec104rt/internal/protocol/iec101"
	"iec104rt/internal/protocol/iec104"
)

// build101Var 拼装 101 可变帧：68 L L 68 C A1 A2 ASDU CS 16
func build101Var(c int, addr int, asdu []byte) []byte {
	body := []byte{byte(c), byte(addr & 0xFF), byte(addr >> 8)}
	body = append(body, asdu...)
	l := len(body)
	cs := common.Sum8(body)
	out := []byte{0x68, byte(l), byte(l), 0x68}
	out = append(out, body...)
	out = append(out, byte(cs), 0x16)
	return out
}

func TestParse101FixedFrame(t *testing.T) {
	buf := []byte{0x10, 0x00, 0x01, 0x00, 0x01, 0x16} // ACK
	f, err := ParseAuto(buf)
	if err != nil {
		t.Fatal(err)
	}
	if f.Proto != "101" || f.Type != "link_frame" || f.Parsed["csValid"] != true {
		t.Fatalf("fixed frame wrong: %#v", f.Parsed)
	}
	if f.Parsed["fcName"] != "认可(ACK)" {
		t.Fatalf("fcName wrong: %#v", f.Parsed)
	}
}

func TestParse101TotalCall(t *testing.T) {
	asdu := []byte{100, 1, 6, 0, 1, 0, 0, 0, 20} // IOA=2字节
	buf := build101Var(0x73, 1, asdu)
	f, err := ParseAuto(buf)
	if err != nil {
		t.Fatal(err)
	}
	if f.Proto != "101" {
		t.Fatalf("proto detect failed: %v %#v", f.Proto, f.Parsed)
	}
	if f.Type != "total_call" || f.Parsed["command"] != "total" {
		t.Fatalf("total_call mismatch: %#v", f.Parsed)
	}
	li, _ := f.Parsed["linkInfo"].(map[string]any)
	if li == nil || li["prm"] != 1 {
		t.Fatalf("linkInfo missing: %#v", f.Parsed)
	}
}

func TestParse101YX(t *testing.T) {
	asdu := []byte{3, 1, 3, 0, 1, 0, 0x01, 0x00, 0x02} // IOA=2字节
	buf := build101Var(0x53, 1, asdu)
	f, err := ParseAuto(buf)
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != "yx" {
		t.Fatalf("expect yx, got %v %#v", f.Type, f.Parsed)
	}
	data, _ := f.Parsed["data"].([]common.M)
	if data[0]["state"] != "合" {
		t.Fatalf("101 yx state wrong: %#v", data[0])
	}
}

func TestParse101ParamRead(t *testing.T) {
	// TI=201 读区号响应：IOA(2) + current(2) + min(2) + max(2)，COT=7
	asdu := []byte{201, 1, 7, 0, 1, 0, 0x01, 0x00, 0x02, 0x00, 0x01, 0x00, 0x0A, 0x00}
	buf := build101Var(0x53, 1, asdu)
	f, err := ParseAuto(buf)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := f.Parsed["data"].([]common.M)
	if len(data) != 1 {
		t.Fatalf("expect 1 param item: %#v", f.Parsed)
	}
	if data[0]["operation"] != "读区号响应" || data[0]["current"] != 2 || data[0]["max"] != 10 {
		t.Fatalf("param read wrong: %#v", data[0])
	}
}

func TestDetect(t *testing.T) {
	if got := Detect([]byte{0x10, 0, 1, 0, 1, 0x16}); got != "101" {
		t.Fatalf("fixed 101 detect: %v", got)
	}
	if got := Detect([]byte{0x68, 0x04, 0x07, 0x00, 0x00, 0x00}); got != "104" {
		t.Fatalf("104 detect: %v", got)
	}
	if got := Detect(build101Var(0x73, 1, []byte{100, 1, 6, 0, 1, 0, 0, 0, 20})); got != "101" {
		t.Fatalf("variable 101 detect: %v", got)
	}
	if got := Detect([]byte{0x55}); got != "" {
		t.Fatalf("unknown detect: %v", got)
	}
}

func TestExtractAndResync(t *testing.T) {
	f1, _ := iec104.HexToBytes("68 04 07 00 00 00")
	total, _ := iec104.HexToBytes("68 0E 04 00 00 00 64 01 06 00 01 00 00 00 00 14")
	stream := append([]byte{0x00, 0x00}, f1...) // 前置垃圾字节 → 再同步
	stream = append(stream, total...)
	frames, rest := Extract104(stream)
	if len(frames) != 2 || len(rest) != 0 {
		t.Fatalf("104 extract wrong: %d frames rest=%v", len(frames), rest)
	}
	ack := []byte{0x10, 0x00, 0x01, 0x00, 0x01, 0x16}
	var101 := build101Var(0x73, 1, []byte{100, 1, 6, 0, 1, 0, 0, 0, 20})
	s2 := append(append([]byte{}, ack...), var101...)
	got101, rest2 := Extract101(s2)
	if len(got101) != 2 || len(rest2) != 0 {
		t.Fatalf("101 extract wrong: %d frames, rest %v", len(got101), rest2)
	}
}

func TestParse101Batch(t *testing.T) {
	var101 := build101Var(0x73, 1, []byte{100, 1, 6, 0, 1, 0, 0, 0, 20})
	ack := []byte{0x10, 0x00, 0x01, 0x00, 0x01, 0x16}
	var buf []byte
	buf = append(buf, var101...)
	buf = append(buf, ack...)
	results := iec101.ParseBuffer(buf)
	if len(results) != 2 {
		t.Fatalf("expect 2 frames, got %d", len(results))
	}
	if results[0]["type"] != "total_call" || results[1]["type"] != "link_frame" {
		t.Fatalf("batch results wrong: %#v", results)
	}
}

func TestParseStream104(t *testing.T) {
	f1, _ := iec104.HexToBytes("68 04 07 00 00 00")
	total, _ := iec104.HexToBytes("68 0E 04 00 00 00 64 01 06 00 01 00 00 00 00 14")
	var stream []byte
	stream = append(stream, f1...)
	stream = append(stream, total...)
	frames := ParseStream("104", stream)
	if len(frames) != 2 {
		t.Fatalf("expect 2, got %d", len(frames))
	}
	if frames[1].Type != "total_call" {
		t.Fatalf("frame type wrong: %#v", frames[1])
	}
}
