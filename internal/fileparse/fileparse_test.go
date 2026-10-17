package fileparse

import (
	"strings"
	"testing"
)

func resolver(kind string, addr int) string {
	if addr == 101 {
		return "断路器合位"
	}
	if addr == 201 {
		return "I段10kV进线电压"
	}
	return ""
}

const xmlSOE = `<?xml version="1.0" encoding="UTF-8"?>
<SOEReport station="1#变电站">
  <Record>
    <Time>2026-10-17 15:00:01.123</Time>
    <Point>101</Point>
    <State>合</State>
  </Record>
  <Record>
    <Time>2026-10-17 15:00:02.456</Time>
    <Point>201</Point>
    <State>分</State>
  </Record>
</SOEReport>`

const msgSOE = `SOE记录
时间,点号,状态,时标
2026-10-17 15:00:01,101,合,2026-10-17 15:00:01.123
2026-10-17 15:00:02,201,分,2026-10-17 15:00:02.456
`

const msgFault = `故障事件记录
序号,时间,点号,值,时标
1,2026-10-17 15:01:00,101,1,2026-10-17 15:01:00.100
`

const msgCtrl = `遥控操作记录
时间,遥控点号,操作,结果
2026-10-17 15:02:00,101,控合,成功
`

func TestParseXMLAuto(t *testing.T) {
	res := Parse([]byte(xmlSOE), "", resolver)
	if res.Format != "xml" {
		t.Fatalf("format = %q（应自动识别为 xml）", res.Format)
	}
	if res.Kind != "soe" || res.KindName != "SOE记录" {
		t.Fatalf("kind = %q / %q", res.Kind, res.KindName)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d（应 2 条）", len(res.Rows))
	}
	if res.Rows[0]["Point"] != "101" || res.Rows[0]["State"] != "合" {
		t.Fatalf("字段解析错误: %#v", res.Rows[0])
	}
	if res.Rows[0]["点名"] != "断路器合位" || res.Rows[1]["点名"] != "I段10kV进线电压" {
		t.Fatalf("点表点名未解析: %#v", res.Rows)
	}
	if !containsStr(res.Columns, "点名") {
		t.Fatalf("columns 应包含点名: %v", res.Columns)
	}
}

func TestParseMSGAuto(t *testing.T) {
	res := Parse([]byte(msgSOE), "", resolver)
	if res.Format != "msg" {
		t.Fatalf("format = %q（应自动识别为 msg）", res.Format)
	}
	if res.Kind != "soe" {
		t.Fatalf("kind = %q（应 soe）", res.Kind)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d", len(res.Rows))
	}
	if res.Rows[0]["点号"] != "101" || res.Rows[0]["点名"] != "断路器合位" {
		t.Fatalf("点名解析失败: %#v", res.Rows[0])
	}
	if !strings.Contains(res.Note, "分隔符 逗号") {
		t.Fatalf("note 应含分隔符说明: %q", res.Note)
	}
}

func TestClassifyKinds(t *testing.T) {
	if got := Parse([]byte(msgFault), "", resolver).Kind; got != "fault" {
		t.Fatalf("故障事件 kind = %q", got)
	}
	if got := Parse([]byte(msgCtrl), "", resolver).Kind; got != "control" {
		t.Fatalf("遥控记录 kind = %q", got)
	}
	if got := Parse([]byte("事件记录\n1,2026-10-17 15:00:00,101\n"), "", resolver).Kind; got != "event" {
		t.Fatalf("事件记录 kind = %q", got)
	}
}

func TestForcedFormat(t *testing.T) {
	// 强制 XML 解析 MSG 内容 → 格式标注为 xml，但解析不出记录 → 有错误提示
	res := Parse([]byte(msgSOE), "xml", resolver)
	if res.Format != "xml" {
		t.Fatalf("forced format = %q", res.Format)
	}
	if res.Error == "" {
		t.Fatalf("强制格式解析失败时应给出错误提示")
	}
	// 强制 MSG 解析 XML 内容 → 不应崩，返回结果结构完整
	res2 := Parse([]byte(xmlSOE), "msg", resolver)
	if res2.Format != "msg" {
		t.Fatalf("forced msg format = %q", res2.Format)
	}
}

func TestAutoFailAndGarbage(t *testing.T) {
	res := Parse([]byte{0x01, 0x02, 0x03, 0x04, 0x05}, "", resolver)
	if res.Format != "" {
		t.Fatalf("二进制内容不应被识别为 xml/msg，got %q", res.Format)
	}
	if res.Error == "" {
		t.Fatalf("无法解析时应给出提示（供用户手动选择格式）")
	}
}

func TestKeyValueFormat(t *testing.T) {
	data := "记录类型=SOE\n点号=101\n状态=合\n"
	res := Parse([]byte(data), "", resolver)
	if len(res.Rows) == 0 {
		t.Fatalf("key=value 应能解析: %+v", res)
	}
	if res.Rows[0]["点名"] != "断路器合位" {
		t.Fatalf("key=value 点名解析失败: %#v", res.Rows[0])
	}
}

func TestNoResolver(t *testing.T) {
	res := Parse([]byte(msgSOE), "", nil)
	if res.HasNames {
		t.Fatal("无点表时不应注入点名")
	}
	if containsStr(res.Columns, "点名") {
		t.Fatalf("无点表时不应出现点名列: %v", res.Columns)
	}
}

func TestUTF16BOM(t *testing.T) {
	// UTF-16LE BOM + "SOE,101\n"（验证编码解码不崩且可解析）
	raw := []byte{0xFF, 0xFE}
	for _, r := range "SOE,101\n" {
		raw = append(raw, byte(r), 0)
	}
	res := Parse(raw, "", resolver)
	if len(res.Rows) == 0 {
		t.Fatalf("UTF-16LE 内容应可解析: %+v", res)
	}
}
