// 应用动作：监听控制 / 报文拉取与选择 / 导入导出 / 配置与主题 / 静态解析。
"use strict";

const AppActions = {
  api() { return window.go && window.go.main && window.go.main.App; },

  // 统一筛选：分类快捷筛选 + Filter 面板（协议/时间/方向/TI/原文等）
  filterList(list) {
    let out = list || [];
    if (this.filterCat) out = out.filter((f) => f.category === this.filterCat);
    const eng = window.SearchEngine;
    if (eng && eng.filterActive(this.filter)) {
      out = out.filter((f) => eng.matchFilter(f, this.filter));
    }
    return out;
  },

  showToast(msg, isErr) {
    this.toast = String(msg == null ? "" : msg);
    this.toastErr = !!isErr;
    clearTimeout(this.toastTimer);
    this.toastTimer = setTimeout(() => (this.toast = ""), 4000);
  },

  // ── 菜单 ──
  toggleMenu(m) { this.openMenu = this.openMenu === m ? "" : m; },
  closeMenus() { this.openMenu = ""; },

  // ── 监听控制（需求第 5 条）──
  startListen() {
    if (this.cfg.mode === "log" && this.cfg.connection.mode !== "log") {
      this.showToast("请先在 Connection 选择 log 监听模式并配置文件路径", true);
      this.openConn();
      this.conn.mode = "log";
      return;
    }
    const api = this.api();
    api.StartCapture()
      .then(() => { this.running = true; this.pollOnce(); this.showToast(this.cfg.mode === "log" ? "已开始日志监听" : "已开始监听（旁听模式，不占用端口）"); })
      .catch((e) => this.showToast(e.message || e, true));
  },
  stopListen() {
    this.api().StopCapture();
    this.running = false;
    this.logStatus = {};
    this.showToast("监听已停止（已缓冲报文保留，可导出/查看）");
  },
  // 静态 log 监听：读取自上次位置以来的新增内容
  logUpdate() {
    this.api().LogUpdate().then(() => {
      this.pollOnce(true);
      this.showToast("日志已更新（仅解析新增内容）");
    }).catch((e) => this.showToast(e.message || e, true));
  },
  pauseListen() {
    this.api().PauseCapture();
    this.paused = true;
    this.showToast("已暂停：界面冻结，后台默认继续缓冲（可在 Connection 配置暂停时丢弃）");
  },
  resumeListen() {
    this.api().ResumeCapture();
    this.paused = false;
    this.pollOnce(true);
    this.showToast("已继续监听");
  },
  clearList() {
    this.userReset = true; // 标记为用户主动清空（避免"会话已重置"提示）
    this.api().ClearSession();
    this.frames = [];
    this.lastId = 0;
    this.selected = null;
    this.showToast("已清空本次监听缓冲");
  },

  // ── 报文拉取（增量）与选择 ──
  pollOnce(catchup) {
    if (this.paused) return; // 暂停时前端冻结；继续后从 lastId 追平后端缓冲
    if ((this.cfg.mode !== "realtime" && this.cfg.mode !== "log") || this.view !== "realtime") return;
    const api = this.api();
    if (this.cfg.mode === "log") {
      api.LogStatus().then((s) => { this.logStatus = s || {}; }).catch(() => {});
    }
    api.GetFrames(this.lastId, 400).then((list) => {
      if (!list || !list.length) return;
      for (const f of list) {
        this.frames.push(f);
        this.lastId = f.id;
      }
      while (this.frames.length > 10000) this.frames.shift(); // DOM 上限，缓冲仍在后端
      if (this.cfg.ui.autoScroll || catchup) this.scrollBottom();
    });
  },
  scrollBottom() {
    this.$nextTick(() => {
      const el = document.querySelector(".listpane .scroller");
      if (el) el.scrollTop = el.scrollHeight;
    });
  },
  selectRealtime(r) {
    this.view = "realtime";
    this.source = "realtime";
    this.api().GetFrame(r.id).then((f) => {
      if (!f || !f.type) {
        // 会话缓冲中已无该报文（已清空 / 被环形上限淘汰）
        this.selected = null;
        this.showToast("该报文已不在当前会话缓冲中（可能已清空或被缓冲上限淘汰）", true);
        return;
      }
      this.selected = f;
    }).catch((e) => this.showToast("获取解析结果失败：" + ((e && e.message) || e), true));
  },
  backToRealtime() {
    this.view = "realtime";
    this.selected = null;
    this.pollOnce();
  },

  // ── 导出（File → Export：markdown / text / log，需求第 8 条）──
  doExport(kind) {
    const api = this.api();
    const p = kind === "md" ? api.ExportMarkdownDialog()
      : kind === "txt" ? api.ExportTextDialog() : api.ExportLogDialog();
    p.then((path) => {
      if (path) this.showToast("已导出：" + path);
      else this.showToast("已取消导出");
    }).catch((e) => this.showToast(e.message || e, true));
  },

  // ── 导入（File → Import，需求第 9 条）──
  doImport() {
    this.api().ImportFileDialog().then((list) => {
      if (!list || !list.length) return;
      this.importedFrames = list;
      this.view = "import";
      this.cfg.mode = "realtime";
      this.selected = null;
      this.showToast("导入成功：" + list.length + " 条报文，点击查看解析结果");
    }).catch((e) => this.showToast(e.message || e, true));
  },
  selectImported(r) {
    this.view = "import";
    this.source = "import";
    this.selected = r;
  },

  // ── 配置（File → SaveConfig / LoadConfig，需求第 10/11 条）──
  saveConfig() {
    this.syncParseCfgFromDb();
    this.api().SaveConfig().then((p) => this.showToast("配置已保存：" + p))
      .catch((e) => this.showToast(e.message || e, true));
  },
  loadConfig() {
    this.api().LoadConfig().then((c) => {
      this.cfg = c;
      this.applyThemeNow();
      this.showToast("配置已加载并应用");
    }).catch((e) => this.showToast(e.message || e, true));
  },

  // ── 主题与模式（需求第 12/13 条）──
  setTheme(t) {
    this.cfg.ui.theme = t;
    this.applyThemeNow();
    this.syncParseCfgFromDb();
    this.api().UpdateConfig(this.cfg);
  },
  applyThemeNow() {
    document.body.classList.toggle("light", this.cfg.ui.theme === "light");
  },
  setMode(m) {
    if (m === "log") {
      const conn = this.cfg.connection || {};
      const lc = conn.log || {};
      if (conn.mode !== "log" || !lc.protocol || !lc.filePath) {
        this.showToast("请先在 Connection 配置 log 监听连接信息", true);
        this.openConn();
        this.conn.mode = "log";
        this.conn.log = Object.assign({}, DefaultConn.log, lc);
        return;
      }
    }
    this.cfg.mode = m;
    this.syncParseCfgFromDb();
    this.api().UpdateConfig(this.cfg);
    this.selected = null;
    const label = m === "static" ? "静态解析模式（Static Parser）"
      : m === "log" ? "log 文件监听模式（Log Monitor）" : "实时解析模式（RealTime Parser）";
    this.showToast("已切换：" + label);
  },
  setAutoScroll() { this.syncParseCfgFromDb(); this.api().UpdateConfig(this.cfg); },
};
