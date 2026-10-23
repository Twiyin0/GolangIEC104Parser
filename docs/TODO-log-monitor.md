# TODO：log 文件监听模式（本机 + SSH/SFTP/SCP/FTP 已实现）

> 状态：**本机文件动态/静态、SSH（tail -F）、SCP、SFTP、FTP 增量监听已实现**；SSH 已用内置测试服务验证 `tail -F`，并在现场终端（192.168.1.101）实测解析 101 报文。
> 能力补充：远程目录浏览（日志文件右侧“列出”）、空字段默认值、断线重连成功提示、静态增量更新。
> 关联：`docs/todoList.md` M19；需求来源 2026-10-18。

## 1. 需求摘要

新增第三种工作模式 **log 文件监听模式**（与 `realtime`、`static` 并列）：

1. `Edit → Mode` 增加“log 文件监听模式”；
2. `Connection` 的“模式”选择 `log 监听模式` 时，应用可自动切到该模式；
3. 点击 `Edit → Mode → log 文件监听模式` 时，如果 `Connection` 里没有可用的 log 连接配置，提示用户先配置 log 监听连接信息；
4. 连接配置项：
   - 网卡（本机监听/出口网卡，仅本机文件或需要绑定时使用）
   - 协议：`ssh` / `ftp` / `sftp` / `scp` / 本机文件（local）
   - 终端 IP、端口、用户名、密码 / SSH Key
   - 监听文件路径（可先连接终端，再浏览终端目录并选择文件）
   - 监听模式：动态监听（文件每次写入都追加解析）/ 静态监听（点“更新”后读取一次）
   - Connection 的“解析栏”“显示栏”配置原样生效（点表、地址进制、转发基址、定值表等）
   - 自定义 log 格式：用户用正则提取 `{time}` 与 `{message}`；为空时用内置默认规则逐行匹配
5. 目标 log 示例：

```text
protolog:2026/10/18-16:36:40: file:Proto.cpp ,line:461If(COM0) Tx(6) --->  10 00 01 00 01 16
protolog:2026/10/18-16:36:40: file:Proto.cpp ,line:353If(COM0) Rx(18) <---   68 0C 0C 68 D3 01 00 64 01 0A 00 01 00 00 00 14 58 16
protolog:2026/10/18-16:36:40:!!!ScanLen[18]
SOCK1log:2026/10/18-16:36:41: file:Proto.cpp ,line:461If(SOCK1) Tx(6) --->  68 04 43 00 00 00
SOCK1log:2026/10/18-16:36:41: file:Proto.cpp ,line:353If(SOCK1) Rx(6) <---   68 04 83 00 00 00
```

用户可指定：

```regex
(?<prefix>protolog|SOCK[1-3]log):(?<time>[^:]+):(?<other>.*?)(?<dir>--->|<---)\s*(?<message>(?:[0-9A-Fa-f]{2}\s*)+)
```

解析器只取 `{time}`（终端时间）与 `{message}`（报文 HEX）；方向可选。

若用户自定义为空，则按内置默认规则匹配：

```regex
^\s*(?<prefix>[A-Za-z0-9_]*log)\s*:\s*(?<time>\d{4}/\d{2}/\d{2}-\d{2}:\d{2}:\d{2})\s*:.*?(?<dir>--->|<---)\s*(?<message>(?:[0-9A-Fa-f]{2}\s*)+)\s*$
```

即：只解析“时间 + 101/104 固定报文格式”，不解析无关行。

## 2. 配置模型（拟）

在 `config.Connection` 增加：

```go
type LogConn struct {
    NetIf      string `json:"netIf"`      // 网卡名（本机/出口绑定用）
    Protocol   string `json:"protocol"`   // local | ssh | sftp | scp | ftp
    Host       string `json:"host"`
    Port       int    `json:"port"`
    User       string `json:"user"`
    Password   string `json:"password"`   // 可空（改用 Key）
    SSHKeyPath string `json:"sshKeyPath"` // 私钥路径
    FilePath   string `json:"filePath"`   // 终端上的绝对/相对路径
    WatchMode  string `json:"watchMode"`  // dynamic | static
    FormatRe   string `json:"formatRe"`   // 自定义正则，空=默认
    Encoding   string `json:"encoding"`   // auto | utf-8 | gbk
    Proto      string `json:"proto"`      // auto | 101 | 104（默认按报文头识别）
    TailBytes  int    `json:"tailBytes"`  // 启动时回溯字节数（默认 64KB）
}
```

`Config.Mode` 增加 `log`；`Connection.Mode` 增加 `log`。

## 3. 后端技术路线

### 3.1 统一 Source 接口

现有 `internal/capture` 已有网口/串口 Source。新增 `internal/capture/logfile`，实现同一接口：

```go
type Source interface {
    Start() error
    Stop()
    Frames() <-chan RawFrame   // 时间/方向/HEX/会话标识
    Status() <-chan StatusEvent
}
```

`StartCapture` 按 `cfg.Connection.Mode` 分支：`net` / `serial` / `log`。

### 3.2 文件获取层（transport）

按协议拆小接口，便于先做本机再扩远程：

| 协议 | 实现思路 | 依赖 |
| --- | --- | --- |
| local | 直接 `os.Open` + 轮询 `Stat` 增量读取 | 标准库 |
| ssh | `golang.org/x/crypto/ssh` 执行 `tail -n +1 -F <path>` 流式读取；或轮询 `wc -c` + `dd` 增量 | `x/crypto`（当前 indirect，提为 direct） |
| sftp | `github.com/pkg/sftp`（或 `x/crypto/ssh` 自带 client）打开文件、`Read`/`ReadAt`、`Readdir` | 新增 |
| scp | 一次性下载到本地临时文件，再按 local 监听（适合静态模式） | `x/crypto/ssh` + `scp` 实现或外部命令 |
| ftp | `github.com/jlaffaye/ftp`：`RETR` 全量/`REST` 断点续传 + `SIZE`/`MDTM` 轮询 | 新增 |

统一抽象：

```go
type Fetcher interface {
    List(dir string) ([]FileEntry, error)     // 目录浏览
    Open(path string) (io.ReadCloser, error)  // 读取（动态流或全量）
    Stat(path string) (FileInfo, error)
    Close() error
}
```

### 3.3 监听策略

- **动态监听**：
  - local：`fsnotify` 或 200ms 轮询 `Stat`，记录 `offset`，只读新增字节；
  - ssh：`tail -F` 流式；
  - sftp/ftp：按 `size`/`mtime` 轮询增量，文件被截断/轮转时从 0 重读并记录事件。
- **静态监听**：只读一次；前端“更新”按钮触发重新读取并解析。
- **文件轮转/截断**：检测 `size < offset` → 重新从 0 开始，避免重复/丢失；日志文件常见按天切割。
- **编码**：`auto` 先 UTF-8，失败回退 GB18030（复用 `internal/fileparse` 已有解码）。

### 3.4 行解析（核心）

- 用户自定义正则优先：
  - 命名组：`time`、`message` 必选；`dir`、`prefix`、`other` 可选；
  - Go 的 `regexp` 不支持 `(?<name>...)`，需转换为 `(?P<name>...)`；前端可提示两种写法；
  - 若用户给的是整行捕获但未命名，则按“第一个捕获组=time、最后一个=message”兜底。
- 默认为空时用内置两条规则：
  1. 优先匹配带 `--->` / `<---` 的 101/104 报文行；
  2. 再匹配“时间 + 连续 HEX（≥4 字节）”的行，按首字节 `10`/`68` 自动判断 101/104。
- 时间解析：支持 `2026/10/18-16:36:40`、`2026-10-18 16:36:40`、`YYMMDD_HHMMSS[_mmm]` 等；统一为 `time.Time`。
- 报文解析：
  - 提取 HEX → `[]byte`；
  - `Proto` 为空/auto：首字节 `68` → 104；`10` → 101；否则按连接配置；
  - 复用现有 `protocol` 解析器（104 APDU / 101 FT1.2），结果与实时模式完全一致；
  - 方向：`--->` = 主站→终端（Tx），`<---` = 终端→主站（Rx）；无方向时按 `Tx`/`Rx` 或未知。
- 只把能解析成完整 101/104 帧的行送 session；不完整/无关行可计数丢弃并显示“忽略 N 行”。

### 3.5 会话与渲染

- `session` 增加 `SourceKind = "log"`，其余环形缓冲/索引/暂停语义完全复用；
- `enrichFrame` 不变：Connection 的解析/显示配置（点表 101/104 独立选择、基址、地址进制、定值表）照常生效；
- 前端详情/导出/搜索筛选零改动即可复用。

### 3.6 新增后端 API（拟）

```go
LogListDir(cfg LogConn, dir string) ([]FileEntry, error) // 连接终端并列出目录
LogTestConn(cfg LogConn) (string, error)                 // 测试连通性
LogStart(cfg LogConn) error                              // 开始监听（动态）
LogUpdate() error                                        // 静态监听：读取一次
LogStop() error
LogStatus() LogStatus                                    // 已读行数/解析帧数/忽略行数/最后错误
```

## 4. 前端技术路线

1. `Edit → Mode` 增加第三项 `log 文件监听模式`：
   - 若 `cfg.connection.mode === "log"` 且字段完整 → 直接切换；
   - 否则弹提示“请先在 Connection 配置 log 监听连接信息”，并自动打开 Connection 面板、定位到 log 分区。
2. `Connection` 模式下拉增加 `log 监听模式`；选择后表单切换为 log 字段（协议/IP/端口/用户名/密码/Key/文件路径/监听模式/格式正则）。
3. “浏览终端目录”按钮：调用 `LogListDir`，弹目录选择器；文件路径可手填。
4. 正则编辑区：
   - 输入框 + “测试”按钮，粘贴一行日志，实时显示提取出的 `time` / `message` / `dir`；
   - 为空时显示“使用默认规则”。
5. 主界面：
   - 顶部模式标识 `log`；状态栏显示 `已读行/解析帧/忽略行/最后错误`；
   - 静态监听显示“更新”按钮；
   - 其余列表/详情/搜索/导出复用现有界面。

## 5. 分阶段任务（建议顺序）

### P0 本机文件 + 默认解析（✅ 已完成 2026-10-20）
- [x] config：LogConn 字段 + Normalize
- [x] capture/logfile：local 动态/静态读取
- [x] 行解析器：默认规则 + time/HEX 提取 + 101/104 识别
- [x] StartCapture 分支 + session SourceKind
- [x] Edit→Mode 第三项 + Connection log 分区 + 提示逻辑
- [x] 单测：默认正则、时间解析、101/104 帧重组、静态/动态增量

### P1 SSH / SFTP 远程（✅ 2026-10-21）
- [x] SSH 密码/Key 认证 + `tail -F` 动态流
- [x] SFTP 增量轮询（最小 v3 客户端：OPEN/STAT/READ）
- [x] 断线重连、文件轮转/截断处理
- [x] 连接测试（SSH/SCP/SFTP/FTP）
- [x] 远程目录浏览：日志文件右侧“列出”按钮，SSH `ls -la` / SFTP `OPENDIR+READDIR` / FTP `PASV+LIST`，双击目录进入，选择文件自动填入绝对路径（2026-10-22）
- [x] 空字段默认值：`192.168.1.101` / `root` / `/data0/log/ftuMessage.log` / 22（FTP 21）；密码/私钥必须填写（2026-10-22）
- [x] 重连成功清除错误并提示成功；主动停止不误报失败（2026-10-21）

### P2 FTP / SCP 与自定义正则增强（✅ 大部分 2026-10-21）
- [x] FTP RETR/REST/SIZE 轮询
- [x] SCP 全量下载（静态，复用 SSH `cat`）
- [x] 自定义正则命名组/多规则/忽略规则
- [x] 编码自动识别 + GBK 日志
- [ ] 性能与压力测试（大文件、长时间运行）

## 6. 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 远程命令/依赖不可用（无 sshd、无权限） | 无法监听 | 连接测试 + 明确错误；本机文件模式始终可用 |
| 日志轮转/截断 | 重复或丢帧 | 记录 offset，检测 size 回退；轮转事件提示 |
| 大文件/高频写入 | 内存/CPU 压力 | 流式读取 + 有界队列 + 只保留 HEX；限制单行长度 |
| 正则写错/灾难性回溯 | 卡死 | 编译失败提示；行长度上限；Go regexp 线性匹配 |
| 凭据落盘 | 安全 | 优先 SSH Key；密码可加“不保存”选项；后续可接 Windows 凭据管理器 |
| 101/104 自动识别错误 | 解析失败 | 首字节判断 + 连接配置可强制 proto；无法解析行计数提示 |
| 与实时/静态模式切换 | 引擎状态混乱 | 统一 StartCapture/Stop；切换模式前先停止当前源 |

## 7. 验收口径（拟）

- 本机 log 动态监听：向文件追加示例行，界面实时出现对应 101/104 帧，点名/详情/导出与实时模式一致；
- 静态监听：点“更新”后读取增量，重复点击不产生重复帧；
- SSH/SFTP：能浏览目录、选择文件、断线后自动重连；
- 自定义正则：示例规则能提取 time/message/dir；“测试”按钮显示提取结果；
- 空正则：默认规则只解析时间+报文，忽略 `ScanLen`、`HandleFrm` 等无关行；
- 单测 + fe-smoke 全绿。
