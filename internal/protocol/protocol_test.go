package protocol

import (
	"testing"

	"iec104rt/internal/protocol/common"
)

// build104 拼装 104 APDU：68 LL ctrl(4) ASDU
func build104(ctrl []byte, asdu []byte) []byte {
	out := []byte{0x68, byte(len(ctrl) + len(asdu))}
	out = append(out, ctrl...)
	out = append(out, asdu...)
	return out
}

var ctrlI = []byte{0x04, 0x00, 0x00, 0x00}

func TestParse104UFrame(t *testing.T) {
	f, err := ParseHexAuto("68 04 07 00 00 00")
	if err != nil {
		t.Fatal(err)
	}
	if f.Parsed["type"] != "other" || f.Parsed["format"] != "U" {
		t.Fatalf("expect U frame, got %#v", f.Parsed)
	}
	if f.Category != "链路" {
		t.Fatalf("category wrong: %v", f.Category)
	}
}

func TestParse104TotalCall(t *testing.T) {
	// 总召唤：TI=100 COT=6 CA=1 IOA=0 QOI=20
	asdu := []byte{100, 1, 6, 0, 1, 0, 0, 0, 0, 20}
	f, err := ParseAuto(build104(ctrlI, asdu))
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != "total_call" || f.Parsed["command"] != "total" {
		t.Fatalf("total_call mismatch: %#v", f.Parsed)
	}
	// [FIX-01] QOI 必须读到 20（不得误判为分组召唤）
	if f.Parsed["group"] != nil {
		t.Fatalf("FIX-01 violated: %#v", f.Parsed)
	}
}

func TestParse104YXDouble(t *testing.T) {
	asdu := []byte{3, 2, 3, 0, 1, 0, 1, 0, 0, 2, 2, 0, 0, 3}
	f, err := ParseAuto(build104(ctrlI, asdu))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := f.Parsed["data"].([]common.M)
	if len(data) != 2 {
		t.Fatalf("expect 2 yx items, got %d", len(data))
	}
	if data[0]["state"] != "合" || data[1]["state"] != "故障" {
		t.Fatalf("DP states wrong: %#v", data)
	}
}

func TestParse104YXSequence(t *testing.T) {
	asdu := []byte{1, 0x83, 3, 0, 1, 0, 0x00, 0x01, 0x00, 0, 1, 0}
	f, err := ParseAuto(build104(ctrlI, asdu))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := f.Parsed["data"].([]common.M)
	if len(data) != 3 {
		t.Fatalf("expect 3 items, got %d", len(data))
	}
	if data[1]["addr"] != 0x101 || data[1]["state"] != "合(1)" {
		t.Fatalf("seq addr/state wrong: %#v", data[1])
	}
}

func TestParse104YCFloatAndNVA(t *testing.T) {
	asdu := []byte{13, 1, 3, 0, 1, 0, 1, 0, 0, 0x00, 0x00, 0x80, 0x3F, 0x00}
	f, err := ParseAuto(build104(ctrlI, asdu))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := f.Parsed["data"].([]common.M)
	if v, ok := data[0]["value"].(float64); !ok || v != 1.0 {
		t.Fatalf("float value wrong: %#v", data[0])
	}
	asdu9 := []byte{9, 1, 3, 0, 1, 0, 1, 0, 0, 0xFF, 0x7F, 0x00}
	f2, err := ParseAuto(build104(ctrlI, asdu9))
	if err != nil {
		t.Fatal(err)
	}
	d2, _ := f2.Parsed["data"].([]common.M)
	val, _ := d2[0]["value"].(common.M)
	if val["eng"] != 1.0 { // [FIX-03] raw=32767 → eng=1.0
		t.Fatalf("NVA eng wrong: %#v", val)
	}
}

func TestParse104Control(t *testing.T) {
	asdu := []byte{45, 1, 6, 0, 1, 0, 0x01, 0x00, 0x00, 0x81}
	f, err := ParseAuto(build104(ctrlI, asdu))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := f.Parsed["data"].([]common.M)
	if data[0]["operation"] != "选择" || data[0]["state"] != "控合" {
		t.Fatalf("control parse wrong: %#v", data[0]) // [FIX-07]
	}
	if data[0]["addr"] != "0x000001" {
		t.Fatalf("addr hex wrong: %#v", data[0])
	}
}

func TestParse104ClockSync(t *testing.T) {
	asdu := []byte{103, 1, 6, 0, 1, 0, 0, 0, 0, 0x80, 0x2B, 0x0C, 0x22, 0x0A, 0x06, 0x50}
	f, err := ParseAuto(build104(ctrlI, asdu))
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != "clock_sync" {
		t.Fatalf("expect clock_sync, got %v", f.Type)
	}
	if tm, _ := f.Parsed["time"].(string); tm == "" {
		t.Fatalf("time not parsed: %#v", f.Parsed)
	}
}

func TestParse104Fault(t *testing.T) {
	asdu := []byte{42, 1, 3, 0, 1, 0,
		1,
		1, 0x01, 0x00, 0x01,
		0x80, 0x2B, 0x0C, 0x22, 0x0A, 0x06, 0x50,
		0x1A,
		1,
		0x0D,
		0x01, 0x00, 0x00, 0x00, 0x00, 0x80, 0x3F,
	}
	f, err := ParseAuto(build104(ctrlI, asdu))
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != "fault" {
		t.Fatalf("expect fault, got %v", f.Type)
	}
	yc, _ := f.Parsed["yc"].([]common.M)
	if len(yc) != 1 { // [FIX-02]
		t.Fatalf("FIX-02 violated: expect 1 yc, got %d: %#v", len(yc), f.Parsed)
	}
	if v, _ := yc[0]["value"].(float64); v != 1.0 {
		t.Fatalf("fault yc value wrong: %#v", yc[0])
	}
}
