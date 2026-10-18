# 任务清单 / 技术路线与实现方法（todoList.md）

> 本文档是项目的实施路线图：技术栈 → 各模块实现方法 → 分阶段任务清单（可勾选跟踪）。

## 1. 技术栈

| 分类 | 技术 | 版本/位置 |
| --- | --- | --- |
| 后端语言 | Go | 1.27.2，`E:\Developer\evn\go-1-27-2` |
| GUI 框架 | Wails v2 | v2.x（纯 Go 编译 Windows，无 cgo） |
| 前端 | Vue 3 + TypeScript + Vite + Tailwind CSS | Node v26.7.0 / npm 11.19.0 / yarn 4.17.1 |
| 网口抓包 | github.com/google/gopacket（pcap 后端） | 依赖 Npcap（本机已装，驱动运行中） |
| 串口 | go.bug.st/serial（或等价） | — |
| 状态管理（前端） | Pinia | 与参考项目一致 |
| 配置 | JSON（UTF-8） | — |
| 构建 | `wails build` → `build/bin/104ParserRealTime.exe` | 单 exe |

## 2. 技术路线与实现方法

### 2.1 协议解析核心（internal/protocol，M1）

**方法**：将 `example/VUE104Parser/src_parsers/104ParserClass.ts`（1240 行）与 `101ParserClass.ts`（1119 行）**逐函数等价移植**为 Go，保留全部修复语义（FIX-01~13、D-01~06）。

实现要点：
1. `common/`：CP56Time2a / CP24Time2a 解析（毫秒+秒+分+时+星期+日+月+年）、TLV 值解析（布尔/整型族/浮点族/字符串）、常量表（COT 文字、QOC、双点状态、DOW）；
2. `iec104/`：APCI（I/S/U 帧识别、序号、STARTDT/STOPDT/TESTFR）、ASDU 头（TI/VSQ/COT 含 T 位与 P/N 位/公共地址）、按 TI 分发的解析函数；IOA=3 字节；TI=9 归一化值 ÷32767 换算工程值；
3. `iec101/`：FT1.2 链路层（固定帧 10 C A1 A2 CS 16；可变帧 68 L L 68 … CS 16，CS 校验、方向位/FCB/FCV/ACD/DFC），剥链路层后复用 ASDU 解析；IOA=2 字节；额外支持 TI=104/105；
4. `detect.go`：101/104 自适应识别器——104 特征（TCP 流内 `68 LL` + 合法长度）；101 特征（10/68 帧头 + CS 校验通过 + 16H 结尾）；多级打分 + 滑动窗口再同步；
5. 解析结果统一为 Go 结构体（`ParsedFrame`），字段语义与 TS 版 `frame` 对象一一对应，便于前端平移渲染逻辑；
6. **单元测试**：构造典型报文样例集（总召唤、遥信遥测批量、遥控、SOE、时钟同步、定值读写、电能量、文件服务、TI=210、101 链路帧等），移植结果与 TS 版输出做快照对齐（M1 验收口径）。

### 2.2 抓包引擎（internal/capture，M2）

**方法**：定义统一抓包后端接口，网口与串口实现同一接口，上层无感知。

实现要点：
1. `backend.go`：
   ```go
   type Source interface {
       Start(cfg) error      // 打开网卡/串口，设置过滤
       Stop()                // 关闭并释放资源
       Frames() <-chan RawFrame // 原始帧（时间/方向/HEX/会话标识）
       Status() <-chan StatusEvent // 连接状态事件
   }
   ```
2. `pcap/`：gopacket 打开 Npcap 句柄（混杂模式）、`SetBPFFilter`；启动前检测管理员权限与 Npcap 可用性；枚举网卡列表供 Connection 配置选择；
3. `stream/`：会话表（四元组→双向缓冲）；按 TCP Seq 容忍重组（简单连续缓冲，乱序尽力、断洞截断并记录事件）；APDU 切分：`68 LL` → 帧长 = LL+2；
4. `serial/`：模式 A 只读旁听（485）；模式 B 内置最小主站问答；模式 C 串口↔TCP 转发（P2）；
5. 连接状态机（并入 session）：TCP SYN/FIN/RST + 104 U 帧（STARTDT_ACT/CON、STOPDT、TESTFR）→ 状态灯（未连接/链路建立/应用层启动/停止/断开）。

### 2.3 会话管理（internal/session，M2）

实现要点：
1. `MsgRecord{CaptureTime, Dir, Proto, Hex, Parsed, SessionKey}`；环形缓冲（默认上限 50000 条，可配置），超限丢最旧；
2. 索引：序号↔记录 O(1) 定位，供前端点击回查与导出全量遍历；
3. 暂停语义：暂停=停止向 UI 推送（缓冲继续，可配置"暂停时丢弃"）；继续=追平显示（可跳转最新）。

### 2.4 GUI 与前端（M3~M5）

**方法**：`wails init`（vue-ts 模板）建工程；Go 侧 `app.go` 暴露绑定方法 + `Events.Emit` 推送；前端 Vue 组件消费渲染。

实现要点：
1. 菜单栏采用 **HTML 菜单栏**（File / Edit / Setting 三组下拉，跨主题一致、可控禁用态与快捷键），不依赖平台原生菜单；
2. 组件：`MenuBar`、`ToolBar`（开始/停止/暂停/继续 + 状态灯）、`MsgList`（实时列表，虚拟滚动）、`ParseRender`（选中报文渲染区）、`ZebraTable`（斑马纹+独立表头色，YX/YC/YK/定值/电能量复用）、`StatusBar`；
3. 渲染逻辑（解析结果→HTML/表格行）从 `example/VUE104Parser/src/composables/useHtmlUtils.ts` 与视图（`HexParser.vue`、`FileParser.vue`）平移为 TS composable；
4. 事件流：Go `Events.Emit("frame:new", record)` → 前端批量合并刷新（≥30fps 节流），避免高流量刷爆 DOM；
5. 主题：`useTheme`（dark/light），Tailwind `dark:` 类 + CSS 变量双轨，主题配色含表头/斑马纹两套；
6. 静态模式（StaticView）：输入区（多行 hex/文本）→ "解析"按钮 → 逐条解析结果列表（布局与实时模式一致）→ 点击查看渲染。

### 2.5 导入导出（M6）

实现要点：
1. `exporter/`：
   - markdown：`# <文件名>` 一次性置顶；每帧一组 `## <捕获时间>` + `### <报文原文HEX>` + 正文（文本摘要 + Markdown 表格 + 图形化符号）；
   - text：行式 `时间<TAB>方向<TAB>协议<TAB>HEX<TAB>摘要`；
   - log：`[yyyy-MM-dd HH:mm:ss.SSS] [方向] [协议] HEX -> 摘要`；
2. `importer/`：md（提取 `###` 行的 HEX）、text/log（提取行内 HEX 字段）→ 逐帧重新送解析管线 → 渲染（与实时布局一致）；
3. HEX 提取正则容错（空格/换行/大小写/前缀文字）。

### 2.6 配置管理（M7）

实现要点：`config/` 单一 JSON（默认 `config.json`，随 exe 同目录或用户目录）；`SaveConfig/LoadConfig` 绑定到 File 菜单；字段含 connection（模式/终端IP/端口/主站IP/主站端口策略/网卡/串口参数）、ui（主题/自动滚动/暂停缓冲策略）、session（缓冲上限）、模式（realtime/static）；加载后热应用到界面（抓包引擎运行中则提示需重启监听）。

## 3. 里程碑任务清单

> [ ] 为待办；[x] 为完成。每个里程碑附验收方法。

### M0 文档（✅ 已完成 2026-10-16）
- [x] README.md（介绍/结构/技术路线）
- [x] require.md（需求文档）
- [x] todoList.md（本清单）
- [x] Goal.md（最终目标与注意事项）

### M1 解析核心移植（✅ 已完成 2026-10-16）
- [x] go.mod 初始化（Go 1.27.2，module iec104rt，GOPROXY=goproxy.cn）
- [x] common：CP56Time2a/CP24Time2a/TLV/常量表
- [x] iec104：APCI(I/S/U 帧) + ASDU 全 TI（含 FIX-01~13 语义）
- [x] iec101：FT1.2 链路层 + ASDU（含 D-01~06 语义、TI=104/105）
- [x] asdu 共享解析包（IOA 长度参数化：104=3 / 101=2 字节，双规约语义一致）
- [x] detect/protocol.go：101/104 自适应识别 + 流切帧（68 LL / FT1.2 边界 + 再同步）
- [x] 典型报文样例集（protocol_test.go + test101.go，14 用例）
- [x] 快照单测：FIX-01/02/03/07、101 链路层、自适应识别、切帧再同步全绿
验收：`go test ./internal/protocol/...` 全绿 ✅

### M2 抓包引擎与会话（✅ 已完成 2026-10-16）
- [x] Source 接口 + **纯 Go wpcap.dll 绑定**（syscall 直调 Npcap，无 cgo：pcap_create/set_* /activate/next_ex/findalldevs）
- [x] 以太网/IPv4/TCP 解析 + 过滤（支持 802.1Q；主站↔终端双向匹配）
- [x] TCP 流重组 + APDU 切分 + 再同步（方向缓冲 256KB 上限）
- [x] serial 后端（485 被动只读旁听 / 232 直接测试最小主站：复位链路→链路测试→总召唤）
- [x] 连接状态机（TCP SYN/FIN/RST + STARTDT/STOPDT/TESTFR）+ 状态灯
- [x] 主站端口双路识别（GetTcpTable 连接表 + SYN/STARTDT 方向推断）
- [x] 环形缓冲 + 二分索引 + 暂停语义（默认暂停不丢数据）
验收：构建通过；状态灯/旁听零干扰待现场实测（见 M9）。

### M3 Wails + 前端骨架（✅ 已完成 2026-10-16）
- [x] Wails v2.16 手工工程（无 CLI 依赖）：main.go + go:embed frontend/dist
- [x] 前端 Vue3 全局运行时（内嵌 vue.global.prod.js，零 npm 构建步骤）
- [x] MenuBar（File/Edit/Setting HTML 菜单 + 二级子菜单 + ✔ 状态）
- [x] ToolBar（开始/停止/暂停/继续 + 状态灯 + 主站端口[扫描] + 统计 + 类别筛选）
- [x] ZebraTable（斑马纹 + 独立表头色，双主题配色 CSS 变量）
- [x] useTheme 等价实现（dark/light）+ StatusBar（状态事件流）
- [x] app.go/appapi.go 绑定（配置/引擎控制/报文查询/导入导出/静态解析/对话框）
验收：`go build` 通过，窗口正常创建 ✅

### M4 实时模式完整功能（✅ 已完成 2026-10-16）
- [x] pcap → 流重组 → 解析 → 事件推送(节流) → 增量拉取刷新全链路
- [x] 点击报文 → 渲染区渲染（文本 + 各类斑马纹表格 + 品质/结果徽标）
- [x] 开始/停止/暂停/继续与引擎状态一致（事件 + 轮询双通道）
- [x] 连接状态灯（TCP + STARTDT/STOPDT 状态机，1s 刷新）
- [x] 高流量批量节流（100ms/500 帧）+ DOM 10000 行上限
验收：构建通过；端到端对通待现场实测（见 M9）。

### M5 静态模式（✅ 已完成 2026-10-16）
- [x] StaticView 输入区（HEX 多行，ParseStatic 逐行提取）
- [x] "解析"→ 逐条解析 → 结果列表（布局同实时）→ 点击渲染
- [x] 示例报文一键填入（104 U帧/总召唤/遥测 + 101 链路帧/总召唤）
验收：ParseStatic 与前端静态视图完成 ✅

### M6 导入导出（✅ 已完成 2026-10-16）
- [x] exporter markdown（层级硬规范 #/##/###/正文）/ text / log
- [x] importer（md ### 提取 / text、log token 化 HEX 提取）→ 重解析渲染
- [x] 导入数据标记来源并与实时缓冲隔离（"返回实时"按钮切换）
验收：导出→导入→渲染闭环单测全绿（roundtrip_test.go）✅

### M7 配置与主题持久化（✅ 已完成 2026-10-16）
- [x] config JSON 模型 + 默认值 + 向后兼容加载
- [x] SaveConfig/LoadConfig 菜单 + 热应用（连接参数运行中提示重启监听）
- [x] 主题持久化；Connection 面板（网卡/终端/主站 IP/端口/串口参数/模式/暂停丢弃/缓冲上限）
验收：配置往返一致（Normalize 单一入口）✅

### M8 构建发布（✅ 已完成 2026-10-16）
- [x] `go build -trimpath -tags desktop,production -ldflags "-s -w -H windowsgui" -o build\bin\104ParserRealTime.exe .`（Wails 纯 go build 必须带 `-tags desktop,production`）
- [x] 单 exe 5.3MB（Vue 运行时与前端资源全部内嵌，零外部依赖）
- [x] 管理员权限/Npcap/WebView2 缺失的兜底提示（静态解析/导入导出不受影响）
- [x] 构建命令固化（README §5）
验收：构建成功 + 启动冒烟测试通过（进程存活、窗口正常创建）✅

### M10 点表数据库（✅ 已完成 2026-10-16，需求补充）
- [x] 纯 Go SQLite 读取（modernc.org/sqlite，无 cgo）
- [x] internal/pointdb：五张点表读取 + 全点表/切换模式映射（优先级与 db.ts 一致）+ 单测（含建库自测）
- [x] Edit → dbSelect 菜单（文件对话框选择 .db/.sqlite/.sqlite3）
- [x] 工具栏点表状态条：✓已加载/未加载 + 表模式下拉（全部/具体表）+ 移除点表
- [x] 点名/参数名注入（enrich）：GetFrame/静态解析/导入/三种导出统一注入；表格渲染"点名"列（页面 + markdown）
- [x] 联动刷新：点表变化后选中帧重渲染、静态模式自动重解析、101 经参数转发表兜底
验收：pointdb 单测 + fe-smoke 点名列断言全绿 ✅

### M11 搜索与筛选系统 + 导出范围（✅ 已完成 2026-10-17，需求补充）
- [x] search.js 匹配引擎：通配符（`*`/`?`，含通配时整串匹配、否则子串包含）与正则表达式，非法正则安全返回
- [x] Search by rules：搜索范围多选（报文原文/摘要/TI/时间/方向/协议/分类/点名），命中**整行高亮** + 命中计数
- [x] Filter 组合筛选：时间范围、方向、协议、分类、TI（列表/通配/正则）、原文报文；`筛选结果 x / 总数` 计数 + 重置
- [x] 工具栏 🔍 Search 按钮与折叠面板（亮/暗主题样式）
- [x] 导出范围询问：有筛选时弹窗选择"导出全部报文 / 仅导出筛选结果"（显示 x/共 y）
- [x] 导出统一按**当前点表**渲染点名（session/import/static 三种数据源，导出前 enrich）
- [x] 导入报文分配 ID 并由后端保存副本，支持筛选后导出
- [x] fe-smoke 新增 22 项搜索/筛选断言（通配符/正则/范围隔离/TI/组合筛选）
验收：`node scripts/fe-smoke.js` + `go test ./...` 全绿 ✅

### M12 矢量图标与 Logo（✅ 已完成 2026-10-17，需求补充）
- [x] 从参考项目 `public/iconLib` 选取 29 个所需图标内置到 `frontend/dist/assets/iconLib/`
- [x] 实现 `icons.js` 图标加载器：运行时 **fetch + 内联 SVG 注入**，规范化时把子元素写死的 `stroke="#000000"` 等颜色统一改写为 `currentColor`（避免恒黑不可见），并保留 mask 兜底；`.app-icon` 随主题/禁用态自动变色
- [x] 图标应用：菜单（Export/Import/SaveConfig/LoadConfig/Mode/dbSelect/Connection/Theme）、工具栏（开始/停止/暂停/继续/清空/返回实时/Search/主站端口/帧统计/自动滚动/点表/移除点表）、搜索面板标题、状态栏
- [x] 界面 Logo：`assets/logo.svg`（品牌区）；状态点改为纯 CSS 圆点（无 emoji）
- [x] exe 图标：`scripts/mkico` 将 `logo.png` 生成 7 尺寸 ICO → `rsrc` 生成 `rsrc.syso` → 构建自动包含
- [x] build.bat 集成图标生成（失败自动降级为默认图标）
验收：ExtractIconEx 检测到 exe 图标；窗口启动正常；`go test ./...` + fe-smoke 全绿 ✅

### M13 文件服务内容解析（msg/xml，✅ 已完成 2026-10-17，需求补充）
- [x] `internal/fileparse`：格式自动识别（XML 优先，其次 MSG 分隔符）、类型判定（SOE/故障/遥控/事件）、key=value 与 UTF-16 BOM 兼容
- [x] 记录点号用当前点表解析点名（遥控→yk、SOE/故障→yx，未命中依次兜底；支持十进制与 0x 十六进制）
- [x] 文件组包：TI=210 op=5 按 fileId 归并分段（去重）、TI=125 按 NOF 归并、TI=126 标记完成
- [x] GetFrame 附带 `fileKey` + `fileParse`（自动识别结果），点击文件传输帧即可查看解析
- [x] 前端"文件内容解析"区：格式/记录类型/点名状态/记录表格 + **手动指定 MSG/XML/自动** 按钮（事件委托）
- [x] 解析缓存按（文件+格式+点表版本）；点表变更自动失效并重新解析
- [x] 单测 9 项（XML/MSG/类型/强制格式/垃圾内容/key=value/无点表/UTF-16）+ fe-smoke 文件解析断言 8 项
验收：`go test ./...`（6 包）+ fe-smoke 全绿 ✅

### M14 界面细节修复（✅ 已完成 2026-10-17）
- [x] **窗口/任务栏图标**：定位到根因——Wails v2 不给窗口设置图标（WM_GETICON=0），exe 资源图标只影响文件图标；
      新增 `winicon.go`：启动后按进程 ID 枚举可见顶层窗口并通过 `WM_SETICON` 应用 exe 内 logo（含大/小图标）
- [x] 差分验证：跳过设置时窗口图标句柄 0/0，应用后非零 → 替换生效；诊断开关 `P104_NO_ICON=1`
- [x] **滚动条主题化**：细圆角滑块 + 透明轨道 + 隐藏箭头按钮 + 悬停/拖动变色，暗/亮主题各一套配色（CSS 变量 `--sb-thumb*`）
- [x] **header 显示优化**：按钮文字不折行（`nowrap`）、空间不足时整项换行、左右分组（操作 / 状态与配置）
- [x] 启动健壮性：导入名冲突修复、启动失败自动重试一次、失败弹窗提示（windowsgui 无控制台）
- [x] 编译脚本：`build.bat` 编译前仅结束本程序进程（**绝不触碰 WebView2 进程**，其运行时为多软件共用）
验收：图标差分验证通过；`go test ./...`（7 包）+ fe-smoke 全绿 ✅

### M15 解析管线与会话稳定性（✅ 已完成 2026-10-17，需求补充）
- [x] **修复"数据量变大后突然不解析"**（根因：点击"自动滚动"触发 UpdateConfig 重建会话 → 引擎写旧会话、查询读新会话）：
      改为 `session.SetMax` 就地调整上限；引擎回调动态读取当前会话；前端对会话重置自愈同步；并补回归测试
- [x] **多线程解析管线**：抓包线程仅做 TCP 解析/流重组/切帧，原始报文**带序号**投递有界队列，
      工作协程池并行解析（**默认 16 并发、上限 256**，队列缓冲默认 4096，均可在 Connection 配置）
- [x] **顺序保持器**：并发解析结果按入队序号重排输出，保证界面/导出顺序与抓包顺序一致
      （测试：首帧故意慢 60ms、8 并发仍严格按序输出）
- [x] 非阻塞投递：队列满丢弃并计数，被丢弃序号自动跳过（后续顺序不乱、不卡死）；
      工具栏显示"队列 N ｜ 重排 M ｜ 丢弃 K"
- [x] 队列语义澄清：队列是**等待解析的报文缓冲（背压）**，不是本次解析总量上限（界面与文档均已说明）
- [x] 停止顺序：先退抓包循环再排空队列与顺序保持器；串口后端同样接入管线
- [x] 单测：按序输出、队列满丢弃后顺序递增不卡死、队列投递、会话 SetMax、会话不重建
验收：`go test ./...`（8 包）+ fe-smoke 全绿 ✅

### M16 转发点表与定值表规则修正（✅ 已完成 2026-10-17，需求补充）
- [x] **转发点表地址基值（选定具体点表时，点名 = SwitchName）**：
      遥信 **0x0000** + SwitchPoint、遥测 **0x4000** + SwitchPoint、遥控 **0x6000** + SwitchPoint、电能量 **0x6400** + SwitchPoint
      （此前仅遥测按 0x4000、其余误按裸 SwitchPoint 并回落到全表 → 点位错乱）
- [x] 四类基值均可在 Setting → Connection 配置（默认如上），随配置文件持久化
- [x] **定值表（国网 `ProjectGWParamPoint` / 南网 `ProjectNWParamPoint`）**：
      字段 Addr（十进制）/Name/Rate/ParaType/StrEncodeName；工具栏"定值表：无/国网/南网"**开放给用户选择**；
      定值信息体地址 = **Addr（十进制）**，名称 = Name；未选择时回落参数转发表
- [x] 定值渲染：**地址默认显示十六进制**（优先 StrEncodeName），列序固定为 **#,地址,点名,值,参数类型,比例,操作,SN**（去掉"类型"列）
- [x] **地址显示进制可配置**：Connection → 显示 → 地址显示（遥信/遥测/遥控/电能量/定值 各自选择十六进制/十进制），
      默认 遥信=十进制、遥测/遥控/电能量/定值=十六进制；**十六进制高位为 0 不补位**
      （3 字节地址 `0x006401` → `0x6401`、`0x004001` → `0x4001`）；页面与 markdown 导出统一使用（`addrShow`）
- [x] **Connection 面板 UI 重构**：加宽至 820px、分区（监听/解析/显示）、字段两列布局、
      长说明改为悬浮提示（title），去除冗余描述
- [x] **定值"参数类型/比例"缺失修复（2026-10-17）**：根因 = 参数类型/比例只来自"当前选定定值表"，
      而定值表选择仅存内存且前端 `cfg.parse.paramTable` 从未与点表状态同步 → 任意一次配置推送
      （主题/自动滚动/Connection"应用"）即把选择清成"无"，重启后"参数类型/比例"整列为"—"。
      修复：① 取值兜底链 选定表 →（无时）参数转发表含 Rate/ParaType → 国网 → 南网；
      ② 选择立即落盘 + 退出再落盘；③ 前端同步 `cfg.parse.paramTable`；④ `UpdateConfig` 空值自愈；
      ⑤ 基址/定值表热应用 + 文件解析缓存失效
- [x] **输入框"无法输入"修复（2026-10-17）**：实测确认真因不在前端（真实 Chromium 下输入框可正常输入），
      而在宿主层——WebView2 内容未获得键盘焦点时**键盘事件不进入页面**
      （窗口可见、鼠标可点、输入框有聚焦样式，但敲键无反应；`GetGUIThreadInfo` 显示 `focusHwnd=0`）。
      修复：启动后 `BringWindowToTop+SetForegroundWindow`（跨线程 `AttachThreadInput`）+ `SetFocus`
      到 WebView2 渲染子窗口并校验；另对输入控件强制 `user-select:text`（避免全局 none 继承影响）、
      打开 Connection 面板自动聚焦首个输入框。验证：`isAppForeground=True`、`focusClass=Chrome_WidgetWin_1`
- [x] Edit 菜单项统一为 **`dbSelect(点表加载)`**
- [x] 网卡下拉 **IP 过多时省略显示**（最多 3 个 + "等 N 个"，完整 IP 见悬浮提示），修复长文本撑破面板
- [x] 重复启动保护：已运行时的第二个实例自行退出（不会出现重复窗口）
- [x] 测试：pointdb（四类基值命中、裸 SwitchPoint 不命中、国网/南网切换、Rate/ParaType/StrEncodeName）
      + main（enrich 定值点名与附加信息、国网↔南网切换）+ fe-smoke（定值列渲染断言 5 项）
验收：`go test ./...`（8 包）+ fe-smoke 全绿 ✅

### M9 验证收尾（⏳ 代码与自测完成，现场实测待进行）
- [x] 自动化测试 15 项全绿（`go test ./internal/...`）
- [x] 四份文档更新至实现终态（结构/命令/勾选状态）
- [ ] 对照规约 PDF 抽样核对解析（遥信/遥测/遥控/定值/电能量/文件服务）——建议现场对照
- [ ] `Goal.md` §6 演示脚本 1~9 现场过一遍（含旁听零干扰实测、485/232 串口实测）
- [ ] 干净 Win10 x64（管理员）机器双击运行验证

### M17 开源发布（GitHub 仓库 / 许可证 / CI 发布）2026-10-17

- [x] **仓库初始化**：本地 `git init` → 关联远程 `https://github.com/Twiyin0/GolangIEC104Parser.git`（默认分支 `master`），
      并**拉取远程 LICENSE**（Apache License 2.0）到本项目
- [x] **`.gitignore`**：忽略构建产物（`/build/`、`*.exe`）、本地配置（`config/config.json`）、点表库（`*.db/*.sqlite*`）、
      日志/临时文件、编辑器目录（`.idea/.vscode/.claude`）、`node_modules`；
      **保留** `frontend/dist`（需内嵌）与 `third_party`（Npcap SDK，1.48 MB）
- [x] **`.gitattributes`**：`* text=auto`；`*.bat/*.cmd/*.ps1` 强制 CRLF；图片/图标/syso/zip/dll/lib/db/pdf 标记 binary
- [x] **`.github/workflows/release.yml`**：推送 `vX.X.X` 标签（或手动 `workflow_dispatch`）触发，`windows-latest` 上
      `go mod download` → `go test ./internal/...` → 生成图标 `rsrc.syso` →
      `go build -trimpath -tags desktop,production -ldflags "-s -w -H windowsgui"` →
      产物命名 **`Golang104Parser-<tag>.exe`** → `softprops/action-gh-release` 创建 Release 并上传资产（含 SHA256 校验信息）
- [x] **发布 v0.0.1（初版）**：Release 资产 `Golang104Parser-v0.0.1.exe`（15.85 MB，
      SHA256 `1F33BD82F6E2F33BFD1C058978CFE98073D8833107891B1A7EB2E6FA42945961`）
- [x] **`example/VUE104Parser`** 以 **git submodule** 引用（原为嵌套仓库，直接提交会生成空 gitlink）
- [x] 新增根 `README.md`（功能表 / 运行要求 / 构建与发布 / 目录结构 / 文档索引 / 许可证）
- [ ] **待办（需 GitHub 侧操作）**：本地 PAT 缺少 `workflow` 权限，`git push` 被拒
      （`refusing to allow a Personal Access Token to create or update workflow`）→
      在 GitHub → Settings → Developer settings → Tokens (classic) 勾选 **`workflow`** 后执行
      `git push origin master` 即生效；随后推送 `vX.X.X` 标签由 CI 自动编译并发布


### M18 101/104 独立点表（✅ 已完成 2026-10-18，需求补充）
- [x] `pointdb.DB` 增加 `CurTable101` / `CurTable104` 与两套协议独立的 Switch 映射；`RebuildMapsFor(proto, table)` 单协议切换
- [x] `PtNameFor(proto, typ, addr)`：101/104 各自按所选表模式查询；`PtName` 保留为 104 兼容入口
- [x] `SetBases`/`SetYCBase` 重建映射时不再重置协议选择（避免配置推送把选择清掉）
- [x] App：`DbSetTableFor(proto,name)` 新 API；`DbInfo` 返回 `curTable101`/`curTable104`
- [x] `enrichFrame` 按 `f.Proto` 选表；101 未命中仍保留参数转发表兜底
- [x] 前端工具栏改为两个下拉：`101：全部/具体表`、`104：全部/具体表`，互不影响
- [x] 单测：pointdb `TestPointDBPerProtocol` + main `TestPointTablePerProtocol`（101 转发表/104 全表，反之亦然）
验收：`go test ./...` + fe-smoke 全绿 ✅

### M19 log 文件监听模式（⏳ 仅设计，待确认后实施）
- [ ] 需求与连接配置模型（协议/终端/账号/文件路径/动态静态/自定义正则）
- [ ] 后端：local / ssh / sftp / ftp / scp 文件获取 + 增量监听 + 行解析（time/message）
- [ ] 前端：Edit→Mode 第三项、Connection log 分区、目录浏览、正则测试
- 技术路线（摘要，完整版见本地 `docs/TODO-log-monitor.md`，该目录默认 gitignore，提交时需 `git add -f`）：
  1. **配置**：`Connection` 增加 `LogConn{Protocol,Host,Port,User,Password,SSHKeyPath,FilePath,WatchMode,FormatRe,Encoding,Proto}`；`Mode` 增加 `log`；
  2. **后端 Source**：新增 `internal/capture/logfile`，实现与网口/串口相同的 `Source` 接口；`StartCapture` 按 `Mode` 分支；
  3. **文件获取**：local 直接增量读；ssh 用 `x/crypto/ssh` 执行 `tail -F`；sftp/ftp 轮询 `size/mtime` 增量；scp 全量下载（静态）；统一 `Fetcher{List/Open/Stat/Close}`；
  4. **监听**：动态=追加解析（记录 offset，检测文件截断/轮转）；静态=“更新”按钮读一次；
  5. **行解析**：自定义正则命名组 `time/message/dir`（`(?<x>)` 自动转 Go `(?P<x>)`）；为空时用内置规则匹配“时间 + 101/104 HEX”；时间支持 `YYYY/MM/DD-HH:MM:SS`、`YYMMDD_HHMMSS[_mmm]`；报文按首字节 `68`/`10` 自动判 101/104，复用现有解析器；
  6. **会话/渲染**：session 增加 `SourceKind="log"`，`enrichFrame` 与现有解析/显示配置（含 101/104 独立点表）完全复用；
  7. **前端**：Edit→Mode 第三项；Connection 模式选 log 时自动进入；无配置时提示并打开 Connection；协议/账号/路径/监听模式/正则编辑 + “测试”按钮 + 目录浏览；静态模式显示“更新”；
  8. **API**：`LogListDir` / `LogTestConn` / `LogStart` / `LogUpdate` / `LogStop` / `LogStatus`；
  9. **阶段**：P0 本机文件+默认解析 → P1 SSH/SFTP 远程 → P2 FTP/SCP + 自定义正则增强 + 编码/压力测试；
  10. **风险**：远程依赖不可用（连接测试兜底）、日志轮转（offset 检测）、大文件（流式+有界队列）、正则错误（编译提示+行长上限）、凭据落盘（优先 SSH Key）。
- 状态：**未开始编码**，等确认方案后再进入 P0

## 4. 风险与对策

| # | 风险 | 影响 | 对策 |
| --- | --- | --- | --- |
| 1 | 非管理员运行 / Npcap 异常 | 网口旁听不可用 | 启动检测 + 明确指引；静态/导入功能不受影响 |
| 2 | 高流量（总召唤风暴）丢帧/卡 UI | 数据不完整 | goroutine 解析 + 事件批量节流 + 环形缓冲有界 |
| 3 | 移植语义漂移（TS→Go） | 解析结果与参考不一致 | M1 快照单测逐字段对齐；以规约 PDF 为最终准绳 |
| 4 | TCP 乱序/重传/分片跨包 | APDU 切分错乱 | 流缓冲按 Seq 容忍重组 + 滑动窗口再同步 + 事件留痕 |
| 5 | 串口"被动旁听"被误解为任何串口可行 | 用户预期落空 | UI 明示 485 总线可行 / 232 仅直连或转发（Goal §4-3） |
| 6 | Wails/依赖版本漂移 | 构建失败 | go.mod / package.json 锁定版本；命令固化 README §5 |
| 7 | WebView2 缺失 | 白屏 | 启动检测 + 引导安装 |
| 8 | 主站端口识别不准（多连接/NAT） | 过滤偏差 | 连接表 + SYN/STARTDT 双路推断，界面可手工锁定/取消过滤 |