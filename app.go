package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"iec104rt/internal/capture"
	"iec104rt/internal/capture/wpcap"
	"iec104rt/internal/config"
	"iec104rt/internal/fileparse"
	"iec104rt/internal/pointdb"
	"iec104rt/internal/protocol"
	"iec104rt/internal/session"
)

// App Wails 绑定层：前后端桥 + 事件推送 + 引擎/会话生命周期。
type App struct {
	ctx    context.Context
	mu     sync.Mutex
	engine *capture.Engine
	sess   *session.Session
	cfg    config.Config

	paused   bool
	lastEmit time.Time
	emitBuf  int

	muMaster   sync.Mutex
	masterPort int

	muPoint sync.Mutex
	pointDB *pointdb.DB // 工程项目点表数据库（nil=未加载）

	muImported sync.Mutex
	imported   []protocol.Frame // 导入回放的报文（带 ID，供筛选后导出）

	muFile   sync.Mutex
	files    map[string]*fileAcc    // 文件服务组包（TI=210 分段 / TI=125 段落）
	fileNames map[int]string        // TI=210 fileId → 文件名（目录/激活/传输帧学习）
	currentFile210 string           // 当前 TI=210 传输文件名（op=5/6 规范不带文件名）
	currentFileSize210 int          // 当前 TI=210 文件大小（激活确认给出）
	fileCache map[string]fileparse.Result // 文件解析结果缓存（key|格式|点表版本）
	dbRev    int                    // 点表版本号（变更时使文件解析缓存失效）
}

// NewApp 创建应用（自动加载既有配置文件，缺失字段补默认值）。
func NewApp() *App {
	a := &App{cfg: config.Default(), sess: session.New(50000),
		files:     map[string]*fileAcc{},
		fileNames: map[int]string{},
		fileCache: map[string]fileparse.Result{},
	}
	if p, err := config.Path(); err == nil {
		if c, err := config.Load(p); err == nil {
			a.cfg = c
			a.sess = session.New(c.Session.MaxFrames)
		}
	}
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Wails 会用内置默认图标覆盖窗口图标 → 启动后用 exe 内嵌 logo 重新设置
	applyWindowIcon(func(msg string) {
		log.Println(msg)
		a.emit("status", map[string]any{"level": "engine", "text": msg,
			"time": time.Now().Format("15:04:05.000")})
	})
	// WebView2 在内容未获键盘焦点前不接收键盘事件（输入框点得进去却打不了字）→ 启动即移交焦点
	focusWebView(func(msg string) {
		log.Println(msg)
		a.emit("status", map[string]any{"level": "engine", "text": msg,
			"time": time.Now().Format("15:04:05.000")})
	})
}

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	e := a.engine
	cfg := a.cfg
	a.mu.Unlock()
	// 退出前落盘配置：保证"定值表选择/地址显示进制/主题"等设置下次启动仍生效
	a.saveConfigQuiet(cfg)
	if e != nil {
		e.Stop()
	}
}

func (a *App) emit(name string, data ...any) {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, name, data...)
	}
}

func (a *App) emitState() {
	a.emit("state", map[string]any{"running": a.IsRunning(), "paused": a.paused})
}

// ── 配置 ─────────────────────────────────────────────────────────────

// GetConfig 当前配置。
func (a *App) GetConfig() config.Config { return a.cfg }

// UpdateConfig 更新内存配置（UI/模式热应用；连接参数需重启监听生效）。
// 注意：**不得替换会话对象**——抓包引擎持有其引用，替换会导致新帧写入旧会话、
// 查询读到空会话（表现为"数据量变大后突然不解析"）。此处仅就地调整缓冲上限。
//
// 点表相关设置（转发基址 / 定值表）热应用：变化时重建映射并失效文件解析缓存，
// 使"参数类型/比例"等点名信息立即生效；且**入参 paramTable 为空时保留当前已选定的定值表**，
// 避免任意一次配置推送（主题/自动滚动/Connection 应用）把定值表清成"无"。
func (a *App) UpdateConfig(c config.Config) config.Config {
	c.Normalize()
	a.mu.Lock()
	prev := a.cfg.Parse
	if c.Session.MaxFrames > 0 {
		a.sess.SetMax(c.Session.MaxFrames)
	}
	a.cfg = c
	a.mu.Unlock()

	changed := false
	a.muPoint.Lock()
	d := a.pointDB
	if d != nil {
		if c.Parse.ParamTable == "" && d.ParamTable != "" {
			c.Parse.ParamTable = d.ParamTable // 自愈：保留已选定的定值表
			a.mu.Lock()
			a.cfg.Parse.ParamTable = d.ParamTable
			a.mu.Unlock()
		}
		if c.Parse.ParamTable != d.ParamTable {
			d.SetParamTable(c.Parse.ParamTable)
			changed = true
		}
		if c.Parse.YXTransferBase != prev.YXTransferBase || c.Parse.YCTransferBase != prev.YCTransferBase ||
			c.Parse.YKTransferBase != prev.YKTransferBase || c.Parse.ENTransferBase != prev.ENTransferBase {
			d.SetBases(c.Parse.YXTransferBase, c.Parse.YCTransferBase,
				c.Parse.YKTransferBase, c.Parse.ENTransferBase)
			changed = true
		}
	}
	a.muPoint.Unlock()
	if changed {
		a.bumpDBRev() // 点名/定值信息可能变化 → 文件解析缓存失效
	}
	a.mu.Lock()
	out := a.cfg
	a.mu.Unlock()
	return out
}

// saveConfigQuiet 静默保存配置（失败不影响主流程）。
func (a *App) saveConfigQuiet(c config.Config) {
	if p, err := config.Path(); err == nil {
		_ = config.Save(p, c)
	}
}

// SaveConfig File→SaveConfig：当前全部配置落盘。
func (a *App) SaveConfig() (string, error) {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	p, err := config.Path()
	if err != nil {
		return "", err
	}
	return p, config.Save(p, cfg)
}

// LoadConfig File→LoadConfig：加载配置文件并热应用。
func (a *App) LoadConfig() (config.Config, error) {
	p, err := config.Path()
	if err != nil {
		return a.cfg, err
	}
	c, err := config.Load(p)
	if err != nil {
		return a.cfg, err
	}
	return a.UpdateConfig(c), nil
}

// ── 设备枚举 ──────────────────────────────────────────────────────────

// DeviceInfo 网卡信息。
type DeviceInfo struct {
	Name string   `json:"name"`
	Desc string   `json:"desc"`
	IPs  []string `json:"ips"`
}

// ListDevices 网卡列表（Npcap 设备名 + IPv4）。
func (a *App) ListDevices() []DeviceInfo {
	devs, err := capture.Devices()
	if err != nil {
		return nil
	}
	out := make([]DeviceInfo, 0, len(devs))
	for _, d := range devs {
		out = append(out, DeviceInfo{Name: d.Name, Desc: d.Description, IPs: d.Addresses})
	}
	return out
}

// ListSerialPorts 串口列表。
func (a *App) ListSerialPorts() []string {
	ports, _ := capture.SerialPorts()
	return ports
}

// PcapInfo Npcap 版本信息。
func (a *App) PcapInfo() string { return capture.PcapVersion() }

// HasNpcap 是否检测到 Npcap 运行时（wpcap.dll 已加载）。
func (a *App) HasNpcap() bool { return wpcap.Available() }

// OpenNpcapDownload 打开 Npcap 官方下载页（免费版由用户自行安装）。
func (a *App) OpenNpcapDownload() error {
	if a.ctx == nil {
		return fmt.Errorf("应用未就绪")
	}
	wruntime.BrowserOpenURL(a.ctx, "https://npcap.com/#download")
	return nil
}

// ── 监听控制 ──────────────────────────────────────────────────────────

// StartCapture 开始监听（网口旁听/串口，按 Connection 配置）。
func (a *App) StartCapture() error {
	a.mu.Lock()
	if a.engine != nil {
		a.mu.Unlock()
		return fmt.Errorf("监听已在运行中，请先停止")
	}
	cfg := a.cfg
	a.mu.Unlock()

	ccfg := capture.Config{
		Mode:         cfg.Connection.Mode,
		Device:       cfg.Connection.Device,
		TerminalIP:   cfg.Connection.TerminalIP,
		TerminalPort: cfg.Connection.TerminalPort,
		MasterIP:     cfg.Connection.MasterIP,
		MasterPort:   cfg.Connection.MasterPort,
		Promisc:      cfg.Connection.Promisc,
		SerialMode:   cfg.Connection.SerialMode,
		BaudRate:     cfg.Connection.BaudRate,
		DataBits:     cfg.Connection.DataBits,
		Parity:       cfg.Connection.Parity,
		StopBits:      cfg.Connection.StopBits,
		LinkAddr:      cfg.Connection.LinkAddr,
		ParseWorkers:  cfg.Session.ParseWorkers,
		ParseQueueSize: cfg.Session.ParseQueueSize,
	}
	// 会话引用动态获取（不捕获局部变量）：即使今后任何代码路径调整了会话，
	// 报文也会写入当前会话，避免"引擎写旧会话、查询读新会话"的错位。
	onFrame := func(f protocol.Frame) {
		a.mu.Lock()
		cur := a.sess
		a.mu.Unlock()
		a.collectFileFrame(&f) // 文件服务帧 → 内容组包（供 msg/xml 文件解析）
		if cur != nil && cur.Add(f) {
			a.throttledNotify()
		}
	}
	onStatus := func(level, text string) {
		a.emit("status", map[string]any{"level": level, "text": text,
			"time": time.Now().Format("15:04:05.000")})
	}
	onMaster := func(port int) {
		a.muMaster.Lock()
		a.masterPort = port
		a.muMaster.Unlock()
		a.emit("master", map[string]any{"port": port})
	}
	var eng *capture.Engine
	var err error
	if ccfg.Mode == "serial" {
		eng, err = capture.StartSerial(ccfg, onFrame, onStatus)
	} else {
		eng, err = capture.StartNet(ccfg, onFrame, onStatus, onMaster)
	}
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.engine = eng
	a.mu.Unlock()
	a.emitState()
	return nil
}

// throttledNotify 节流通知前端有新帧（前端增量拉取，防高流量刷爆事件）。
func (a *App) throttledNotify() {
	a.mu.Lock()
	a.emitBuf++
	since := time.Since(a.lastEmit)
	n := a.emitBuf
	if since < 100*time.Millisecond && n < 500 {
		a.mu.Unlock()
		return
	}
	a.emitBuf = 0
	a.lastEmit = time.Now()
	a.mu.Unlock()
	a.emit("frames:new", map[string]any{"count": n})
}

// StopCapture 停止监听（已缓冲报文保留可查/可导出）。
func (a *App) StopCapture() {
	a.mu.Lock()
	e := a.engine
	a.engine = nil
	a.mu.Unlock()
	if e != nil {
		go e.Stop()
	}
	a.emitState()
}

// PauseCapture 暂停监听：界面冻结；默认后台继续缓冲不丢数据（可配置暂停时丢弃）。
func (a *App) PauseCapture() {
	a.mu.Lock()
	a.paused = true
	a.sess.SetPaused(true, a.cfg.UI.DropWhenPaused)
	a.mu.Unlock()
	a.emitState()
}

// ResumeCapture 继续监听：追平并恢复刷新。
func (a *App) ResumeCapture() {
	a.mu.Lock()
	a.paused = false
	a.sess.SetPaused(false, false)
	a.mu.Unlock()
	a.emitState()
}

// IsRunning 引擎是否运行中。
func (a *App) IsRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.engine != nil
}

// IsPaused 是否处于暂停。
func (a *App) IsPaused() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.paused
}

// GetConnStatus 终端连接状态（状态灯）。
func (a *App) GetConnStatus() string {
	a.mu.Lock()
	e := a.engine
	a.mu.Unlock()
	if e == nil {
		return "未监听"
	}
	return e.ConnStatus()
}

// GetMasterPort 主站端口（自动识别结果）。
func (a *App) GetMasterPort() int {
	a.muMaster.Lock()
	defer a.muMaster.Unlock()
	return a.masterPort
}

// GetStats 会话统计（含解析队列状态：queue=积压，dropped=队列满丢弃数）。
func (a *App) GetStats() map[string]int64 {
	st := a.sess.Stats()
	a.mu.Lock()
	e := a.engine
	a.mu.Unlock()
	if e != nil {
		st["queue"] = int64(e.QueueLen())
		st["pending"] = int64(e.PendingLen())
		st["dropped"] = e.DroppedFrames()
	} else {
		st["queue"] = 0
		st["pending"] = 0
		st["dropped"] = 0
	}
	return st
}
