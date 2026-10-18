// 应用动作（三）：Edit → dbSelect 点表数据库（选择/切换模式/移除 + 点名联动刷新）。
"use strict";

const AppActions3 = {
  // 同步"定值表选择"到 cfg：前端 cfg 会被整体推送给后端（UpdateConfig/SaveConfig），
  // 若不同步，任何一次配置推送（主题/自动滚动/Connection 应用）都会把定值表清成"无"，
  // 表现为定值表"参数类型/比例"整列变成"—"。
  syncParseCfgFromDb() {
    if (!this.cfg || !this.cfg.parse) return;
    this.cfg.parse.paramTable = (this.dbInfo && this.dbInfo.paramTable) || "";
  },
  // 统一刷新点表状态：101/104 各自的选择分开保存，互不覆盖。
  applyDbInfo(info) {
    this.dbInfo = info || { ok: false, detail: "", tableNames: [], curTable: "", curTable101: "", curTable104: "", paramTable: "" };
    this.dbTable101 = this.dbInfo.curTable101 || "__all__";
    this.dbTable104 = this.dbInfo.curTable104 || "__all__";
    this.dbParamTable = this.dbInfo.paramTable || "";
  },
  // 选择点表数据库（.db/.sqlite/.sqlite3）
  dbSelect() {
    this.api().DbSelectDialog().then((info) => {
      this.applyDbInfo(info);
      this.syncParseCfgFromDb();
      // 提示只显示文件名（明细悬浮点表标识查看），避免占用空间
      const brief = this.dbInfo.ok ? String(this.dbInfo.detail || "").split("  ")[0] : "";
      this.showToast(this.dbInfo.ok ? "点表已加载：" + brief : "点表未加载（已取消或失败）");
      this.refreshAfterDbChange();
    }).catch((e) => this.showToast(((e && e.message) || e), true));
  },
  // 切换指定协议（"101"/"104"）的点表模式（全部 / 具体表），另一协议不受影响
  setDbTable(proto) {
    const v = proto === "101" ? this.dbTable101 : this.dbTable104;
    const name = v === "__all__" ? "" : v;
    this.api().DbSetTableFor(proto, name).then((info) => {
      this.applyDbInfo(info);
      this.syncParseCfgFromDb();
      this.showToast(proto + " 点表模式：" + (name || "全部点表"));
      this.refreshAfterDbChange();
    }).catch((e) => this.showToast(((e && e.message) || e), true));
  },
  // 切换定值表（国网 / 南网 / 无）：定值信息体地址 = Addr（十进制），名称 = Name
  setDbParamTable() {
    this.api().DbSetParamTable(this.dbParamTable || "").then((info) => {
      this.applyDbInfo(info);
      this.syncParseCfgFromDb();
      const label = this.dbParamTable === "gw" ? "国网" : this.dbParamTable === "nw" ? "南网" : "无";
      this.showToast("定值表：" + label);
      this.refreshAfterDbChange();
    }).catch((e) => this.showToast(((e && e.message) || e), true));
  },
  // 移除点表
  dbClear() {
    this.api().DbClear().then((info) => {
      this.applyDbInfo(info);
      this.syncParseCfgFromDb();
      this.showToast("已移除点表数据库");
      this.refreshAfterDbChange();
    });
  },
  // 点表变化后联动刷新：实时选中帧重新拉取（重新注入点名）；静态模式自动重新解析
  refreshAfterDbChange() {
    if (this.selected && this.source === "realtime" && this.selected.id) {
      this.selectRealtime(this.selected);
    }
    if (this.cfg.mode === "static" && (this.staticText || "").trim()) {
      this.parseStatic();
    }
  },
};

for (const k in AppActions3) AppActions[k] = AppActions3[k];
