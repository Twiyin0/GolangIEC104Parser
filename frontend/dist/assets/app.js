// 应用入口：挂载 Vue、注册 Wails 事件、轮询、分栏拖动、初始化配置。
"use strict";

const app = Vue.createApp(Object.assign({}, AppCore, { methods: AppActions }));
app.config.errorHandler = (err) => {
  console.error(err);
  if (window.__vm) window.__vm.showToast("渲染错误：" + ((err && err.message) || err), true);
};
const vm = app.mount("#app");
window.__vm = vm;

// 矢量图标：首屏加载 + 监听 DOM 变化（动态面板/行）自动补齐
if (window.Icons) {
  Icons.loadIcons();
  Icons.watchIcons();
}

// Wails 事件注册
if (window.runtime && window.runtime.EventsOn) {
  window.runtime.EventsOn("frames:new", () => vm.pollOnce());
  window.runtime.EventsOn("status", (s) => {
    if (!s) return;
    vm.statusEvents.push(s);
    if (vm.statusEvents.length > 60) vm.statusEvents.shift();
  });
  window.runtime.EventsOn("master", (m) => { if (m && m.port) vm.masterPort = m.port; });
  window.runtime.EventsOn("state", (st) => {
    if (!st) return;
    vm.running = !!st.running;
    vm.paused = !!st.paused;
  });
  window.runtime.EventsOn("db", (info) => {
    if (!info) return;
    vm.dbInfo = info;
    vm.dbTable = info.curTable || "__all__";
    vm.syncParseCfgFromDb(); // 定值表选择同步进 cfg（避免后续配置推送清空）
  });
}

// 轮询：新帧（事件驱动为主，轮询兜底）+ 连接状态灯 + 统计
setInterval(() => {
  if (vm.cfg.mode === "realtime" && vm.view === "realtime" && !vm.paused) vm.pollOnce();
}, 400);
setInterval(() => {
  vm.api().GetConnStatus().then((s) => (vm.connStatus = s));
  vm.api().GetStats().then((s) => {
    const prevTotal = vm.stats && vm.stats.total ? vm.stats.total : 0;
    vm.stats = s || {};
    const nowTotal = vm.stats.total || 0;
    if (nowTotal < prevTotal) {
      // 会话被重置（用户清空或异常）→ 列表自愈同步，避免"列表有数据但点击为空"
      vm.frames = [];
      vm.lastId = 0;
      vm.selected = null;
      if (!vm.userReset) vm.showToast("检测到会话已重置，列表已同步");
      vm.userReset = false;
    }
  });
  vm.api().GetMasterPort().then((p) => { if (p > 0) vm.masterPort = p; });
  // 文件传输未完成：持续刷新当前选中项，传输完成后自动显示解析结果并停止轮询
  const sel = vm.selected;
  const fp = sel && sel.parsed && sel.parsed.fileParse;
  if (sel && sel.parsed && sel.parsed.fileKey && sel.parsed.fileKey.indexOf("210:") === 0 &&
      fp && fp.complete === false && vm.view === "realtime" && vm.source === "realtime") {
    vm.api().GetFrame(sel.id).then((f) => {
      if (!f || !f.type || !vm.selected || vm.selected.id !== sel.id) return;
      const nf = f.parsed && f.parsed.fileParse;
      // 没有新分段且仍在传输：不重复替换，避免详情每秒闪烁/滚动跳回顶部
      if (nf && nf.complete === false && nf.bytes === fp.bytes) return;
      if (nf && nf.complete && fp.complete === false) {
        vm.showToast("文件" + (nf.fileName ? " " + nf.fileName : "") +
          " 传输完成，已解析 " + (nf.rows ? nf.rows.length : 0) + " 条记录");
      }
      const pane = document.querySelector(".detailpane");
      const top = pane ? pane.scrollTop : 0;
      vm.selected = f;
      Vue.nextTick(() => { if (pane) pane.scrollTop = top; });
    }).catch(() => {});
  }
}, 1000);

// 初始化：配置 / 主题 / Npcap 信息
(function init() {
  const api = vm.api();
  if (!api) {
    vm.showToast("后端绑定未就绪（window.go.main.App 缺失）", true);
    return;
  }
  api.GetConfig().then((c) => {
    vm.cfg = c;
    vm.applyThemeNow();
  }).catch((e) => vm.showToast(e.message || e, true));
  api.PcapInfo().then((s) => (vm.pcapInfo = s));
  // 未安装 Npcap：弹窗引导安装（网口旁听依赖 Npcap 驱动）
  api.HasNpcap().then((ok) => {
    if (!ok) vm.showNpcapGuide = true;
  }).catch(() => {});
  api.IsRunning().then((r) => (vm.running = !!r));
  vm.pollOnce();
})();

// 输入安全网：若任一输入控件被祖先的 user-select:none 影响（WebView2 下会"可聚焦但无法输入"），
// 聚焦时强制恢复可编辑/可选中，避免"所有输入框均无法输入"。
(function inputEditableGuard() {
  const fix = (el) => {
    if (!el || !el.tagName) return;
    const t = el.tagName.toLowerCase();
    if (t !== "input" && t !== "textarea" && t !== "select") return;
    try {
      const cs = getComputedStyle(el);
      if (cs && (cs.userSelect === "none" || cs.webkitUserSelect === "none")) {
        el.style.webkitUserSelect = "text";
        el.style.userSelect = "text";
      }
      if (el.readOnly && t === "input" && el.type !== "checkbox" && el.type !== "radio") {
        el.readOnly = false; // 兜底：任何意外 readonly 的输入框都恢复可输入
      }
    } catch (e) { /* 忽略：不影响主流程 */ }
  };
  document.addEventListener("focusin", (e) => fix(e.target), true);
  document.addEventListener("mousedown", (e) => fix(e.target), true);
})();

// 点击空白关闭菜单 + 文件解析格式按钮（事件委托）
document.addEventListener("click", (ev) => {
  const fmtBtn = ev.target.closest && ev.target.closest("[data-filefmt]");
  if (fmtBtn) {
    vm.reparseFile(fmtBtn.getAttribute("data-filekey"), fmtBtn.getAttribute("data-filefmt"));
    return;
  }
  if (!ev.target.closest(".menu")) vm.closeMenus();
});

// ── 左右分栏拖动（需求：边界可调节）──
(function splitterInit() {
  let pane = null;    // 被调节宽度的左侧 pane
  let container = null; // 容器（.main 或 .staticbody）
  document.addEventListener("mousedown", (e) => {
    const el = e.target;
    if (!el || !el.classList || !el.classList.contains("splitter")) return;
    pane = el.previousElementSibling;
    container = el.parentElement;
    if (!pane || !container || !pane.classList.contains("listpane")) {
      pane = null;
      return;
    }
    el.classList.add("active");
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    e.preventDefault();
    e.stopPropagation();
  });
  document.addEventListener("mousemove", (e) => {
    if (!pane) return;
    const rect = container.getBoundingClientRect();
    let pct = ((e.clientX - rect.left) / rect.width) * 100;
    pct = Math.max(20, Math.min(80, pct));
    pane.style.flex = "0 0 " + pct + "%";
    e.preventDefault();
  });
  document.addEventListener("mouseup", () => {
    if (pane) {
      document.querySelectorAll(".splitter.active").forEach((el) => el.classList.remove("active"));
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
      pane = null;
      container = null;
    }
  });
})();
