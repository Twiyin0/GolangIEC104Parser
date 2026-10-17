// 应用核心：数据模型与计算属性（Vue Options API，无构建步骤）。
"use strict";

const DefaultConn = {
  mode: "net", device: "", terminalIp: "192.168.1.101", terminalPort: 2404,
  masterIp: "", masterPort: 0, promisc: false,
  serialMode: "passive", baudRate: 9600, dataBits: 8, parity: "even", stopBits: 1, linkAddr: 1,
};

const AppCore = {
  data() {
    return {
      cfg: {
        connection: Object.assign({}, DefaultConn),
        ui: { theme: "dark", autoScroll: true, dropWhenPaused: false },
        session: { maxFrames: 50000, exportDir: "", parseWorkers: 16, parseQueueSize: 4096 },
        parse: {
          yxTransferBase: 0, ycTransferBase: 16384, ykTransferBase: 24576, enTransferBase: 25600,
          paramTable: "",
          addrFormat: { yx: "dec", yc: "hex", yk: "hex", en: "hex", para: "hex" },
        },
        mode: "realtime",
      },
      openMenu: "",
      view: "realtime", // realtime | import（导入回放视图）
      running: false,
      paused: false,
      connStatus: "未监听",
      masterPort: 0,
      stats: {},
      frames: [],
      lastId: 0,
      selected: null,
      source: "realtime", // realtime | static | import
      statusEvents: [],
      toast: "",
      toastErr: false,
      toastTimer: 0,
      showConn: false,
      conn: Object.assign({}, DefaultConn),
      devices: [],
      serialPorts: [],
      pcapInfo: "",
      staticText: "",
      staticFrames: [],
      importedFrames: [],
      filterCat: "",
      dbInfo: { ok: false, detail: "", tableNames: [], curTable: "", paramTable: "", hasGwParam: false, hasNwParam: false },
      dbTable: "__all__",
      dbParamTable: "",
      userReset: false, // 用户主动清空标记（用于会话重置自愈提示）
      // 搜索与筛选系统
      searchOpen: false,
      search: { pattern: "", mode: "wildcard", fields: ["hex", "summary"], active: false },
      filter: { timeFrom: "", timeTo: "", dir: "", proto: "", category: "", ti: "", hex: "", mode: "wildcard" },
      exportAsk: null, // { kind, source, total, shown }
      searchFields: (typeof SEARCH_FIELDS !== "undefined" ? SEARCH_FIELDS : []),
    };
  },
  computed: {
    connClass() {
      const s = this.connStatus;
      if (s.indexOf("已连接") >= 0) return "up";
      if (s === "握手中") return "syn";
      if (s === "未监听") return "off";
      return "down";
    },
    masterPortText() {
      return this.masterPort > 0 ? String(this.masterPort) : "自动识别中…";
    },
    // 当前所选网卡的完整描述 + 全部 IP（用于下拉的悬浮提示）
    deviceFullTitle() {
      const d = (this.devices || []).find((x) => x.name === this.conn.device);
      if (!d) return "";
      const ips = (d.ips || []).join(", ");
      return (d.desc || d.name) + (ips ? " [" + ips + "]" : "");
    },
    detailHTML() {
      return this.selected ? FrameRender.frameDetailHTML(this.selected) : "";
    },
    statusTail() {
      return this.statusEvents.slice(-4);
    },
    listFrames() {
      return this.view === "import" ? this.importedFrames : this.frames;
    },
    displayFrames() {
      let list = this.listFrames;
      if (this.filterCat) list = list.filter((f) => f.category === this.filterCat);
      const eng = window.SearchEngine;
      if (eng && eng.filterActive(this.filter)) {
        list = list.filter((f) => eng.matchFilter(f, this.filter));
      }
      return list;
    },
    categories() {
      const set = {};
      this.listFrames.forEach((f) => (set[f.category] = 1));
      return Object.keys(set);
    },
    // 规则搜索命中集合（整行高亮）：{ id: 1 }
    hitIds() {
      const out = {};
      const eng = window.SearchEngine;
      const s = this.search;
      if (!eng || !s.active || !s.pattern || !(s.fields || []).length) return out;
      const re = eng.compileMatcher(s.pattern, s.mode);
      if (!re) return out;
      for (const x of this.displayFrames) {
        if (eng.matchSearch(x, s.fields, re)) out[x.id] = 1;
      }
      return out;
    },
    hitCount() {
      return Object.keys(this.hitIds).length;
    },
    filterActiveC() {
      const eng = window.SearchEngine;
      return (eng && eng.filterActive(this.filter)) || !!this.filterCat;
    },
    filterInfo() {
      return this.displayFrames.length + " / " + this.listFrames.length;
    },
    modeLabel() {
      return this.cfg.mode === "static" ? "Static Parser" : "RealTime Parser";
    },
  },
};
