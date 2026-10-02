# token-usage-stats

[CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 的用量统计插件：按「天 × 模型」聚合 token 用量（输入、缓存命中率、输出），本地 JSON 文件持久化最近 30 天数据。

## 功能

- 每条请求完成后自动聚合：请求数、输入 tokens（含缓存口径换算）、缓存读取/写入、缓存命中率、输出 tokens（含推理）
- 每天一个 JSON 文件（`./token-usage-stats-data/YYYY-MM-DD.json`），保留最近 30 天（可配置 1-365），重启不丢
- 认证 JSON API：`GET /v0/management/usage-stats`
- 浏览器页面：`/v0/resource/plugins/token-usage-stats/stats`

## 构建

需要 Go ≥ 1.23 与支持 CGO 的 C 工具链（macOS: Xcode CLT）。

```bash
make build        # 产出 bin/token-usage-stats.dylib（macOS）/ .so（Linux）
make test         # 运行单元测试
```

跨平台部署时需在目标平台重新构建。

## 部署

1. 把动态库拷入 CPA 的插件目录（CPA 会扫描 `<plugins.dir>/<goos>/<goarch>/` 与 `<plugins.dir>/`）：

   ```bash
   make install CPA_PLUGINS_DIR=/path/to/CLIProxyAPI/plugins
   ```

2. 在 CPA 的 `config.yaml` 中启用：

   ```yaml
   plugins:
     enabled: true
     configs:
       token-usage-stats:
         enabled: true
         data_dir: ./token-usage-stats-data   # 可选，默认此值
         retention_days: 30                    # 可选，默认 30（1-365）
   ```

3. 重启 CPA（或触发热重载）。

## 查看数据

### 浏览器页面

打开 `http://<cpa-host>:<port>/v0/resource/plugins/token-usage-stats/stats`，输入管理密码（Management Key）即可查看表格。密码仅保存在浏览器 localStorage。

### JSON API

```bash
curl -H "Authorization: Bearer <management-key>" \
  "http://localhost:8317/v0/management/usage-stats?from=2026-09-03&to=2026-10-02"
```

响应示例：

```json
{
  "days": [
    {
      "day": "2026-10-02",
      "models": [
        {
          "model": "gpt-5",
          "requests": 12,
          "failed_requests": 0,
          "input_tokens": 120000,
          "cache_read_tokens": 80000,
          "cache_creation_tokens": 0,
          "cache_hit_rate": 0.6667,
          "output_tokens": 8000,
          "reasoning_tokens": 2000,
          "total_tokens": 128000
        }
      ],
      "totals": { "requests": 12, "...": "..." }
    }
  ],
  "totals": { "requests": 12, "...": "..." },
  "generated_at": "2026-10-02T15:04:05+08:00"
}
```

## 口径说明

- **天**：按 CPA 运行机器的本地时区划分。
- **模型**：取请求的实际模型（`Model` → `ResponseModel` → `Alias` → `unknown`）。
- **输入 tokens（总输入）**：不同 provider 的原始 `input_tokens` 语义不同，插件统一换算——Claude 系为 `input + cache_read + cache_creation`；OpenAI/Gemini 系的 `input_tokens` 本身已含缓存，直接使用。
- **缓存命中率** = `cache_read_tokens / 输入 tokens`（输入为 0 时为 `null`）。
- **输出 tokens**：Gemini 系的推理 tokens 单独上报，已并入输出总量。

## 开发

纯 Go 标准库，无第三方依赖。结构：

- `main.go` — C ABI 入口、信封编解码、method 分发
- `config.go` — 配置解析（flat-YAML 子集）
- `register.go` — 注册与生命周期
- `aggregator.go` — 内存聚合与 flush 生命周期
- `store.go` — 按天 JSON 持久化
- `api.go` — Management JSON API
- `page.go` — 内嵌浏览器页面
