// 矢量图标加载器：把 assets/iconLib/*.svg 内联注入 <span class="app-icon" data-icon="name">，
// 图标继承 currentColor（随主题/禁用态变色），不依赖 CSS mask，避免 WebView2 兼容问题。
"use strict";

const ICON_CACHE = {};

async function fetchIcon(name) {
  if (ICON_CACHE[name]) return ICON_CACHE[name];
  const res = await fetch("assets/iconLib/" + name + ".svg");
  if (!res.ok) throw new Error("icon not found: " + name);
  const txt = await res.text();
  const svg = normalizeSvg(txt);
  ICON_CACHE[name] = svg;
  return svg;
}

// 规范化 SVG：去声明/注释、去掉固定宽高、把所有子元素的 stroke/fill 颜色统一为 currentColor
// （iconLib 的 SVG 在 <path> 上写死了 #000000，必须一并改写，否则图标恒为黑色）
function normalizeSvg(txt) {
  let s = String(txt)
    .replace(/<\?xml[\s\S]*?\?>/gi, "")
    .replace(/<!DOCTYPE[\s\S]*?>/gi, "")
    .replace(/<!--[\s\S]*?-->/g, "")
    .trim();
  // 1) 全量颜色改写（保留 none / currentColor / inherit）
  s = s.replace(/\sstroke="(?!none|currentColor|inherit)([^"]*)"/gi, ' stroke="currentColor"');
  s = s.replace(/\sfill="(?!none|currentColor|inherit)([^"]*)"/gi, ' fill="currentColor"');
  // 2) 根元素：去固定宽高、补齐尺寸与默认属性
  s = s.replace(/<svg([^>]*)>/i, (m, attrs) => {
    let a = String(attrs)
      .replace(/\swidth="[^"]*"/gi, "")
      .replace(/\sheight="[^"]*"/gi, "");
    if (!/\sstroke="/i.test(a)) a += ' stroke="currentColor"';
    if (!/\sfill="/i.test(a)) a += ' fill="none"';
    return '<svg' + a + ' width="1em" height="1em" focusable="false" aria-hidden="true">';
  });
  return s;
}

// 扫描并加载页面中所有未加载的图标（可重复调用）
async function loadIcons(root) {
  const scope = root || document;
  const nodes = scope.querySelectorAll(".app-icon[data-icon]:not([data-icon-loaded])");
  await Promise.all(
    Array.from(nodes).map(async (el) => {
      const name = el.getAttribute("data-icon");
      if (!name) return;
      try {
        el.innerHTML = await fetchIcon(name);
        el.setAttribute("data-icon-loaded", "1");
      } catch (e) {
        // 兜底：若 fetch 不可用，退回 CSS mask 方式（至少能显示形状）
        const u = 'url("assets/iconLib/' + name + '.svg")';
        el.style.webkitMaskImage = u;
        el.style.maskImage = u;
        el.style.webkitMaskRepeat = "no-repeat";
        el.style.maskRepeat = "no-repeat";
        el.style.webkitMaskPosition = "center";
        el.style.maskPosition = "center";
        el.style.webkitMaskSize = "contain";
        el.style.maskSize = "contain";
        el.style.backgroundColor = "currentColor";
        el.setAttribute("data-icon-loaded", "1");
      }
    })
  );
}

// 监听 DOM 变化（Vue 动态渲染的面板/行），自动加载新出现的图标
function watchIcons() {
  const mo = new MutationObserver(() => {
    loadIcons();
  });
  mo.observe(document.body, { childList: true, subtree: true });
}

window.Icons = { loadIcons, watchIcons, normalizeSvg };
