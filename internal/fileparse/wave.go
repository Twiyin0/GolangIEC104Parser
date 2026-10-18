package fileparse

// WaveChart COMTRADE 波形数据（前端 SVG 渲染）。
type WaveChart struct {
	Station     string        `json:"station"`
	Device      string        `json:"device"`
	StartTime   string        `json:"startTime"`
	TriggerTime string        `json:"triggerTime"`
	SampleRate  float64       `json:"sampleRate"`
	Duration    float64       `json:"duration"`
	Times       []float64     `json:"times"` // 相对起始时刻的秒数
	Channels    []WaveChannel `json:"channels"`
	Digital     []WaveDigital `json:"digital,omitempty"`
	Note        string        `json:"note"`
}

// WaveChannel 一路模拟量波形。
type WaveChannel struct {
	Name  string    `json:"name"`
	Phase string    `json:"phase"`
	Unit  string    `json:"unit"`
	Min   float64   `json:"min"`
	Max   float64   `json:"max"`
	Data  []float64 `json:"data"`
}

// WaveDigital 一路开关量（0/1）。
type WaveDigital struct {
	Name string `json:"name"`
	Data []int  `json:"data"`
}
