# token-usage-stats 插件设计（Spec）

- 日期：2026-10-02
- 状态：已确认（用户已批准设计）
- 项目位置：`/Users/knox/Documents/GitWorkSpace/token-usage-stats`（独立 git 仓库）
- 目标宿主：CLIProxyAPI（https://github.com/router-for-me/CLIProxyAPI）

## 1. 背景与目标

CLIProxyAPI（下称 CPA）目前没有「按天聚合 + 持久化」的 token 用量统计：现有管理接口只有内存中的原始请求记录队列（默认保留 60 秒、重启即失）和不含 token 的请求计数。

本插件以 CPA 官方 C ABI 插件机制实现：

- 按 **天 × 模型** 聚合每个请求的 token 用量：输入（含缓存口径换算）、缓存读取/写入、缓存命中率、输出（含推理）、请求数/失败数
- 数据**持久化到本地 JSON 文件**，保留最近 **30 天**（可配置），重启不丢
- 两种查看方式：
  - **认证 JSON API**：`GET /v0/management/usage-stats`（走 CPA 现有管理认证）
  - **浏览器页面**：`/v0/resource/plugins/token-usage-stats/stats`（静态页壳 + JS 带管理密码拉取 JSON API）

### 非目标（YAGNI）

- 不做 CLI flag 查看方式
- 不做按 provider / API Key / AuthID 维度的聚合（数据模型里的 provider 字段仅用于缓存口径换算，不做聚合 key）
- 不做实时监控、图表可视化（纯表格）
- 不修改 CPA 主程序任何代码
- 不引入任何第三方 Go 依赖（纯标准库）

## 2. 方案概述

C ABI 动态库插件（Go，`go build -buildmode=c-shared`），声明 `usage_plugin` + `management_api` 双能力：

```
CPA host ──RPC──▶ usage.handle（每条请求一条 UsageRecord）
                        │
                        ▼
              ┌──────────────────┐   每30s+shutdown    ┌─────────────────────┐
              │ 内存聚合 map      │ ───原子写─────────▶  │ data/2026-10-02.json │
              │ (day, model) →    │                     │ （每天一个文件，       │
              │  counters        │ ◀──启动时加载──────── │  30天滚动删除）        │
              └──────────────────┘                      └─────────────────────┘
                        │
        ┌───────────────┴────────────────┐
        ▼                                ▼
 management.handle              management.handle
 GET /v0/management/            GET /v0/resource/plugins/
   usage-stats（Bearer 认证）      token-usage-stats/stats（静态页壳）
        ▼                                │
      JSON ◀──────── fetch(Bearer) ──────┘ 页面本身无数据，JS 带 key 拉 JSON
```

### 备选方案（已否决）

- **sidecar 轮询 usage-queue**：队列是内存环形缓冲（默认 60s、上限 1h），sidecar 停机即丢数据；多一个常驻进程。否决。
- **改 CPA 主程序内建统计**：需维护 fork，违背插件形态诉求。否决。

## 3. 插件机制契约（已对照 CPA 源码核实）

### 3.1 C ABI 导出

参照官方示例 `examples/plugin/usage/go/main.go`，cgo 头中定义 `cliproxy_host_api` / `cliproxy_plugin_api` 结构体，Go 侧导出：

- `cliproxy_plugin_init(host, plugin)`：保存 host API 指针，填充 plugin 回调表
- `cliproxyPluginCall(method, request, requestLen, response)`：method 分发入口
- `cliproxyPluginFree(ptr, len)`
- `cliproxyPluginShutdown()`：最终 flush

method 调用与响应均为 JSON 信封：`{"ok": bool, "result": ..., "error": {"code","message"}}`。

### 3.2 生命周期方法

**`plugin.register` / `plugin.reconfigure`**（热重载时调后者）

- 请求体：`{"config_yaml": "<base64>", "schema_version": 6}`（`config_yaml` 即 config.yaml 中本插件配置子树的 YAML 字节，Go `[]byte` 以 base64 编码）
- 响应体：

```json
{
  "schema_version": 6,
  "metadata": {
    "Name": "token-usage-stats",
    "Version": "0.1.0",
    "Author": "zpvan",
    "GitHubRepository": "",
    "Logo": "",
    "ConfigFields": [
      {"Name": "data_dir", "Type": "string", "Description": "Directory for daily JSON aggregate files (default ./token-usage-stats-data)"},
      {"Name": "retention_days", "Type": "integer", "Description": "Days of daily aggregates to retain (default 30, range 1-365)"}
    ]
  },
  "capabilities": {"usage_plugin": true, "management_api": true}
}
```

（注：metadata 字段名为 Go 结构体默认 JSON 名，首字母大写。）

- 两个方法都必须在此刻解析 `config_yaml` 并应用配置；reconfigure 配置非法时返回错误信封并保留旧配置运行。

**`usage.handle`**

- 请求体：`pluginapi.UsageRecord` 的 JSON（Go 默认字段名，大写开头），关键字段：
  - `RequestID`, `TraceID`, `Provider`, `ExecutorType`, `Model`, `Alias`, `ResponseModel`
  - `APIKey`, `AuthID`, `AuthType`, `Source`
  - `RequestedAt`（RFC3339 时间字符串）, `Latency`, `TTFT`
  - `Failed`（bool）, `Failure`（`{"StatusCode","Body"}`）
  - `Stream`, `Generate`
  - `Detail`: `{"InputTokens","OutputTokens","ReasoningTokens","CachedTokens","CacheReadTokens","CacheCreationTokens","TotalTokens"}`（int64）
- 响应：`{"ok":true,"result":{}}`
- 推送由 CPA 的 usage Manager 单 dispatcher goroutine 串行调用；host 侧对插件调用有 panic recover + 熔断，插件内部不再 recover。

**`management.register`**

- 响应体：

```json
{
  "routes": [
    {"Method": "GET", "Path": "/usage-stats", "Description": "Daily per-model token usage statistics"}
  ],
  "resources": [
    {"Path": "/stats", "Menu": "Usage Stats", "Description": "Daily per-model token usage page"}
  ]
}
```

- `routes[].Path` 解析到 `/v0/management/` 前缀下 → 实际端点 `GET /v0/management/usage-stats`，host 会先做管理认证（`Authorization: Bearer <key>` 或 `X-Management-Key`）再转发插件。
- `resources[].Path` 解析到 `/v0/resource/plugins/<pluginID>/` 前缀下 → 实际端点 `GET /v0/resource/plugins/token-usage-stats/stats`，**无管理认证**（仅 GET）。注意：host 对 resource 路径做 `TrimRight("/")` 且拒绝空路径，因此必须注册非根路径（`/stats`），不能注册 `/`。
- host 按「HTTP 方法 + 完整请求路径」精确匹配路由。

**`management.handle`**

- 请求体：`{"Method","Path","Headers","Query","Body"}`；`Headers`/`Query` 为 `map[string][]string`；`Body` 为 base64。
- 响应体：`{"StatusCode":200,"Headers":{"content-type":["application/json"]},"Body":"<base64>"}`
- schema_version ≥ 6 时 JSON 响应不做 HTML 实体转义。

### 3.3 插件 ID 与文件部署

- 插件文件名去扩展名即 pluginID：`token-usage-stats.dylib` → ID `token-usage-stats`（可选 `-v<version>` 后缀携带版本，如 `token-usage-stats-v0.1.0.dylib`，ID 仍为 `token-usage-stats`）
- CPA 扫描 `<plugins.dir>/<goos>/<goarch>/` 与 `<plugins.dir>/` 两处（`plugins.dir` 默认 `plugins`）
- 启用方式（config.yaml）：

```yaml
plugins:
  enabled: true
  configs:
    token-usage-stats:
      enabled: true
      data_dir: ./token-usage-stats-data   # 可选
      retention_days: 30                    # 可选
```

## 4. 数据模型与口径

### 4.1 聚合 key 与归属

- **day**：`record.RequestedAt` 在**服务器本地时区**（`time.Local`）下的日期，格式 `YYYY-MM-DD`；`RequestedAt` 为零值时回退 `time.Now()`
- **model**：依次取 `record.Model` → `record.ResponseModel` → `record.Alias` → `"unknown"`（取第一个非空、trim 后值）

### 4.2 每个 (day, model) 的累计字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `requests` | int64 | 请求总数（含失败、含 Generate=false） |
| `failed_requests` | int64 | `Failed=true` 的请求数 |
| `input_tokens` | int64 | **总输入**（含缓存，口径换算见 4.3） |
| `cache_read_tokens` | int64 | 缓存读取（命中） |
| `cache_creation_tokens` | int64 | 缓存写入 |
| `output_tokens` | int64 | 输出总量（含推理） |
| `reasoning_tokens` | int64 | 输出中的推理部分 |
| `total_tokens` | int64 | `input_tokens + output_tokens` |
| `decode_tokens` | int64 | 参与 TPS 统计的输出 tokens（仅成功且有输出的请求） |
| `decode_ms` | int64 | 生成（decode）耗时毫秒数（同上口径） |

### 4.3 缓存命中率口径（关键）

不同 provider 的 `Detail.InputTokens` 语义不同，需先换算成统一「总输入」（复刻 CPA `sdk/cliproxy/usage/accounting.go` 的 `tokenAccountingSemanticsFor` 映射）：

| 语义 | 匹配条件（对 `Provider + " " + ExecutorType` 小写串判断） | 总输入 = |
|---|---|---|
| independent | 含 `claude` 或 `anthropic` | `InputTokens + CacheReadTokens + CacheCreationTokens` |
| separateReasoning | 含 `gemini`/`aistudio`/`antigravity`/`vertex`/`interaction` | `InputTokens`（缓存已含） |
| subset | 含 `openai`/`codex`/`xai`/`grok`/`kimi`/`qwen`/`deepseek`/`openrouter`，或 provider 为 `openai-compatibility` / 前缀 `openai-compatible-`，或 executor 为 `openaicompatexecutor` | `InputTokens`（缓存已含） |
| unknown | 其余 | `InputTokens`（按 subset 惯例处理） |

注意：匹配顺序为 openai-compat → claude/anthropic → gemini 系 → openai 系 → unknown（与 CPA 一致）。

- **缓存命中率 = `cache_read_tokens / input_tokens`**（换算后的总输入；为 0 时输出 `null`）
- **TPS（tokens/秒）= `decode_tokens / (decode_ms / 1000)`**：decode 耗时 = 流式请求 `Latency − TTFT`，非流式或 TTFT 异常时退化为 `Latency`；仅统计**成功且输出 > 0** 的请求；无有效计时样本时输出 `null`；保留 1 位小数
- 单条记录换算时各分量先 clamp 到 ≥ 0 再累加

### 4.4 输出 tokens 口径

- subset / independent：`output_tokens += OutputTokens`（其中已含 reasoning）
- separateReasoning（Gemini 系）：`output_tokens += OutputTokens + ReasoningTokens`（该语义下 reasoning 独立于 output 上报）
- `reasoning_tokens` 始终累加 `ReasoningTokens`

## 5. 存储

### 5.1 文件布局

- 目录：`data_dir`（默认 `./token-usage-stats-data`，相对 CPA 进程工作目录）
- 每天一个文件：`<data_dir>/YYYY-MM-DD.json`

```json
{
  "day": "2026-10-02",
  "models": {
    "gpt-5": {
      "requests": 12, "failed_requests": 0,
      "input_tokens": 120000, "cache_read_tokens": 80000, "cache_creation_tokens": 0,
      "output_tokens": 8000, "reasoning_tokens": 2000, "total_tokens": 128000
    }
  }
}
```

（`cache_hit_rate` 不持久化，读取/输出时现算。）

### 5.2 写入策略

- 内存聚合为权威；`usage.handle` 更新内存后把对应 day 标记 dirty
- 后台 goroutine 每 **30 秒** flush：对每个 dirty day 写 `<day>.json.tmp` 后 `rename` 原子替换，清除 dirty 标记
- `cliproxyPluginShutdown` 时同步最终 flush
- flush 失败仅 log warn（插件无 host 日志回调，写到 stderr 由 CPA 捕获；不丢内存数据，下轮重试）

### 5.3 启动加载与滚动清理

- register/reconfigure 生效配置后：扫描 `data_dir` 中匹配 `YYYY-MM-DD.json` 的文件，加载**最近 retention_days 天**进内存；解析失败的文件跳过（stderr warn），不中断
- 启动及每次 flush 时：删除日期早于 `today - retention_days + 1` 的匹配文件（含无法解析日期的同名异常文件不删，仅严格匹配日期格式的参与滚动）
- `retention_days` 默认 30，合法范围 1–365，越界 clamp

## 6. 对外接口

### 6.1 认证 JSON API

```
GET /v0/management/usage-stats?from=YYYY-MM-DD&to=YYYY-MM-DD
```

- 认证：host 侧管理中间件（`Authorization: Bearer` / `X-Management-Key`），插件不重复校验
- query 均可选；缺省 = 最近 retention_days 内有数据的所有天；非法日期 → 400 `{"error":"invalid from/to date, want YYYY-MM-DD"}`
- `from > to` → 400；范围裁剪到实际有数据的天
- 200 响应（按 day 升序，每天内 models 按 total_tokens 降序）：

```json
{
  "days": [
    {
      "day": "2026-10-02",
      "models": [
        {
          "model": "gpt-5",
          "requests": 12, "failed_requests": 0,
          "input_tokens": 120000,
          "cache_read_tokens": 80000, "cache_creation_tokens": 0,
          "cache_hit_rate": 0.6667,
          "output_tokens": 8000, "reasoning_tokens": 2000,
          "total_tokens": 128000
        }
      ],
      "totals": { "requests": 12, "...": "各字段当天合计，cache_hit_rate 按合计现算" }
    }
  ],
  "totals": { "requests": 12, "...": "全范围合计" },
  "generated_at": "2026-10-02T15:04:05+08:00"
}
```

- `cache_hit_rate` 保留 4 位小数；分母为 0 → `null`
- 无数据 → `{"days":[],"totals":{...全零},"generated_at":...}`

### 6.2 浏览器页面

- `GET /v0/resource/plugins/token-usage-stats/stats` → `200 text/html; charset=utf-8`，单文件 HTML（内联 CSS/JS，无外部依赖、无框架）
- 页面行为：
  1. 首次打开弹窗输入管理密码（Management Key），存 `localStorage`；提供「清除密码」按钮
  2. JS 用 `Authorization: Bearer <key>` fetch `/v0/management/usage-stats?from=&to=`；401 时提示密码错误并允许重输
  3. **每日 Tokens 直方图**：按天堆叠柱状图（SVG 手绘，无框架），**分段 = 当天各模型用量**（每模型固定一个 categorical 槽位色，跨天/跨时间范围保持一致；>8 个模型折叠为「其他」灰色段），legend 标明每段模型名，每柱 hover tooltip（各模型用量 + 总计 + 命中率）
  4. **模型用量排行**：横向条形图（total tokens 降序，单色蓝，>8 个模型折叠为「其他」），条端标注紧凑数值，hover 显示完整明细
  5. 明细表格列：**日期 | 模型 | 请求数 | 输入 tokens | 缓存读取 | 命中率 | 输出 tokens | 总计**
     （`cache_creation_tokens`/`reasoning_tokens`/`failed_requests` 只在 JSON 中，不上表格）
  6. 图表按时间升序、表格日期倒序；每天一个「当日小计」行；页面底部全范围合计行
  7. 范围切换按钮：近 7 / 14 / 30 天（默认 30），作用于所有图表与表格；数字千分位格式化（tooltip/表格）与紧凑格式（坐标轴/条端，K/M）；命中率显示百分比（`null` 显示 `-`）
  8. 配色遵循 dataviz 规范：categorical slots 1–3（蓝/橙/青），亮暗双主题均通过 CVD 校验；refetch 时保留旧帧降透明度
- 页面不内嵌任何数据，静态壳本身无认证也无可泄露信息

## 7. 配置项

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `data_dir` | string | `./token-usage-stats-data` | 聚合文件目录；相对路径基于 CPA 工作目录 |
| `retention_days` | integer | `30` | 保留天数，clamp 到 1–365 |

（`enabled`/`priority` 由 host 消费，插件不解析。）

- 配置解析在 `plugin.register` / `plugin.reconfigure` 收到 `config_yaml` 时进行
- reconfigure 时 `data_dir` 变更：以新目录重新加载（内存中已有数据保留并继续按新配置 flush）；`retention_days` 变更：立即按新值清理
- 未知字段忽略；YAML 解析失败 → 错误信封 + 保留旧配置

## 8. 并发与错误处理

- 聚合 map + dirty 集由 `sync.RWMutex` 保护（usage 推送串行，但管理查询/flush goroutine 并发读）
- flush goroutine 用 `time.Ticker` + stop channel；shutdown 时先停 ticker 再最终 flush
- 单个文件读写失败不影响其他天；所有错误路径仅 stderr 日志，不 panic
- 管理端读取：优先内存（含当天未 flush 数据）；范围内早于内存最早天的日期不补读磁盘（启动已加载全部保留天数，正常不存在缺口）

## 9. 项目结构与构建

```
token-usage-stats/
├── go.mod              # module token-usage-stats；go 1.26；零依赖
├── main.go             # C ABI 导出 + method 分发 + 信封编解码
├── plugin.go           # register/reconfigure、配置解析、生命周期（init/flush/shutdown）
├── aggregator.go       # 内存聚合 + provider 口径换算 + 命中率
├── store.go            # 按天 JSON 读写、原子写、滚动清理
├── api.go              # /usage-stats JSON handler
├── page.go             # 内嵌 HTML 页面常量
├── aggregator_test.go
├── store_test.go
├── api_test.go
├── plugin_test.go
├── Makefile            # build / test / install（拷入 CPA plugins 目录）
└── README.md           # 中文使用说明（构建、部署、配置、接口示例）
```

Makefile 目标：

- `make build`：按当前平台 `go build -buildmode=c-shared -o bin/token-usage-stats.<ext> .`（darwin→dylib，linux→so，windows→dll）
- `make test`：`go test ./...`
- `make install CPA_PLUGINS_DIR=../CLIProxyAPI/plugins`：构建并拷贝

## 10. 测试策略

`go test` 直接运行（纯 Go 测试，无需构建动态库）：

- **aggregator**：多条记录累加；本地时区跨天归属（显式构造 RequestedAt）；四类 provider 口径换算（claude / openai / gemini / unknown）；reasoning 叠加规则；负值 clamp；model 回退链
- **store**：写读往返；原子写不留 tmp；滚动清理删除过期文件、保留边界当天；损坏 JSON 跳过不中断（日期显式传参，不用 `time.Sleep`，遵守 CPA 仓库测试时钟约定）
- **api**：query 解析（缺省/非法/from>to）；范围裁剪；排序（day 升序、models 按 total 降序）；空数据响应；命中率 null
- **plugin**：register/reconfigure 配置解析（默认、自定义、非法 YAML、retention clamp）；method 分发未知方法错误信封
- **页面冒烟**：HTML 含 `<table`、fetch URL、`Bearer` 关键字

手动验证清单：

1. `make build` 产出 `bin/token-usage-stats.dylib`
2. 拷入 CPA `plugins/darwin/arm64/`，config.yaml 启用插件
3. 启动 CPA，经代理发若干请求（至少覆盖 claude / openai / gemini 各一）
4. `curl -H "Authorization: Bearer <key>" localhost:8317/v0/management/usage-stats` 验证聚合数字与口径
5. 浏览器打开 `/v0/resource/plugins/token-usage-stats/stats`，输密码看表格
6. 重启 CPA 后数据仍在；构造 31 天前文件验证被清理

## 11. 风险与备注

- **跨平台编译**：c-shared 需目标平台工具链；用户主环境 macOS（darwin/arm64），Linux 部署需在对应平台构建或用交叉工具链
- **CPA 版本耦合**：插件只依赖 C ABI JSON 契约（schema_version 6），不 import CPA 代码；CPA 升级若保持 ABI 兼容则无需重编译
- **页面认证依赖 host 管理中间件**：若 CPA 未设置管理密码（`MANAGEMENT_PASSWORD` 等），JSON API 行为以 host 默认策略为准（页面会如实展示 host 返回的状态）
- **时区**：跨天时区以 CPA 运行机器本地时区为准，文档中明示
