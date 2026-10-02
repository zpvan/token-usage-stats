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
  :root { color-scheme: light dark; }
  * { box-sizing: border-box; }
  body { font-family: -apple-system, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif; margin: 0; padding: 24px; background: #f5f6f8; color: #1f2329; }
  h1 { font-size: 20px; margin: 0 0 16px; }
  .toolbar { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; margin-bottom: 16px; }
  .toolbar .spacer { flex: 1; }
  button { padding: 6px 14px; border: 1px solid #c9cdd4; border-radius: 6px; background: #fff; color: #1f2329; cursor: pointer; font-size: 13px; }
  button:hover { background: #f0f1f3; }
  button.active { background: #2563eb; border-color: #2563eb; color: #fff; }
  table { width: 100%; border-collapse: collapse; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 3px rgba(0,0,0,.08); font-size: 13px; }
  th, td { padding: 8px 12px; text-align: right; border-bottom: 1px solid #eceef1; white-space: nowrap; }
  th { background: #f7f8fa; font-weight: 600; color: #4e5563; }
  td.l, th.l { text-align: left; }
  tr.sub td { background: #f7f8fa; font-weight: 600; }
  tr.grand td { background: #eaf1fe; font-weight: 700; }
  .msg { padding: 12px 16px; border-radius: 6px; margin-bottom: 12px; display: none; }
  .msg.err { display: block; background: #fee4e2; color: #b42318; }
  .msg.info { display: block; background: #e0eaff; color: #1d4ed8; }
  #overlay { position: fixed; inset: 0; background: rgba(15,18,22,.45); display: none; align-items: center; justify-content: center; }
  #overlay.show { display: flex; }
  .dialog { background: #fff; border-radius: 10px; padding: 24px; width: 360px; box-shadow: 0 8px 30px rgba(0,0,0,.2); }
  .dialog h2 { font-size: 16px; margin: 0 0 8px; }
  .dialog p { font-size: 13px; color: #4e5563; margin: 0 0 12px; }
  .dialog input { width: 100%; padding: 8px 10px; border: 1px solid #c9cdd4; border-radius: 6px; font-size: 14px; margin-bottom: 12px; }
  @media (prefers-color-scheme: dark) {
    body { background: #14171a; color: #e4e7eb; }
    table { background: #1d2126; }
    th { background: #23282e; color: #a7b0bc; }
    th, td { border-bottom-color: #2c323a; }
    button { background: #23282e; border-color: #39414b; color: #e4e7eb; }
    button:hover { background: #2c323a; }
    button.active { background: #2563eb; border-color: #2563eb; color: #fff; }
    tr.sub td { background: #23282e; }
    tr.grand td { background: #1f3a6e; }
    .dialog { background: #1d2126; }
    .dialog p { color: #a7b0bc; }
    .dialog input { background: #14171a; border-color: #39414b; color: #e4e7eb; }
  }
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
<table>
  <thead>
    <tr>
      <th class="l">日期</th><th class="l">模型</th><th>请求数</th><th>输入 tokens</th>
      <th>缓存读取</th><th>命中率</th><th>输出 tokens</th><th>总计</th>
    </tr>
  </thead>
  <tbody id="rows"></tbody>
</table>
<div id="overlay">
  <div class="dialog">
    <h2>输入管理密码</h2>
    <p>数据来自认证接口 /v0/management/usage-stats。请输入 CPA 的 Management Key（仅保存在本浏览器 localStorage）。</p>
    <input id="keyInput" type="password" placeholder="Management Key" autocomplete="off">
    <button id="saveKey">保存并加载</button>
  </div>
</div>
<script>
var KEY_STORAGE = 'token-usage-stats.management-key';
var API_PATH = '/v0/management/usage-stats';
var currentDays = 30;

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

function load() {
  var key = getKey();
  if (!key) { showOverlay(true); return; }
  var to = new Date();
  var from = new Date();
  from.setDate(from.getDate() - (currentDays - 1));
  hideMsg();
  fetch(API_PATH + '?from=' + fmtDate(from) + '&to=' + fmtDate(to), {
    headers: { 'Authorization': 'Bearer ' + key }
  }).then(function (resp) {
    if (resp.status === 401 || resp.status === 403) {
      showMsg('认证失败（' + resp.status + '）：请重新输入管理密码。', true);
      showOverlay(true);
      throw new Error('unauthorized');
    }
    if (!resp.ok) { throw new Error('HTTP ' + resp.status); }
    return resp.json();
  }).then(function (data) {
    render(data);
  }).catch(function (err) {
    if (err.message !== 'unauthorized') { showMsg('加载失败：' + err.message, true); }
  });
}

function render(data) {
  var tbody = document.getElementById('rows');
  tbody.innerHTML = '';
  var days = (data && data.days) || [];
  if (days.length === 0) { showMsg('所选范围内没有用量数据。', false); }
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
