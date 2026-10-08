# CPA 主题适配实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让插件仪表盘支持 CPA Management Center 的四套主题(跟随系统/纯白/羊毛纸/暗色),同源时自动跟随 Management Center 的主题选择,页面内可覆盖。

**Architecture:** 改动全部在 `page.go` 的内嵌 HTML 常量(`pageHTML`,Go raw string,**禁止反引号**)与 `page_test.go` 的 marker 测试。CSS 层:调色板变量替换为 CPA `themes.scss` 的设计 token,`:root` = 羊毛纸默认,`[data-theme="white"]`/`[data-theme="dark"]` 覆盖,删除 `@media prefers-color-scheme` 块。JS 层:`<head>` 内同步脚本解析主题(本地覆盖 → CPA localStorage → auto)并设置 `data-theme`;底部主脚本负责切换器、storage/matchMedia 联动与重绘。

**Tech Stack:** Go(raw string 常量)、纯 HTML/CSS/JS(零依赖)、Go testing(marker 断言)。

**Spec:** `docs/superpowers/specs/2026-10-08-cpa-theme-adaptation-design.md`

**上游事实(已核实,勿改动语义):**
- CPA 主题存于 `localStorage["cli-proxy-theme"]`,zustand persist JSON:`{"state":{"theme":"auto|light|white|dark",...},"version":0}`
- CPA auto 解析:系统暗 → `dark`;系统亮 → `white`(auto 永不落到羊毛纸)
- Management Center 用裸 iframe 加载插件页,不传主题;同源时 iframe 可读其 localStorage 并收到 `storage` 事件

---

### Task 1: CSS 调色板迁移到 CPA 设计 token

**Files:**
- Modify: `page.go`(pageHTML 常量内 `<style>` 块,约 13-72 行)

- [ ] **Step 1: 替换 `<style>` 头部(变量定义块)**

将现有内容(从 `<style>` 后的 `:root { color-scheme: light; }` 到 `@media (prefers-color-scheme: dark) { ... }` 整块结束)替换为:

```css
  * { box-sizing: border-box; }
  /* 调色板与 CLIProxyAPI Management Center 对齐(themes.scss):
     :root = 羊毛纸(默认浅色);[data-theme="white"] = 纯白;[data-theme="dark"] = 暗色。
     data-theme 由 <head> 内主题脚本设置;羊毛纸不设 CSS 块,即 :root 默认值。 */
  :root {
    color-scheme: light;
    --page: #faf9f5; --surface-1: #f0eee8; --surface-2: #e9e6df;
    --ink-1: #2d2a26; --ink-2: #6d6760; --ink-3: #a29c95;
    --grid: #e3e1db; --baseline: #d5d2cb; --border: rgba(45,42,38,0.10);
    --series-1: #2a78d6; --series-2: #eb6834; --series-3: #1baf7a;
    --series-4: #eda100; --series-5: #e87ba4; --series-6: #008300;
    --series-7: #4a3aa7; --series-8: #e34948;
    --msg-err-bg: rgba(198,87,70,0.12); --msg-err-fg: #c65746;
    --msg-info-bg: rgba(139,134,128,0.14); --msg-info-fg: #6d6760;
  }
  [data-theme="white"] {
    --page: #ffffff; --surface-1: #ffffff; --surface-2: #f6f6f6;
    --grid: #e5e5e5; --baseline: #d9d9d9;
    --msg-err-bg: rgba(198,87,70,0.10);
  }
  [data-theme="dark"] {
    color-scheme: dark;
    --page: #151412; --surface-1: #1d1b18; --surface-2: #262320;
    --ink-1: #f6f4f1; --ink-2: #c9c3bb; --ink-3: #9c958d;
    --grid: #3a3530; --baseline: #4a453f; --border: rgba(246,244,241,0.10);
    --series-1: #3987e5; --series-2: #d95926; --series-3: #199e70;
    --series-4: #c98500; --series-5: #d55181; --series-6: #008300;
    --series-7: #9085e9; --series-8: #e66767;
    --msg-err-bg: rgba(198,87,70,0.18);
    --msg-info-bg: rgba(139,134,128,0.20); --msg-info-fg: #c9c3bb;
  }
```

注意:原 `* { box-sizing: border-box; }` 与 `:root { color-scheme: light; }` 两行一并并入上方块,不要重复保留;`@media (prefers-color-scheme: dark)` 整块删除。

- [ ] **Step 2: 更新引用旧变量的规则**

在 `<style>` 剩余规则中做 5 处替换:

1. `button:hover { background: var(--grid); }` → `button:hover { background: var(--surface-2); }`
2. 在 `button.active { ... }` 规则之后新增一行:
   ```css
   select { padding: 6px 10px; border: 1px solid var(--border); border-radius: 6px; background: var(--surface-1); color: var(--ink-1); font-size: 13px; }
   ```
3. `tr.sub td { background: var(--grid); font-weight: 600; }` → `tr.sub td { background: var(--surface-2); font-weight: 600; }`
4. `.msg.err { display: block; background: #fee4e2; color: #b42318; }` → `.msg.err { display: block; background: var(--msg-err-bg); color: var(--msg-err-fg); }`
5. `.msg.info { display: block; background: #e0eaff; color: #1d4ed8; }` → `.msg.info { display: block; background: var(--msg-info-bg); color: var(--msg-info-fg); }`

- [ ] **Step 3: 跑现有测试确认无回归**

Run: `go test ./... -run TestResourcePageServed -v`
Expected: PASS(现有 marker 如 `--series-1`、`--series-8`、`localStorage` 均仍存在于页面源码)

- [ ] **Step 4: Commit**

```bash
git add page.go
git commit -m "refactor: migrate dashboard palette to CLIProxyAPI design tokens"
```

---

### Task 2: 新增主题功能的失败测试(TDD)

**Files:**
- Modify: `page_test.go`(marker 列表)

- [ ] **Step 1: 在 marker 列表末尾追加新 marker**

`page_test.go` 中 `for _, marker := range []string{...}` 的列表,在 `"TPS",` 之后追加:

```go
			"--surface-2", "[data-theme=\"white\"]", "[data-theme=\"dark\"]",
			"cli-proxy-theme", "token-usage-stats.theme", "data-theme",
			"THEME_LOCAL_KEY", "THEME_CPA_KEY", "readCpaTheme", "resolveTheme", "applyTheme",
			"themeSel", "onThemeChanged", "lastData",
			"跟随 CPA", "跟随系统", "纯白", "羊毛纸", "暗色",
			"prefers-color-scheme",
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./... -run TestResourcePageServed -v`
Expected: FAIL,报 `page missing marker "--surface-2"`(或列表中第一个缺失的 marker)

不要 commit。

---

### Task 3: 主题决策脚本 + 切换器 + 联动

**Files:**
- Modify: `page.go`(pageHTML 常量:`<head>` 新增脚本、工具栏新增 `<select>`、底部主脚本新增联动)

- [ ] **Step 1: 在 `</style>` 之后、`</head>` 之前插入主题解析脚本**

```html
<script>
// 主题:优先跟随 CPA Management Center(localStorage["cli-proxy-theme"],
// zustand persist JSON,theme 取值 auto|light|white|dark);
// 本页 localStorage["token-usage-stats.theme"] 可覆盖,取值 cpa|auto|white|light|dark。
// auto 解析与 CPA 一致:系统暗 → dark,系统亮 → white(永不落到羊毛纸)。
// 在 <head> 内同步执行,先于 body 渲染,避免主题闪烁。
var THEME_LOCAL_KEY = 'token-usage-stats.theme';
var THEME_CPA_KEY = 'cli-proxy-theme';
function getLocalTheme() {
  try { return localStorage.getItem(THEME_LOCAL_KEY) || 'cpa'; } catch (e) { return 'cpa'; }
}
function readCpaTheme() {
  try {
    var raw = localStorage.getItem(THEME_CPA_KEY);
    if (!raw) { return 'auto'; }
    var v = JSON.parse(raw);
    v = v && v.state && v.state.theme;
    return (v === 'auto' || v === 'white' || v === 'light' || v === 'dark') ? v : 'auto';
  } catch (e) { return 'auto'; }
}
function resolveTheme(pref) {
  var t = pref === 'cpa' ? readCpaTheme() : pref;
  if (t === 'auto') {
    return (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) ? 'dark' : 'white';
  }
  return (t === 'white' || t === 'light' || t === 'dark') ? t : 'white';
}
function applyTheme() {
  document.documentElement.setAttribute('data-theme', resolveTheme(getLocalTheme()));
}
applyTheme();
</script>
```

- [ ] **Step 2: 工具栏插入主题切换器**

在 `<span class="spacer"></span>` 之后、`<button id="clearKey">` 之前插入:

```html
  <select id="themeSel" aria-label="主题">
    <option value="cpa">跟随 CPA</option>
    <option value="auto">跟随系统</option>
    <option value="white">纯白</option>
    <option value="light">羊毛纸</option>
    <option value="dark">暗色</option>
  </select>
```

- [ ] **Step 3: 主脚本中缓存数据用于主题切换重绘**

1. 在 `var currentDays = 30;` 之后新增一行:`var lastData = null;`
2. 在 `function render(data) {` 函数体的第一行(`var days = ...` 之前)新增:`lastData = data;`

- [ ] **Step 4: 主脚本末尾(最后的 `load();` 调用之前)插入联动代码**

```js
// 主题联动:切换器写本地覆盖;同源 iframe 场景下 CPA 改主题触发 storage 事件,
// 仅当本地覆盖为 cpa 时跟随;系统主题变化仅当当前路径经过 auto 时重算。
// 图表颜色取自 CSS 变量,主题变化后用缓存数据重绘。
function onThemeChanged() {
  applyTheme();
  if (lastData) { render(lastData); }
}
var themeSel = document.getElementById('themeSel');
themeSel.value = getLocalTheme();
themeSel.addEventListener('change', function () {
  try { localStorage.setItem(THEME_LOCAL_KEY, themeSel.value); } catch (e) {}
  onThemeChanged();
});
window.addEventListener('storage', function (e) {
  if (e.key === THEME_CPA_KEY && getLocalTheme() === 'cpa') { onThemeChanged(); }
});
if (window.matchMedia) {
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', function () {
    var pref = getLocalTheme();
    if (pref === 'auto' || (pref === 'cpa' && readCpaTheme() === 'auto')) { onThemeChanged(); }
  });
}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./... -run TestResourcePageServed -v`
Expected: PASS

- [ ] **Step 6: 全量校验**

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: `gofmt -l` 无输出;vet 无输出;全部测试 PASS

- [ ] **Step 7: Commit**

```bash
git add page.go page_test.go
git commit -m "feat: dashboard themes — follow CPA / system / white / wool-paper / dark"
```

---

### Task 4: 同步 README 描述

**Files:**
- Modify: `README.md:33`
- Modify: `README_CN.md:33`

- [ ] **Step 1: 更新英文 README**

`README.md` 第 33 行:

旧:`- **Histogram dashboard**: daily stacked columns per model + per-model ranking bars + detail table, light/dark themes, no external JS dependencies`

新:`- **Histogram dashboard**: daily stacked columns per model + per-model ranking bars + detail table, four themes matching the CLIProxyAPI Management Center (follow system / pure white / wool paper / dark, auto-follows the management UI when same-origin), no external JS dependencies`

- [ ] **Step 2: 更新中文 README**

`README_CN.md` 第 33 行结尾的 `亮暗双主题`:

旧:`……模型用量排行条形图 + 明细表格，亮暗双主题`

新:`……模型用量排行条形图 + 明细表格，四套主题与 CLIProxyAPI 管理界面一致（跟随系统/纯白/羊毛纸/暗色，同源时自动跟随管理界面主题）`

- [ ] **Step 3: Commit**

```bash
git add README.md README_CN.md
git commit -m "docs: describe the four CPA-aligned dashboard themes"
```

---

## 人工验收(自动化测试覆盖不到,执行完 Task 3 后建议做)

marker 测试只验证源码包含关键片段,不验证渲染。建议在装有本插件的 CPA 实例上打开 `/v0/resource/plugins/token-usage-stats/stats`:

1. 切换器 5 个选项逐一切换,页面立即换肤且图表重绘不报错;
2. 同源经 Management Center 打开(iframe),在管理界面切主题,仪表盘实时跟随(切换器处于「跟随 CPA」时);
3. 不同源直接打开时,「跟随 CPA」退化为跟随系统;
4. 暗色主题下消息条、tooltip、对话框无刺眼配色;
5. 页面源码无反引号(已有测试兜底)。
