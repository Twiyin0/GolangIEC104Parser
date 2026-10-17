# GolangIEC104Parser（104ParserRealTime）

Windows 桌面版 **IEC 60870-5-101/104 实时报文解析工具**：Wireshark 式旁听抓包 + 多线程实时解析 + 点表点名，
单文件免安装 exe（Wails v2 + Vue3 静态前端，**纯 Go 实现，无 cgo**）。

## 主要功能

| 能力 | 说明 |
| --- | --- |
| 网口旁听（104） | 旁听模式 **bind/connect 终端端口**（零占用、对现场无干扰），按主站/终端 IP·端口过滤，自动识别主站端口 |
| 串口旁听（101） | 485 总线并联挂接被动旁听 / 232 点对点直接测试，链路地址可配 |
| 实时解析 | 多线程并行解析（默认 16，上限 256）+ 有界队列 + **顺序保持器**（结果仍按抓包顺序输出） |
| 点名（点表） | `Edit → dbSelect(点表加载)` 加载工程 SQLite 库：遥信/遥测/遥控/电能量/参数转发表；转发点表（`信息体地址 = 基址 + SwitchPoint`，点名为 `SwitchName`），四类基址可配置 |
| 定值表 | 国网 `ProjectGWParamPoint` / 南网 `ProjectNWParamPoint`（Addr 十进制匹配），可切换"无/国网/南网"并持久化 |
| 搜索与筛选 | 通配符/正则搜索、范围多选、组合条件筛选（时间/方向/协议/分类/TI/原文），命中整行高亮 |
| 导入导出 | 导出 markdown/text/log（按当前点表渲染点名，有筛选时询问范围）；导入 md/text/log 重新解析回放 |
| 文件服务解析 | 文件目录 / 文件列表 / 段落数据（msg / xml）解析并渲染 |
| 显示与体验 | 地址进制可配（各类地址十六进制/十进制，高位 0 不补位）、深浅主题、矢量图标、会话缓冲上限、暂停不缓冲 |
| 静态解析 | `Edit → Mode → Static Parser`：粘贴/导入报文文本直接解析 |

## 运行要求

- Windows 10/11 x64；需 **WebView2 Runtime**（系统一般自带）
- **网口旁听**需安装 **Npcap**（建议 1.16+，勾选 *WinPcap API-compatible Mode*），并以**管理员身份**运行
- 串口 / 静态解析 / 文件解析 / 导入导出**无需管理员权限**

## 获取与构建

- **下载**：见 [Releases](../../releases) —— 资产 `Golang104Parser-vX.X.X.exe`，单文件免安装，双击即可运行
- **本地构建**：双击 `build.bat`（或 `build.bat --nopause`），产物 `build\bin\104ParserRealTime.exe`
- **CI 发布**：推送形如 `vX.X.X` 的标签（如 `v0.0.1`）即触发 [`.github/workflows/release.yml`](.github/workflows/release.yml)：
  在 `windows-latest` 上跑单元测试 → 编译 `Golang104Parser-<tag>.exe` → 自动创建 Release 并上传资产

```bat
git tag v0.0.1
git push origin v0.0.1
```

## 目录结构

```
├─ main.go / app.go / appapi.go / dbapi.go / fileapi.go …   Wails 宿主与后端 API
├─ internal/protocol/   101/104 解析核心（common/asdu/iec104/iec101/detect/stream/classify）
├─ internal/capture/    抓包引擎（纯 Go syscall 绑定 wpcap.dll、TCP 流重组、串口、解析管线）
├─ internal/pointdb/    点表数据库（SQLite，转发点表 / 国网·南网定值表）
├─ internal/exporter/   导出渲染（markdown/text/log）
├─ internal/fileparse/  文件服务内容解析（msg/xml）
├─ frontend/dist/       Vue3 静态前端（无 npm 构建，直接内嵌）
├─ docs/                需求 / 设计 / 里程碑文档
├─ scripts/             构建辅助（mkico 图标生成、fe-smoke 前端冒烟）
└─ third_party/         Npcap SDK（运行时通过 wpcap.dll 调用，构建不需 cgo）
```

## 文档

- 需求与规则：[`docs/require.md`](docs/require.md)
- 使用与功能说明：[`docs/README.md`](docs/README.md)
- 里程碑与变更记录：[`docs/todoList.md`](docs/todoList.md)
- 目标与约束：[`docs/Goal.md`](docs/Goal.md)

## 许可证

[Apache License 2.0](LICENSE)
