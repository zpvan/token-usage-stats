# 仪表盘主题适配 CLIProxyAPI — 设计文档

日期:2026-10-08
状态:已获用户批准
范围:仅 `page.go`(内嵌仪表盘 HTML/CSS/JS)与 `page_test.go`

## 背景与目标

token-usage-stats 插件的统计页(`/v0/resource/plugins/token-usage-stats/stats`)目前只支持 `prefers-color-scheme` 自动明暗两套配色,无手动切换。而 CPA Management Center([Cli-Proxy-API-Management-Center](https://github.com/router-for-me/Cli-Proxy-API-Management-Center))提供 4 种主题:**跟随系统(auto)/ 羊毛纸(light,默认,暖灰纸感)/ 纯白(white)/ 暗色(dark)**。

目标:让插件仪表盘支持这 4 种主题,调色板与 CPA Management Center 完全一致,并尽可能跟随用户在 Management Center 中的主题选择。

## 关键约束(已核实的上游事实)

- Management Center 把插件页面放在**裸 iframe** 中加载(`PluginResourcePage.tsx`),**不传主题**:无 URL query 参数、无 postMessage。
- CPA 主题存储:`localStorage["cli-proxy-theme"]`,zustand persist 序列化的 JSON,形如 `{"state":{"theme":"auto","resolvedTheme":"light"},"version":0}`。`theme` 取值 `auto | light | white | dark`。
- CPA 应用方式:`<html data-theme="dark">`、`data-theme="white"`,light(羊毛纸)为 `:root` 默认(移除属性)。
- CPA auto 解析逻辑(`useThemeStore.resolveAutoTheme`):系统暗 → `dark`;系统亮 → `white`(**注意:auto 永不落到羊毛纸**)。
- 同源时(管理界面由 CPA 同源托管),iframe 内可直接读 `localStorage["cli-proxy-theme"]`,且 CPA 改主题会触发本页的 `storage` 事件;跨源时读不到。
- 本页为 Go raw string 常量,**禁止出现反引号**;零第三方 JS 依赖。

## 决策(用户已确认)

1. **主题来源**:优先跟随 CPA,页面可覆盖。同源时自动读 `cli-proxy-theme`;读不到则退化为本地逻辑。
2. **切换器选项**:5 项——**跟随 CPA(默认)/ 跟随系统 / 纯白 / 羊毛纸 / 暗色**。

## 设计

### 1. 主题决策逻辑(页面内嵌 JS)

存储 key:

- `cli-proxy-theme`:CPA 的 key(只读,不写)。
- `token-usage-stats.theme`:本页本地覆盖,取值 `cpa | auto | white | light | dark`,默认 `cpa`。

解析流程:

```
本地覆盖 (默认 "cpa")
  ├─ "cpa"  → 尝试 JSON.parse(localStorage["cli-proxy-theme"]).state.theme
  │           ├─ 得到 auto/white/light/dark → 用之
  │           └─ 解析失败/不存在(跨域、无 CPA) → 按 "auto" 处理
  ├─ "auto" → matchMedia("(prefers-color-scheme: dark)") ? "dark" : "white"
  └─ "white" / "light" / "dark" → 直接使用
```

应用:`document.documentElement.setAttribute("data-theme", resolved)`(三个值显式设置,包括 `light`,便于 CSS 选择器统一),并按 resolved 设置 `color-scheme`(`dark` → dark,其余 → light)。

实时联动:

- `window` 的 `storage` 事件:`e.key === "cli-proxy-theme"` 且当前本地覆盖为 `cpa` 时,重新解析并应用 —— 同源 iframe 中用户在 Management Center 切主题,本页立即跟随。
- `matchMedia("(prefers-color-scheme: dark)")` 的 `change` 事件:当前生效路径经过 `auto`(本地覆盖为 `auto`,或覆盖为 `cpa` 且 CPA 侧为 `auto`/缺失)时重新解析。

防闪烁:解析脚本为 `<head>` 内同步执行的 `<script>`,在 body 渲染前完成 `data-theme` 设置。脚本与样式均不得含反引号。

### 2. 调色板

保留现有 CSS 变量名,值替换为 CPA `themes.scss` 的 token;新增 `--surface-2`(hover、小计行背景,替代原先用 `--grid` 的两处)。

| 变量 | 羊毛纸 light(`:root` 默认) | 纯白 white | 暗色 dark |
|---|---|---|---|
| `--page` | `#faf9f5`(bg-secondary) | `#ffffff` | `#151412` |
| `--surface-1`(卡片/浮层) | `#f0eee8`(bg-primary) | `#ffffff` | `#1d1b18` |
| `--surface-2`(hover/小计行,新增) | `#e9e6df`(bg-tertiary) | `#f6f6f6` | `#262320` |
| `--ink-1` | `#2d2a26` | `#2d2a26` | `#f6f4f1` |
| `--ink-2` | `#6d6760` | `#6d6760` | `#c9c3bb` |
| `--ink-3` | `#a29c95` | `#a29c95` | `#9c958d` |
| `--grid` | `#e3e1db`(border-color) | `#e5e5e5` | `#3a3530` |
| `--baseline` | `#d5d2cb`(border-primary) | `#d9d9d9` | `#4a453f` |
| `--border` | `rgba(45,42,38,0.10)` | `rgba(45,42,38,0.10)` | `rgba(246,244,241,0.10)` |

说明:

- 8 个 `--series-*` 颜色 CPA 无对应 token,保持现状:浅色组用于 light/white,深色组用于 dark。
- 消息条(`.msg.err` / `.msg.info`)改用 CPA warning 语义色:err 用 `rgba(198,87,70,0.12)` 底 + `#c65746` 字(dark 下底 `rgba(198,87,70,0.18)`),info 沿用中性灰 `#8b8680` 系;不再使用当前暗色下刺眼的 `#fee4e2`/`#e0eaff`。
- CSS 结构:`:root` 放羊毛纸值作为兜底(JS 禁用时页面仍可读);`[data-theme="white"]`、`[data-theme="dark"]` 覆盖;`[data-theme="light"]` **有意不设 CSS 块**(羊毛纸即 `:root` 默认值,显式设置该属性仅为 JS 语义统一);**删除现有 `@media (prefers-color-scheme: dark)` 块**(由 JS 接管,避免与显式 `data-theme` 冲突)。
- `button:hover`、`tr.sub td` 的背景从 `var(--grid)` 改为 `var(--surface-2)`。

### 3. 工具栏切换器

在工具栏 `.spacer` 之后、「清除密码」之前插入:

```html
<select id="themeSel" aria-label="主题">
  <option value="cpa">跟随 CPA</option>
  <option value="auto">跟随系统</option>
  <option value="white">纯白</option>
  <option value="light">羊毛纸</option>
  <option value="dark">暗色</option>
</select>
```

- 样式与现有 `button` 一致(相同的 border、radius、字体、背景变量)。
- 初始值 = 当前本地覆盖;变更时写 `localStorage["token-usage-stats.theme"]` 并立即应用。

### 4. 测试

`page_test.go` 的 marker 列表追加(全部需出现在页面源码中):

- `"cli-proxy-theme"`(读取 CPA 存储的代码)
- `"token-usage-stats.theme"`(本地覆盖 key)
- `"data-theme"`、`"[data-theme=\"white\"]"`、`"[data-theme=\"dark\"]"`(或等价的 CSS/JS 片段)
- `"跟随 CPA"`、`"跟随系统"`、`"纯白"`、`"羊毛纸"`、`"暗色"`(切换器选项)
- `"prefers-color-scheme"`(auto 解析)
- `"--surface-2"`

「无反引号」约束与现有 marker 全部保留。

## 非目标(YAGNI)

- 不向 CPA 上游提 iframe 主题传递(postMessage/query)的改动 —— 先用同源 localStorage 方案。
- 不为图表新增 CPA 风格 series 调色板,保留现有 8 色。
- 不做主题切换动画。
- 不引入任何外部 JS/CSS。

## 风险与缓解

- **跨源部署读不到 CPA 设置**:解析失败静默回退到 auto 逻辑,页面始终可用。
- **CPA 存储格式变化**(zustand persist 结构):解析做防御式判空,失败回退 auto。
- **localStorage 抛异常**(隐私模式):所有读写包 try/catch。
