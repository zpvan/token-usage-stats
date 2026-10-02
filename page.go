package main

// pageHTML is the self-contained usage statistics page served under
// /v0/resource/plugins/token-usage-stats/stats. It contains no data; the JS
// fetches the authenticated management JSON API with a user-supplied
// management key stored in localStorage. Must not contain backticks.
const pageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Token Usage Stats</title>
<style>
  :root { color-scheme: light; }
  * { box-sizing: border-box; }
  :root {
    --page: #f9f9f7; --surface-1: #fcfcfb;
    --ink-1: #0b0b0b; --ink-2: #52514e; --ink-3: #898781;
    --grid: #e1e0d9; --baseline: #c3c2b7; --border: rgba(11,11,11,0.10);
    --series-1: #2a78d6; --series-2: #eb6834; --series-3: #1baf7a;
    --series-4: #eda100; --series-5: #e87ba4; --series-6: #008300;
    --series-7: #4a3aa7; --series-8: #e34948;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      color-scheme: dark;
      --page: #0d0d0d; --surface-1: #1a1a19;
      --ink-1: #ffffff; --ink-2: #c3c2b7; --ink-3: #898781;
      --grid: #2c2c2a; --baseline: #383835; --border: rgba(255,255,255,0.10);
      --series-1: #3987e5; --series-2: #d95926; --series-3: #199e70;
      --series-4: #c98500; --series-5: #d55181; --series-6: #008300;
      --series-7: #9085e9; --series-8: #e66767;
    }
  }
  body { font-family: system-ui, -apple-system, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif; margin: 0; padding: 24px; background: var(--page); color: var(--ink-1); }
  h1 { font-size: 20px; margin: 0 0 16px; }
  h2 { font-size: 15px; margin: 0; font-weight: 600; }
  .toolbar { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; margin-bottom: 16px; }
  .toolbar .spacer { flex: 1; }
  button { padding: 6px 14px; border: 1px solid var(--border); border-radius: 6px; background: var(--surface-1); color: var(--ink-1); cursor: pointer; font-size: 13px; }
  button:hover { background: var(--grid); }
  button.active { background: var(--series-1); border-color: var(--series-1); color: #fff; }
  .card { background: var(--surface-1); border: 1px solid var(--border); border-radius: 10px; padding: 16px; margin-bottom: 16px; }
  .card-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; flex-wrap: wrap; gap: 8px; }
  .legend { display: flex; gap: 14px; align-items: center; }
  .legend .item { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--ink-2); }
  .legend .sw { width: 10px; height: 10px; border-radius: 2px; display: inline-block; }
  .chart-scroll { overflow-x: auto; }
  svg { display: block; }
  table { width: 100%; border-collapse: collapse; font-size: 13px; font-variant-numeric: tabular-nums; }
  th, td { padding: 8px 12px; text-align: right; border-bottom: 1px solid var(--grid); white-space: nowrap; }
  th { color: var(--ink-2); font-weight: 600; }
  td.l, th.l { text-align: left; }
  tr.sub td { background: var(--grid); font-weight: 600; }
  tr.grand td { font-weight: 700; border-top: 2px solid var(--baseline); }
  .msg { padding: 12px 16px; border-radius: 6px; margin-bottom: 12px; display: none; }
  .msg.err { display: block; background: #fee4e2; color: #b42318; }
  .msg.info { display: block; background: #e0eaff; color: #1d4ed8; }
  #content { transition: opacity .15s ease; }
  .tooltip { position: absolute; z-index: 50; pointer-events: none; background: var(--surface-1); border: 1px solid var(--border); border-radius: 8px; box-shadow: 0 4px 16px rgba(0,0,0,.16); padding: 8px 10px; font-size: 12px; min-width: 150px; }
  .tt-title { font-weight: 600; margin-bottom: 6px; color: var(--ink-1); }
  .tt-row { display: flex; align-items: center; gap: 6px; margin: 3px 0; color: var(--ink-2); }
  .tt-row .tt-key { width: 10px; height: 2px; display: inline-block; }
  .tt-row .tt-val { margin-left: auto; font-weight: 600; color: var(--ink-1); padding-left: 12px; font-variant-numeric: tabular-nums; }
  .tt-foot { margin-top: 6px; padding-top: 6px; border-top: 1px solid var(--grid); color: var(--ink-2); font-variant-numeric: tabular-nums; }
  #overlay { position: fixed; inset: 0; background: rgba(15,18,22,.45); display: none; align-items: center; justify-content: center; }
  #overlay.show { display: flex; }
  .dialog { background: var(--surface-1); border-radius: 10px; padding: 24px; width: 360px; box-shadow: 0 8px 30px rgba(0,0,0,.2); }
  .dialog h2 { font-size: 16px; margin: 0 0 8px; }
  .dialog p { font-size: 13px; color: var(--ink-2); margin: 0 0 12px; }
  .dialog input { width: 100%; padding: 8px 10px; border: 1px solid var(--border); border-radius: 6px; font-size: 14px; margin-bottom: 12px; background: var(--page); color: var(--ink-1); }
</style>
</head>
<body>
<h1>Token Usage Stats（按天 × 模型）</h1>
<div class="toolbar">
  <button data-days="7">近 7 天</button>
  <button data-days="14">近 14 天</button>
  <button data-days="30" class="active">近 30 天</button>
  <span class="spacer"></span>
  <button id="clearKey">清除密码</button>
  <button id="reload">刷新</button>
</div>
<div id="msg" class="msg"></div>
<div id="content">
  <div class="card viz-root">
    <div class="card-head">
      <h2>每日 Tokens 直方图</h2>
      <div class="legend" id="dailyLegend"></div>
    </div>
    <div class="chart-scroll"><svg id="dailyChart" role="img" aria-label="每日 tokens 直方图"></svg></div>
  </div>
  <div class="card viz-root">
    <div class="card-head"><h2>模型用量排行</h2></div>
    <div class="chart-scroll"><svg id="modelChart" role="img" aria-label="模型用量排行"></svg></div>
  </div>
  <div class="card">
    <div class="card-head"><h2>明细数据</h2></div>
    <table>
      <thead>
        <tr>
          <th class="l">日期</th><th class="l">模型</th><th>请求数</th><th>输入 tokens</th>
          <th>缓存读取</th><th>命中率</th><th>输出 tokens</th><th>总计</th>
        </tr>
      </thead>
      <tbody id="rows"></tbody>
    </table>
  </div>
</div>
<div id="overlay">
  <div class="dialog">
    <h2>输入管理密码</h2>
    <p>数据来自认证接口 /v0/management/usage-stats。请输入 CPA 的 Management Key（仅保存在本浏览器 localStorage）。</p>
    <input id="keyInput" type="password" placeholder="Management Key" autocomplete="off">
    <button id="saveKey">保存并加载</button>
  </div>
</div>
<div id="tooltip" class="tooltip" hidden></div>
<script>
var KEY_STORAGE = 'token-usage-stats.management-key';
var API_PATH = '/v0/management/usage-stats';
var SVGNS = 'http://www.w3.org/2000/svg';
var currentDays = 30;

// Model colors: categorical slots 1-8 in fixed palette order, assigned once
// per model name and never reassigned, so a model keeps its hue across days
// and range switches. Models past the slots fold into 其他 (muted gray).
var SLOT_MAX = 8;
var modelSlots = {};
var nextSlot = 0;
function slotFor(model) {
  if (Object.prototype.hasOwnProperty.call(modelSlots, model)) { return modelSlots[model]; }
  if (nextSlot < SLOT_MAX) { modelSlots[model] = nextSlot; nextSlot++; return modelSlots[model]; }
  return -1;
}
function slotColors() {
  var out = [];
  for (var i = 1; i <= SLOT_MAX; i++) { out.push(cssVar('--series-' + i)); }
  return out;
}

function getKey() { try { return localStorage.getItem(KEY_STORAGE) || ''; } catch (e) { return ''; } }
function setKey(k) { try { localStorage.setItem(KEY_STORAGE, k); } catch (e) {} }
function clearStoredKey() { try { localStorage.removeItem(KEY_STORAGE); } catch (e) {} }

function showMsg(text, isError) {
  var el = document.getElementById('msg');
  el.textContent = text;
  el.className = 'msg ' + (isError ? 'err' : 'info');
}
function hideMsg() { document.getElementById('msg').className = 'msg'; }
function showOverlay(show) { document.getElementById('overlay').className = show ? 'show' : ''; }

function fmtDate(d) {
  var y = d.getFullYear();
  var m = ('0' + (d.getMonth() + 1)).slice(-2);
  var day = ('0' + d.getDate()).slice(-2);
  return y + '-' + m + '-' + day;
}
function fmtNum(n) { return (n == null ? 0 : n).toLocaleString('en-US'); }
function fmtRate(r) { return r == null ? '-' : (r * 100).toFixed(1) + '%'; }
function fmtCompact(n) {
  if (n == null) { n = 0; }
  if (n < 1000) { return String(Math.round(n)); }
  if (n < 1000000) {
    var k = n / 1000;
    return (k >= 100 ? String(Math.round(k)) : k.toFixed(1).replace(/\.0$/, '')) + 'K';
  }
  var m = n / 1000000;
  return (m >= 100 ? String(Math.round(m)) : m.toFixed(1).replace(/\.0$/, '')) + 'M';
}

function cssVar(name) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function svgEl(tag, attrs) {
  var e = document.createElementNS(SVGNS, tag);
  if (attrs) { for (var k in attrs) { e.setAttribute(k, attrs[k]); } }
  return e;
}
function svgText(x, y, anchor, fill, size, value) {
  var t = svgEl('text', { x: x, y: y, 'text-anchor': anchor, fill: fill, 'font-size': size, 'font-variant-numeric': 'tabular-nums' });
  t.textContent = value;
  return t;
}

var tooltip = null;
function getTooltip() {
  if (!tooltip) { tooltip = document.getElementById('tooltip'); }
  return tooltip;
}
function showTooltip(x, y, title, rows, foot) {
  var t = getTooltip();
  while (t.firstChild) { t.removeChild(t.firstChild); }
  var h = document.createElement('div');
  h.className = 'tt-title';
  h.textContent = title;
  t.appendChild(h);
  for (var i = 0; i < rows.length; i++) {
    var row = document.createElement('div');
    row.className = 'tt-row';
    var key = document.createElement('span');
    key.className = 'tt-key';
    if (rows[i].color) { key.style.background = rows[i].color; }
    var label = document.createElement('span');
    label.textContent = rows[i].label;
    var val = document.createElement('span');
    val.className = 'tt-val';
    val.textContent = rows[i].value;
    row.appendChild(key); row.appendChild(label); row.appendChild(val);
    t.appendChild(row);
  }
  if (foot) {
    var f = document.createElement('div');
    f.className = 'tt-foot';
    f.textContent = foot;
    t.appendChild(f);
  }
  t.hidden = false;
  var rect = t.getBoundingClientRect();
  var left = x + 14, top = y + 14;
  if (left + rect.width > window.innerWidth - 8) { left = x - rect.width - 14; }
  if (top + rect.height > window.innerHeight + window.scrollY - 8) { top = y - rect.height - 14; }
  t.style.left = left + 'px';
  t.style.top = (top + window.scrollY) + 'px';
}
function hideTooltip() { getTooltip().hidden = true; }

function niceCeil(v) {
  if (v <= 0) { v = 1; }
  var pow = Math.pow(10, Math.floor(Math.log10(v)));
  var cands = [1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10];
  for (var i = 0; i < cands.length; i++) {
    if (cands[i] * pow >= v) { return cands[i] * pow; }
  }
  return 10 * pow;
}

function topRoundedRect(x, y, w, h, r) {
  r = Math.max(0, Math.min(r, w / 2, h));
  return 'M' + x + ' ' + (y + h) + ' L' + x + ' ' + (y + r) +
    ' Q' + x + ' ' + y + ' ' + (x + r) + ' ' + y +
    ' L' + (x + w - r) + ' ' + y +
    ' Q' + (x + w) + ' ' + y + ' ' + (x + w) + ' ' + (y + r) +
    ' L' + (x + w) + ' ' + (y + h) + ' Z';
}
function rightRoundedRect(x, y, w, h, r) {
  r = Math.max(0, Math.min(r, w / 2, h));
  if (w <= r) { r = w / 2; }
  return 'M' + x + ' ' + y +
    ' L' + (x + w - r) + ' ' + y +
    ' Q' + (x + w) + ' ' + y + ' ' + (x + w) + ' ' + (y + r) +
    ' L' + (x + w) + ' ' + (y + h - r) +
    ' Q' + (x + w) + ' ' + (y + h) + ' ' + (x + w - r) + ' ' + (y + h) +
    ' L' + x + ' ' + (y + h) + ' Z';
}

function dayValues(day) {
  var cacheRead = 0, uncached = 0, output = 0, total = 0, requests = 0, cacheCreation = 0;
  var models = day.models || [];
  for (var i = 0; i < models.length; i++) {
    var m = models[i];
    cacheRead += m.cache_read_tokens || 0;
    output += m.output_tokens || 0;
    total += m.total_tokens || 0;
    requests += m.requests || 0;
    cacheCreation += m.cache_creation_tokens || 0;
    var input = m.input_tokens || 0;
    var cr = m.cache_read_tokens || 0;
    uncached += Math.max(input - cr, 0);
  }
  return { cacheRead: cacheRead, uncached: uncached, output: output, total: total, requests: requests, cacheCreation: cacheCreation };
}

function renderLegend(days) {
  var legend = document.getElementById('dailyLegend');
  while (legend.firstChild) { legend.removeChild(legend.firstChild); }
  var ranked = aggregateModels(days);
  var colors = slotColors();
  var otherColor = cssVar('--ink-3') || '#898781';
  var entries = [];
  var hasOther = false;
  for (var i = 0; i < ranked.length; i++) {
    var slot = slotFor(ranked[i].model);
    if (slot < 0) { hasOther = true; continue; }
    entries.push({ label: ranked[i].model, color: colors[slot], slot: slot });
  }
  entries.sort(function (a, b) { return a.slot - b.slot; });
  if (hasOther) { entries.push({ label: '其他', color: otherColor, slot: 99 }); }
  // a single series needs no legend box: the chart title already names it
  if (entries.length <= 1) { return; }
  for (var j = 0; j < entries.length; j++) {
    var item = document.createElement('span');
    item.className = 'item';
    var sw = document.createElement('span');
    sw.className = 'sw';
    sw.style.background = entries[j].color;
    var tx = document.createElement('span');
    tx.textContent = entries[j].label;
    item.appendChild(sw); item.appendChild(tx);
    legend.appendChild(item);
  }
}

// daySegments splits one day's total into per-model segments ordered by
// color slot (largest-assigned first at the bottom), with unslotted models
// folded into a trailing 其他 segment.
function daySegments(day) {
  var byModel = {};
  var other = 0;
  var models = day.models || [];
  for (var i = 0; i < models.length; i++) {
    var m = models[i];
    var slot = slotFor(m.model);
    if (slot < 0) { other += m.total_tokens || 0; continue; }
    if (!byModel[m.model]) { byModel[m.model] = { slot: slot, model: m.model, total: 0 }; }
    byModel[m.model].total += m.total_tokens || 0;
  }
  var segs = [];
  for (var k in byModel) { segs.push(byModel[k]); }
  segs.sort(function (a, b) { return a.slot - b.slot; });
  if (other > 0) { segs.push({ slot: -1, model: '其他', total: other }); }
  return segs;
}

function renderDailyChart(days) {
  var svg = document.getElementById('dailyChart');
  while (svg.firstChild) { svg.removeChild(svg.firstChild); }
  var per = 36, padL = 56, padR = 16, padT = 14, axisH = 30;
  var n = Math.max(days.length, 1);
  var width = Math.max(padL + padR + per * n, 320);
  var height = 230;
  var plotH = height - padT - axisH;
  svg.setAttribute('width', width);
  svg.setAttribute('height', height);
  svg.setAttribute('viewBox', '0 0 ' + width + ' ' + height);

  // assign color slots by range total rank before stacking
  var ranked = aggregateModels(days);
  for (var r0 = 0; r0 < ranked.length; r0++) { slotFor(ranked[r0].model); }

  var maxV = 1;
  var vals = [];
  var segList = [];
  for (var i = 0; i < days.length; i++) {
    var v = dayValues(days[i]);
    vals.push(v);
    segList.push(daySegments(days[i]));
    if (v.total > maxV) { maxV = v.total; }
  }
  var ceil = niceCeil(maxV);
  var yBase = padT + plotH;
  function y(val) { return padT + plotH * (1 - val / ceil); }

  var gridColor = cssVar('--grid') || '#e1e0d9';
  var baseColor = cssVar('--baseline') || '#c3c2b7';
  var ink3 = cssVar('--ink-3') || '#898781';
  var colors = slotColors();
  function segColor(seg) { return seg.slot < 0 ? ink3 : colors[seg.slot]; }

  var ticks = 4;
  for (var g = 0; g <= ticks; g++) {
    var gv = ceil * g / ticks;
    var gy = y(gv);
    svg.appendChild(svgEl('line', { x1: padL, y1: gy, x2: width - padR, y2: gy, stroke: g === 0 ? baseColor : gridColor, 'stroke-width': 1 }));
    svg.appendChild(svgText(padL - 8, gy + 4, 'end', ink3, 11, fmtCompact(gv)));
  }

  var barW = Math.min(24, per * 0.66);
  for (var d = 0; d < days.length; d++) {
    var v = vals[d];
    var segs = segList[d];
    var cx = padL + d * per + per / 2;
    var group = svgEl('g', {});
    var cum = 0;
    for (var s = 0; s < segs.length; s++) {
      var seg = segs[s];
      if (seg.total <= 0) { continue; }
      var y0 = y(cum), y1 = y(cum + seg.total);
      cum += seg.total;
      var h = Math.max(y0 - y1 - 2, 0.6);
      var rect;
      if (s === segs.length - 1) {
        rect = svgEl('path', { d: topRoundedRect(cx - barW / 2, y1 + 1, barW, h, 4), fill: segColor(seg) });
      } else {
        rect = svgEl('rect', { x: cx - barW / 2, y: y1 + 1, width: barW, height: h, fill: segColor(seg) });
      }
      group.appendChild(rect);
    }
    svg.appendChild(group);

    var hit = svgEl('rect', { x: padL + d * per, y: padT, width: per, height: plotH, fill: 'transparent', tabindex: '0' });
    (function (idx, grp) {
      function on(evt) {
        grp.setAttribute('opacity', '0.78');
        var dv = vals[idx];
        var dsegs = segList[idx];
        var rows = [];
        for (var si = 0; si < dsegs.length; si++) {
          rows.push({ color: segColor(dsegs[si]), label: dsegs[si].model, value: fmtNum(dsegs[si].total) });
        }
        var hitRate = dv.cacheRead + dv.uncached > 0 ? dv.cacheRead / (dv.cacheRead + dv.uncached) : null;
        var foot = '总计 ' + fmtNum(dv.total) + ' · 命中率 ' + fmtRate(hitRate) + ' · ' + fmtNum(dv.requests) + ' 请求';
        var px = evt && typeof evt.clientX === 'number' ? evt.clientX : grp.getBoundingClientRect().left;
        var py = evt && typeof evt.clientY === 'number' ? evt.clientY : grp.getBoundingClientRect().top;
        showTooltip(px, py, days[idx].day, rows, foot);
      }
      function off() { grp.setAttribute('opacity', '1'); hideTooltip(); }
      hit.addEventListener('pointermove', on);
      hit.addEventListener('pointerleave', off);
      hit.addEventListener('focus', on);
      hit.addEventListener('blur', off);
    })(d, group);
    svg.appendChild(hit);

    svg.appendChild(svgText(cx, height - 10, 'middle', ink3, 11, days[d].day.slice(5)));
  }
}

function aggregateModels(days) {
  var map = {};
  for (var i = 0; i < days.length; i++) {
    var models = days[i].models || [];
    for (var j = 0; j < models.length; j++) {
      var m = models[j];
      var e = map[m.model];
      if (!e) {
        e = { model: m.model, requests: 0, input: 0, cacheRead: 0, cacheCreation: 0, output: 0, total: 0 };
        map[m.model] = e;
      }
      e.requests += m.requests || 0;
      e.input += m.input_tokens || 0;
      e.cacheRead += m.cache_read_tokens || 0;
      e.cacheCreation += m.cache_creation_tokens || 0;
      e.output += m.output_tokens || 0;
      e.total += m.total_tokens || 0;
    }
  }
  var arr = [];
  for (var k in map) { arr.push(map[k]); }
  arr.sort(function (a, b) { return b.total - a.total || (a.model < b.model ? -1 : 1); });
  return arr;
}

function renderModelChart(days) {
  var svg = document.getElementById('modelChart');
  while (svg.firstChild) { svg.removeChild(svg.firstChild); }
  var models = aggregateModels(days);
  var folded = null;
  if (models.length > 8) {
    folded = { model: '其他', requests: 0, input: 0, cacheRead: 0, cacheCreation: 0, output: 0, total: 0, folded: true };
    for (var i = 8; i < models.length; i++) {
      folded.requests += models[i].requests;
      folded.input += models[i].input;
      folded.cacheRead += models[i].cacheRead;
      folded.cacheCreation += models[i].cacheCreation;
      folded.output += models[i].output;
      folded.total += models[i].total;
    }
    models = models.slice(0, 8);
    models.push(folded);
  }
  var rowH = 30, padL = 170, padR = 70, padT = 6;
  var width = 720;
  var height = padT * 2 + rowH * Math.max(models.length, 1);
  svg.setAttribute('width', width);
  svg.setAttribute('height', height);
  svg.setAttribute('viewBox', '0 0 ' + width + ' ' + height);
  if (models.length === 0) { return; }

  var maxV = 1;
  for (var m = 0; m < models.length; m++) { if (models[m].total > maxV) { maxV = models[m].total; } }
  var ink2 = cssVar('--ink-2') || '#52514e';
  var ink3 = cssVar('--ink-3') || '#898781';
  var fill = cssVar('--series-1') || '#2a78d6';
  var plotW = width - padL - padR;

  for (var r = 0; r < models.length; r++) {
    var e = models[r];
    var rowY = padT + r * rowH;
    var name = e.model.length > 20 ? e.model.slice(0, 19) + '…' : e.model;
    svg.appendChild(svgText(padL - 10, rowY + rowH / 2 + 4, 'end', ink2, 12, name));
    var bw = Math.max(plotW * e.total / maxV, e.total > 0 ? 2 : 0);
    if (bw > 0) {
      var barColor = e.folded ? ink3 : fill;
      svg.appendChild(svgEl('path', { d: rightRoundedRect(padL, rowY + (rowH - 18) / 2, bw, 18, 4), fill: barColor }));
    }
    svg.appendChild(svgText(padL + bw + 6, rowY + rowH / 2 + 4, 'start', ink2, 12, fmtCompact(e.total)));

    var hit = svgEl('rect', { x: 0, y: rowY, width: width, height: rowH, fill: 'transparent', tabindex: '0' });
    (function (entry) {
      function on(evt) {
        var uncached = Math.max(entry.input - entry.cacheRead, 0);
        var rows = [
          { color: fill, label: '缓存读取', value: fmtNum(entry.cacheRead) },
          { color: cssVar('--series-2'), label: '非缓存输入', value: fmtNum(uncached) },
          { color: cssVar('--series-3'), label: '输出', value: fmtNum(entry.output) }
        ];
        var hitRate = entry.input > 0 ? entry.cacheRead / entry.input : null;
        var foot = '总计 ' + fmtNum(entry.total) + ' · 命中率 ' + fmtRate(hitRate) + ' · ' + fmtNum(entry.requests) + ' 请求';
        var px = evt && typeof evt.clientX === 'number' ? evt.clientX : 100;
        var py = evt && typeof evt.clientY === 'number' ? evt.clientY : 100;
        showTooltip(px, py, entry.folded ? '其他（合并模型）' : entry.model, rows, foot);
      }
      function off() { hideTooltip(); }
      hit.addEventListener('pointermove', on);
      hit.addEventListener('pointerleave', off);
      hit.addEventListener('focus', on);
      hit.addEventListener('blur', off);
    })(e);
    svg.appendChild(hit);
  }
}

function load() {
  var key = getKey();
  if (!key) { showOverlay(true); return; }
  var to = new Date();
  var from = new Date();
  from.setDate(from.getDate() - (currentDays - 1));
  hideMsg();
  var content = document.getElementById('content');
  content.style.opacity = '0.5';
  fetch(API_PATH + '?from=' + fmtDate(from) + '&to=' + fmtDate(to), {
    headers: { 'Authorization': 'Bearer ' + key }
  }).then(function (resp) {
    if (resp.status === 401 || resp.status === 403) {
      content.style.opacity = '1';
      showMsg('认证失败（' + resp.status + '）：请重新输入管理密码。', true);
      showOverlay(true);
      throw new Error('unauthorized');
    }
    if (!resp.ok) { throw new Error('HTTP ' + resp.status); }
    return resp.json();
  }).then(function (data) {
    content.style.opacity = '1';
    render(data);
  }).catch(function (err) {
    content.style.opacity = '1';
    if (err.message !== 'unauthorized') { showMsg('加载失败：' + err.message, true); }
  });
}

function render(data) {
  var days = (data && data.days) || [];
  var tbody = document.getElementById('rows');
  while (tbody.firstChild) { tbody.removeChild(tbody.firstChild); }
  if (days.length === 0) {
    showMsg('所选范围内没有用量数据。', false);
  } else {
    hideMsg();
  }
  renderLegend(days);
  renderDailyChart(days);
  renderModelChart(days);
  var desc = days.slice().reverse();
  for (var i = 0; i < desc.length; i++) { appendDay(tbody, desc[i]); }
  if (data && data.totals) { appendRow(tbody, 'grand', '合计', '', data.totals); }
}

function appendDay(tbody, day) {
  var models = day.models || [];
  for (var i = 0; i < models.length; i++) {
    appendRow(tbody, '', i === 0 ? day.day : '', models[i].model, models[i]);
  }
  appendRow(tbody, 'sub', '', '小计', day.totals);
}

function appendRow(tbody, cls, dayText, modelText, c) {
  var tr = document.createElement('tr');
  if (cls) { tr.className = cls; }
  var cells = [dayText, modelText, fmtNum(c.requests), fmtNum(c.input_tokens),
    fmtNum(c.cache_read_tokens), fmtRate(c.cache_hit_rate),
    fmtNum(c.output_tokens), fmtNum(c.total_tokens)];
  for (var i = 0; i < cells.length; i++) {
    var td = document.createElement('td');
    td.textContent = cells[i];
    if (i < 2) { td.className = 'l'; }
    tr.appendChild(td);
  }
  tbody.appendChild(tr);
}

document.getElementById('saveKey').addEventListener('click', function () {
  var v = document.getElementById('keyInput').value.trim();
  if (!v) { return; }
  setKey(v);
  document.getElementById('keyInput').value = '';
  showOverlay(false);
  load();
});
document.getElementById('keyInput').addEventListener('keydown', function (e) {
  if (e.key === 'Enter') { document.getElementById('saveKey').click(); }
});
document.getElementById('clearKey').addEventListener('click', function () {
  clearStoredKey();
  showOverlay(true);
});
document.getElementById('reload').addEventListener('click', load);
var rangeButtons = document.querySelectorAll('button[data-days]');
for (var bi = 0; bi < rangeButtons.length; bi++) {
  rangeButtons[bi].addEventListener('click', function () {
    for (var bj = 0; bj < rangeButtons.length; bj++) { rangeButtons[bj].className = ''; }
    this.className = 'active';
    currentDays = parseInt(this.getAttribute('data-days'), 10);
    load();
  });
}
load();
</script>
</body>
</html>
`
