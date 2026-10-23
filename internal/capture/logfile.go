package capture

import (
	"fmt"
	"strings"
	"time"

	"iec104rt/internal/logfile"
	"iec104rt/internal/protocol"
	"iec104rt/internal/protocol/iec104"
)

// LogStats 日志监听统计（供前端状态栏/错误提示）。
type LogStats struct {
	Running   bool   `json:"running"`
	Mode      string `json:"mode"`
	File      string `json:"file"`
	Lines     int64  `json:"lines"`
	Frames    int64  `json:"frames"`
	Ignored   int64  `json:"ignored"`
	Bad       int64  `json:"bad"` // 不完整/无法识别的报文行
	Offset    int64  `json:"offset"`
	LastError string `json:"lastError"`
}

// StartLog 启动日志监听（本机文件 / SSH tail -F / SFTP / SCP / FTP）。
func StartLog(cfg Config, onFrame FrameFunc, onStatus StatusFunc) (*Engine, error) {
	proto := strings.ToLower(strings.TrimSpace(cfg.LogProtocol))
	if proto == "" {
		proto = "local"
	}
	if strings.TrimSpace(cfg.LogPath) == "" {
		return nil, fmt.Errorf("请先在 Connection 中配置日志文件路径")
	}
	lp, err := logfile.NewParser(cfg.LogFormatRe)
	if err != nil {
		return nil, err
	}
	e := &Engine{
		cfg: cfg, onFrame: onFrame, onStatus: onStatus,
		sourceKind: "log", stopCh: make(chan struct{}), logParser: lp,
	}
	e.running.Store(true)
	e.startWorkers()

	switch proto {
	case "local":
		if err := e.startLocalLog(cfg); err != nil {
			e.Stop()
			return nil, err
		}
	case "ssh", "scp":
		if err := e.startSSHLog(proto == "scp"); err != nil {
			e.Stop()
			return nil, err
		}
	case "sftp":
		if err := e.startSFTPLog(); err != nil {
			e.Stop()
			return nil, err
		}
	case "ftp":
		if err := e.startFTPLog(); err != nil {
			e.Stop()
			return nil, err
		}
	default:
		e.Stop()
		return nil, fmt.Errorf("不支持的日志协议: %s", proto)
	}
	return e, nil
}

// startLocalLog 本机文件监听。
func (e *Engine) startLocalLog(cfg Config) error {
	lr, err := logfile.NewReader(cfg.LogPath, cfg.LogEncoding)
	if err != nil {
		return err
	}
	e.logReader = lr
	mode := strings.ToLower(strings.TrimSpace(cfg.LogWatchMode))
	if mode == "static" {
		if err := e.readLog(true); err != nil {
			return err
		}
		e.status("log", fmt.Sprintf("日志静态监听已就绪：%s（点击“更新”读取增量）", cfg.LogPath))
		return nil
	}
	tail := cfg.LogTailBytes
	if tail <= 0 {
		tail = 65536
	}
	if err := lr.StartFromTail(int64(tail)); err != nil {
		return err
	}
	e.status("log", fmt.Sprintf("日志动态监听已启动：%s（回溯 %d 字节）", cfg.LogPath, tail))
	e.wg.Add(1)
	go e.logLoop()
	return nil
}

// UpdateLog 静态监听：读取自上次位置以来的新增内容（重复点击不会重复解析）。
func (e *Engine) UpdateLog() error {
	if e.logReader != nil {
		return e.readLog(true)
	}
	if e.remoteKind != "" {
		return e.syncRemote(true)
	}
	return fmt.Errorf("当前不是日志监听模式")
}

// LogStats 返回日志监听统计。
func (e *Engine) LogStats() LogStats {
	e.logMu.Lock()
	defer e.logMu.Unlock()
	st := LogStats{
		Running: e.running.Load(), Mode: e.cfg.LogWatchMode, File: e.cfg.LogPath,
		Lines: e.logLines, Frames: e.logFrames, Ignored: e.logIgnored,
		Bad: e.logBad, LastError: e.logErr,
	}
	if e.logReader != nil {
		st.Offset = e.logReader.Offset()
	} else if e.remoteKind != "" {
		e.remoteMu.Lock()
		st.Offset = e.remoteOffset
		e.remoteMu.Unlock()
	}
	return st
}

func (e *Engine) logLoop() {
	defer e.wg.Done()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	lastStatus := time.Now()
	var lastLines, lastFrames int64
	for {
		select {
		case <-e.stopCh:
			_ = e.readLog(true)
			return
		case <-ticker.C:
			if err := e.readLog(false); err != nil {
				e.setLogErr(err.Error())
				e.status("log", "日志读取失败: "+err.Error())
			}
			if time.Since(lastStatus) >= 2*time.Second {
				st := e.LogStats()
				if st.Lines != lastLines || st.Frames != lastFrames {
					lastLines, lastFrames = st.Lines, st.Frames
					e.status("log", fmt.Sprintf("日志监听: 已读 %d 行，解析 %d 帧，忽略 %d 行",
						st.Lines, st.Frames, st.Ignored))
				}
				lastStatus = time.Now()
			}
		}
	}
}

// readLog 读取新增行并投递解析；flush=true 时处理文件末尾无换行的最后一行。
func (e *Engine) readLog(flush bool) error {
	lines, err := e.logReader.ReadNew(flush)
	if err != nil {
		e.setLogErr(err.Error())
		return err
	}
	e.ingestLines(lines)
	return nil
}

// ingestLines 解析日志行并投递到现有解析管线。
func (e *Engine) ingestLines(lines []string) {
	if len(lines) == 0 {
		return
	}
	now := time.Now().Format("2006-01-02 15:04:05.000")
	for _, line := range lines {
		e.ingestLine(line, now)
	}
}

func (e *Engine) ingestLine(line, now string) {
	e.logMu.Lock()
	e.logLines++
	e.logMu.Unlock()
	rec, ok := e.logParser.Parse(line)
	if !ok {
		e.logMu.Lock()
		e.logIgnored++
		e.logMu.Unlock()
		return
	}
	buf, err := iec104.HexToBytes(rec.Message)
	if err != nil || len(buf) == 0 {
		e.logMu.Lock()
		e.logIgnored++
		e.logMu.Unlock()
		return
	}
	if chk := protocol.CheckFrame(buf); !chk.Complete {
		e.logMu.Lock()
		e.logBad++
		e.logIgnored++
		e.logErr = chk.Reason
		e.logMu.Unlock()
		return
	}
	ts := rec.Time
	if ts == "" {
		ts = now
	}
	forced := ""
	switch strings.ToLower(strings.TrimSpace(e.cfg.LogProto)) {
	case "101":
		forced = "101"
	case "104":
		forced = "104"
	}
	e.enqueueProto(buf, ts, rec.Dir, forced)
	e.logMu.Lock()
	e.logFrames++
	e.logMu.Unlock()
}

func (e *Engine) setLogErr(s string) {
	e.logMu.Lock()
	e.logErr = s
	e.logMu.Unlock()
}

// clearLogErr 连接恢复后清除上一次连接错误（状态栏红色提示随之消失）。
func (e *Engine) clearLogErr() {
	e.logMu.Lock()
	e.logErr = ""
	e.logMu.Unlock()
}
