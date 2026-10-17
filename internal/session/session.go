// Package session 会话管理：报文环形缓冲（有界）、暂停语义、统计、索引查询。
// 暂停语义（Goal.md §4-15）：暂停=界面冻结；默认后台继续缓冲不丢数据，
// 可配置"暂停时丢弃"。
package session

import (
	"sync"

	"iec104rt/internal/protocol"
)

type Session struct {
	mu       sync.RWMutex
	frames   []protocol.Frame
	max      int
	nextID   int64
	paused   bool
	dropMode bool
	total    int64
	droppedP int64
	droppedO int64
}

func New(maxFrames int) *Session {
	if maxFrames <= 0 {
		maxFrames = 50000
	}
	return &Session{max: maxFrames}
}

// Add 追加一帧（分配递增 ID）。返回是否入库。
func (s *Session) Add(f protocol.Frame) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total++
	if s.paused && s.dropMode {
		s.droppedP++
		return false
	}
	s.nextID++
	f.ID = s.nextID
	s.frames = append(s.frames, f)
	if len(s.frames) > s.max { // 环形上限，丢最旧
		overflow := len(s.frames) - s.max
		s.frames = s.frames[overflow:]
		s.droppedO += int64(overflow)
	}
	return true
}

// SnapshotFrom 增量拉取：ID 大于 fromID 的帧（limit 上限）。
func (s *Session) SnapshotFrom(fromID int64, limit int) []protocol.Frame {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	// 二分定位
	lo, hi := 0, len(s.frames)
	for lo < hi {
		mid := (lo + hi) / 2
		if s.frames[mid].ID <= fromID {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo >= len(s.frames) {
		return nil
	}
	n := limit
	if lo+n > len(s.frames) {
		n = len(s.frames) - lo
	}
	out := make([]protocol.Frame, n)
	copy(out, s.frames[lo:lo+n])
	return out
}

// Get 按 ID 精确取帧（点击报文渲染）。
func (s *Session) Get(id int64) (protocol.Frame, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.frames {
		if s.frames[i].ID == id {
			return s.frames[i], true
		}
	}
	return protocol.Frame{}, false
}

// All 全量导出快照。
func (s *Session) All() []protocol.Frame {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]protocol.Frame, len(s.frames))
	copy(out, s.frames)
	return out
}

// Stats 统计信息。
func (s *Session) Stats() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]int64{
		"count":    int64(len(s.frames)),
		"total":    s.total,
		"droppedP": s.droppedP,
		"droppedO": s.droppedO,
	}
}

// SetPaused 设置暂停/继续（dropMode=暂停时丢弃）。
func (s *Session) SetPaused(p bool, dropMode bool) {
	s.mu.Lock()
	s.paused = p
	s.dropMode = dropMode
	s.mu.Unlock()
}

// Paused 当前是否暂停。
func (s *Session) Paused() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.paused
}

// SetMax 调整环形缓冲上限（不重建会话，保证抓包引擎持有的引用始终有效）。
func (s *Session) SetMax(maxFrames int) {
	if maxFrames <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.max = maxFrames
	if len(s.frames) > s.max {
		overflow := len(s.frames) - s.max
		s.frames = s.frames[overflow:]
		s.droppedO += int64(overflow)
	}
}

// Clear 清空缓冲（统计归零，ID 序号继续递增）。
func (s *Session) Clear() {
	s.mu.Lock()
	s.frames = nil
	s.total = 0
	s.droppedP = 0
	s.droppedO = 0
	s.mu.Unlock()
}
