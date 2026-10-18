// 前端渲染冒烟测试（Node 离线执行，验证 render.js/render2.js 完整且无运行时异常）：
//   node scripts/fe-smoke.js
"use strict";
const fs = require("fs");
const path = require("path");
const vm = require("vm");

const ctx = { window: {}, console, fetch: () => Promise.reject(new Error("no fetch in test")) };
vm.createContext(ctx);
for (const f of ["icons.js", "search.js", "render.js", "render2.js"]) {
  const p = path.join(__dirname, "..", "frontend", "dist", "assets", f);
  vm.runInContext(fs.readFileSync(p, "utf8"), ctx, { filename: f });
}
const { frameDetailHTML } = ctx.window.FrameRender;
const SE = ctx.window.SearchEngine;
const ICONS = ctx.window.Icons;

const frames = [
  { id: 1, time: "2026-10-16 18:00:00.000", dir: "M→T", proto: "104", hex: "68 04 07 00 00 00",
    type: "other", category: "链路", summary: "STARTDT_ACT(启动数据传输)",
    parsed: { type: "other", format: "U", ctrl: "07 00 00 00", desc: "STARTDT_ACT(启动数据传输)" } },
  { id: 2, time: "2026-10-16 18:00:01.000", dir: "T→M", proto: "104", hex: "68 12 04 00 00 00 09 01 03 00 01 00 01 00 00 FF 7F 00",
    type: "yc", tiName: "M_ME_NA_1(归一化遥测)", category: "遥测", summary: "遥测 1 点",
    parsed: { type: "yc", ti: 9, addr: 1, cotDesc: "spont", pn: 0, test: 0,
      data: [{ slot: 1, addr: 1, value: { raw: 32767, eng: 1 }, valueType: "NVA(归一化)", qds: 0 }] } },
  { id: 3, time: "2026-10-16 18:00:02.000", dir: "T→M", proto: "104", hex: "68 0D 04 00 00 00 2D 01 07 00 01 00 01 00 00 81",
    type: "control", category: "遥控", summary: "0x000001 选择确认 控合",
    parsed: { type: "control", ti: 45, cotDesc: "actcon",
      data: [{ slot: 1, addr: "0x000001", state: "控合", operation: "选择确认", qoc: "无附加定义", return: "success" }] } },
  { id: 4, time: "2026-10-16 18:00:03.000", dir: "T→M", proto: "104", hex: "68 1E 04 00 00 00 2A 01 03 00 01 00",
    type: "fault", category: "事件", summary: "故障事件: 1个遥信, 1个遥测",
    parsed: { type: "fault", addr: 1,
      yx: [{ type: 1, typeHex: "0x01", point: 1, pointHex: "0x0001", state: "合(1)", time: "2026-10-16 17:59:59.500(周五)" }],
      yc: [{ addr: 1, hexAddr: "0x000001", value: 220.5, valueType: "float" }],
      description: "故障事件: 1个遥信, 1个遥测" } },
  { id: 5, time: "2026-10-16 18:00:04.000", dir: "T→M", proto: "101", hex: "10 00 01 00 01 16",
    type: "link_frame", category: "链路", summary: "认可(ACK)",
    parsed: { type: "link_frame", frameType: "fixed", linkAddr: 1, csValid: true, fcName: "认可(ACK)", prm: 0 } },
  // 带点名的遥测帧（点表 dbSelect 加载后，Go enrich 注入 name 字段）
  { id: 6, time: "2026-10-16 18:00:05.000", dir: "T→M", proto: "104", hex: "68 12 04 00 00 00 0D 01 03 00 01 00 01 00 00 00 00 80 3F 00",
    type: "yc", tiName: "M_ME_NC_1(短浮点遥测)", category: "遥测", summary: "遥测 1 点",
    parsed: { type: "yc", ti: 13, addr: 1, cotDesc: "spont",
      data: [{ slot: 1, addr: 1, value: 1.0, valueType: "float(短浮点)", qds: 0, name: "10kV进线A相电压" }] } },
];

let failed = 0;
for (const f of frames) {
  try {
    const html = frameDetailHTML(f);
    if (typeof html !== "string" || html.length < 50 || html.indexOf("报文原文") < 0) {
      throw new Error("输出异常: length=" + (html && html.length));
    }
    console.log(`[OK] #${f.id} ${f.type} -> ${html.length} 字节 HTML`);
  } catch (e) {
    failed++;
    console.error(`[FAIL] #${f.id} ${f.type}: ${e.message}`);
  }
}
// 关键内容断言
const yc = frameDetailHTML(frames[1]);
if (yc.indexOf("工程值 1") < 0 || yc.indexOf("信息对象地址") < 0) { failed++; console.error("[FAIL] 遥测表格内容缺失"); }
const ctrl = frameDetailHTML(frames[2]);
if (ctrl.indexOf('ret-success">成功<') < 0) { failed++; console.error("[FAIL] 遥控结果徽标缺失"); }
const link = frameDetailHTML(frames[4]);
if (link.indexOf("认可(ACK)") < 0) { failed++; console.error("[FAIL] 101 链路帧字段缺失"); }
// 点名列（点表加载后 enrich 注入 name）
const ycn = frameDetailHTML(frames[5]);
if (ycn.indexOf("点名") < 0 || ycn.indexOf("10kV进线A相电压") < 0) {
  failed++; console.error("[FAIL] 点名列/点名内容缺失");
}
// 点名列必须紧随"信息对象地址"之后（位置断言）
if (ycn.indexOf("信息对象地址</th><th>点名</th><th>值") < 0) {
  failed++; console.error("[FAIL] 点名列位置错误（应紧随信息体地址列）");
}
// 无点表（无 name 字段）时不应出现点名列
if (frameDetailHTML(frames[1]).indexOf("点名") >= 0) {
  failed++; console.error("[FAIL] 未加载点表时不应出现点名列");
}
// 混合场景：仅部分项命中点名 → 仍显示点名列，未命中项显示 "—"
const mixed = { id: 7, time: "2026-10-16 18:00:06.000", dir: "T→M", proto: "104",
  hex: "68 12 04 00 00 00 0D 01 03 00 01 00 01 00 00 00 00 80 3F 00",
  type: "yc", category: "遥测", summary: "遥测 1 点",
  parsed: { type: "yc", ti: 13, data: [
    { slot: 1, addr: 1, value: 1.0, valueType: "float", qds: 0, name: "10kV进线A相电压" },
    { slot: 2, addr: 2, value: 2.0, valueType: "float", qds: 0 },
  ] } };
const mh = frameDetailHTML(mixed);
if (mh.indexOf("点名") < 0 || mh.indexOf("10kV进线A相电压") < 0 || mh.indexOf("—") < 0) {
  failed++; console.error("[FAIL] 混合点名场景渲染错误");
}
// ── 搜索与筛选引擎测试 ──
function assert(cond, msg) {
  if (!cond) {
    failed++;
    console.error("[FAIL] " + msg);
  }
}
// 通配符：含通配符 → 整串匹配；无通配符 → 子串包含
assert(SE.compileMatcher("68*", "wildcard").test("68 04 07 00 00 00"), "通配符 68* 应整串匹配");
assert(SE.compileMatcher("68 0E", "wildcard").test("68 0E 04 00 00 00"), "通配符无 * 时按子串包含");
assert(!SE.compileMatcher("68 0E", "wildcard").test("68 04 07 00 00 00"), "子串包含反向用例");
assert(SE.compileMatcher("68 0?", "wildcard").test("68 04"), "通配符 ? 单字符（含通配符时整串匹配）");
assert(SE.compileMatcher("68 0?", "wildcard").test("68 0E"), "通配符 ? 单字符（第二用例）");
// 正则模式与非法正则
assert(SE.compileMatcher("^68 (0E|12)", "regex").test("68 0E 04"), "正则匹配");
assert(SE.compileMatcher("(unclosed", "regex") === null, "非法正则应返回 null");
// 规则搜索（字段范围隔离）
const reName = SE.compileMatcher("10kV*", "wildcard");
assert(SE.matchSearch(frames[5], ["name"], reName), "按点名范围搜索应命中");
assert(!SE.matchSearch(frames[5], ["hex"], reName), "按报文范围不应命中点名内容");
assert(SE.matchSearch(frames[1], ["summary"], SE.compileMatcher("*归一化*", "wildcard")), "按摘要范围搜索（含类型名）");
assert(SE.matchSearch(frames[4], ["proto"], SE.compileMatcher("101", "wildcard")), "按协议范围搜索");
// Filter：TI 列表 / 通配
assert(SE.matchTi({ ti: 13 }, "1,3,13", "wildcard"), "TI 列表命中");
assert(!SE.matchTi({ ti: 9 }, "1,3,13", "wildcard"), "TI 列表反向用例");
assert(SE.matchTi({ ti: 130 }, "13*", "wildcard"), "TI 通配符命中");
assert(SE.matchTi({ ti: 13, tiName: "M_ME_NC_1" }, "ME_NC", "regex"), "TI 正则匹配类型名");
// Filter：组合条件（时间范围 + 方向 + 协议 + 原文）
assert(SE.matchFilter(frames[1], { timeFrom: "2026-10-16 18:00:00", timeTo: "2026-10-16 18:00:02", dir: "T→M", proto: "104" }), "组合筛选命中");
assert(!SE.matchFilter(frames[1], { dir: "M→T" }), "方向筛选反向用例");
assert(!SE.matchFilter(frames[1], { timeFrom: "2026-10-16 18:00:05" }), "时间范围反向用例");
assert(SE.matchFilter(frames[0], { hex: "^68 04", mode: "regex" }), "原文正则筛选");
assert(SE.filterActive({ dir: "T→M" }), "filterActive 判定");
assert(!SE.filterActive({}), "filterActive 空条件");

// ── 矢量图标测试（防回归：图标必须能正常内联渲染）──
const distDir = path.join(__dirname, "..", "frontend", "dist");
const html = fs.readFileSync(path.join(distDir, "index.html"), "utf8");
const iconNames = new Set();
for (const m of html.matchAll(/data-icon="([a-z0-9-]+)"/g)) iconNames.add(m[1]);
for (const m of html.matchAll(/data-icon="cfg\.mode==='static' \? 'code' : 'bolt'"/g)) {
  iconNames.add("code");
  iconNames.add("bolt");
}
assert(iconNames.size >= 20, "index.html 中图标引用数量过少：" + iconNames.size);
for (const n of iconNames) {
  const f = path.join(distDir, "assets", "iconLib", n + ".svg");
  assert(fs.existsSync(f), "图标文件缺失: " + n + ".svg");
}
assert(html.indexOf("--icon-url") < 0, "index.html 不应残留 mask 方式的图标标记");
// normalizeSvg：注入 currentColor / 1em 尺寸 / 去声明（含子元素写死颜色改写）
const raw = fs.readFileSync(path.join(distDir, "assets", "iconLib", "search.svg"), "utf8");
const svg = ICONS.normalizeSvg(raw);
assert(svg.indexOf('stroke="currentColor"') >= 0, "图标应继承 currentColor（stroke）");
assert(svg.indexOf("#000000") < 0 && svg.indexOf("#000") < 0, "图标中不应残留写死颜色（如 #000000）");
assert(!/stroke="#[0-9a-f]{3,6}"/i.test(svg), "所有元素 stroke 颜色都应改写为 currentColor");
assert(!/fill="#[0-9a-f]{3,6}"/i.test(svg), "所有元素 fill 颜色都应改写为 currentColor");
assert(svg.indexOf('width="1em"') >= 0 && svg.indexOf('height="1em"') >= 0, "图标尺寸应为 1em");
assert(svg.indexOf("<?xml") < 0 && svg.indexOf("<!--") < 0, "图标应去除 XML 声明与注释");
assert(/^<svg/.test(svg.trim()), "图标输出应以 <svg 开头");
// 全部内置图标都应可规范化（无异常、无写死颜色）
const iconDir = path.join(distDir, "assets", "iconLib");
for (const f of fs.readdirSync(iconDir)) {
  if (!f.endsWith(".svg")) continue;
  const out = ICONS.normalizeSvg(fs.readFileSync(path.join(iconDir, f), "utf8"));
  assert(/^<svg/.test(out.trim()), "图标规范化失败: " + f);
  assert(!/stroke="#[0-9a-f]{3,6}"/i.test(out), "图标存在写死 stroke 颜色: " + f);
}

// 文件服务报文内容解析区（msg/xml 记录文件 + 手动格式切换按钮）
const fileFrame = {
  id: 9, time: "2026-10-17 15:17:26.562", dir: "T→M", proto: "104",
  hex: "68 E1 60 00 06 00 D2 80 05 00 01 00 00 00 00 02 05 00 00 00 C8 00 00 01",
  type: "file_service", category: "文件", summary: "读文件数据传输",
  parsed: {
    type: "file_service", service: "F_FR_NA_1", op: 5, opName: "读文件数据传输",
    fileKey: "210:1",
    fileParse: {
      format: "xml", kind: "soe", kindName: "SOE记录", hasNames: true,
      note: "文件 HISTORY/SOE.xml（126 字节），自动识别为 XML 格式，共 2 条记录",
      columns: ["Time", "Point", "State", "点名"],
      rows: [
        { Time: "2026-10-17 15:00:01.123", Point: "101", State: "合", 点名: "断路器合位" },
        { Time: "2026-10-17 15:00:02.456", Point: "201", State: "分", 点名: "I段10kV进线电压" },
      ],
    },
  },
};
const fh = frameDetailHTML(fileFrame);
assert(fh.indexOf("文件内容解析") >= 0, "应渲染文件内容解析区");
assert(fh.indexOf("SOE记录") >= 0 && fh.indexOf("XML") >= 0, "应显示识别格式与记录类型");
assert(fh.indexOf("断路器合位") >= 0, "文件记录应包含点表点名");
assert(fh.indexOf('data-filefmt="msg"') >= 0 && fh.indexOf('data-filefmt="xml"') >= 0,
  "应提供手动指定 MSG/XML 格式按钮");
assert(fh.indexOf("记录（2 条）") >= 0, "应显示记录数");

// 自动识别失败 → 提示 + 手动选择按钮
const badFileFrame = {
  id: 10, parsed: {
    type: "file_service", service: "F_FR_NA_1", op: 5, fileKey: "210:2",
    fileParse: { format: "", kind: "unknown", kindName: "未知记录", error: "未能从文件中解析出记录；自动识别失败，请手动选择 MSG 或 XML 格式" },
  },
};
const bh = frameDetailHTML(badFileFrame);
assert(bh.indexOf("未能自动识别") >= 0, "自动识别失败应明确提示");
assert(bh.indexOf("请手动选择") >= 0, "应提示手动选择格式");
assert(bh.indexOf('data-filefmt="msg"') >= 0, "失败时仍应提供手动格式按钮");

// COMTRADE 波形：cfg+dat 完成后应渲染 SVG 波形图与图例
const waveFrame = {
  id: 12, parsed: {
    type: "file_service", service: "F_FR_NA_1", op: 5, fileKey: "210:name:test.cfg",
    fileParse: {
      format: "comtrade", kind: "wave", kindName: "波形文件", fileName: "test.cfg",
      bytes: 100, size: 100, complete: true,
      chart: {
        station: "TEST", device: "DEV", startTime: "01/01/2026,00:00:00",
        times: [0, 0.001, 0.002], duration: 0.002,
        channels: [{ name: "IA", unit: "A", min: 0, max: 1000, data: [0, 1000, -1000] }],
        digital: [{ name: "TRIP", data: [0, 1, 0] }],
      },
    },
  },
};
const wh = frameDetailHTML(waveFrame);
assert(wh.indexOf("wavechart") >= 0 && wh.indexOf("IA") >= 0, "COMTRADE 波形图应渲染");

// 定值参数渲染：地址默认十六进制 + 点名 + 参数类型 + 比例（国网/南网定值表）
const paramFrame = {
  id: 11, time: "2026-10-17 17:40:00.000", dir: "M→T", proto: "104",
  hex: "68 14 04 00 00 00 CB 01 06 00 01 00 01 00 80 65 00 00 2B 02 0A 00",
  type: "param", category: "定值", summary: "写预置 SN=1",
  parsed: {
    type: "param", ti: 203, cotDesc: "act",
    data: [{
      slot: 1, type: "Para_read", operation: "读响应", sn: 1,
      addr: "0x000065", addrHex: "0x0065", name: "国网-过流I段定值",
      rate: "0.01", paraType: "短整形", value: 10,
    }, {
      slot: 2, type: "Para_read", operation: "读响应", sn: 1,
      addr: "0x000066", addrHex: "0x0066", name: "国网-零序定值",
      rate: "0.1", paraType: "无符号短整形", value: 20,
    }],
  },
};
const ph = frameDetailHTML(paramFrame);
assert(ph.indexOf("定值参数") >= 0, "应渲染定值参数表");
assert(ph.indexOf("undefined") < 0, "定值表不应出现 undefined（序号必须有值）");
assert(ph.indexOf("0x65") >= 0, "定值地址应显示十六进制（StrEncodeName，高位 0 不补位）");
assert(ph.indexOf("0x0065") < 0, "定值地址不应补高位 0");
assert(ph.indexOf("国网-过流I段定值") >= 0, "应显示定值点名");
assert(ph.indexOf("短整形") >= 0, "应显示参数类型");
assert(ph.indexOf("0.01") >= 0, "应显示比例");
// 无 addrShow 时的回退路径同样按"高位为 0 不补位"显示
const fallbackFrame = {
  id: 15, type: "yc", category: "遥测", summary: "遥测 1 点",
  parsed: { type: "yc", data: [{ slot: 1, addr: 25601, hexAddr: "0x006401", value: 1, qds: 0 }] },
};
assert(frameDetailHTML(fallbackFrame).indexOf("0x6401") >= 0, "旧字段回退（hexAddr）应显示 0x6401");
// 列顺序：# , 地址 , 点名 , 值 , 参数类型 , 比例 , 操作 , SN
const expectHead = "<th>#</th><th>地址</th><th>点名</th><th>值</th><th>参数类型</th><th>比例</th><th>操作</th><th>SN</th>";
assert(ph.indexOf(expectHead) >= 0, "定值表列顺序应为 #,地址,点名,值,参数类型,比例,操作,SN（实际：" + ph.slice(ph.indexOf("<thead>"), ph.indexOf("</thead>") + 8) + "）");
assert(ph.indexOf("<th>类型</th>") < 0, "定值表不应包含“类型”列");

// 地址显示进制（addrShow 优先）：遥测/遥控/电能量默认十六进制，且高位为 0 不补位
const ycHex = {
  id: 12, time: "2026-10-17 18:00:00.000", dir: "T→M", proto: "104",
  hex: "68 12 04 00 00 00 0D 01 03 00 01 00 01 40 00 00 00 80 3F 00",
  type: "yc", category: "遥测", summary: "遥测 1 点",
  parsed: { type: "yc", ti: 13, data: [
    { slot: 1, addr: 16385, addrShow: "0x4001", value: 1.0, valueType: "float(短浮点)", qds: 0, name: "Uab/Ua电压" },
  ] },
};
const yh = frameDetailHTML(ycHex);
assert(yh.indexOf("0x4001") >= 0, "遥测地址应显示十六进制（高位 0 不补位）");
assert(yh.indexOf("0x004001") < 0, "十六进制地址不应补高位 0");
assert(yh.indexOf("16385") < 0, "配置为十六进制时不应显示十进制地址");
// 电能量 0x006401 → 0x6401（用户示例）
const enHex = {
  id: 14, type: "energy", category: "电能量", summary: "电能量(累计量) 1 点",
  parsed: { type: "energy", ti: 206, data: [
    { slot: 1, addr: 25601, addrShow: "0x6401", value: 0, qds: 0, name: "当前正向有功电能" },
  ] },
};
const eh = frameDetailHTML(enHex);
assert(eh.indexOf("0x6401") >= 0, "电能量地址应显示 0x6401");
// 遥信默认十进制
const yxDec = {
  id: 13, type: "yx", category: "遥信", summary: "遥信 1 点",
  parsed: { type: "yx", data: [{ slot: 1, addr: 17, addrShow: 17, state: "合(1)", qds: 0, name: "过流I段" }] },
};
const xh = frameDetailHTML(yxDec);
assert(xh.indexOf(">17<") >= 0, "遥信地址默认应显示十进制");

if (failed) { console.error(`FAILED: ${failed}`); process.exit(1); }
console.log("PASS: 渲染管线 + 搜索筛选引擎 + 矢量图标 + 文件解析 + 定值渲染 + 地址进制冒烟测试全部通过");
