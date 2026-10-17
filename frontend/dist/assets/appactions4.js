// 应用动作（四）：Search by rules + Filter 搜索筛选系统；导出范围询问（全部/筛选结果）。
"use strict";

const AppActions4 = {
  // ── Search by rules（命中整行高亮）──
  applySearch() {
    const pat = (this.search.pattern || "").trim();
    if (!pat) {
      this.search.active = false;
      this.showToast("请输入搜索内容", true);
      return;
    }
    if (!(this.search.fields || []).length) {
      this.showToast("请至少选择一个搜索范围（时间/报文等）", true);
      return;
    }
    if (this.search.mode === "regex" && !window.SearchEngine.compileMatcher(pat, "regex")) {
      this.showToast("正则表达式无效，请检查语法", true);
      return;
    }
    this.search.active = true;
    this.showToast("搜索已应用：命中 " + this.hitCount + " 条（整行高亮）");
  },
  clearSearch() {
    this.search.active = false;
    this.search.pattern = "";
  },

  // ── Filter（组合筛选）──
  resetFilter() {
    const mode = this.filter.mode;
    this.filter = { timeFrom: "", timeTo: "", dir: "", proto: "", category: "", ti: "", hex: "", mode };
    this.filterCat = "";
    this.showToast("筛选条件已重置");
  },

  // ── 导出（需求：使用当前点表渲染；有筛选时询问范围）──
  currentExportSource() {
    if (this.cfg.mode === "static") return "static";
    return this.view === "import" ? "import" : "session";
  },
  doExport(kind) {
    const source = this.currentExportSource();
    const total = this.listFrames.length;
    const shown = this.displayFrames.length;
    if (this.filterActiveC && shown < total) {
      // 存在筛选且筛掉了部分报文 → 询问导出范围
      this.exportAsk = { kind, source, total, shown };
      return;
    }
    this.doExportNow(kind, source, false);
  },
  doExportNow(kind, source, onlyFiltered) {
    const list = source === "static" ? this.staticFrames
      : source === "import" ? this.importedFrames : this.frames;
    const ids = onlyFiltered ? this.displayFrames.map((f) => f.id) : [];
    const payload = source === "static" ? list : [];
    this.api().ExportDialog(kind, source, payload, ids).then((path) => {
      if (path) this.showToast("已导出" + (onlyFiltered ? "筛选结果" : "全部报文") + "（按当前点表渲染）：" + path);
      else this.showToast("已取消导出");
    }).catch((e) => this.showToast(((e && e.message) || e), true));
  },
  exportAskCancel() { this.exportAsk = null; },
  exportAskAll() {
    const a = this.exportAsk;
    this.exportAsk = null;
    if (a) this.doExportNow(a.kind, a.source, false);
  },
  exportAskFiltered() {
    const a = this.exportAsk;
    this.exportAsk = null;
    if (a) this.doExportNow(a.kind, a.source, true);
  },
};

for (const k in AppActions4) AppActions[k] = AppActions4[k];
