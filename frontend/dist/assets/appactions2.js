// 应用动作（二）：Connection 面板 / 主站端口扫描 / 静态解析 / 示例。
"use strict";

const AppActions2 = {
  // ── Connection（需求第 7 条）──
  openConn() {
    this.conn = Object.assign({}, DefaultConn, this.cfg.connection);
    this.showConn = true;
    this.closeMenus();
    if (this.conn.mode === "net") this.loadDevices();
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
    else this.loadSerialPorts();
  },
  applyConn() {
    this.cfg.connection = Object.assign({}, this.conn);
    this.syncParseCfgFromDb(); // 防止把定值表选择清成"无"
    this.api().UpdateConfig(this.cfg).then(() => {
      this.showConn = false;
      this.showToast("连接配置已应用" + (this.running ? "（监听运行中，重启监听后生效）" : ""));
    }).catch((e) => this.showToast(e.message || e, true));
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
      "68 0D 0D 68 73 01 00 64 01 06 00 01 00 00 00 14 F4 16",
    ].join("\n");
    this.showToast("已填入示例报文（104 U帧/总召唤/遥测 + 101 链路帧），点击解析");
  },
};

// 合并动作
for (const k in AppActions2) AppActions[k] = AppActions2[k];
