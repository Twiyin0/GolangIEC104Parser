package fileparse

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// COMTRADE（GB/T 22386 / IEEE C37.111）CFG/DAT 解析：
// 读取通道定义、采样率与波形数据，供前端绘制多通道波形图。

type ctAnalog struct {
	Index     string
	Name      string
	Phase     string
	Unit      string
	A, B      float64
	Min, Max  float64
	Primary   float64
	Secondary float64
}

type ctDigital struct {
	Index string
	Name  string
	Phase string
}

type ctCfg struct {
	Station    string
	Device     string
	Revision   string
	Total      int
	Analog     int
	An         []ctAnalog
	Dig        []ctDigital
	SampleRate float64
	StartTime  string
	Trigger    string
	FileType   string
	TimeMult   float64
}

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func floatSafe(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

func splitCSVLine(s string) []string {
	var out []string
	var b strings.Builder
	quoted := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			if quoted && i+1 < len(s) && s[i+1] == '"' {
				b.WriteByte('"')
				i++
			} else {
				quoted = !quoted
			}
			continue
		}
		if c == ',' && !quoted {
			out = append(out, strings.TrimSpace(b.String()))
			b.Reset()
			continue
		}
		b.WriteByte(c)
	}
	out = append(out, strings.TrimSpace(b.String()))
	return out
}

func isDateTimeLine(s string) bool {
	f := splitCSVLine(s)
	return len(f) >= 2 && strings.Contains(f[0], "/") && strings.Contains(f[1], ":")
}

func parseCTCFG(data []byte) (*ctCfg, error) {
	lines := splitNonEmpty(decodeText(data))
	if len(lines) < 2 {
		return nil, fmt.Errorf("CFG 内容不足")
	}
	h := splitCSVLine(lines[0])
	cfg := &ctCfg{TimeMult: 1}
	if len(h) > 0 {
		cfg.Station = h[0]
	}
	if len(h) > 1 {
		cfg.Device = h[1]
	}
	if len(h) > 2 {
		cfg.Revision = h[2]
	}
	cnt := splitCSVLine(lines[1])
	if len(cnt) < 2 {
		return nil, fmt.Errorf("CFG 通道数量行无效")
	}
	cfg.Total = atoiSafe(cnt[0])
	cfg.Analog = atoiSafe(cnt[1])
	if cfg.Total < 0 || cfg.Analog < 0 || cfg.Analog > cfg.Total {
		return nil, fmt.Errorf("CFG 通道数量无效（TT=%d, NA=%d）", cfg.Total, cfg.Analog)
	}
	idx := 2
	for i := 0; i < cfg.Analog; i++ {
		if idx >= len(lines) {
			return nil, fmt.Errorf("CFG 模拟通道定义不足")
		}
		f := splitCSVLine(lines[idx])
		idx++
		if len(f) < 5 {
			return nil, fmt.Errorf("CFG 模拟通道第 %d 行字段不足", i+1)
		}
		a := ctAnalog{Index: f[0], Name: f[1], Phase: f[2], Unit: f[4]}
		if len(f) > 5 {
			a.A = floatSafe(f[5])
		} else {
			a.A = 1
		}
		if len(f) > 6 {
			a.B = floatSafe(f[6])
		}
		if len(f) > 8 {
			a.Min = floatSafe(f[8])
		}
		if len(f) > 9 {
			a.Max = floatSafe(f[9])
		}
		if len(f) > 10 {
			a.Primary = floatSafe(f[10])
		}
		if len(f) > 11 {
			a.Secondary = floatSafe(f[11])
		}
		if a.A == 0 {
			a.A = 1
		}
		cfg.An = append(cfg.An, a)
	}
	for i := 0; i < cfg.Total-cfg.Analog; i++ {
		if idx >= len(lines) {
			break
		}
		f := splitCSVLine(lines[idx])
		idx++
		d := ctDigital{Index: f[0]}
		if len(f) > 1 {
			d.Name = f[1]
		}
		if len(f) > 2 {
			d.Phase = f[2]
		}
		cfg.Dig = append(cfg.Dig, d)
	}
	// 通道定义之后：采样率段、起始/触发时标、文件类型、时间倍率。
	startIdx := -1
	for i := idx; i < len(lines); i++ {
		if isDateTimeLine(lines[i]) {
			startIdx = i
			break
		}
	}
	rateEnd := len(lines)
	if startIdx >= 0 {
		rateEnd = startIdx
		cfg.StartTime = lines[startIdx]
		if startIdx+1 < len(lines) && isDateTimeLine(lines[startIdx+1]) {
			cfg.Trigger = lines[startIdx+1]
			if startIdx+2 < len(lines) {
				cfg.FileType = strings.ToUpper(strings.TrimSpace(lines[startIdx+2]))
			}
			if startIdx+3 < len(lines) {
				cfg.TimeMult = floatSafe(lines[startIdx+3])
				if cfg.TimeMult == 0 {
					cfg.TimeMult = 1
				}
			}
		}
	}
	// 采样率：优先取 "samp,endsamp" 行的第一列；1991 格式为单行 samp。
	rate := 0.0
	for _, ln := range lines[idx:rateEnd] {
		f := splitCSVLine(ln)
		if len(f) >= 2 {
			if v := floatSafe(f[0]); v > 0 {
				rate = v
				break
			}
		}
	}
	if rate == 0 {
		for _, ln := range lines[idx:rateEnd] {
			f := splitCSVLine(ln)
			if len(f) == 1 {
				if v := floatSafe(f[0]); v > 0 {
					rate = v
					break
				}
			}
		}
	}
	cfg.SampleRate = rate
	if cfg.TimeMult == 0 {
		cfg.TimeMult = 1
	}
	return cfg, nil
}

// ParseComtradeCFG 解析 .cfg 配置；有 .dat 时用 ParseComtrade 出波形。
func ParseComtradeCFG(data []byte) (*ctCfg, Result, error) {
	cfg, err := parseCTCFG(data)
	if err != nil {
		return nil, Result{Kind: "wave", KindName: "波形文件", Format: "cfg", Error: err.Error()}, err
	}
	res := Result{Kind: "wave", KindName: "波形文件", Format: "cfg"}
	res.Columns = []string{"通道号", "通道名称", "相位", "单位", "一次系数", "二次系数", "最小值", "最大值", "一次值", "二次值"}
	for _, a := range cfg.An {
		res.Rows = append(res.Rows, Record{
			"通道号": a.Index, "通道名称": a.Name, "相位": a.Phase, "单位": a.Unit,
			"一次系数": fmt.Sprintf("%g", a.A), "二次系数": fmt.Sprintf("%g", a.B),
			"最小值": fmt.Sprintf("%g", a.Min), "最大值": fmt.Sprintf("%g", a.Max),
			"一次值": fmt.Sprintf("%g", a.Primary), "二次值": fmt.Sprintf("%g", a.Secondary),
		})
	}
	res.Note = fmt.Sprintf("COMTRADE 配置（版本 %s）：%d 个模拟通道、%d 个开关量；等待同名 .dat 数据文件绘制波形",
		cfg.Revision, len(cfg.An), len(cfg.Dig))
	return cfg, res, nil
}

// ParseComtrade 解析 CFG+DAT，返回波形数据。
func ParseComtrade(cfgData, datData []byte) (Result, error) {
	cfg, res, err := ParseComtradeCFG(cfgData)
	if err != nil {
		return res, err
	}
	chart, err := parseCTDAT(cfg, datData)
	if err != nil {
		res.Error = err.Error()
		return res, err
	}
	res.Format = "comtrade"
	res.Chart = chart
	res.Note = fmt.Sprintf("COMTRADE 波形：%d 个采样点，%d 个模拟通道，时长 %.3f 秒", len(chart.Times), len(chart.Channels), chart.Duration)
	return res, nil
}

func parseCTDAT(cfg *ctCfg, data []byte) (*WaveChart, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("DAT 内容为空")
	}
	binaryMode := strings.Contains(strings.ToUpper(cfg.FileType), "BINARY")
	if cfg.FileType == "" {
		c := data[0]
		binaryMode = !(c >= '0' && c <= '9' || c == '-' || c == '+' || c == '.')
	}
	text := ""
	if !binaryMode {
		text = decodeText(data)
		if strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("DAT 内容为空")
		}
	}
	var times []float64
	analog := make([][]float64, len(cfg.An))
	digital := make([][]int, len(cfg.Dig))
	for i := range analog {
		analog[i] = []float64{}
	}
	for i := range digital {
		digital[i] = []int{}
	}
	const maxSamples = 200000
	baseTS := -1.0
	addSample := func(ts float64, raw []float64, dig []int) {
		if baseTS < 0 {
			baseTS = ts
		}
		times = append(times, (ts-baseTS)/1e6*cfg.TimeMult)
		for i := range analog {
			v := cfg.An[i].B
			if i < len(raw) {
				v += cfg.An[i].A * raw[i]
			}
			analog[i] = append(analog[i], v)
		}
		for i := range digital {
			v := 0
			if i < len(dig) {
				v = dig[i]
			}
			digital[i] = append(digital[i], v)
		}
	}
	if binaryMode {
		pos := 0
		words := (len(cfg.Dig) + 15) / 16
		for pos+8 <= len(data) && len(times) < maxSamples {
			_ = binary.LittleEndian.Uint32(data[pos:]) // sample no
			ts := float64(binary.LittleEndian.Uint32(data[pos+4:]))
			pos += 8
			if pos+2*len(cfg.An)+2*words > len(data) {
				break
			}
			raw := make([]float64, len(cfg.An))
			for i := range raw {
				raw[i] = float64(int16(binary.LittleEndian.Uint16(data[pos:])))
				pos += 2
			}
			dig := make([]int, 0, len(cfg.Dig))
			for w := 0; w < words; w++ {
				v := binary.LittleEndian.Uint16(data[pos:])
				pos += 2
				for b := 0; b < 16 && len(dig) < len(cfg.Dig); b++ {
					dig = append(dig, int((v>>uint(b))&1))
				}
			}
			addSample(ts, raw, dig)
		}
	} else {
		for _, ln := range splitNonEmpty(text) {
			f := splitCSVLine(ln)
			if len(f) < 2+len(cfg.An) {
				continue
			}
			ts := floatSafe(f[1])
			raw := make([]float64, len(cfg.An))
			for i := range raw {
				raw[i] = floatSafe(f[2+i])
			}
			dig := make([]int, 0, len(cfg.Dig))
			for i := 0; i < len(cfg.Dig); i++ {
				if 2+len(cfg.An)+i < len(f) {
					dig = append(dig, atoiSafe(f[2+len(cfg.An)+i]))
				} else {
					dig = append(dig, 0)
				}
			}
			addSample(ts, raw, dig)
			if len(times) >= maxSamples {
				break
			}
		}
	}
	if len(times) == 0 {
		return nil, fmt.Errorf("DAT 中未解析到采样点")
	}
	// 时间戳为 0 时用采样率/序号兜底
	allZero := true
	for _, t := range times {
		if t != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		if cfg.SampleRate > 0 {
			for i := range times {
				times[i] = float64(i) / cfg.SampleRate
			}
		} else {
			for i := range times {
				times[i] = float64(i)
			}
		}
	}
	chart := &WaveChart{
		Station: cfg.Station, Device: cfg.Device, StartTime: cfg.StartTime,
		TriggerTime: cfg.Trigger, SampleRate: cfg.SampleRate,
		Times: times, Duration: times[len(times)-1],
		Note: "各通道按自身量程显示；横轴为相对时间（秒）",
	}
	for i, a := range cfg.An {
		min, max := math.Inf(1), math.Inf(-1)
		for _, v := range analog[i] {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
		if math.IsInf(min, 1) {
			min, max = 0, 0
		}
		chart.Channels = append(chart.Channels, WaveChannel{
			Name: a.Name, Phase: a.Phase, Unit: a.Unit,
			Min: min, Max: max, Data: analog[i],
		})
	}
	for i, d := range cfg.Dig {
		chart.Digital = append(chart.Digital, WaveDigital{Name: d.Name, Data: digital[i]})
	}
	return chart, nil
}
