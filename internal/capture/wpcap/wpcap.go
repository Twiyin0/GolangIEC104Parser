// Package wpcap 通过 syscall 直接调用 Npcap 的 wpcap.dll（libpcap C API），
// 实现纯 Go（无 cgo）的网卡旁听抓包，语义等价 gopacket/pcap。
//
// 旁听红线（Goal.md 注意事项 1）：本后端只 open 网卡旁听，绝不 bind/connect
// 被监听的终端端口，也不向链路注入任何报文。
package wpcap

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ── DLL 加载辅助 ────────────────────────────────────────────────────────

var (
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procLoadLibraryW = kernel32.NewProc("LoadLibraryW")
)

// loadLibraryW 直接 LoadLibraryW（不带 LOAD_LIBRARY_SEARCH 标志），
// 成功后模块常驻进程，供后续 LazyDLL 绑定与依赖解析。
func loadLibraryW(path string) (uintptr, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	h, _, callErr := procLoadLibraryW.Call(uintptr(unsafe.Pointer(p)))
	if h == 0 {
		if callErr == nil {
			callErr = fmt.Errorf("LoadLibraryW 失败: %s", path)
		}
		return 0, callErr
	}
	return h, nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// tryBind 先用 LoadLibraryW 常驻加载（依赖已预加载时成功），
// 再以 LazyDLL 绑定取过程地址；失败返回 nil。
func tryBind(path string) *windows.LazyDLL {
	if h, err := loadLibraryW(path); err == nil && h != 0 {
		if d := windows.NewLazyDLL(path); d.Load() == nil {
			return d
		}
	}
	d := windows.NewLazyDLL(path)
	if d.Load() == nil {
		return d
	}
	return nil
}

type pcapPkthdr struct {
	Sec    int32
	Usec   int32
	Caplen uint32
	Len    uint32
}

var (
	dll             *windows.LazyDLL
	procCreate      *windows.LazyProc
	procSetSnaplen  *windows.LazyProc
	procSetPromisc  *windows.LazyProc
	procSetTimeout  *windows.LazyProc
	procActivate    *windows.LazyProc
	procNextEx      *windows.LazyProc
	procClose       *windows.LazyProc
	procGeterr      *windows.LazyProc
	procDatalink    *windows.LazyProc
	procLibVersion  *windows.LazyProc
	procFindAllDevs *windows.LazyProc
	procFreeAllDevs *windows.LazyProc
)

func init() {
	// Npcap 标准路径：%SystemRoot%\System32\Npcap\（wpcap.dll 依赖同目录 Packet.dll）
	// WinPcap 兼容模式：%SystemRoot%\System32\
	//
	// 加载要点（Wireshark 同款做法）：
	// 1) NewLazyDLL.Load 使用 LoadLibraryEx(LOAD_LIBRARY_SEARCH_DEFAULT_DIRS)，
	//    该标志下依赖 DLL 只在应用目录/System32 等搜索，不含 Npcap 子目录——
	//    因此必须先从同目录显式预加载 Packet.dll，再加载 wpcap.dll；
	// 2) 必须用绝对路径（含分隔符的相对路径按进程工作目录解析）。
	sysroot := os.Getenv("SystemRoot")
	var dirs []string
	if sysroot != "" {
		dirs = append(dirs, sysroot+`\System32\Npcap`, sysroot+`\System32`)
	}
	for _, dir := range dirs {
		wpcapPath := filepath.Join(dir, "wpcap.dll")
		if _, err := os.Stat(wpcapPath); err != nil {
			continue
		}
		// 预加载依赖 Packet.dll（常驻进程，供 wpcap.dll 依赖解析）
		if packetPath := filepath.Join(dir, "Packet.dll"); fileExists(packetPath) {
			_, _ = loadLibraryW(packetPath)
		}
		if d := tryBind(wpcapPath); d != nil {
			dll = d
			break
		}
	}
	if dll == nil {
		if d := tryBind("wpcap.dll"); d != nil { // 兜底：标准搜索路径裸名
			dll = d
		}
	}
	if dll == nil {
		return
	}
	procCreate = dll.NewProc("pcap_create")
	procSetSnaplen = dll.NewProc("pcap_set_snaplen")
	procSetPromisc = dll.NewProc("pcap_set_promisc")
	procSetTimeout = dll.NewProc("pcap_set_timeout")
	procActivate = dll.NewProc("pcap_activate")
	procNextEx = dll.NewProc("pcap_next_ex")
	procClose = dll.NewProc("pcap_close")
	procGeterr = dll.NewProc("pcap_geterr")
	procDatalink = dll.NewProc("pcap_datalink")
	procLibVersion = dll.NewProc("pcap_lib_version")
	procFindAllDevs = dll.NewProc("pcap_findalldevs")
	procFreeAllDevs = dll.NewProc("pcap_freealldevs")
}

// Available 检测 wpcap.dll（Npcap）是否可用。
func Available() bool { return dll != nil }

// LibVersion 返回 libpcap 版本描述。
func LibVersion() string {
	if !Available() {
		return "Npcap 不可用"
	}
	r1, _, _ := procLibVersion.Call()
	return cstr(r1)
}

// Handle 活动的 pcap 句柄。
type Handle struct {
	h uintptr
}

// OpenLive 打开网卡旁听（混杂模式可选）。只旁听，不发送任何报文。
func OpenLive(device string, snaplen int, promisc bool, timeoutMs int) (*Handle, error) {
	if !Available() {
		return nil, fmt.Errorf("Npcap 未安装或 wpcap.dll 不可用（请安装 Npcap，并以管理员权限运行本程序）")
	}
	errbuf := make([]byte, 256)
	dev, err := windows.BytePtrFromString(device)
	if err != nil {
		return nil, err
	}
	h, _, _ := procCreate.Call(uintptr(unsafe.Pointer(dev)), uintptr(unsafe.Pointer(&errbuf[0])))
	if h == 0 {
		return nil, fmt.Errorf("pcap_create 失败: %s", cstrFromBuf(errbuf))
	}
	if r, _, _ := procSetSnaplen.Call(h, uintptr(snaplen)); r != 0 {
		return nil, fmt.Errorf("pcap_set_snaplen 失败: %d", r)
	}
	p := 0
	if promisc {
		p = 1
	}
	if r, _, _ := procSetPromisc.Call(h, uintptr(p)); r != 0 {
		return nil, fmt.Errorf("pcap_set_promisc 失败: %d", r)
	}
	if r, _, _ := procSetTimeout.Call(h, uintptr(timeoutMs)); r != 0 {
		return nil, fmt.Errorf("pcap_set_timeout 失败: %d", r)
	}
	if r, _, _ := procActivate.Call(h); r != 0 {
		msg := ""
		if e, _, _ := procGeterr.Call(h); e != 0 {
			msg = cstr(e)
		}
		return nil, fmt.Errorf("pcap_activate 失败(%d): %s（旁听通常需要管理员权限）", int32(r), msg)
	}
	return &Handle{h: h}, nil
}

// Datalink 返回链路类型（1=Ethernet）。
func (h *Handle) Datalink() int {
	r, _, _ := procDatalink.Call(h.h)
	return int(int32(r))
}

// NextEx 读取下一帧。返回 (数据, 时间戳Unix秒, 微秒, 是否成功/超时)。
// ret=1 有数据；0 超时；-1 错误。
func (h *Handle) NextEx() (data []byte, sec int64, usec int64, ret int, err error) {
	var hdr *pcapPkthdr
	var pkt *byte
	r, _, _ := procNextEx.Call(h.h,
		uintptr(unsafe.Pointer(&hdr)),
		uintptr(unsafe.Pointer(&pkt)))
	ret = int(int32(r))
	if ret == 1 && hdr != nil && pkt != nil {
		data = unsafe.Slice(pkt, hdr.Caplen)
		sec = int64(hdr.Sec)
		usec = int64(hdr.Usec)
		return
	}
	if ret == -1 {
		if e, _, _ := procGeterr.Call(h.h); e != 0 {
			err = fmt.Errorf("pcap_next_ex: %s", cstr(e))
		} else {
			err = fmt.Errorf("pcap_next_ex 错误")
		}
	}
	return nil, 0, 0, ret, err
}

// Close 关闭句柄。
func (h *Handle) Close() {
	if h != nil && h.h != 0 {
		procClose.Call(h.h)
		h.h = 0
	}
}

// Device 网卡设备（\Device\NPF_{GUID}）。
type Device struct {
	Name        string
	Description string
	Addresses   []string // IPv4 地址列表
}

type pcapIf struct { // x64: 40 字节
	Next         uintptr
	Name         uintptr
	Description  uintptr
	Addresses    uintptr
	Flags        uint32
	_            uint32
}

type pcapAddr struct { // x64: 40 字节
	Next, Addr, Netmask, Broadaddr, Dstaddr uintptr
}

type sockaddrIn struct { // 16 字节
	Family uint16
	Port   uint16
	Addr   [4]byte
	Zero   [8]byte
}

// FindAllDevs 枚举抓包网卡（含 IPv4 地址，用于界面选择）。
func FindAllDevs() ([]Device, error) {
	if !Available() {
		return nil, fmt.Errorf("Npcap 未安装或 wpcap.dll 不可用")
	}
	errbuf := make([]byte, 256)
	var devsp uintptr
	r, _, _ := procFindAllDevs.Call(uintptr(unsafe.Pointer(&devsp)), uintptr(unsafe.Pointer(&errbuf[0])))
	if r != 0 {
		return nil, fmt.Errorf("pcap_findalldevs 失败: %s", cstrFromBuf(errbuf))
	}
	defer procFreeAllDevs.Call(devsp)
	var out []Device
	for p := devsp; p != 0; {
		iface := (*pcapIf)(unsafe.Pointer(p))
		dev := Device{Name: cstr(iface.Name), Description: cstr(iface.Description)}
		for ap := iface.Addresses; ap != 0; {
			addr := (*pcapAddr)(unsafe.Pointer(ap))
			if addr.Addr != 0 {
				sa := (*sockaddrIn)(unsafe.Pointer(addr.Addr))
				if sa.Family == 2 { // AF_INET
					dev.Addresses = append(dev.Addresses,
						fmt.Sprintf("%d.%d.%d.%d", sa.Addr[0], sa.Addr[1], sa.Addr[2], sa.Addr[3]))
				}
			}
			ap = addr.Next
		}
		out = append(out, dev)
		p = iface.Next
	}
	return out, nil
}

func cstr(p uintptr) string {
	if p == 0 {
		return ""
	}
	b := make([]byte, 0, 64)
	for i := uintptr(0); i < 4096; i++ {
		c := *(*byte)(unsafe.Pointer(p + i))
		if c == 0 {
			break
		}
		b = append(b, c)
	}
	return string(b)
}

func cstrFromBuf(buf []byte) string {
	for i, c := range buf {
		if c == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}
