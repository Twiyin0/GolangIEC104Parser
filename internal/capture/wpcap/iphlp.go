package wpcap

import (
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var iphlpapi = windows.NewLazySystemDLL("iphlpapi.dll")

// TCPRow 一条本机 TCP 连接表项（GetTcpTable）。
type TCPRow struct {
	State      int
	LocalAddr  string
	LocalPort  int
	RemoteAddr string
	RemotePort int
}

type mibTCPRow struct {
	State, LocalAddr, LocalPort, RemoteAddr, RemotePort uint32
} // 20 字节

// GetTCPTable 枚举本机 TCP 连接表（用于"主站端口扫描识别"，需求第 7 条）。
func GetTCPTable() ([]TCPRow, error) {
	proc := iphlpapi.NewProc("GetTcpTable")
	var size uint32 = 0
	r, _, _ := proc.Call(0, uintptr(unsafe.Pointer(&size)), 0)
	if r != 122 && r != 0 { // ERROR_INSUFFICIENT_BUFFER
		return nil, fmt.Errorf("GetTcpTable 大小查询失败: %d", r)
	}
	if size == 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	r, _, _ = proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 1)
	if r != 0 {
		return nil, fmt.Errorf("GetTcpTable 失败: %d", r)
	}
	n := binary.LittleEndian.Uint32(buf[:4])
	var rows []TCPRow
	for i := uint32(0); i < n; i++ {
		off := 4 + i*20
		row := mibTCPRow{
			State:      binary.LittleEndian.Uint32(buf[off:]),
			LocalAddr:  binary.LittleEndian.Uint32(buf[off+4:]),
			LocalPort:  binary.LittleEndian.Uint32(buf[off+8:]),
			RemoteAddr: binary.LittleEndian.Uint32(buf[off+12:]),
			RemotePort: binary.LittleEndian.Uint32(buf[off+16:]),
		}
		rows = append(rows, TCPRow{
			State:      int(row.State),
			LocalAddr:  dwordToIP(row.LocalAddr),
			LocalPort:  int(uint16(row.LocalPort>>8&0xFF | row.LocalPort<<8&0xFF00)), // 网络字节序
			RemoteAddr: dwordToIP(row.RemoteAddr),
			RemotePort: int(uint16(row.RemotePort>>8&0xFF | row.RemotePort<<8&0xFF00)),
		})
	}
	return rows, nil
}

func dwordToIP(v uint32) string {
	return net.IPv4(byte(v), byte(v>>8), byte(v>>16), byte(v>>24)).String()
}

// FindLocalPortTo 查找本机→remoteIP:remotePort 的连接，返回本机端口（主站端口）。
func FindLocalPortTo(remoteIP string, remotePort int) (int, error) {
	rows, err := GetTCPTable()
	if err != nil {
		return 0, err
	}
	for _, r := range rows {
		if r.RemoteAddr == remoteIP && r.RemotePort == remotePort && r.State == 5 { // ESTABLISHED
			return r.LocalPort, nil
		}
	}
	return 0, syscall.ENOENT
}
