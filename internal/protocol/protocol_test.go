package protocol

import (
	"testing"

	"iec104rt/internal/protocol/common"
	"iec104rt/internal/protocol/iec104"
)

// 101 完整帧地址/值校验 + 截断帧 L 检查（回归：D3 控制帧 36 点）。
func TestCheck101LAndAddresses(t *testing.T) {
	const hex = "68 BF BF 68 D3 01 00 0D A4 14 00 01 00 11 40 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 80 3F 00 00 00 48 42 00 E4 AC DE 41 00 00 00 C8 42 00 52 50 E8 BC 00 F5 E0 FD BC 00 96 EA 95 3B 00 98 79 3A 42 00 00 00 00 40 00 60 A1 BA 36 00 00 00 61 45 00 EB 32 5F 43 00 4E AB 5B 43 00 6A A2 5B 43 00 4C 96 A6 36 00 E5 69 B3 3D 00 12 CC 37 3B 00 1D 41 0E 3D 00 C7 78 37 3B 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 61 95 6A 43 00 00 00 00 00 00 8B E5 03 3F 00 70 16"
	buf, err := iec104.HexToBytes(hex)
	if err != nil {
		t.Fatal(err)
	}
	chk := CheckFrame(buf)
	if !chk.Complete || chk.Protocol != "101" || chk.Expected != 197 || chk.Actual != 197 {
		t.Fatalf("完整帧检查错误: %+v", chk)
	}
	fs := ParseBest(buf)
	if len(fs) != 1 || fs[0].Proto != "101" || fs[0].Ti != 13 {
		t.Fatalf("完整帧解析错误: %+v", fs)
	}
	d, ok := fs[0].Parsed["data"].([]common.M)
	if !ok || len(d) != 36 {
		t.Fatalf("点数错误: %d", len(d))
	}
	if d[0]["addr"] != 0x4011 || d[0]["value"] != float64(0) {
		t.Fatalf("首点错误: %#v", d[0])
	}
	if d[11]["addr"] != 0x401C || d[11]["value"] != float64(1) {
		t.Fatalf("第12点错误: %#v", d[11])
	}
	if d[21]["value"] != float64(3600) || d[35]["addr"] != 0x4034 {
		t.Fatalf("末点错误: #22=%#v #36=%#v", d[21], d[35])
	}
	// 截断 4 字节后：L 仍为 191，必须判定 101 不完整，不能猜成 104
	trunc := buf[:len(buf)-4]
	if got := Detect(trunc); got != "101" {
		t.Fatalf("截断帧 Detect=%q，want 101", got)
	}
	if chk := CheckFrame(trunc); chk.Complete || chk.Expected != 197 || chk.Actual != 193 {
		t.Fatalf("截断帧检查错误: %+v", chk)
	}
	if fs := ParseBest(trunc); len(fs) != 0 {
		t.Fatalf("截断帧不应解析出帧: %+v", fs)
	}
}

// 101 可变帧：68 L L 68 C A ASDU CS 16。按 104 切会截断成 68 L …，
// 自适应必须识别为 101（回归：静态解析曾因“104 帧数不少于 101”而误判）。
func TestDetect101VariableFrame(t *testing.T) {
	const hex = "68 BF BF 68 F3 01 00 0D A4 14 00 01 00 11 40 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 80 3F 00 00 00 48 42 00 00 0B DE 41 00 00 00 C8 42 00 E9 75 AF BC 00 9A 5C B4 BC 00 3F 04 76 3A 00 0E E4 30 42 00 00 00 00 40 00 0E 84 9E B3 00 00 00 61 45 00 EB 32 5F 43 00 4E AB 5B 43 00 6A A2 5B 43 00 4C 96 A6 36 00 E5 69 B3 3D 00 12 CC 37 3B 00 1D 41 0E 3D 00 C7 78 37 3B 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 2A AE 73 3E 00 00 00 00 00 00 AE 3B 5A 43 00 8C 16"
	buf, err := iec104.HexToBytes(hex)
	if err != nil {
		t.Fatal(err)
	}
	if got := Detect(buf); got != "101" {
		t.Fatalf("Detect = %q, want 101 (len=%d)", got, len(buf))
	}
	fs := ParseBest(buf)
	if len(fs) != 1 || fs[0].Proto != "101" {
		t.Fatalf("ParseBest = %d frames, proto=%v", len(fs), fs)
	}
	if fs[0].Ti != 13 {
		t.Fatalf("101 遥测 TI 应为 13，实际 %d（%s）", fs[0].Ti, fs[0].TiName)
	}
	if d, ok := fs[0].Parsed["data"].([]common.M); !ok || len(d) != 36 {
		t.Fatalf("101 遥测应为 36 点（VSQ=0xA4, SQ=1），实际 %d", len(d))
	}
	// 网口自适应切帧也必须按完整 101 帧切出，而不是截成 104
	raws, rest := ExtractAuto(buf)
	firstLen := 0
	if len(raws) > 0 {
		firstLen = len(raws[0])
	}
	if len(raws) != 1 || len(rest) != 0 || firstLen != len(buf) {
		t.Fatalf("ExtractAuto 101 = %d frames, rest=%d, firstLen=%d (want 1/0/%d)",
			len(raws), len(rest), firstLen, len(buf))
	}
	// 普通 104 APDU 不受影响
	raws, rest = ExtractAuto([]byte{0x68, 0x04, 0x07, 0x00, 0x00, 0x00})
	if len(raws) != 1 || len(rest) != 0 || len(raws[0]) != 6 {
		t.Fatalf("ExtractAuto 104 = %d frames, rest=%d", len(raws), len(rest))
	}
}

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

func fileServiceNum(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case uint8:
		return int(n)
	case uint16:
		return int(n)
	case uint32:
		return int(n)
	case uint64:
		return int(n)
	}
	return -1
}

// 规范 7.6.3.2 d) 读文件数据传输确认：fileId(4,LE)+segmentNo(4,LE)+result(1)。
// 取自现场报文：fileId=0，segmentNo=56400，result=0。
func TestParseFileService210DataConfirm(t *testing.T) {
	f, err := ParseHexAuto("68 18 0C 00 8C 04 D2 81 05 00 01 00 00 00 00 02 06 00 00 00 00 50 DC 00 00 00")
	if err != nil {
		t.Fatal(err)
	}
	p := f.Parsed
	if f.Type != "file_service" || p["service"] != "F_FR_NA_1" {
		t.Fatalf("file service parse failed: %#v", p)
	}
	if fileServiceNum(p["op"]) != 6 {
		t.Fatalf("op = %v（应 6 读文件数据传输确认）", p["op"])
	}
	if fileServiceNum(p["fileId"]) != 0 {
		t.Fatalf("fileId = %v（规范预留恒 0）", p["fileId"])
	}
	if fileServiceNum(p["segmentNo"]) != 56400 {
		t.Fatalf("segmentNo = %v（应为 56400 偏移）", p["segmentNo"])
	}
	if fileServiceNum(p["resultCode"]) != 0 {
		t.Fatalf("resultCode = %v（应 0 无后续）", p["resultCode"])
	}
}

// 规范 7.6.3.2 c) 读文件数据传输：fileId(4)+segmentNo(4)+后续标志(1)+data+checksum(1)。
// 旧实现误把首字节当文件名长度，导致 fileId 错位、出现 0x08000000 等假文件号。
func TestParseFileService210DataLE(t *testing.T) {
	asdu := []byte{
		0xD2, 0x01, 0x05, 0x00, 0x01, 0x00, // TI/VSQ/COT/CA
		0x00, 0x00, 0x00, // IOA=0
		0x02,                   // 附加数据包类型=文件传输
		0x05,                   // 读文件数据传输
		0x00, 0x00, 0x00, 0x00, // fileId=0
		0x00, 0x00, 0x00, 0x00, // segmentNo=0
		0x01,             // 后续标志=有后续
		0x61, 0x62, 0x63, // data "abc"
		0x26, // checksum = 0x61+0x62+0x63 的低字节
	}
	f, err := ParseAuto(build104(ctrlI, asdu))
	if err != nil {
		t.Fatal(err)
	}
	p := f.Parsed
	if fileServiceNum(p["op"]) != 5 {
		t.Fatalf("op = %v（应 5 读文件数据传输）", p["op"])
	}
	if fileServiceNum(p["fileId"]) != 0 || fileServiceNum(p["segmentNo"]) != 0 {
		t.Fatalf("fileId/segmentNo 错位: %#v", p)
	}
	if fileServiceNum(p["contFlag"]) != 1 || fileServiceNum(p["dataLen"]) != 3 {
		t.Fatalf("后续标志/数据长度错误: %#v", p)
	}
	if p["dataHex"] != "61 62 63" || fileServiceNum(p["checksum"]) != 0x26 || p["checksumValid"] != true {
		t.Fatalf("数据/校验码解析错误: %#v", p)
	}
	if _, ok := p["fileName"]; ok {
		t.Fatalf("规范 op=5 不含文件名，不应解析出 fileName: %#v", p)
	}
}

// 规范 7.6.3.2 b) 读文件激活确认：文件名 + fileId(4,LE) + 文件大小(4,LE)。
func TestParseFileService210ActivateLE(t *testing.T) {
	asdu := []byte{
		0xD2, 0x01, 0x07, 0x00, 0x01, 0x00, // TI/VSQ/COT=7/CA
		0x00, 0x00, 0x00, // IOA=0
		0x02,                                     // 文件传输
		0x04,                                     // 读文件激活确认
		0x00,                                     // 成功
		0x07,                                     // 文件名长度
		0x73, 0x6F, 0x65, 0x2E, 0x78, 0x6D, 0x6C, // soe.xml
		0x00, 0x00, 0x00, 0x00, // fileId=0
		0x5D, 0xDC, 0x00, 0x00, // fileSize=56413，低字节在前
	}
	f, err := ParseAuto(build104(ctrlI, asdu))
	if err != nil {
		t.Fatal(err)
	}
	p := f.Parsed
	if p["fileName"] != "soe.xml" || fileServiceNum(p["fileSize"]) != 56413 {
		t.Fatalf("激活确认文件名/大小解析错误: %#v", p)
	}
}
