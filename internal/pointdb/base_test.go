package pointdb

import "testing"

// 101 与 104 使用各自的转发基址，互不影响。
func TestBasesForProtocolIndependent(t *testing.T) {
	d, err := Load(buildTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	d.SetTable("101", "1#主变")
	d.SetTable("104", "1#主变")
	d.SetBasesFor("101", 0, 0x5000, 0x7000, 0x7400)
	d.SetBasesFor("104", 0, 0x4000, 0x6000, 0x6400)
	if got := d.PtNameFor("101", "yc", 0x5000+201); got != "I段10kV进线电压" {
		t.Fatalf("101 基址点名 = %q", got)
	}
	if got := d.PtNameFor("104", "yc", 0x5000+201); got != "" {
		t.Fatalf("104 不应命中 101 基址 = %q", got)
	}
	if got := d.PtNameFor("104", "yc", 0x4000+201); got != "I段10kV进线电压" {
		t.Fatalf("104 基址点名 = %q", got)
	}
	if got := d.PtNameFor("101", "yc", 0x4000+201); got != "" {
		t.Fatalf("101 不应命中 104 基址 = %q", got)
	}
}
