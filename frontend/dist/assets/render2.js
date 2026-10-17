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

// 文件服务报文的内容解析区（msg/xml 记录文件：SOE记录/故障事件/遥控记录/事件记录）
function fileParseHTML(p) {
  const r = p.fileParse || {};
  let h = '<div class="sec"><div class="sec-title">文件内容解析（文件服务报文）</div>';
  h += '<div class="kv"><span class="k">文件标识：</span>' + esc(p.fileKey || "") + "</div>";
  if (r.note) h += '<div class="kv"><span class="k">说明：</span>' + esc(r.note) + "</div>";
  h += '<div class="kv"><span class="k">识别格式：</span>' +
    (r.format ? esc(r.format === "xml" ? "XML" : "MSG") : '<span class="ret-failed">未能自动识别</span>') +
    '<span class="k" style="margin-left:14px">记录类型：</span>' + esc(r.kindName || "未知记录") +
    '<span class="k" style="margin-left:14px">点名：</span>' +
    (r.hasNames ? "已按点表解析" : "未解析（未加载点表或无匹配点）") + "</div>";
  if (r.error) {
    h += '<div class="kv" style="color:var(--err)"><span class="k">提示：</span>' + esc(r.error) + "</div>";
  }
  if (Array.isArray(r.columns) && r.columns.length && Array.isArray(r.rows) && r.rows.length) {
    const rows = r.rows.map((row) => r.columns.map((c) => esc(row[c] == null ? "" : row[c])));
    h += tableHTML("记录（" + r.rows.length + " 条）", r.columns, rows);
  }
  h += '<div class="kv"><span class="k">手动指定格式：</span>' +
    '<button class="btn tiny" data-filekey="' + esc(p.fileKey || "") + '" data-filefmt="">自动识别</button>' +
    '<button class="btn tiny" data-filekey="' + esc(p.fileKey || "") + '" data-filefmt="msg">按 MSG 解析</button>' +
    '<button class="btn tiny" data-filekey="' + esc(p.fileKey || "") + '" data-filefmt="xml">按 XML 解析</button>' +
    "</div>";
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

