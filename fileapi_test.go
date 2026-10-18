package main

import (
	"encoding/hex"
	"strings"
	"testing"

	"iec104rt/internal/fileparse"
	"iec104rt/internal/protocol"
	"iec104rt/internal/protocol/common"
)

func newFileTestApp() *App {
	return &App{
		files:     map[string]*fileAcc{},
		fileNames: map[int]string{},
		fileCache: map[string]fileparse.Result{},
	}
}

func fileServiceFrame(p map[string]any) *protocol.Frame {
	return &protocol.Frame{Type: "file_service", Time: "2026-10-18 12:00:00.000", Parsed: p}
}

// 目录响应里的文件名后缀 → 预登记格式（soe.xml → xml，event.msg → msg）。
func TestFileDirectoryNameChoosesFormat(t *testing.T) {
	a := newFileTestApp()
	a.collectFileFrame(fileServiceFrame(map[string]any{
		"service": "F_FR_NA_1",
		"op":      2,
		"entries": []common.M{
			{"fileName": "soe.xml", "fileSize": 123},
			{"fileName": "event.msg", "fileSize": 45},
		},
	}))
	if acc, ok := a.files["210:name:soe.xml"]; !ok || acc.FormatHint != "xml" || acc.Size != 123 {
		t.Fatalf("soe.xml 预登记失败: %+v", acc)
	}
	if acc, ok := a.files["210:name:event.msg"]; !ok || acc.FormatHint != "msg" {
		t.Fatalf("event.msg 预登记失败: %+v", acc)
	}
}

// 文件分段未收满时返回“等待传输完成”；收满后按 .xml 后缀自动解析出记录。
func TestFileTransferWaitsForCompletionThenParsesXML(t *testing.T) {
	a := newFileTestApp()
	xmlText := `<?xml version="1.0"?><root><record addr="1"><value>10</value></record><record addr="2"><value>20</value></record></root>`
	size := len(xmlText)
	key := "210:name:soe.xml"

	a.collectFileFrame(fileServiceFrame(map[string]any{
		"service": "F_FR_NA_1", "op": 4, "fileId": 7, "fileName": "soe.xml", "fileSize": size,
	}))
	half := size / 2
	a.collectFileFrame(fileServiceFrame(map[string]any{
		"service": "F_FR_NA_1", "op": 5, "fileId": 7, "fileName": "soe.xml", "segmentNo": 0,
		"dataHex": hex.EncodeToString([]byte(xmlText[:half])),
	}))

	res, err := a.ParseFile(key, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Complete {
		t.Fatal("只收到一半数据时不应标记完成")
	}
	if res.Format != "xml" {
		t.Fatalf("应按文件名后缀使用 XML 格式，实际 %q", res.Format)
	}
	if !strings.Contains(res.Error, "传输未完成") {
		t.Fatalf("应提示等待传输完成，实际 %q", res.Error)
	}
	if len(res.Rows) != 0 {
		t.Fatalf("半包不应产出记录，实际 %d 条", len(res.Rows))
	}

	a.collectFileFrame(fileServiceFrame(map[string]any{
		"service": "F_FR_NA_1", "op": 5, "fileId": 7, "fileName": "soe.xml", "segmentNo": half,
		"dataHex": hex.EncodeToString([]byte(xmlText[half:])),
	}))
	if k := a.fileKeyOf(map[string]any{"service": "F_FR_NA_1", "op": 6, "fileId": 7}); k != key {
		t.Fatalf("op=6 等无文件名帧应按 fileId 找到同一文件名 key，实际 %q", k)
	}

	res, err = a.ParseFile(key, "")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Complete || res.Bytes != size || res.Size != size {
		t.Fatalf("传输状态错误: complete=%v bytes=%d size=%d", res.Complete, res.Bytes, res.Size)
	}
	if res.Format != "xml" || res.FileName != "soe.xml" || len(res.Rows) != 2 {
		t.Fatalf("XML 解析结果错误: %+v", res)
	}
}

// 规范现场报文：fileId 恒 0，op=5/6 不带文件名，靠 op=4 激活确认的文件名补全。
func TestFileTransferByNameFromActivation(t *testing.T) {
	a := newFileTestApp()
	xmlText := `<root><Record><Point>1</Point><State>合</State></Record></root>`
	size := len(xmlText)
	a.collectFileFrame(fileServiceFrame(map[string]any{
		"service": "F_FR_NA_1", "op": 4, "fileId": 0, "fileName": "soe.xml", "fileSize": size,
	}))
	seg := map[string]any{
		"service": "F_FR_NA_1", "op": 5, "fileId": 0, "segmentNo": 0, "contFlag": 1,
		"dataHex": hex.EncodeToString([]byte(xmlText)),
	}
	a.collectFileFrame(fileServiceFrame(seg))
	if seg["fileName"] != "soe.xml" {
		t.Fatalf("op=5 应补全当前文件名，实际 %#v", seg["fileName"])
	}
	if _, ok := a.files["210:name:soe.xml"]; !ok {
		t.Fatalf("文件应按名称归并，实际 keys=%v", func() []string {
			var ks []string
			for k := range a.files {
				ks = append(ks, k)
			}
			return ks
		}())
	}
	a.collectFileFrame(fileServiceFrame(map[string]any{
		"service": "F_FR_NA_1", "op": 6, "fileId": 0, "segmentNo": 0, "resultCode": 0,
	}))
	res, _ := a.ParseFile("210:name:soe.xml", "")
	if !res.Complete || len(res.Rows) != 1 {
		t.Fatalf("按激活确认文件名补全后应完成解析: %+v", res)
	}
}
// 规范 7.6.3.2 c) 后续标志=0 表示最后一段：即使没有 op=6 确认也应完成。
func TestFileTransferCompletesOnContFlagZero(t *testing.T) {
	a := newFileTestApp()
	a.collectFileFrame(fileServiceFrame(map[string]any{
		"service": "F_FR_NA_1", "op": 4, "fileId": 0, "fileName": "soe.xml", "fileSize": 100,
	}))
	a.collectFileFrame(fileServiceFrame(map[string]any{
		"service": "F_FR_NA_1", "op": 5, "fileId": 0, "segmentNo": 0, "contFlag": 0,
		"dataHex": hex.EncodeToString([]byte("<root><Record><Point>1</Point></Record></root>")),
	}))
	res, _ := a.ParseFile("210:name:soe.xml", "")
	if !res.Complete || len(res.Rows) != 1 {
		t.Fatalf("后续标志=0 应完成并解析: %+v", res)
	}
	if !strings.Contains(res.Error, "长度不符") {
		t.Fatalf("声明大小 100、实际 46 字节时应提示长度不符: %q", res.Error)
	}
}

// 未拿到文件大小时，靠 op=6 确认帧 resultCode=0（无后续）判定传输完成。
func TestFileTransferCompletesOnNoMore(t *testing.T) {
	a := newFileTestApp()
	key := "210:name:soe.xml"
	a.collectFileFrame(fileServiceFrame(map[string]any{
		"service": "F_FR_NA_1", "op": 5, "fileId": 9, "fileName": "soe.xml", "segmentNo": 0,
		"dataHex": hex.EncodeToString([]byte("<root><Record><Point>1</Point></Record></root>")),
	}))
	res, _ := a.ParseFile(key, "")
	if res.Complete {
		t.Fatal("未收到结束确认前不应完成")
	}
	a.collectFileFrame(fileServiceFrame(map[string]any{
		"service": "F_FR_NA_1", "op": 6, "fileId": 9, "resultCode": 0,
	}))
	res, _ = a.ParseFile(key, "")
	if !res.Complete || len(res.Rows) != 1 {
		t.Fatalf("op=6 无后续后应完成并解析: %+v", res)
	}
}

// .cfg/.dat 同名配对：cfg 先到只显示配置，dat 完成后自动出波形。
func TestComtradePairing(t *testing.T) {
	a := newFileTestApp()
	cfg := "TEST,DEV,1999\n4,2\n1,IA,A,,A,1,0,0,-32768,32767,1,1,P\n2,IB,B,,A,1,0,0,-32768,32767,1,1,P\n3,TRIP,,,0,0,0,0,0,1\n4,CLOSE,,,0,0,0,0,0,1\n1\n1000,3\n01/01/2026,00:00:00.000000\n01/01/2026,00:00:00.000000\nASCII\n1\n"
	dat := "1,0,0,0,0,0\n2,1000,1000,2000,1,0\n3,2000,-1000,500,0,1\n"
	add := func(name, data string) {
		a.files["210:name:"+name] = &fileAcc{
			Key: "210:name:" + name, Name: name, Size: len(data), MaxEnd: len(data), Bytes: len(data),
			Complete: true, Segments: map[int][]byte{0: []byte(data)},
		}
	}
	add("test.cfg", cfg)
	res, err := a.ParseFile("210:name:test.cfg", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Chart != nil || res.Complete {
		t.Fatalf("只有 cfg 时不应出波形: %+v", res)
	}
	if !strings.Contains(res.Error, ".dat") {
		t.Fatalf("应提示等待 .dat: %q", res.Error)
	}
	add("test.dat", dat)
	res, err = a.ParseFile("210:name:test.cfg", "")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Complete || res.Chart == nil || len(res.Chart.Channels) != 2 {
		t.Fatalf("cfg+dat 应生成双通道波形: %+v", res)
	}
}
