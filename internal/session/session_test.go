package session

import (
	"testing"

	"iec104rt/internal/protocol"
)

// TestSessionSetMax 缓冲上限可就地调整（不重建会话）。
func TestSessionSetMax(t *testing.T) {
	s := New(10)
	for i := 0; i < 5; i++ {
		if !s.Add(protocol.Frame{}) {
			t.Fatalf("第 %d 帧入库失败", i)
		}
	}
	if got := s.Stats()["count"]; got != 5 {
		t.Fatalf("count = %d（应 5）", got)
	}
	s.SetMax(3)
	if got := s.Stats()["count"]; got != 3 {
		t.Fatalf("SetMax(3) 后 count = %d（应裁剪到 3）", got)
	}
	s.SetMax(100)
	if !s.Add(protocol.Frame{}) {
		t.Fatal("调大上限后 Add 失败")
	}
	if got := s.Stats()["count"]; got != 4 {
		t.Fatalf("调大上限后 count = %d（应 4）", got)
	}
	// 非法值应被忽略（保持原上限）
	s.SetMax(0)
	if got := s.Stats()["count"]; got != 4 {
		t.Fatalf("SetMax(0) 不应影响数据: %d", got)
	}
}

// TestSessionPausedDrop 暂停 + 丢弃模式：暂停期间不入库。
func TestSessionPausedDrop(t *testing.T) {
	s := New(10)
	s.SetPaused(true, true)
	if s.Add(protocol.Frame{}) {
		t.Fatal("暂停+丢弃模式下不应入库")
	}
	s.SetPaused(false, false)
	if !s.Add(protocol.Frame{}) {
		t.Fatal("恢复后应入库")
	}
}
