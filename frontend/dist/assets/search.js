// 搜索与筛选匹配引擎：通配符（* ?）与正则表达式，支持字段范围与组合筛选条件。
"use strict";

// 可搜索范围（字段）
const SEARCH_FIELDS = [
  { k: "hex", label: "报文原文" },
  { k: "summary", label: "摘要" },
  { k: "ti", label: "TI/类型" },
  { k: "time", label: "时间" },
  { k: "dir", label: "方向" },
  { k: "proto", label: "协议" },
  { k: "category", label: "分类" },
  { k: "name", label: "点名/参数名" },
];

function escapeRe(s) {
  return s.replace(/[.+^${}()|[\]\\]/g, "\\$&");
}

// 编译匹配器：mode=regex 直接用正则；mode=wildcard 时——
//   含 * ? → 整串通配匹配；不含 → 子串包含（更符合直觉）
function compileMatcher(pat, mode) {
  if (!pat) return null;
  try {
    if (mode === "regex") return new RegExp(pat, "i");
    const hasWild = /[*?]/.test(pat);
    if (hasWild) {
      return new RegExp("^" + escapeRe(pat).replace(/\*/g, ".*").replace(/\?/g, ".") + "$", "i");
    }
    return new RegExp(escapeRe(pat), "i");
  } catch (e) {
    return null;
  }
}

// 取某字段的待匹配值（可能多值：TI/类型、点名可能多点）
function fieldValues(f, field) {
  const p = f.parsed || {};
  switch (field) {
    case "hex": return [f.hex || ""];
    case "summary": return [f.summary || "", f.tiName || ""];
    case "ti": return [String(f.ti || 0), f.tiName || ""];
    case "time": return [f.time || ""];
    case "dir": return [f.dir || ""];
    case "proto": return [f.proto || ""];
    case "category": return [f.category || ""];
    case "name": {
      const out = [];
      (p.data || []).forEach((d) => d && d.name && out.push(d.name));
      (p.yx || []).forEach((d) => d && d.name && out.push(d.name));
      (p.yc || []).forEach((d) => d && d.name && out.push(d.name));
      return out;
    }
  }
  return [];
}

// 规则搜索：任一选中字段命中即算命中
function matchSearch(f, fields, re) {
  if (!re) return false;
  for (const fl of fields || []) {
    for (const v of fieldValues(f, fl)) {
      if (v && re.test(String(v))) return true;
    }
  }
  return false;
}

// TI 匹配：正则模式整体匹配；通配符模式支持逗号分隔列表（如 1,3,9 或 1*,3?）
function matchTi(f, spec, mode) {
  const ti = String(f.ti || 0);
  const typeName = String(f.tiName || "");
  if (mode === "regex") {
    const re = compileMatcher(spec, "regex");
    return !!re && (re.test(ti) || re.test(typeName));
  }
  const tokens = String(spec).split(/[,，\s]+/).filter(Boolean);
  for (const tok of tokens) {
    if (/[*?]/.test(tok)) {
      const re = compileMatcher(tok, "wildcard");
      if (re && re.test(ti)) return true;
    } else if (tok === ti) {
      return true;
    }
  }
  return false;
}

// 组合筛选：时间范围（前缀比较，支持 "2026-10-16 15:00" 等）、方向、协议、分类、TI、原文
function matchFilter(f, flt) {
  if (!flt) return true;
  const t = String(f.time || "");
  if (flt.timeFrom && t.slice(0, flt.timeFrom.length) < flt.timeFrom) return false;
  if (flt.timeTo && t.slice(0, flt.timeTo.length) > flt.timeTo) return false;
  if (flt.dir && f.dir !== flt.dir) return false;
  if (flt.proto && f.proto !== flt.proto) return false;
  if (flt.category && f.category !== flt.category) return false;
  if (flt.ti && !matchTi(f, flt.ti, flt.mode)) return false;
  if (flt.hex) {
    const re = compileMatcher(flt.hex, flt.mode);
    if (!re || !re.test(String(f.hex || ""))) return false;
  }
  return true;
}

function filterActive(flt) {
  return !!flt && !!(flt.timeFrom || flt.timeTo || flt.dir || flt.proto || flt.category || flt.ti || flt.hex);
}

window.SearchEngine = {
  SEARCH_FIELDS,
  compileMatcher,
  fieldValues,
  matchSearch,
  matchTi,
  matchFilter,
  filterActive,
};
