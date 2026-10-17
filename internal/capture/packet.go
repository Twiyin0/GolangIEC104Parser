// Package capture 实现抓包引擎：以太网/IPv4/TCP 解析与过滤。
package capture

import (
	"encoding/binary"
	"fmt"
)

// ConnKey 连接四元组（会话标识）。
type ConnKey struct {
	SrcIP   string
	SrcPort int
	DstIP   string
	DstPort int
}

// Pair 无序连接对（双向归并到同一会话状态）。
func (k ConnKey) Pair() string {
	a := fmt.Sprintf("%s:%d", k.SrcIP, k.SrcPort)
	b := fmt.Sprintf("%s:%d", k.DstIP, k.DstPort)
	if a < b {
		return a + "<->" + b
	}
	return b + "<->" + a
}

// TCPPacket 解析出的 TCP 报文段。
type TCPPacket struct {
	Key      ConnKey
	Flags    int // SYN/FIN/RST/ACK/PSH 位
	Seq      uint32
	Payload  []byte
}

const (
	flagFIN = 0x01
	flagSYN = 0x02
	flagRST = 0x04
	flagACK = 0x10
)

// ParseEthernet 从以太网帧解析 IPv4/TCP；返回 nil 表示非关注流量（非 IP/非 TCP/非 IPv4）。
// 支持 802.1Q VLAN。
func ParseEthernet(frame []byte) *TCPPacket {
	if len(frame) < 14 {
		return nil
	}
	ethType := binary.BigEndian.Uint16(frame[12:14])
	off := 14
	for ethType == 0x8100 { // VLAN 标签
		if len(frame) < off+4 {
			return nil
		}
		ethType = binary.BigEndian.Uint16(frame[off+2 : off+4])
		off += 4
	}
	if ethType != 0x0800 { // 非 IPv4
		return nil
	}
	return parseIPv4(frame[off:])
}

func parseIPv4(b []byte) *TCPPacket {
	if len(b) < 20 {
		return nil
	}
	ver := b[0] >> 4
	if ver != 4 {
		return nil
	}
	ihl := int(b[0]&0x0F) * 4
	if ihl < 20 || len(b) < ihl {
		return nil
	}
	totalLen := int(binary.BigEndian.Uint16(b[2:4]))
	if totalLen > len(b) {
		totalLen = len(b)
	}
	proto := b[9]
	if proto != 6 { // 非 TCP
		return nil
	}
	src := fmt.Sprintf("%d.%d.%d.%d", b[12], b[13], b[14], b[15])
	dst := fmt.Sprintf("%d.%d.%d.%d", b[16], b[17], b[18], b[19])
	return parseTCP(b[ihl:totalLen], src, dst)
}

func parseTCP(b []byte, srcIP, dstIP string) *TCPPacket {
	if len(b) < 20 {
		return nil
	}
	srcPort := int(binary.BigEndian.Uint16(b[0:2]))
	dstPort := int(binary.BigEndian.Uint16(b[2:4]))
	seq := binary.BigEndian.Uint32(b[4:8])
	flags := int(b[13])
	dataOff := int(b[12]>>4) * 4
	if dataOff < 20 || len(b) < dataOff {
		return nil
	}
	payload := b[dataOff:]
	return &TCPPacket{
		Key:     ConnKey{SrcIP: srcIP, SrcPort: srcPort, DstIP: dstIP, DstPort: dstPort},
		Flags:   flags,
		Seq:     seq,
		Payload: payload,
	}
}

// IsSyn 无 ACK 的 SYN（连接发起方）。
func (p *TCPPacket) IsSyn() bool  { return p.Flags&flagSYN != 0 && p.Flags&flagACK == 0 }
func (p *TCPPacket) IsFin() bool { return p.Flags&flagFIN != 0 }
func (p *TCPPacket) IsRst() bool { return p.Flags&flagRST != 0 }
