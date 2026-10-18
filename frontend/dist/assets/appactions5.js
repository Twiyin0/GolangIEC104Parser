// 应用动作（五）：文件服务内容解析（msg/xml 格式，自动识别 + 手动指定）。
"use strict";

const AppActions5 = {
  // 文件传输原始报文折叠：展开/折叠一组 TI=210 数据传输帧
  toggleFileGroup(row) {
    if (!row || !row.key) return;
    const expand = !this.expandedFileGroups[row.key];
    if (expand) this.expandedFileGroups[row.key] = true;
    else delete this.expandedFileGroups[row.key];
    // 展开时选中最后一条文件服务帧（S 帧可能夹在末尾，跳过），便于直接查看文件解析
    if (expand && row.rows && row.rows.length) {
      let last = row.rows[row.rows.length - 1];
      for (let i = row.rows.length - 1; i >= 0; i--) {
        const p = row.rows[i].parsed || {};
        if (p.service === "F_FR_NA_1") { last = row.rows[i]; break; }
      }
      if (this.view === "import") this.selectImported(last);
      else this.selectRealtime(last);
    }
  },

  // 重新解析文件内容：fmt = "" 自动识别 | "msg" | "xml"
  reparseFile(key, fmt) {
    if (!key) return;
    this.api().ParseFile(key, fmt || "").then((res) => {
      if (this.selected && this.selected.parsed) {
        this.selected.parsed.fileParse = res;
      }
      if (res && res.complete === false) {
        this.showToast((res.error || "文件传输未完成，等待中…"), true);
        return;
      }
      const kind = (res && res.kindName) || "未知记录";
      const n = res && res.rows ? res.rows.length : 0;
      this.showToast((fmt ? "已按 " + String(fmt).toUpperCase() + " 格式解析" : "已自动识别") +
        "：" + kind + "，" + n + " 条记录" + (res && res.hasNames ? "（含点表点名）" : ""));
    }).catch((e) => this.showToast(((e && e.message) || e), true));
  },
};

for (const k in AppActions5) AppActions[k] = AppActions5[k];
