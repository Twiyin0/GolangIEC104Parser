// 应用动作（二）：Connection 面板 / 主站端口扫描 / 静态解析 / 示例。
"use strict";

const AppActions2 = {
  // 远程 log 连接空字段补默认值（密码/私钥保持为空，必须由用户填写）
  normalizeLogConn(lc) {
    if (!lc) return lc;
    if (!lc.protocol) lc.protocol = "ssh";
    if (lc.protocol !== "local") {
      if (!lc.host) lc.host = "192.168.1.101";
      if (!lc.user) lc.user = "root";
      if (!lc.filePath) lc.filePath = "/data0/log/ftuMessage.log";
      if (!lc.port) lc.port = lc.protocol === "ftp" ? 21 : 22;
    }
    return lc;
  },
  // ── Connection（需求第 7 条）──
  openConn() {
    this.conn = Object.assign({}, DefaultConn, this.cfg.connection);
    this.conn.log = this.normalizeLogConn(Object.assign({}, DefaultConn.log, (this.cfg.connection || {}).log || {}));
    this.showConn = true;
    this.closeMenus();
    if (this.conn.mode === "net") this.loadDevices();
    else if (this.conn.mode === "log") { /* no device enumeration */ }
    else this.loadSerialPorts();
    // 打开面板即聚焦首个文本输入框：WebView2 在页面内容获得焦点前不接收键盘事件，
    // 主动聚焦可避免"点进输入框却打不了字"（无需先点一下界面）。
    this.$nextTick(() => {
      const el = document.querySelector(".modal input[type=text], .modal input[type=number]");
      if (el) { try { el.focus(); } catch (e) { /* 忽略 */ } }
    });
  },
  loadDevices() {
    this.api().ListDevices().then((list) => {
      this.devices = list || [];
      if (!this.conn.device && this.devices.length) {
        // 默认自动匹配与终端网段一致的网卡
        const seg = (this.conn.terminalIp || "").split(".").slice(0, 3).join(".");
        const hit = this.devices.find((d) => (d.ips || []).some((ip) => ip.indexOf(seg + ".") === 0));
        this.conn.device = (hit || this.devices[0]).name;
      }
    });
  },
  // 网卡下拉显示：IP 过多时省略（最多 3 个 + "等N个"），完整 IP 见悬浮提示
  deviceLabel(d) {
    if (!d) return "";
    const desc = d.desc || d.name || "";
    const ips = d.ips || [];
    if (!ips.length) return desc;
    const head = ips.slice(0, 3).join(", ");
    const more = ips.length > 3 ? ` 等${ips.length}个` : "";
    return `${desc} [${head}${more}]`;
  },
  loadSerialPorts() {
    this.api().ListSerialPorts().then((list) => {
      this.serialPorts = list || [];
      if (!this.conn.device && this.serialPorts.length) this.conn.device = this.serialPorts[0];
    });
  },
  onConnModeChange() {
    if (this.conn.mode === "net") this.loadDevices();
    else if (this.conn.mode === "log") { /* log 不需要枚举本机设备 */ }
    else this.loadSerialPorts();
  },
  // 切换 log 传输协议时同步默认端口（SSH 22 / FTP 21 / 本机 0）
  onLogProtocolChange() {
    const p = this.conn.log.protocol;
    if (p && p !== "local") {
      if (!this.conn.log.host) this.conn.log.host = "192.168.1.101";
      if (!this.conn.log.user) this.conn.log.user = "root";
      if (!this.conn.log.filePath) this.conn.log.filePath = "/data0/log/ftuMessage.log";
    }
    this.conn.log.port = p === "ftp" ? 21 : (p === "local" ? 0 : 22);
  },
  // 选择本机日志文件（log 监听配置）
  pickLogFile() {
    this.api().LogPickFileDialog().then((p) => {
      if (p) this.conn.log.filePath = p;
    }).catch((e) => this.showToast(e.message || e, true));
  },
  // 测试 log 连接（当前支持本机文件可读性）
  testLogConn() {
    this.api().LogTestConn(this.conn.log).then((msg) => this.showToast(msg))
      .catch((e) => this.showToast(e.message || e, true));
  },
  // ── log 远程文件浏览器 ──
  openLogBrowser() {
    const p = this.conn.log.filePath || "/data0/log/ftuMessage.log";
    const dir = p.substring(0, p.lastIndexOf("/")) || "/";
    // 标记文件选择按钮可用：目录只显示条目，点“选择”需命中文件
    this.showLogBrowser = true;
    this.loadLogDir(dir);
  },
  closeLogBrowser() {
    this.showLogBrowser = false;
  },
  logParent(dir) {
    const d = (dir || "/").replace(/\/+$/, "");
    if (!d || d === "/") return "/";
    const i = d.lastIndexOf("/");
    return i <= 0 ? "/" : d.substring(0, i);
  },
  loadLogDir(dir) {
    const target = dir || "/";
    this.logBrowser.dir = target;
    this.logBrowser.loading = true;
    this.logBrowser.error = "";
    this.logBrowser.items = [];
    this.logBrowser.sel = "";
    this.logBrowser.isFileSel = false;
    this.api().LogListDir(this.conn.log, target).then((items) => {
      if (this.logBrowser.dir !== target) return; // 已切换到其它目录，忽略过期结果
      this.logBrowser.items = items || [];
      this.logBrowser.loading = false;
    }).catch((e) => {
      if (this.logBrowser.dir !== target) return;
      this.logBrowser.loading = false;
      this.logBrowser.error = (e && e.message) || String(e);
    });
  },
  chooseLogFile(it) {
    if (!it || it.dir) return;
    this.conn.log.filePath = it.path;
    this.showLogBrowser = false;
    this.showToast("已选择日志文件：" + it.path);
  },
  chooseLogFileBySel() {
    const it = (this.logBrowser.items || []).find((x) => x.path === this.logBrowser.sel);
    if (it && !it.dir) this.chooseLogFile(it);
  },
  fmtSize(n) {
    if (!n) return "";
    if (n < 1024) return n + " B";
    if (n < 1024 * 1024) return (n / 1024).toFixed(1) + " KB";
    return (n / 1024 / 1024).toFixed(1) + " MB";
  },
  applyConn() {
    this.cfg.connection = Object.assign({}, this.conn);
    if (this.conn.mode === "log") this.cfg.mode = "log"; // Connection 选 log 时自动进入 log 监听模式
    this.syncParseCfgFromDb(); // 防止把定值表选择清成"无"
    this.api().UpdateConfig(this.cfg).then(() => {
      this.showConn = false;
      this.showToast("连接配置已应用" + (this.running ? "（监听运行中，重启监听后生效）" : ""));
    }).catch((e) => this.showToast(e.message || e, true));
  },

  // ── Npcap 缺失引导（网口旁听依赖 Npcap 驱动）──
  npcapDownload() {
    this.api().OpenNpcapDownload().then(() => {
      this.showNpcapGuide = false;
      this.showToast("已打开 Npcap 下载页；安装完成后请重启本程序");
    }).catch((e) => this.showToast(((e && e.message) || e), true));
  },

  // ── 主站端口扫描（需求第 7 条：默认本机，端口自动识别）──
  detectMaster() {
    this.api().DetectMasterPortManually().then((p) => {
      this.masterPort = p;
      this.showToast("主站端口识别：" + p);
    }).catch(() => this.showToast("未找到本机→终端的 TCP 连接（主站可能尚未连接）", true));
  },

  // ── 静态解析（需求第 13 条）──
  parseStatic() {
    this.api().ParseStatic(this.staticText).then((list) => {
      this.staticFrames = list;
      if (list.length) this.selected = list[0];
      this.source = "static";
      this.showToast("解析完成：" + list.length + " 条");
    }).catch((e) => this.showToast(e.message || e, true));
  },
  // 静态模式：选择 log 文件，自动提取 Tx/Rx 行中的 101/104 报文
  parseLogFile() {
    this.api().ParseLogFileDialog().then((res) => {
      if (res && res.note === "已取消选择") return;
      this.applyLogResult(res);
    }).catch((e) => this.showToast(((e && e.message) || e), true));
  },
  // 静态模式：把文本框内容当 log 解析（支持直接粘贴终端日志）
  parseLogText() {
    const text = (this.staticText || "").trim();
    if (!text) {
      this.showToast("请先粘贴 log 文本，或点击“解析 Log 文件”", true);
      return;
    }
    this.api().ParseLogText(text).then((res) => this.applyLogResult(res))
      .catch((e) => this.showToast(((e && e.message) || e), true));
  },
  applyLogResult(res) {
    const list = (res && res.frames) || [];
    if (!list.length) {
      this.showToast((res && res.note) || "未识别到 101/104 报文", true);
      return;
    }
    this.staticFrames = list;
    this.selected = list[0];
    this.source = "static";
    const prefix = res.fileName ? res.fileName + "：" : "";
    this.showToast("Log " + prefix + (res.note || ("解析 " + list.length + " 帧")));
  },
  // 静态解析页：拖入 .log/.txt/.md 后自动按 Log 规则解析
  async onDropLog(e) {
    this.dragOverLog = false;
    const file = e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0];
    if (!file) return;
    const ext = (file.name.split(".").pop() || "").toLowerCase();
    if (["log", "txt", "md"].indexOf(ext) < 0) {
      this.showToast("拖入文件仅支持 .log / .txt / .md", true);
      return;
    }
    try {
      const buf = await file.arrayBuffer();
      const text = decodeDroppedText(buf);
      if (!text.trim()) {
        this.showToast("文件内容为空：" + file.name, true);
        return;
      }
      // 小文件同步显示到文本框，便于核对；大文件只解析，避免 UI 卡顿
      if (text.length <= 200000) this.staticText = text;
      const res = await this.api().ParseLogText(text);
      if (res) res.fileName = file.name;
      this.applyLogResult(res);
    } catch (err) {
      this.showToast("读取/解析文件失败：" + ((err && err.message) || err), true);
    }
  },
  selectStatic(r) {
    this.selected = r;
    this.source = "static";
  },
  clearStatic() {
    this.staticText = "";
    this.staticFrames = [];
    this.selected = null;
  },
  fillSample() {
    this.staticText = [
      "68 04 07 00 00 00",
      "68 0E 04 00 00 00 64 01 06 00 01 00 00 00 00 14",
      "68 12 04 00 00 00 0D 01 03 00 01 00 01 00 00 00 00 00 80 3F 00",
      "10 00 01 00 01 16",
      "68 0C 0C 68 73 01 00 64 01 06 00 01 00 00 00 14 F4 16",
    ].join("\n");
    this.showToast("已填入示例报文（104 U帧/总召唤/遥测 + 101 链路帧），点击解析");
  },
};

// 拖入文件解码：UTF-8 优先，失败回退 GBK（浏览器 TextDecoder 支持 gbk）
function decodeDroppedText(buf) {
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(buf);
  } catch (_) {
    try {
      return new TextDecoder("gbk").decode(buf);
    } catch (_) {
      return new TextDecoder().decode(buf);
    }
  }
}

// 合并动作
for (const k in AppActions2) AppActions[k] = AppActions2[k];
