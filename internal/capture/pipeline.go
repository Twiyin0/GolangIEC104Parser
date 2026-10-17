package capture

import (
	"fmt"
	"sort"
	"time"

	"iec104rt/internal/protocol"
)

// rawFrame 待解析的原始报文（带序号，用于保证输出顺序）。
type rawFrame struct {
	seq  uint64
	data []byte
	time string
	dir  string
}

// parsedResult 解析结果（乱序完成，由顺序保持器按 seq 重排）。
type parsedResult struct {
	seq   uint64
	frame protocol.Frame
	ok    bool
}

// 解析并发与队列参数：
//   - ParseQueueSize 是"等待解析的报文缓冲"（背压缓冲），**不是本次解析总量上限**；
//   - 队列满时丢弃并计数（绝不阻塞抓包），被丢弃的序号会被顺序保持器跳过。
const (
	defaultParseWorkers   = 16
	maxParseWorkers       = 256
	defaultParseQueueSize = 4096
)

// startWorkers 启动解析工作协程池 + 顺序保持器：
// 抓包线程只做 TCP 解析/流重组/切帧，原始报文投递有界队列，
// 多协程并行解析（不同长度报文耗时不同），解析结果按抓包顺序输出。
func (e *Engine) startWorkers() {
	workers := e.cfg.ParseWorkers
	if workers <= 0 {
		workers = defaultParseWorkers
	}
	if workers > maxParseWorkers {
		workers = maxParseWorkers
	}
	qsize := e.cfg.ParseQueueSize
	if qsize <= 0 {
		qsize = defaultParseQueueSize
	}
	e.parseCh = make(chan rawFrame, qsize)
	e.parseResCh = make(chan parsedResult, qsize)
	e.pending = map[uint64]parsedResult{}
	e.skipped = map[uint64]bool{}
	e.nextEmit = 1
	e.workers = workers

	for i := 0; i < workers; i++ {
		e.parseWG.Add(1)
		go e.parseWorker()
	}
	e.orderWG.Add(1)
	go e.orderLoop()
	e.status("engine", fmt.Sprintf("解析管线已启动：并发 %d，队列缓冲 %d（并行解析、按序输出）", workers, qsize))
}

func (e *Engine) parseWorker() {
	defer e.parseWG.Done()
	for rf := range e.parseCh {
		if e.testParseDelay != nil {
			if d := e.testParseDelay(rf.data); d > 0 {
				time.Sleep(d) // 仅测试用：模拟不同报文解析耗时差异
			}
		}
		f, err := protocol.ParseAuto(rf.data)
		if err != nil {
			e.parseResCh <- parsedResult{seq: rf.seq, ok: false}
			continue
		}
		f.Time = rf.time
		f.Dir = rf.dir
		e.parseResCh <- parsedResult{seq: rf.seq, frame: f, ok: true}
	}
}

// orderLoop 顺序保持器：按入队序号依次输出解析结果（并发完成、顺序不乱）。
func (e *Engine) orderLoop() {
	defer e.orderWG.Done()
	for res := range e.parseResCh {
		e.pending[res.seq] = res
		e.drainInOrder()
	}
	// 通道关闭后按序号排空残余（正常情况下已全部输出）
	keys := make([]uint64, 0, len(e.pending))
	for k := range e.pending {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, k := range keys {
		if r, ok := e.pending[k]; ok && r.ok && e.onFrame != nil {
			e.onFrame(r.frame)
		}
	}
	e.pending = map[uint64]parsedResult{}
}

// drainInOrder 依次输出已就绪的连续序号（跳过被丢弃的序号）。
func (e *Engine) drainInOrder() {
	for {
		if e.isSkipped(e.nextEmit) {
			e.clearSkipped(e.nextEmit)
			e.nextEmit++
			continue
		}
		r, ok := e.pending[e.nextEmit]
		if !ok {
			return // 该序号尚未解析完成，等待
		}
		delete(e.pending, e.nextEmit)
		if r.ok && e.onFrame != nil {
			e.onFrame(r.frame)
		}
		e.nextEmit++
	}
}

func (e *Engine) markSkipped(seq uint64) {
	if e.skipped == nil {
		return
	}
	e.skipMu.Lock()
	e.skipped[seq] = true
	e.skipMu.Unlock()
}

func (e *Engine) isSkipped(seq uint64) bool {
	if e.skipped == nil {
		return false
	}
	e.skipMu.Lock()
	defer e.skipMu.Unlock()
	return e.skipped[seq]
}

func (e *Engine) clearSkipped(seq uint64) {
	if e.skipped == nil {
		return
	}
	e.skipMu.Lock()
	delete(e.skipped, seq)
	e.skipMu.Unlock()
}

// enqueue 投递原始报文到解析队列（非阻塞：队列满则丢弃并计数，绝不阻塞抓包循环）。
func (e *Engine) enqueue(raw []byte, capTime, dir string) {
	if e.parseCh == nil {
		return
	}
	seq := e.seqCounter.Add(1)
	cp := make([]byte, len(raw))
	copy(cp, raw)
	select {
	case e.parseCh <- rawFrame{seq: seq, data: cp, time: capTime, dir: dir}:
	default:
		e.droppedFrames.Add(1)
		e.markSkipped(seq) // 通知顺序保持器跳过该序号（保持后续顺序不乱）
	}
}

// QueueLen 当前解析队列积压长度（等待解析的报文数，非总量上限）。
func (e *Engine) QueueLen() int {
	if e.parseCh == nil {
		return 0
	}
	return len(e.parseCh)
}

// PendingLen 顺序保持器中待重排的结果数。
func (e *Engine) PendingLen() int {
	e.skipMu.Lock()
	defer e.skipMu.Unlock()
	return len(e.pending)
}

// DroppedFrames 因解析队列满被丢弃的报文数（供界面提示）。
func (e *Engine) DroppedFrames() int64 { return e.droppedFrames.Load() }

// stopWorkers 关闭解析管线：先结束工作协程，再排空顺序保持器（已入队报文不丢、顺序不变）。
func (e *Engine) stopWorkers() {
	if e.parseCh != nil {
		close(e.parseCh)
		e.parseWG.Wait()
		close(e.parseResCh)
		e.orderWG.Wait()
		e.parseCh = nil
		e.parseResCh = nil
	}
}

