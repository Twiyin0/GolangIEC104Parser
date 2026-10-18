package fileparse

import (
	"encoding/binary"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
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

// 国内工程文件常见 GBK 编码（XML 声明也可能是 GBK）：应先转 UTF-8 再解析。
func TestGBKXML(t *testing.T) {
	src := `<?xml version="1.0" encoding="GBK"?><root><Record><Point>101</Point><State>合</State></Record></root>`
	gbk, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	res := Parse(gbk, "xml", resolver)
	if res.Format != "xml" || len(res.Rows) != 1 {
		t.Fatalf("GBK XML 解析失败: %+v", res)
	}
	if res.Rows[0]["State"] != "合" || res.Rows[0]["点名"] != "断路器合位" {
		t.Fatalf("GBK 字段/点名解析错误: %#v", res.Rows[0])
	}
}

// 规约 B.3.2 SOE XML：表头/顺序应为 信息体地址、点名、值、时间。
func TestSOEXMLNamedColumns(t *testing.T) {
	src := `<DataFile><Header fileType="SOE"/><DataRec>
	  <DI ioa="101" tm="2026-10-18_12:00:00.000" val="0"/>
	  <DI ioa="201" tm="2026-10-18_12:00:01.000" val="1"/>
	</DataRec></DataFile>`
	res := Parse([]byte(src), "xml", resolver)
	if len(res.Rows) != 2 {
		t.Fatalf("SOE rows=%d: %+v", len(res.Rows), res)
	}
	want := []string{"信息体地址", "点名", "值", "时间"}
	for i := range want {
		if res.Columns[i] != want[i] {
			t.Fatalf("SOE columns=%v", res.Columns)
		}
	}
	if res.Rows[0]["点名"] != "断路器合位" || res.Rows[1]["点名"] != "I段10kV进线电压" {
		t.Fatalf("SOE 点名错位: %#v", res.Rows)
	}
}

// 遥控 XML：cmd 的 select/oper 标识应转成中文“选择/执行”。
func TestCOXMLChineseOperation(t *testing.T) {
	src := `<DataFile><Header fileType="CO"/><DataRec>
	  <DI ioa="101" cmd="select" val="0" tm="t1"/>
	  <DI ioa="101" cmd="oper" val="1" tm="t2"/>
	</DataRec></DataFile>`
	res := Parse([]byte(src), "xml", resolver)
	if len(res.Rows) != 2 {
		t.Fatalf("CO rows=%d", len(res.Rows))
	}
	if res.Rows[0]["操作"] != "选择" || res.Rows[1]["操作"] != "执行" {
		t.Fatalf("操作未中文化: %#v", res.Rows)
	}
	if res.Rows[0]["命令状态"] != "0" || res.Rows[1]["命令状态"] != "1" {
		t.Fatalf("命令状态应保留原值: %#v", res.Rows)
	}
}

// MSG 遥控记录同样应中文化表头，并把 select 转成“选择”。
func TestMSGCOChineseColumns(t *testing.T) {
	text := "遥控操作记录\nioa,cmd,val,tm\n101,select,0,2026-10-18 12:00:00.000\n"
	res := Parse([]byte(text), "msg", resolver)
	if res.Kind != "control" || len(res.Rows) != 1 {
		t.Fatalf("MSG CO 分类/行错误: %+v", res)
	}
	want := []string{"信息体地址", "点名", "操作", "命令状态", "时间"}
	for i := range want {
		if res.Columns[i] != want[i] {
			t.Fatalf("MSG CO columns=%v", res.Columns)
		}
	}
	if res.Rows[0]["操作"] != "选择" || res.Rows[0]["点名"] != "断路器合位" {
		t.Fatalf("MSG CO 内容错误: %#v", res.Rows[0])
	}
}

// MSG SOE：表头顺序固定为 信息体地址、点名、值、时间。
func TestMSGSOEOrder(t *testing.T) {
	text := "SOE事件记录\nioa,val,tm\n101,0,2026-10-18 12:00:00.000\n"
	res := Parse([]byte(text), "msg", resolver)
	if len(res.Rows) != 1 {
		t.Fatalf("MSG SOE 行错误: %+v", res)
	}
	want := []string{"信息体地址", "点名", "值", "时间"}
	for i := range want {
		if res.Columns[i] != want[i] {
			t.Fatalf("MSG SOE columns=%v", res.Columns)
		}
	}
	if res.Rows[0]["点名"] != "断路器合位" {
		t.Fatalf("MSG SOE 点名错误: %#v", res.Rows[0])
	}
}

// 极值 XML：DataAttr 与 DataRec 按同一顺序合并，不能把 float/A 当成独立列。
func TestEXVXMLAligned(t *testing.T) {
	src := `<DataFile><Header fileType="EXV"/>
	  <DataAttr num="2"><DI ioa="101" type="float" unit="A"/><DI ioa="201" type="float" unit="V"/></DataAttr>
	  <DataRec><DI max="10.5" max_tm="2026-10-18_12:00:00" min="1.5" min_tm="2026-10-18_01:00:00"/>
	          <DI max="220.1" max_tm="2026-10-18_13:00:00" min="219.9" min_tm="2026-10-18_02:00:00"/></DataRec>
	</DataFile>`
	res := Parse([]byte(src), "xml", resolver)
	if len(res.Rows) != 2 {
		t.Fatalf("EXV rows=%d", len(res.Rows))
	}
	if res.Rows[0]["信息体地址"] != "101" || res.Rows[0]["点名"] != "断路器合位" ||
		res.Rows[0]["单位"] != "A" || res.Rows[0]["最大值"] != "10.5" {
		t.Fatalf("EXV 第一行错位: %#v", res.Rows[0])
	}
	if res.Rows[1]["最大值"] != "220.1" || res.Rows[1]["最小值"] != "219.9" {
		t.Fatalf("EXV 第二行错位: %#v", res.Rows[1])
	}
	for _, c := range res.Columns {
		if c == "float" || c == "A" {
			t.Fatalf("EXV 表头不应出现原始属性列: %v", res.Columns)
		}
	}
}

// 定点 XML：每个 DataRec 一节，点值按 DataAttr 顺序展开成矩阵。
func TestFIXPTXMLAligned(t *testing.T) {
	src := `<DataFile><Header fileType="FIXPT"/>
	  <DataAttr dataNum="2" sectNum="2" interval="15">
	    <DI ioa="101" type="float" unit="A"/><DI ioa="201" type="float" unit="V"/>
	  </DataAttr>
	  <DataRec sect="1" tm="261018_120000"><DI val="1.1"/><DI val="10.1"/></DataRec>
	  <DataRec sect="2" tm="261018_121500"><DI val="1.2"/><DI val="10.2"/></DataRec>
	</DataFile>`
	res := Parse([]byte(src), "xml", resolver)
	if len(res.Rows) != 4 {
		t.Fatalf("FIXPT rows=%d", len(res.Rows))
	}
	want := []string{"节数", "地址", "遥测名称", "遥测值", "时间"}
	if len(res.Columns) != len(want) {
		t.Fatalf("FIXPT 列数错误: %v", res.Columns)
	}
	for i := range want {
		if res.Columns[i] != want[i] {
			t.Fatalf("FIXPT columns=%v", res.Columns)
		}
	}
	if res.Rows[0]["节数"] != "1" || res.Rows[0]["地址"] != "65" ||
		res.Rows[0]["遥测名称"] != "断路器合位" || res.Rows[0]["遥测值"] != "1.1" ||
		res.Rows[0]["时间"] != "2026-10-18 12:00:00" {
		t.Fatalf("FIXPT 第一节第一点错误: %#v", res.Rows[0])
	}
	if res.Rows[1]["地址"] != "c9" || res.Rows[1]["遥测值"] != "10.1" {
		t.Fatalf("FIXPT 第一节第二点错位: %#v", res.Rows[1])
	}
	if res.Rows[2]["节数"] != "2" || res.Rows[2]["遥测值"] != "1.2" ||
		res.Rows[2]["时间"] != "2026-10-18 12:15:00" {
		t.Fatalf("FIXPT 第二节错位: %#v", res.Rows[2])
	}
}

const testCFG = `TEST,DEV,1999
4,2
1,IA,A,,A,1,0,0,-32768,32767,1,1,P
2,IB,B,,A,1,0,0,-32768,32767,1,1,P
3,TRIP,,,0,0,0,0,0,1
4,CLOSE,,,0,0,0,0,0,1
1
1000,3
01/01/2026,00:00:00.000000
01/01/2026,00:00:00.000000
ASCII
1
`

const testDAT = `1,0,0,0,0,0
2,1000,1000,2000,1,0
3,2000,-1000,500,0,1
`

func TestComtradeCFG(t *testing.T) {
	_, res, err := ParseComtradeCFG([]byte(testCFG))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 2 || res.Columns[0] != "通道号" || res.Columns[1] != "通道名称" {
		t.Fatalf("CFG 表头/行错误: %+v", res)
	}
	if res.Rows[0]["通道名称"] != "IA" || res.Rows[0]["单位"] != "A" {
		t.Fatalf("CFG 通道错位: %#v", res.Rows[0])
	}
}

func TestComtradeWaveform(t *testing.T) {
	res, err := ParseComtrade([]byte(testCFG), []byte(testDAT))
	if err != nil {
		t.Fatal(err)
	}
	c := res.Chart
	if c == nil || len(c.Channels) != 2 || len(c.Times) != 3 {
		t.Fatalf("波形数据错误: %+v", c)
	}
	if c.Times[1] < 0.0009 || c.Times[1] > 0.0011 {
		t.Fatalf("时间轴错误: %v", c.Times)
	}
	if c.Channels[0].Name != "IA" || c.Channels[0].Data[1] != 1000 || c.Channels[0].Data[2] != -1000 {
		t.Fatalf("IA 波形错误: %+v", c.Channels[0])
	}
	if c.Channels[1].Data[1] != 2000 || c.Channels[1].Unit != "A" {
		t.Fatalf("IB 波形错误: %+v", c.Channels[1])
	}
	if len(c.Digital) != 2 || c.Digital[0].Data[1] != 1 {
		t.Fatalf("开关量错误: %+v", c.Digital)
	}
}

// 二进制 DAT：4 字节采样号 + 4 字节时标 + int16 模拟量 + 16 位开关量。
func TestComtradeBinaryDAT(t *testing.T) {
	cfg := "TEST,DEV,1999\n2,1\n1,IA,A,,A,1,0,0,-32768,32767,1,1,P\n2,TRIP,,,0,0,0,0,0,1\n1\n1000,2\n01/01/2026,00:00:00.000000\n01/01/2026,00:00:00.000000\nBINARY\n1\n"
	dat := make([]byte, 0, 24)
	put := func(sample, ts uint32, analog int16, digital uint16) {
		var b [12]byte
		binary.LittleEndian.PutUint32(b[0:], sample)
		binary.LittleEndian.PutUint32(b[4:], ts)
		binary.LittleEndian.PutUint16(b[8:], uint16(analog))
		binary.LittleEndian.PutUint16(b[10:], digital)
		dat = append(dat, b[:]...)
	}
	put(1, 0, 100, 1)
	put(2, 1000, -200, 0)
	res, err := ParseComtrade([]byte(cfg), dat)
	if err != nil {
		t.Fatal(err)
	}
	c := res.Chart
	if c == nil || len(c.Channels) != 1 || len(c.Channels[0].Data) != 2 {
		t.Fatalf("二进制 DAT 波形错误: %+v", c)
	}
	if c.Channels[0].Data[0] != 100 || c.Channels[0].Data[1] != -200 {
		t.Fatalf("二进制模拟量错误: %v", c.Channels[0].Data)
	}
	if len(c.Digital) != 1 || c.Digital[0].Data[0] != 1 || c.Digital[0].Data[1] != 0 {
		t.Fatalf("二进制开关量错误: %+v", c.Digital)
	}
}
