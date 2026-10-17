// 详情渲染（基础工具）：解析结果（Go JSON）→ HTML。与 exporter/render.go 语义一致。
"use strict";

function esc(s) {
  return String(s == null ? "" : s)
    .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

// 值格式化（含 NVA {raw, eng} 对象）
function valStr(v) {
  if (v == null) return "—";
  if (typeof v === "object") {
    if (v.raw !== undefined && v.eng !== undefined) return esc(v.raw) + " / 工程值 " + esc(v.eng);
    return esc(JSON.stringify(v));
  }
  if (typeof v === "boolean") return v ? "1" : "0";
  return esc(v);
}

// 斑马纹表格（偶/奇行双色由 CSS nth-child 实现）
function tableHTML(title, headers, rows) {
  if (!rows || rows.length === 0) return "";
  let h = '<div class="sec"><div class="sec-title">' + esc(title) + "</div>";
  h += '<table class="dtable"><thead><tr>';
  headers.forEach((x) => (h += "<th>" + esc(x) + "</th>"));
  h += "</tr></thead><tbody>";
  rows.forEach((r) => {
    h += "<tr>";
    r.forEach((c) => (h += "<td>" + c + "</td>"));
    h += "</tr>";
  });
  h += "</tbody></table></div>";
  return h;
}

function kvHTML(k, v) {
  if (v == null || v === "") return "";
  return '<div class="kv"><span class="k">' + esc(k) + "：</span>" + valStr(v) + "</div>";
}

function retHTML(ret) {
  if (ret === "success") return '<span class="ret-success">成功</span>';
  if (ret === "failed") return '<span class="ret-failed">失败</span>';
  return "—";
}

// 品质 QDS 文字化
function qdsHTML(qds) {
  const v = qds == null ? 0 : Number(qds);
  if (v === 0) return "正常";
  const f = [];
  if (v & 0x10) f.push("无效");
  if (v & 0x20) f.push("非当前");
  if (v & 0x08) f.push("被替代");
  if (v & 0x04) f.push("被闭锁");
  if (v & 0x03) f.push("溢出");
  return f.length ? "0x" + v.toString(16) + "(" + f.join("/") + ")" : "0x" + v.toString(16);
}

function dataRows(p, fn) {
  const d = p && p.data;
  return Array.isArray(d) ? d.map(fn) : [];
}
