package iec104

import (
	"fmt"
	"regexp"
	"strings"
)

var hexClean = regexp.MustCompile(`[\s,;:]+`)

// HexToBytes 十六进制字符串转字节（容忍空白与分隔符）。
func HexToBytes(hex string) ([]byte, error) {
	clean := strings.ToUpper(hexClean.ReplaceAllString(hex, ""))
	if len(clean)%2 != 0 {
		return nil, fmt.Errorf("Invalid hex string length")
	}
	out := make([]byte, len(clean)/2)
	for i := 0; i < len(out); i++ {
		hi, ok1 := hexVal(clean[i*2])
		lo, ok2 := hexVal(clean[i*2+1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("invalid hex character")
		}
		out[i] = byte(hi<<4 | lo)
	}
	return out, nil
}

func hexVal(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}

// ParseLines 按行解析多条 104 报文（每行一条完整 APDU）。
func ParseLines(hex string) []M {
	items := strings.FieldsFunc(hex, func(r rune) bool { return r == '\n' || r == '\r' })
	var out []M
	for _, it := range items {
		if strings.TrimSpace(it) == "" {
			continue
		}
		b, err := HexToBytes(it)
		if err != nil {
			continue
		}
		out = append(out, ParseAPDU(b))
	}
	return out
}
