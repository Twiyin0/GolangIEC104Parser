// Package config 配置管理：单一 JSON，向后兼容（缺失字段补默认值）。
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type LogConn struct {
	NetIf      string `json:"netIf"`      // 网卡名（本机/出口绑定用，可选）
	Protocol   string `json:"protocol"`   // local | ssh | sftp | scp | ftp
	Host       string `json:"host"`       // 终端 IP/主机
	Port       int    `json:"port"`       // SSH/SFTP/FTP 端口
	User       string `json:"user"`       // 用户名
	Password   string `json:"password"`   // 密码（可空，改用 Key）
	SSHKeyPath string `json:"sshKeyPath"` // SSH 私钥路径
	FilePath   string `json:"filePath"`   // 监听文件路径
	WatchMode  string `json:"watchMode"`  // dynamic | static
	FormatRe   string `json:"formatRe"`   // 自定义提取正则，空=内置默认
	Encoding   string `json:"encoding"`   // auto | utf-8 | gbk
	Proto      string `json:"proto"`      // auto | 101 | 104
	TailBytes  int    `json:"tailBytes"`  // 启动回溯字节数（默认 64KB）
}

// Log 监听默认值：空配置自动补全，密码/私钥保持为空（必须由用户填写）。
const (
	DefaultLogHost = "192.168.1.101"
	DefaultLogUser = "root"
	DefaultLogPath = "/data0/log/ftuMessage.log"
)

// NormalizeLog 规范化 log 连接配置；远程协议的空主机/用户/路径补默认值。
func NormalizeLog(c *LogConn) {
	if c == nil {
		return
	}
	c.Protocol = strings.ToLower(strings.TrimSpace(c.Protocol))
	if c.Protocol == "" {
		c.Protocol = "ssh"
	}
	switch strings.ToLower(strings.TrimSpace(c.WatchMode)) {
	case "static":
		c.WatchMode = "static"
	default:
		c.WatchMode = "dynamic"
	}
	switch strings.ToLower(strings.TrimSpace(c.Encoding)) {
	case "utf-8", "utf8":
		c.Encoding = "utf-8"
	case "gbk", "gb18030":
		c.Encoding = "gbk"
	default:
		c.Encoding = "auto"
	}
	switch strings.ToLower(strings.TrimSpace(c.Proto)) {
	case "101":
		c.Proto = "101"
	case "104":
		c.Proto = "104"
	default:
		c.Proto = "auto"
	}
	if c.TailBytes <= 0 {
		c.TailBytes = 65536
	}
	if c.TailBytes > 16<<20 {
		c.TailBytes = 16 << 20
	}
	if c.Protocol != "local" {
		if strings.TrimSpace(c.Host) == "" {
			c.Host = DefaultLogHost
		}
		if strings.TrimSpace(c.User) == "" {
			c.User = DefaultLogUser
		}
		if strings.TrimSpace(c.FilePath) == "" {
			c.FilePath = DefaultLogPath
		}
	}
	if c.Port == 0 {
		switch c.Protocol {
		case "ssh", "sftp", "scp":
			c.Port = 22
		case "ftp":
			c.Port = 21
		}
	}
}

type Connection struct {
	Mode         string `json:"mode"` // "net" 网口旁听(104) | "serial" 串口(101) | "log" 日志监听
	Device       string `json:"device"`
	TerminalIP   string `json:"terminalIp"`
	TerminalPort int    `json:"terminalPort"`
	MasterIP     string `json:"masterIp"` // "" = 任意（默认本机可填实际 IP）
	MasterPort   int    `json:"masterPort"`
	Promisc      bool   `json:"promisc"`

	SerialMode string `json:"serialMode"` // passive | direct
	BaudRate   int    `json:"baudRate"`
	DataBits   int    `json:"dataBits"`
	Parity     string `json:"parity"` // none|even|odd
	StopBits   int    `json:"stopBits"`
	LinkAddr   int    `json:"linkAddr"`

	Log LogConn `json:"log"`
}

type UI struct {
	Theme          string `json:"theme"` // dark | light
	AutoScroll     bool   `json:"autoScroll"`
	DropWhenPaused bool   `json:"dropWhenPaused"`
}

type SessionCfg struct {
	MaxFrames      int    `json:"maxFrames"`
	ExportDir      string `json:"exportDir"`
	ParseWorkers   int    `json:"parseWorkers"`   // 解析并发数（默认 16，上限 256）
	ParseQueueSize int    `json:"parseQueueSize"` // 解析队列缓冲长度（等待解析的报文数，非总量上限）
}

// AddrFormatCfg 各类型信息体地址显示进制（"hex" 十六进制 | "dec" 十进制）。
type AddrFormatCfg struct {
	YX   string `json:"yx"`   // 遥信（默认 dec）
	YC   string `json:"yc"`   // 遥测（默认 hex）
	YK   string `json:"yk"`   // 遥控（默认 hex）
	EN   string `json:"en"`   // 电能量（默认 hex）
	Para string `json:"para"` // 定值（默认 hex）
}

// ParseCfg 解析相关参数。
type ParseCfg struct {
	// 转发点表基值：选定具体点表时 信息体地址 = 基值 + SwitchPoint，点名为 SwitchName
	// 101 与 104 各自独立配置；旧字段（yxTransferBase 等）为 104 基址。
	YXTransferBase int `json:"yxTransferBase"` // 遥信（默认 0x0000）
	YCTransferBase int `json:"ycTransferBase"` // 遥测（默认 0x4000）
	YKTransferBase int `json:"ykTransferBase"` // 遥控（默认 0x6000）
	ENTransferBase int `json:"enTransferBase"` // 电能量（默认 0x6400）

	// 101 转发点表基值；缺省（nil）时沿用 104 基址，便于旧配置兼容。
	Base101 *ProtocolBases `json:"base101,omitempty"`
	// 104 转发点表基值；缺省时由上面的旧字段初始化。
	Base104 *ProtocolBases `json:"base104,omitempty"`

	// ParamTable 定值表选择："" 无 | "gw" 国网 ProjectGWParamPoint | "nw" 南网 ProjectNWParamPoint
	ParamTable string `json:"paramTable"`
	// AddrFormat 各类型信息体地址显示进制
	AddrFormat AddrFormatCfg `json:"addrFormat"`
}

// ProtocolBases 单个协议的转发点表基值。
type ProtocolBases struct {
	YX int `json:"yx"` // 遥信（默认 0x0000）
	YC int `json:"yc"` // 遥测（默认 0x4000）
	YK int `json:"yk"` // 遥控（默认 0x6000）
	EN int `json:"en"` // 电能量（默认 0x6400）
}

type Config struct {
	Connection Connection `json:"connection"`
	UI         UI         `json:"ui"`
	Session    SessionCfg `json:"session"`
	Parse      ParseCfg   `json:"parse"`
	Mode       string     `json:"mode"` // realtime | static
}

func Default() Config {
	return Config{
		Connection: Connection{
			Mode:         "net",
			TerminalIP:   "192.168.1.101",
			TerminalPort: 2404,
			MasterIP:     "",
			Promisc:      false,
			SerialMode:   "passive",
			BaudRate:     9600,
			DataBits:     8,
			Parity:       "even",
			StopBits:     1,
			LinkAddr:     1,
			Log: LogConn{
				Protocol: "ssh", Host: DefaultLogHost, Port: 22, User: DefaultLogUser,
				FilePath: DefaultLogPath, WatchMode: "dynamic", Encoding: "auto", Proto: "auto", TailBytes: 65536,
			},
		},
		UI: UI{Theme: "dark", AutoScroll: true, DropWhenPaused: false},
		Session: SessionCfg{MaxFrames: 50000, ExportDir: "",
			ParseWorkers: 16, ParseQueueSize: 4096},
		Parse: ParseCfg{YXTransferBase: 0x0000, YCTransferBase: 0x4000,
			YKTransferBase: 0x6000, ENTransferBase: 0x6400, ParamTable: "",
			Base101:    &ProtocolBases{YX: 0x0000, YC: 0x4000, YK: 0x6000, EN: 0x6400},
			Base104:    &ProtocolBases{YX: 0x0000, YC: 0x4000, YK: 0x6000, EN: 0x6400},
			AddrFormat: AddrFormatCfg{YX: "dec", YC: "hex", YK: "hex", EN: "hex", Para: "hex"}},
		Mode: "realtime",
	}
}

// Normalize 缺失/非法字段补默认值（向后兼容）。
func (c *Config) Normalize() {
	d := Default()
	if c.Connection.Mode == "" {
		c.Connection.Mode = d.Connection.Mode
	}
	if c.Connection.TerminalPort == 0 {
		c.Connection.TerminalPort = d.Connection.TerminalPort
	}
	if c.Connection.TerminalIP == "" {
		c.Connection.TerminalIP = d.Connection.TerminalIP
	}
	if c.Connection.SerialMode == "" {
		c.Connection.SerialMode = d.Connection.SerialMode
	}
	if c.Connection.BaudRate == 0 {
		c.Connection.BaudRate = d.Connection.BaudRate
	}
	if c.Connection.DataBits == 0 {
		c.Connection.DataBits = d.Connection.DataBits
	}
	if c.Connection.Parity == "" {
		c.Connection.Parity = d.Connection.Parity
	}
	if c.Connection.StopBits == 0 {
		c.Connection.StopBits = d.Connection.StopBits
	}
	if c.Connection.LinkAddr == 0 {
		c.Connection.LinkAddr = d.Connection.LinkAddr
	}
	// Log 监听配置兼容旧配置：缺失字段补默认值。
	NormalizeLog(&c.Connection.Log)
	if c.UI.Theme == "" {
		c.UI.Theme = d.UI.Theme
	}
	if c.Session.MaxFrames == 0 {
		c.Session.MaxFrames = d.Session.MaxFrames
	}
	if c.Session.ParseWorkers <= 0 {
		c.Session.ParseWorkers = d.Session.ParseWorkers
	}
	if c.Session.ParseWorkers > 256 { // 并发上限 256
		c.Session.ParseWorkers = 256
	}
	if c.Session.ParseQueueSize <= 0 {
		c.Session.ParseQueueSize = d.Session.ParseQueueSize
	}
	// 转发点表基值：0 表示使用默认（遥信 0x0000 / 遥测 0x4000 / 遥控 0x6000 / 电能量 0x6400）
	if c.Parse.YCTransferBase < 0 {
		c.Parse.YCTransferBase = d.Parse.YCTransferBase
	}
	if c.Parse.YKTransferBase < 0 {
		c.Parse.YKTransferBase = d.Parse.YKTransferBase
	}
	if c.Parse.ENTransferBase < 0 {
		c.Parse.ENTransferBase = d.Parse.ENTransferBase
	}
	if c.Parse.YXTransferBase < 0 {
		c.Parse.YXTransferBase = d.Parse.YXTransferBase
	}
	// 101/104 分协议基址：缺失时继承旧字段（104 基址），保证旧配置行为不变。
	// 旧字段全为 0（未配置）时用默认基址。
	legacy := ProtocolBases{
		YX: c.Parse.YXTransferBase, YC: c.Parse.YCTransferBase,
		YK: c.Parse.YKTransferBase, EN: c.Parse.ENTransferBase,
	}
	if legacy == (ProtocolBases{}) {
		legacy = *d.Parse.Base104
	}
	c.Parse.Base104 = normBases(c.Parse.Base104, legacy)
	c.Parse.Base101 = normBases(c.Parse.Base101, *c.Parse.Base104)
	switch strings.ToLower(c.Parse.ParamTable) {
	case "gw", "nw":
		c.Parse.ParamTable = strings.ToLower(c.Parse.ParamTable)
	default:
		c.Parse.ParamTable = ""
	}
	c.Parse.AddrFormat = normAddrFormat(c.Parse.AddrFormat, d.Parse.AddrFormat)
	if c.Mode == "" {
		c.Mode = d.Mode
	}
}

// normBases 基址归一化：负值补 fallback，保持 0 也是合法基址（遥信默认 0）。
func normBases(cur *ProtocolBases, fallback ProtocolBases) *ProtocolBases {
	out := fallback
	if cur != nil {
		out = ProtocolBases{YX: cur.YX, YC: cur.YC, YK: cur.YK, EN: cur.EN}
	}
	if out.YX < 0 {
		out.YX = fallback.YX
	}
	if out.YC < 0 {
		out.YC = fallback.YC
	}
	if out.YK < 0 {
		out.YK = fallback.YK
	}
	if out.EN < 0 {
		out.EN = fallback.EN
	}
	return &out
}

// BasesFor 返回指定协议（"101"/"104"）的转发基址。
func (p ParseCfg) BasesFor(proto string) ProtocolBases {
	if strings.Contains(proto, "101") {
		if p.Base101 != nil {
			return *p.Base101
		}
	} else if p.Base104 != nil {
		return *p.Base104
	}
	return ProtocolBases{YX: p.YXTransferBase, YC: p.YCTransferBase,
		YK: p.YKTransferBase, EN: p.ENTransferBase}
}

// normAddrFormat 地址进制归一化（仅允许 hex/dec，缺省用默认）。
func normAddrFormat(cur, def AddrFormatCfg) AddrFormatCfg {
	pick := func(v, dv string) string {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "hex":
			return "hex"
		case "dec":
			return "dec"
		}
		return dv
	}
	return AddrFormatCfg{
		YX:   pick(cur.YX, def.YX),
		YC:   pick(cur.YC, def.YC),
		YK:   pick(cur.YK, def.YK),
		EN:   pick(cur.EN, def.EN),
		Para: pick(cur.Para, def.Para),
	}
}

// Path 配置文件路径：exe 同目录 config\config.json。
func Path() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "config.json", err
	}
	return filepath.Join(filepath.Dir(exe), "config", "config.json"), nil
}

// Load 加载配置（不存在则返回默认值）。
func Load(path string) (Config, error) {
	c := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return Default(), err
	}
	c.Normalize()
	return c, nil
}

// Save 保存配置。
func Save(path string, c Config) error {
	c.Normalize()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
