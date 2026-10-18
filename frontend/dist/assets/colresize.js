// 表格列宽拖动：所有 .dtable / .msgtable 表头右边缘可拖动，宽度持久化到 localStorage。
(function () {
  "use strict";
  const PREFIX = "104.colw.v1.";
  let drag = null;

  const tables = () => document.querySelectorAll("table.dtable, table.msgtable");

  function tableKey(table) {
    const heads = Array.from(table.querySelectorAll("thead th")).map((th) => th.textContent.trim()).join("\u0001");
    return PREFIX + encodeURIComponent((table.className + "\u0002" + heads).slice(0, 400));
  }

  function loadWidths(table) {
    try {
      const raw = localStorage.getItem(tableKey(table));
      const ws = raw ? JSON.parse(raw) : null;
      return Array.isArray(ws) ? ws : null;
    } catch (_) {
      return null;
    }
  }

  function saveWidths(table, widths) {
    try { localStorage.setItem(tableKey(table), JSON.stringify(widths)); } catch (_) { /* ignore */ }
  }

  function applyWidths(table, widths) {
    const ths = Array.from(table.querySelectorAll("thead th"));
    if (!widths || widths.length !== ths.length) return;
    table.style.tableLayout = "fixed";
    let sum = 0;
    ths.forEach((th, i) => {
      const w = Math.max(38, Math.round(Number(widths[i]) || 0));
      th.style.width = w + "px";
      sum += w;
    });
    const paneW = table.parentElement ? table.parentElement.clientWidth : 0;
    table.style.width = Math.max(sum, paneW) + "px";
    table.style.minWidth = "100%";
  }

  function beginDrag(e) {
    const th = e.target && e.target.closest ? e.target.closest("th") : null;
    if (!th) return;
    const table = th.closest("table");
    if (!table) return;
    const r = th.getBoundingClientRect();
    // 只有靠近表头右边缘 7px 才进入拖动，避免影响表头点击/排序
    if (e.clientX < r.right - 7) return;
    e.preventDefault();
    e.stopPropagation();
    const ths = Array.from(table.querySelectorAll("thead th"));
    const idx = ths.indexOf(th);
    if (idx < 0) return;
    const cur = ths.map((x) => Math.max(38, x.getBoundingClientRect().width));
    table.style.tableLayout = "fixed";
    ths.forEach((x, i) => { x.style.width = Math.round(cur[i]) + "px"; });
    const sum = cur.reduce((a, b) => a + b, 0);
    table.style.width = Math.max(sum, table.parentElement ? table.parentElement.clientWidth : 0) + "px";
    table.style.minWidth = "100%";
    drag = { table, ths, idx, startX: e.clientX, startW: cur[idx] };
    document.body.classList.add("colresizing");
    window.addEventListener("pointermove", onMove, { passive: false });
    window.addEventListener("pointerup", onUp, { once: true });
  }

  function onMove(e) {
    if (!drag) return;
    e.preventDefault();
    const w = Math.max(38, Math.round(drag.startW + e.clientX - drag.startX));
    drag.ths[drag.idx].style.width = w + "px";
    let sum = 0;
    drag.ths.forEach((th) => { sum += parseFloat(th.style.width) || 0; });
    const paneW = drag.table.parentElement ? drag.table.parentElement.clientWidth : 0;
    drag.table.style.width = Math.max(sum, paneW) + "px";
  }

  function onUp() {
    if (!drag) return;
    const widths = drag.ths.map((th) => Math.round(parseFloat(th.style.width) || 0));
    saveWidths(drag.table, widths);
    document.body.classList.remove("colresizing");
    window.removeEventListener("pointermove", onMove);
    drag = null;
  }

  function resetWidths(e) {
    const th = e.target && e.target.closest ? e.target.closest("th") : null;
    if (!th) return;
    const table = th.closest("table");
    if (!table) return;
    const r = th.getBoundingClientRect();
    if (e.clientX < r.right - 7) return;
    try { localStorage.removeItem(tableKey(table)); } catch (_) { /* ignore */ }
    Array.from(table.querySelectorAll("thead th")).forEach((x) => { x.style.width = ""; });
    table.style.width = "";
    table.style.tableLayout = "";
    table.style.minWidth = "";
  }

  function ensure(table) {
    if (table.dataset.colResizeReady === "1") return;
    table.dataset.colResizeReady = "1";
    const ws = loadWidths(table);
    if (ws) applyWidths(table, ws);
    table.addEventListener("pointerdown", beginDrag);
    table.addEventListener("dblclick", resetWidths);
    Array.from(table.querySelectorAll("thead th")).forEach((th) => {
      th.title = th.title || "拖动右边缘调整列宽；双击边缘恢复";
    });
  }

  let pending = false;
  function scan() {
    if (pending) return;
    pending = true;
    requestAnimationFrame(() => {
      pending = false;
      tables().forEach(ensure);
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", scan);
  } else {
    scan();
  }
  new MutationObserver(scan).observe(document.body, { childList: true, subtree: true });
})();

