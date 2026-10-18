// 详情渲染（主函数）：一帧解析结果 → 完整 HTML。
// 点表加载后（Go enrich 注入 name 字段），遥信/遥测/遥控/定值/电能量表格带"点名"列。
"use strict";

// 在指定列位置插入"点名"列（紧随信息体地址/地址列之后）；
// 任一项存在 name 字段即显示，未命中项显示 "—"（绝不显示空名）。
function insCol(headers, rows, data, at) {
  if (!data || !data.length) return [headers, rows];
  const hasAny = data.some((d) => d && d.name !== undefined && d.name !== null && d.name !== "");
  if (!hasAny) return [headers, rows];
  const ins = (a, v) => a.slice(0, at).concat([v], a.slice(at));
  return [
    ins(headers, "点名"),
    rows.map((r, i) => ins(r, data[i] && data[i].name ? esc(data[i].name) : "—")),
  ];
}

// COMTRADE 波形图（SVG）：模拟量按通道分轨显示（类似波形分析软件），下方可带开关量。
function waveChartHTML(c) {
  const ch = c.channels || [];
  const times = c.times || [];
  const digi = c.digital || [];
  if (!ch.length || !times.length) return "";
  const W = 1100, L = 132, R = 18, T = 24, B = 38;
  const laneH = 62, laneGap = 8, digH = 26, digGap = 6;
  const pw = W - L - R;
  const analogH = ch.length * (laneH + laneGap);
  const digitalH = digi.length * (digH + digGap);
  const plotBottom = T + analogH + digitalH;
  const H = plotBottom + B;
  const t0 = times[0] || 0, t1 = times[times.length - 1] || 0;
  const span = (t1 - t0) || 1;
  const colors = ["#3b9dff", "#f5a623", "#2ecc71", "#e74c3c", "#9b59b6", "#1abc9c", "#e67e22", "#f1c40f", "#00bcd4", "#ff6b9d", "#8e44ad", "#16a085"];
  const fmt = (v) => (Math.abs(v) >= 1000 ? v.toFixed(1) : v.toFixed(3)).replace(/\.?0+$/, "");
  let grid = "";
  for (let i = 0; i <= 4; i++) {
    const x = L + (pw * i) / 4;
    grid += '<line x1="' + x + '" y1="' + T + '" x2="' + x + '" y2="' + plotBottom + '" stroke="currentColor" opacity=".13"/>';
    const tv = t0 + (span * i) / 4;
    grid += '<text x="' + x + '" y="' + (H - 14) + '" fill="currentColor" opacity=".7" font-size="11" text-anchor="middle">' + tv.toFixed(3) + "s</text>";
  }
  let traces = "";
  ch.forEach((d, idx) => {
    const y0 = T + idx * (laneH + laneGap);
    const color = colors[idx % colors.length];
    if (idx % 2 === 1) {
      grid += '<rect x="' + L + '" y="' + y0 + '" width="' + pw + '" height="' + laneH + '" fill="currentColor" opacity=".025"/>';
    }
    grid += '<line x1="' + L + '" y1="' + y0 + '" x2="' + (L + pw) + '" y2="' + y0 + '" stroke="currentColor" opacity=".18"/>';
    grid += '<line x1="' + L + '" y1="' + (y0 + laneH) + '" x2="' + (L + pw) + '" y2="' + (y0 + laneH) + '" stroke="currentColor" opacity=".10"/>';
    const name = d.name || ("通道" + (idx + 1));
    grid += '<text x="' + (L - 8) + '" y="' + (y0 + laneH / 2 - 2) + '" fill="' + color + '" font-size="11" font-weight="600" text-anchor="end">' + esc(name) + "</text>";
    if (d.unit) {
      grid += '<text x="' + (L - 8) + '" y="' + (y0 + laneH / 2 + 12) + '" fill="currentColor" opacity=".6" font-size="10" text-anchor="end">' + esc(d.unit) + "</text>";
    }
    const data = d.data || [];
    if (!data.length) return;
    let min = Number(d.min), max = Number(d.max);
    if (!isFinite(min) || !isFinite(max) || max <= min) {
      min = Infinity; max = -Infinity;
      for (const v of data) { if (v < min) min = v; if (v > max) max = v; }
    }
    if (!isFinite(min) || !isFinite(max) || max <= min) { min = 0; max = 1; }
    grid += '<text x="' + (L + pw - 2) + '" y="' + (y0 + 11) + '" fill="currentColor" opacity=".55" font-size="9" text-anchor="end">' + fmt(max) + "</text>";
    grid += '<text x="' + (L + pw - 2) + '" y="' + (y0 + laneH - 3) + '" fill="currentColor" opacity=".55" font-size="9" text-anchor="end">' + fmt(min) + "</text>";
    const step = Math.max(1, Math.ceil(data.length / 4000));
    const pts = [];
    for (let i = 0; i < data.length; i += step) {
      const x = L + (((times[i] ?? t0) - t0) / span) * pw;
      const y = y0 + laneH - ((data[i] - min) / (max - min)) * laneH;
      pts.push(x.toFixed(1) + "," + y.toFixed(1));
    }
    const lastI = data.length - 1;
    if ((lastI % step) !== 0) {
      const x = L + (((times[lastI] ?? t0) - t0) / span) * pw;
      const y = y0 + laneH - ((data[lastI] - min) / (max - min)) * laneH;
      pts.push(x.toFixed(1) + "," + y.toFixed(1));
    }
    traces += '<polyline fill="none" stroke="' + color + '" stroke-width="1.4" stroke-linejoin="round" points="' + pts.join(" ") + '"><title>' +
      esc(name) + "：" + fmt(min) + " ~ " + fmt(max) + (d.unit ? " " + esc(d.unit) : "") + "</title></polyline>";
  });
  digi.forEach((d, idx) => {
    const y0 = T + analogH + idx * (digH + digGap);
    const color = "#7fd1ff";
    grid += '<line x1="' + L + '" y1="' + (y0 + digH) + '" x2="' + (L + pw) + '" y2="' + (y0 + digH) + '" stroke="currentColor" opacity=".10"/>';
    grid += '<text x="' + (L - 8) + '" y="' + (y0 + digH / 2 + 4) + '" fill="' + color + '" font-size="10" text-anchor="end">' + esc(d.name || ("开关" + (idx + 1))) + "</text>";
    const data = d.data || [];
    if (!data.length) return;
    const step = Math.max(1, Math.ceil(data.length / 4000));
    const pts = [];
    let prevY = y0 + digH - (data[0] ? 0 : digH);
    for (let i = 0; i < data.length; i += step) {
      const x = L + (((times[i] ?? t0) - t0) / span) * pw;
      const y = y0 + digH - (data[i] ? 0 : digH);
      if (i > 0) pts.push(x.toFixed(1) + "," + prevY.toFixed(1));
      pts.push(x.toFixed(1) + "," + y.toFixed(1));
      prevY = y;
    }
    traces += '<polyline fill="none" stroke="' + color + '" stroke-width="1.1" points="' + pts.join(" ") + '"/>';
  });
  const title = [c.station, c.device].filter((x) => x).join(" / ");
  const meta = [];
  if (c.sampleRate) meta.push("采样率 " + (c.sampleRate >= 1000 ? (c.sampleRate / 1000).toFixed(3) + " kHz" : c.sampleRate + " Hz"));
  if (c.duration) meta.push("时长 " + c.duration.toFixed(3) + " s");
  return '<div class="wavebox"><div class="wv-title">波形图' + (title ? "：" + esc(title) : "") +
    (c.startTime ? ' <span class="wv-time">起始 ' + esc(c.startTime) + "</span>" : "") +
    (meta.length ? ' <span class="wv-time">' + esc(meta.join(" · ")) + "</span>" : "") + "</div>" +
    '<svg class="wavechart" viewBox="0 0 ' + W + " " + H + '" preserveAspectRatio="xMidYMin meet" role="img" aria-label="COMTRADE 波形">' +
    grid + traces + "</svg>" +
    '<div class="wv-note">' + esc(c.note || "各通道按自身量程分轨显示；横轴为相对时间（秒）") + "</div></div>";
}

// 文件服务报文的内容解析区（msg/xml 记录文件：SOE记录/故障事件/遥控记录/事件记录）
function fileParseHTML(p) {
  const r = p.fileParse || {};
  let h = '<div class="sec"><div class="sec-title">文件内容解析（文件服务报文）</div>';
  h += '<div class="kv"><span class="k">文件标识：</span>' + esc(p.fileKey || "") + "</div>";
  if (r.fileName) h += '<div class="kv"><span class="k">文件名：</span>' + esc(r.fileName) + "</div>";
  h += '<div class="kv"><span class="k">传输进度：</span>' +
    (r.bytes || 0) + (r.size ? " / " + r.size : "") + " 字节 " +
    (r.complete === false ? '<span class="ret-failed">传输中…（完成后自动解析）</span>' : '<span class="ret-success">已完成</span>') +
    "</div>";
  if (r.chart && Array.isArray(r.chart.channels) && r.chart.channels.length) h += waveChartHTML(r.chart);
  if (r.note) h += '<div class="kv"><span class="k">说明：</span>' + esc(r.note) + "</div>";
  const fmtText = r.format === "xml" ? "XML" : r.format === "msg" ? "MSG" :
    r.format === "comtrade" ? "COMTRADE" : r.format === "cfg" ? "COMTRADE CFG" :
    r.format === "dat" ? "COMTRADE DAT" : (r.format || "");
  const fmt = fmtText ? esc(fmtText) : "";
  h += '<div class="kv"><span class="k">识别格式：</span>' +
    (fmt || (r.complete === false ? '<span class="ret-failed">待传输完成</span>' : '<span class="ret-failed">未能自动识别</span>')) +
    '<span class="k" style="margin-left:14px">记录类型：</span>' + esc(r.kindName || "未知记录") +
    '<span class="k" style="margin-left:14px">点名：</span>' +
    (r.hasNames ? "已按点表解析" : "未解析（未加载点表或无匹配点）") + "</div>";
  if (r.error) {
    h += '<div class="kv" style="color:var(--err)"><span class="k">提示：</span>' + esc(r.error) + "</div>";
  }
  if (Array.isArray(r.columns) && r.columns.length && Array.isArray(r.rows) && r.rows.length) {
    const rows = r.rows.map((row) => r.columns.map((c) => esc(row[c] == null ? "" : row[c])));
    const tableTitle = (r.format === "cfg" || r.format === "comtrade")
      ? "通道（" + r.rows.length + " 路）" : "记录（" + r.rows.length + " 条）";
    h += tableHTML(tableTitle, r.columns, rows);
  }
  // COMTRADE 的 .cfg/.dat 不走 msg/xml 强制解析，不显示手动格式按钮
  if (r.format !== "cfg" && r.format !== "dat" && r.format !== "comtrade") {
    h += '<div class="kv"><span class="k">手动指定格式：</span>' +
      '<button class="btn tiny" data-filekey="' + esc(p.fileKey || "") + '" data-filefmt="">自动识别</button>' +
      '<button class="btn tiny" data-filekey="' + esc(p.fileKey || "") + '" data-filefmt="msg">按 MSG 解析</button>' +
      '<button class="btn tiny" data-filekey="' + esc(p.fileKey || "") + '" data-filefmt="xml">按 XML 解析</button>' +
      "</div>";
  }
  return h + "</div>";
}

// 十六进制显示规范化：0x006401 → 0x6401（高位为 0 不补位）；非十六进制原样返回
function normHexShow(v) {
  if (typeof v !== "string") return v;
  const m = /^0[xX]([0-9a-fA-F]+)$/.exec(v.trim());
  if (!m) return v;
  const d = m[1].replace(/^0+/, "") || "0";
  return "0x" + d.toUpperCase();
}

// 地址显示：优先使用 Go 侧按配置写入的 addrShow（十六进制/十进制）
function addrOf(d) {
  if (!d) return "—";
  if (d.addrShow !== undefined && d.addrShow !== null) return esc(d.addrShow);
  if (d.addrHex) return esc(normHexShow(d.addrHex));
  if (d.hexAddr) return esc(normHexShow(d.hexAddr));
  if (d.pointHex) return esc(normHexShow(d.pointHex));
  if (d.addrDec !== undefined && d.addrDec !== null) return esc(d.addrDec);
  return esc(d.addr != null ? d.addr : "—");
}

function frameDetailHTML(f) {
  const p = (f && f.parsed) || {};
  // 标题：仅显示非空字段（避免出现空的 ｜ 分隔）
  const headParts = [f.category, f.proto, f.tiName, f.dir].filter((v) => v != null && String(v).trim() !== "");
  let h = '<h4 class="dhead">' + headParts.map((v) => esc(v)).join(" ｜ ") + "</h4>";
  h += '<div class="kv"><span class="k">捕获时间：</span>' + esc(f.time || "") + "</div>";
  h += '<div class="kv"><span class="k">摘要：</span>' + esc(f.summary || "") + "</div>";
  ["desc", "uName", "fcName", "operation", "coiDesc", "qccDesc", "qrpDesc", "scqDesc",
    "lsqDesc", "ackType", "reason", "subTypeName", "opName", "packetTypeName"].forEach(
    (k) => (h += kvHTML(k, p[k]))
  );
  if (p.cotDesc) h += kvHTML("传送原因(COT)", p.cotDesc);
  if (p.addr != null) h += kvHTML("公共地址", p.addr);
  if (p.linkAddr != null) h += kvHTML("链路地址", p.linkAddr);
  if (p.pn) h += kvHTML("P/N", "否定");
  if (p.fbpHex) h += kvHTML("固定测试字(FBP)", p.fbpHex + (p.fbpValid ? "（有效）" : "（无效）"));
  if (p.time) h += kvHTML("报文内时标", p.time);
  if (p.csValid != null) h += kvHTML("帧校验(CS)", p.csValid ? "正确" : "错误");
  if (p.size) h += kvHTML("大小", p.size);
  if (p.direction) h += kvHTML("链路方向", p.direction);

  const data = p.data || [];
  switch (f.type) {
    case "yx": {
      const [hh, rr] = insCol(["#", "信息对象地址", "状态", "品质"],
        dataRows(p, (d) => [d.slot, addrOf(d), esc(d.state), qdsHTML(d.qds)]), data, 2);
      h += tableHTML("遥信", hh, rr);
      break;
    }
    case "soe": {
      const [hh, rr] = insCol(["#", "信息对象地址", "状态", "时标"],
        dataRows(p, (d) => [d.slot, addrOf(d), esc(d.state), esc(d.time)]), data, 2);
      h += tableHTML("SOE 带时标遥信", hh, rr);
      break;
    }
    case "yc": {
      const [hh, rr] = insCol(["#", "信息对象地址", "值", "类型", "品质(QDS)"],
        dataRows(p, (d) => [d.slot, addrOf(d), valStr(d.value), esc(d.valueType), qdsHTML(d.qds)]), data, 2);
      h += tableHTML("遥测", hh, rr);
      break;
    }
    case "bitstring":
      h += tableHTML("位串", ["#", "信息对象地址", "值", "位串"],
        dataRows(p, (d) => [d.slot, d.addr, esc(d.valueHex), esc(d.bits)]));
      break;
    case "control": {
      const [hh, rr] = insCol(["#", "地址", "状态", "操作", "QOC", "结果"],
        dataRows(p, (d) => [d.slot, addrOf(d), esc(d.state), esc(d.operation), esc(d.qoc), retHTML(d.return)]), data, 2);
      h += tableHTML("遥控", hh, rr);
      break;
    }
    case "param": {
      // 定值参数表（列序：#,地址,点名,值,参数类型,比例,操作,SN）；地址默认显示十六进制
      const data = p.data || [];
      const headers = ["#", "地址", "点名", "值", "参数类型", "比例", "操作", "SN"];
      const rows = data.map((d, i) => [
        d.slot != null ? d.slot : i + 1,
        addrOf(d),
        d.name ? esc(d.name) : "—",
        valStr(d.value),
        d.paraType ? esc(d.paraType) : "—",
        d.rate !== undefined && d.rate !== "" ? esc(d.rate) : "—",
        esc(d.operation || "—"),
        d.sn != null ? esc(d.sn) : "—",
      ]);
      h += tableHTML("定值参数", headers, rows);
      break;
    }
    case "energy": {
      const [hh, rr] = insCol(["#", "信息对象地址", "值", "品质", "时标"],
        dataRows(p, (d) => [d.slot, addrOf(d), valStr(d.value), qdsHTML(d.qds), esc(d.time)]), data, 2);
      h += tableHTML("电能量(累计量)", hh, rr);
      break;
    }
    case "fault": {
      if (Array.isArray(p.yx)) {
        const [hh, rr] = insCol(["类型", "点号", "状态", "时标"],
          p.yx.map((d) => [esc(d.typeHex), addrOf(d), esc(d.state), esc(d.time)]), p.yx, 2);
        h += tableHTML("故障遥信", hh, rr);
      }
      if (Array.isArray(p.yc)) {
        const [hh, rr] = insCol(["信息对象地址", "值", "类型"],
          p.yc.map((d) => [addrOf(d), valStr(d.value), esc(d.valueType)]), p.yc, 1);
        h += tableHTML("故障遥测", hh, rr);
      }
      break;
    }
    case "file_service":
      if (Array.isArray(p.entries) && p.entries.length) h += tableHTML("文件目录",
        ["文件名", "属性", "大小", "时间"],
        p.entries.map((d) => [esc(d.fileName), esc(d.fileAttr), esc(d.fileSize), esc(d.fileTime)]));
      if (Array.isArray(p.files) && p.files.length) h += tableHTML("文件列表",
        ["文件名", "类型", "开始时间", "结束时间"],
        p.files.map((d) => [esc(d.name), esc(d.fileCategory), esc(d.startTime), esc(d.endTime)]));
      if (p.dataHex) h += '<div class="sec"><div class="sec-title">文件段落数据</div><div class="hexblock">' + esc(p.dataHex) + "</div></div>";
      break;
    case "link_frame":
      if (p.acd != null) h += kvHTML("ACD(1级数据)", p.acd ? "1(有数据)" : "0");
      if (p.dfc != null) h += kvHTML("DFC(数据流控)", p.dfc ? "1(满载)" : "0");
      if (p.fcb != null) h += kvHTML("FCB", p.fcb);
      if (p.fcv != null) h += kvHTML("FCV", p.fcv);
      break;
    case "other":
      h += kvHTML("格式", p.format === "U" ? "U 帧(控制)" : "S 帧(监视)");
      if (p.ctrl) h += kvHTML("控制域", p.ctrl);
      break;
    case "error":
      h += '<div class="kv"><span class="k" style="color:var(--err)">解析错误：</span>' + esc(p.error) + "</div>";
      break;
  }
  h += '<div class="sec"><div class="sec-title">报文原文</div><div class="hexblock">' + esc(f.hex || "") + "</div></div>";
  // 文件服务报文：附带文件内容解析（msg/xml → 记录表格，SOE/故障事件含点名）
  if (p.fileKey) h += fileParseHTML(p);
  return h;
}

window.FrameRender = { frameDetailHTML: frameDetailHTML, esc: esc, tableHTML: tableHTML };

