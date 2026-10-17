package protocol

import (
	"testing"

	"iec104rt/internal/protocol/common"
)

// TestParse104ParamSlots 定值参数条目必须带序号（此前缺失导致界面 # 显示 undefined）。
func TestParse104ParamSlots(t *testing.T) {
	// TI=202 读响应：SN(2)+PI(1)+[地址(3)+Tag(1)+Len(1)+值]×2
	asdu := []byte{202, 1, 7, 0, 1, 0,
		0x01, 0x00, // SN=1
		0x80,                   // PI
		0x65, 0x00, 0x00, 0x2B, 0x02, 0x0A, 0x00, // addr=0x65 tag=小整形 len=2 val=10
		0x66, 0x00, 0x00, 0x2B, 0x02, 0x14, 0x00, // addr=0x66 val=20
	}
	f, err := ParseAuto(build104(ctrlI, asdu))
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != "param" {
		t.Fatalf("expect param, got %v", f.Type)
	}
	data, _ := f.Parsed["data"].([]common.M)
	if len(data) != 2 {
		t.Fatalf("expect 2 param items, got %d: %#v", len(data), f.Parsed)
	}
	if data[0]["slot"] != 1 || data[1]["slot"] != 2 {
		t.Fatalf("定值条目序号缺失/错误: %#v", data)
	}
	if data[0]["addr"] != "0x000065" || data[1]["addr"] != "0x000066" {
		t.Fatalf("定值地址解析错误: %#v", data)
	}
	if v, _ := data[1]["value"].(int); v != 20 {
		t.Fatalf("定值 TLV 值错误: %#v", data[1])
	}
}
