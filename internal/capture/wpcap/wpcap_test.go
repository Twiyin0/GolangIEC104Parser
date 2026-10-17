package wpcap

import "testing"

// TestAvailableAndFindAllDevs 验证 wpcap.dll 纯 syscall 绑定与已安装 Npcap 的联通性。
// 设备枚举一般无需管理员；打开旁听句柄在无管理员权限时可能失败（仅记录，不算失败）。
func TestAvailableAndFindAllDevs(t *testing.T) {
	if !Available() {
		t.Skip("Npcap（wpcap.dll）不可用：请安装 Npcap")
	}
	t.Logf("libpcap 版本: %s", LibVersion())

	devs, err := FindAllDevs()
	if err != nil {
		t.Fatalf("pcap_findalldevs 失败: %v", err)
	}
	if len(devs) == 0 {
		t.Skip("未枚举到网卡")
	}
	for _, d := range devs {
		t.Logf("网卡: %s | %v | %s", d.Name, d.Addresses, d.Description)
	}

	// 尝试打开第一块网卡旁听（非混杂），并读取一次
	h, err := OpenLive(devs[0].Name, 65535, false, 100)
	if err != nil {
		t.Logf("OpenLive 未成功（通常需要管理员权限，正式使用请以管理员运行 exe）: %v", err)
		return
	}
	defer h.Close()
	t.Logf("Datalink: %d (1=Ethernet)", h.Datalink())
	_, _, _, ret, rerr := h.NextEx()
	t.Logf("NextEx: ret=%d err=%v（0=超时正常）", ret, rerr)
}
