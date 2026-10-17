// 应用动作（五）：文件服务内容解析（msg/xml 格式，自动识别 + 手动指定）。
"use strict";

const AppActions5 = {
  // 重新解析文件内容：fmt = "" 自动识别 | "msg" | "xml"
  reparseFile(key, fmt) {
    if (!key) return;
    this.api().ParseFile(key, fmt || "").then((res) => {
      if (this.selected && this.selected.parsed) {
        this.selected.parsed.fileParse = res;
      }
      const kind = (res && res.kindName) || "未知记录";
      const n = res && res.rows ? res.rows.length : 0;
      this.showToast((fmt ? "已按 " + String(fmt).toUpperCase() + " 格式解析" : "已自动识别") +
        "：" + kind + "，" + n + " 条记录" + (res && res.hasNames ? "（含点表点名）" : ""));
    }).catch((e) => this.showToast(((e && e.message) || e), true));
  },
};

for (const k in AppActions5) AppActions[k] = AppActions5[k];
