# third_party — 随项目内置的第三方组件

## npcap-sdk-1.16/

来源：https://npcap.com/dist/npcap-sdk-1.16.zip（已下载原始 zip：`npcap-sdk-1.16.zip`，并解压为 `npcap-sdk-1.16/`）。

### 内容

| 目录 | 内容 |
| --- | --- |
| `Include/` | Npcap/libpcap/Packet32 C 头文件（23 个） |
| `Lib/`（x64/x86/ARM64） | `wpcap.lib`、`Packet.lib` 导入库 |
| `docs/wpcap/` | libpcap API 官方文档（HTML） |
| `Examples-pcap/`、`Examples-remote/` | 官方 C 示例工程 |

### 与本项目的关系

1. **当前构建不依赖 SDK**：本项目采用**纯 Go 运行时动态绑定**（`internal/capture/wpcap/wpcap.go` 通过 syscall 直接加载已安装的 `%SystemRoot%\System32\Npcap\wpcap.dll`，无 cgo、无链接期依赖），因此编译时**不需要** SDK 的头文件与导入库。
2. **为何仍内置**：
   - 项目自包含——开发者无需再访问 npcap.com 下载；
   - 供后续切换 cgo/gopacket 编译期链接方案时直接使用（见下）；
   - `docs/wpcap/` 的 API 文档可用于核对绑定函数语义（本项目绑定的函数：`pcap_create / pcap_set_snaplen / pcap_set_promisc / pcap_set_timeout / pcap_activate / pcap_next_ex / pcap_close / pcap_geterr / pcap_datalink / pcap_lib_version / pcap_findalldevs / pcap_freealldevs`）。
3. **若走 cgo 路线**（需要 C 编译器，本项目当前刻意避免）：
   ```powershell
   $env:CGO_ENABLED = "1"
   $env:CGO_CFLAGS   = "-IE:\project\Golang\104ParserRealTime\third_party\npcap-sdk-1.16\Include"
   $env:CGO_LDFLAGS  = "-LE:\project\Golang\104ParserRealTime\third_party\npcap-sdk-1.16\Lib\x64 -lwpcap"
   # 之后 gopacket/pcap 等库即可编译期链接（运行时仍加载已安装的 Npcap DLL）
   ```

### 重要边界：SDK ≠ Npcap 驱动

SDK **不包含** `wpcap.dll`/`Packet.dll` 运行时库与 NPF 内核驱动，**也不能随 exe 分发替代 Npcap 安装**。程序运行的机器必须安装 Npcap 驱动（旁听抓包需要；标准安装路径为 `C:\Windows\System32\Npcap\`）。本开发机已安装 **Npcap 1.88**（Packet Driver 服务运行中）。未安装的机器上：网口旁听不可用（程序会给出明确提示），静态解析 / 导入导出 / 串口功能不受影响。
