package main

import (
	"testing"

	"iec104rt/internal/protocol"
)


// TestUpdateConfigKeepsSession 回归：UpdateConfig 不得替换会话对象
//（此前会重建会话 → 抓包引擎仍写旧会话、查询读新会话，表现为"数据量大后突然不解析"）。
func TestUpdateConfigKeepsSession(t *testing.T) {
	a := NewApp()
	f, err := protocol.ParseHexAuto("68 04 07 00 00 00")
	if err != nil {
		t.Fatal(err)
	}
	a.sess.Add(f)
	if got := a.GetStats()["total"]; got != 1 {
		t.Fatalf("初始统计异常: %d", got)
	}

	cfg := a.GetConfig()
	cfg.UI.AutoScroll = false
	a.UpdateConfig(cfg) // 勾选自动滚动/切主题等都会走这里

	if got := a.GetStats()["total"]; got != 1 {
		t.Fatalf("UpdateConfig 后会话统计被重置（会话被替换）: total=%d", got)
	}
	frames := a.GetFrames(0, 10)
	if len(frames) != 1 {
		t.Fatalf("UpdateConfig 后应仍能取到报文: %d", len(frames))
	}
	if got := a.GetFrame(frames[0].ID); got.Type == "" {
		t.Fatal("UpdateConfig 后按 ID 取报文失败（会话被替换）")
	}

	// 缓冲上限就地生效，会话对象保持不变
	cfg.Session.MaxFrames = 1
	a.UpdateConfig(cfg)
	if a.sess == nil {
		t.Fatal("会话对象不应为 nil")
	}
	if got := a.GetStats()["count"]; got != 1 {
		t.Fatalf("MaxFrames 调整后 count=%d", got)
	}
}
