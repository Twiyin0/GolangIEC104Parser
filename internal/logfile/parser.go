// Package logfile 解析终端日志行，提取时间/方向/101-104 HEX。
package logfile

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// Record 从一行日志中提取出的报文记录。
type Record struct {
	Time    string `json:"time"`
	Dir     string `json:"dir"`
	Message string `json:"message"` // 连续 HEX（空格分隔）
}

// 默认规则：优先匹配 Tx/Rx 行，其次匹配带方向箭头的行，最后匹配“时间 + 连续 HEX”。
var (
	defaultDirRe = regexp.MustCompile(`(?i)\b(?:Tx|Rx)\s*\(\s*\d+\s*\)\s*(?P<dir>-+>|<-+)\s*(?P<message>(?:[0-9A-Fa-f]{2})(?:\s+[0-9A-Fa-f]{2})*)`)
	arrowRe      = regexp.MustCompile(`(?P<dir>--->|<---)\s*(?P<message>(?:[0-9A-Fa-f]{2})(?:\s+[0-9A-Fa-f]{2})*)`)
	timeRe       = regexp.MustCompile(`(\d{4})[/-](\d{2})[/-](\d{2})[-_ T](\d{2}):(\d{2}):(\d{2})(?:[.,](\d{1,6}))?`)
	hexOnlyRe    = regexp.MustCompile(`(?P<message>(?:[0-9A-Fa-f]{2})(?:\s+[0-9A-Fa-f]{2}){3,})`)
)

// Parser 日志行提取器；re 为空时使用内置默认规则。
type Parser struct {
	re     *regexp.Regexp
	custom bool
}

// NewParser 编译自定义正则；支持 (?<name>...) 与 (?P<name>...)。
func NewParser(expr string) (*Parser, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return &Parser{}, nil
	}
	// Go regexp 只支持 (?P<name>...)，兼容常见的 (?<name>...)。
	expr = strings.ReplaceAll(expr, "(?<", "(?P<")
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("日志正则编译失败: %v", err)
	}
	if re.SubexpIndex("time") < 0 {
		return nil, fmt.Errorf("日志正则必须包含命名组 (?P<time>...) 或 (?<time>...)")
	}
	if re.SubexpIndex("message") < 0 {
		return nil, fmt.Errorf("日志正则必须包含命名组 (?P<message>...) 或 (?<message>...)")
	}
	return &Parser{re: re, custom: true}, nil
}

// Parse 提取一行；返回 false 表示该行不是报文行。
func (p *Parser) Parse(line string) (Record, bool) {
	line = strings.TrimSpace(strings.TrimRight(line, "\r"))
	if line == "" {
		return Record{}, false
	}
	var rec Record
	var message, dir string
	if p.custom {
		m := p.re.FindStringSubmatch(line)
		if m == nil {
			return Record{}, false
		}
		get := func(name string) string {
			i := p.re.SubexpIndex(name)
			if i < 0 || i >= len(m) {
				return ""
			}
			return strings.TrimSpace(m[i])
		}
		rec.Time = normalizeTime(get("time"))
		message = get("message")
		dir = get("dir")
	} else {
		rec.Time = extractTime(line)
		if m := defaultDirRe.FindStringSubmatch(line); m != nil {
			message = m[defaultDirRe.SubexpIndex("message")]
			dir = m[defaultDirRe.SubexpIndex("dir")]
		} else if m := arrowRe.FindStringSubmatch(line); m != nil {
			message = m[arrowRe.SubexpIndex("message")]
			dir = m[arrowRe.SubexpIndex("dir")]
		} else if m := hexOnlyRe.FindStringSubmatch(line); m != nil {
			message = m[hexOnlyRe.SubexpIndex("message")]
		}
	}
	message = strings.Join(strings.Fields(message), " ")
	if message == "" {
		return Record{}, false
	}
	rec.Message = strings.ToUpper(message)
	rec.Dir = direction(dir)
	return rec, true
}

// direction 把日志箭头转换成主站/终端方向。
func direction(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case strings.Contains(s, ">"):
		return "M→T"
	case strings.Contains(s, "<"):
		return "T→M"
	}
	if strings.EqualFold(s, "Tx") {
		return "M→T"
	}
	if strings.EqualFold(s, "Rx") {
		return "T→M"
	}
	return ""
}

// extractTime 从任意位置提取终端时间并统一格式。
func extractTime(line string) string {
	m := timeRe.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	return normalizeTime(m[0])
}

// normalizeTime 统一为 2006-01-02 15:04:05[.mmm]。
func normalizeTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	layouts := []string{
		"2006/01/02-15:04:05.999999",
		"2006/01/02-15:04:05",
		"2006/01/02 15:04:05.999999",
		"2006/01/02 15:04:05",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
		"2006-01-02-15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			if strings.Contains(s, ".") || strings.Contains(s, ",") {
				return t.Format("2006-01-02 15:04:05.000")
			}
			return t.Format("2006-01-02 15:04:05")
		}
	}
	return s
}

// Decode 按配置解码日志字节：auto 先 UTF-8，失败回退 GB18030。
func Decode(data []byte, encoding string) string {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "utf-8", "utf8":
		return string(data)
	case "gbk", "gb18030":
		if out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data); err == nil {
			return string(out)
		}
		return string(data)
	default:
		if utf8.Valid(data) {
			return string(data)
		}
		if out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data); err == nil {
			return string(out)
		}
		return string(data)
	}
}
