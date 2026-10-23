package config

import "testing"

func TestBases101And104Independent(t *testing.T) {
	c := Config{}
	c.Normalize()
	b101, b104 := c.Parse.BasesFor("101"), c.Parse.BasesFor("104")
	if b101 != b104 || b101.YC != 0x4000 || b101.YK != 0x6000 || b101.EN != 0x6400 {
		t.Fatalf("默认 101/104 基址应一致: 101=%+v 104=%+v", b101, b104)
	}
	// 只改 101，104 不受影响
	c.Parse.Base101 = &ProtocolBases{YX: 1, YC: 0x5000, YK: 0x7000, EN: 0x7400}
	c.Normalize()
	if got := c.Parse.BasesFor("101"); got.YC != 0x5000 {
		t.Fatalf("101 基址未生效: %+v", got)
	}
	if got := c.Parse.BasesFor("104"); got.YC != 0x4000 {
		t.Fatalf("104 基址被 101 影响: %+v", got)
	}
	c.Parse.Base104 = &ProtocolBases{YC: 0x9000}
	c.Normalize()
	if c.Parse.BasesFor("104").YC != 0x9000 || c.Parse.BasesFor("101").YC != 0x5000 {
		t.Fatalf("101/104 未独立: %+v vs %+v", c.Parse.BasesFor("101"), c.Parse.BasesFor("104"))
	}
}

func TestLegacyBasesInitBoth(t *testing.T) {
	c := Config{Parse: ParseCfg{YCTransferBase: 0x4100}}
	c.Normalize()
	if c.Parse.BasesFor("104").YC != 0x4100 || c.Parse.BasesFor("101").YC != 0x4100 {
		t.Fatalf("旧配置应同时初始化 101/104: %+v %+v", c.Parse.BasesFor("101"), c.Parse.BasesFor("104"))
	}
}
