# token-usage-stats

[![CI](https://github.com/zpvan/token-usage-stats/actions/workflows/ci.yml/badge.svg)](https://github.com/zpvan/token-usage-stats/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/zpvan/token-usage-stats)](https://github.com/zpvan/token-usage-stats/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/zpvan/token-usage-stats)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) plugin that aggregates per-day, per-model LLM token usage — input, cache hit rate, output and TPS (tokens/sec) — persists it to local JSON files with a 30-day retention, and serves an authenticated JSON API plus a histogram dashboard.

[中文文档](README_CN.md)

![dashboard](docs/assets/dashboard.png)

## ⚠️ Compatibility notice (read first)

**Official CLIProxyAPI release binaries currently crash when loading any self-compiled Go plugin** — reproducible on v7.2.151 and v8.0.11 (`fatal error: unknown caller pc` / SIGSEGV inside the plugin's `dlopen`ed init, a cross-runtime cgo ABI issue between the CI-built host and the plugin runtime).

Until this is fixed upstream, you must run this plugin with a **CPA core built from source on your own machine**, matching your platform:

```bash
git clone https://github.com/router-for-me/CLIProxyAPI.git
cd CLIProxyAPI
go build -o cli-proxy-api ./cmd/server
```

Use this binary in place of the release binary. A corresponding upstream issue is being prepared; this notice will be removed once plugin loading works on release binaries.

## Features

- **Per-day × per-model aggregation**: requests, input tokens (with cross-provider cache normalization), cache read/creation, cache hit rate, output tokens (incl. reasoning), **TPS** (output tokens per second)
- **30-day retention**: one JSON file per day (`YYYY-MM-DD.json`), atomic writes, rolling cleanup, survives restarts
- **Authenticated JSON API**: `GET /v0/management/usage-stats` (uses the existing management auth)
- **Histogram dashboard**: daily stacked columns per model + per-model ranking bars + detail table, light/dark themes, no external JS dependencies
- **Zero third-party dependencies**: pure Go standard library, single C ABI shared library

## Installation

### From release (recommended)

Download the artifact for your platform from [Releases](https://github.com/zpvan/token-usage-stats/releases):

| Platform | Artifact |
|---|---|
| macOS Apple Silicon | `token-usage-stats-vX.Y.Z-darwin-arm64.dylib` |
| macOS Intel | `token-usage-stats-vX.Y.Z-darwin-amd64.dylib` |
| Linux x86_64 | `token-usage-stats-vX.Y.Z-linux-amd64.so` |
| Linux arm64 | `token-usage-stats-vX.Y.Z-linux-arm64.so` |

Copy it into the CPA plugins directory — either the arch-specific subdirectory (recommended) or the plugins root:

```bash
mkdir -p /path/to/cpa/plugins/darwin/arm64
cp token-usage-stats-vX.Y.Z-darwin-arm64.dylib /path/to/cpa/plugins/darwin/arm64/
```

### Build from source

Requires Go ≥ 1.23 and a C toolchain (macOS: Xcode CLT).

```bash
make build        # outputs bin/token-usage-stats.dylib (.so on Linux)
make test
make install CPA_PLUGINS_DIR=/path/to/cpa/plugins
```

Cross-platform deployment needs a build on the target platform/architecture.

## Configuration

Enable the plugin in CPA's `config.yaml`:

```yaml
plugins:
  enabled: true
  configs:
    token-usage-stats:
      enabled: true
      data_dir: ./token-usage-stats-data   # optional, default shown
      retention_days: 30                    # optional, 1-365
```

Restart CPA (or trigger a config hot reload).

## Usage

### Dashboard

Open `http://<cpa-host>:<port>/v0/resource/plugins/token-usage-stats/stats` and enter your management key (stored in the browser's localStorage only).

### JSON API

```bash
curl -H "Authorization: Bearer <management-key>" \
  "http://localhost:8317/v0/management/usage-stats?from=2026-09-03&to=2026-10-02"
```

<details><summary>Example response</summary>

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
          "total_tokens": 128000,
          "decode_tokens": 8000,
          "decode_ms": 240000,
          "tps": 33.3
        }
      ],
      "totals": { "requests": 12, "...": "..." }
    }
  ],
  "totals": { "requests": 12, "...": "..." },
  "generated_at": "2026-10-02T15:04:05+08:00"
}
```

</details>

## Metrics semantics

- **Day**: boundaries follow the CPA host's local timezone.
- **Model**: the effective request model (`Model` → `ResponseModel` → `Alias` → `unknown`).
- **Input tokens**: providers report input differently; the plugin normalizes — Claude-family: `input + cache_read + cache_creation`; OpenAI/Gemini-family already include cache tokens in `input_tokens`.
- **Cache hit rate** = `cache_read_tokens / input_tokens` (`null` when input is 0).
- **TPS** = output tokens ÷ generation time. Generation time is `Latency − TTFT` for streaming requests (true decode speed) and full `Latency` otherwise. Only successful requests with output count; `null` when no samples exist.

## Development

Pure Go standard library, no third-party dependencies.

- `main.go` — C ABI entrypoint, envelope codec, method dispatch
- `config.go` — flat-YAML-subset config parser
- `register.go` — registration & lifecycle
- `aggregator.go` — in-memory aggregation, provider token semantics, flush lifecycle
- `store.go` — per-day JSON persistence
- `api.go` — management JSON API
- `page.go` — embedded dashboard

CI runs `gofmt`, `go vet`, and `go test -race` on macOS and Linux; tagged releases build the four platform artifacts automatically.

## License

[MIT](LICENSE)
