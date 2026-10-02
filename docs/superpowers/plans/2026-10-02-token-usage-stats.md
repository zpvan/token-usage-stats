# token-usage-stats 插件实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 CLIProxyAPI 的 C ABI 插件，按「天 × 模型」聚合 token 用量（输入/缓存命中率/输出），JSON 文件持久化保留 30 天，提供认证 JSON API 与浏览器页面。

**Architecture:** Go `c-shared` 动态库插件，声明 `usage_plugin` + `management_api` 能力。host 推送每条 `UsageRecord` → 内存聚合（带版本号脏追踪）→ 每 30s + shutdown 原子写按天 JSON 文件 → 管理路由返回聚合 JSON / 静态 HTML 页。纯标准库，零第三方依赖。

**Tech Stack:** Go（cgo c-shared）、encoding/json、标准库-only、CPA C ABI（schema_version 6）

**Spec:** `docs/superpowers/specs/2026-10-02-token-usage-stats-design.md`（同仓库）

**Working directory:** `/Users/knox/Documents/GitWorkSpace/token-usage-stats`（已 git init，spec 已提交）

**环境前置：** 本机当前**没有 Go 工具链**（Task 1 第一步处理）；clang/Xcode CLT 已就绪（Apple clang 21，arm64）。

**文件职责一览：**

| 文件 | 职责 |
|---|---|
| `main.go` | C ABI 导出（init/call/free/shutdown）、JSON 信封编解码、method 分发、`usage.handle` |
| `config.go` | `pluginConfig`、flat-YAML 迷你解析器、全局配置状态 |
| `register.go` | `plugin.register`/`plugin.reconfigure`：注册载荷、`ConfigFields`、`applyConfig` |
| `aggregator.go` | provider 口径换算、`UsageRecord`/`ModelStats`/`DayData` 类型、内存聚合、flush 生命周期 |
| `store.go` | 按天 JSON 文件读写（原子写）、滚动清理（纯文件操作，无状态） |
| `api.go` | `management.register`、`management.handle` 路由、`/usage-stats` JSON 报告 |
| `page.go` | 内嵌 HTML 页面常量 |

---

### Task 1: 项目骨架与 C ABI 入口

**Files:**
- Create: `go.mod`
- Create: `main.go`
- Create: `main_test.go`
- Create: `.gitignore`

- [ ] **Step 1: 安装 Go 工具链**

本机没有 Go。用 Homebrew 安装（会修改系统环境，如需密码会提示）：

```bash
brew install go
go version
```

Expected: `go version go1.26.x darwin/arm64`（或更新；go.mod 用 `go 1.23` 指令，≥1.23 的工具链均可）

- [ ] **Step 2: 写失败测试**

`main_test.go`:

```go
package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHandleMethodUnknown(t *testing.T) {
	raw, err := handleMethod("no.such.method", nil)
	if err != nil {
		t.Fatalf("handleMethod returned error: %v", err)
	}
	var env envelope
	if errUnmarshal := json.Unmarshal(raw, &env); errUnmarshal != nil {
		t.Fatalf("response is not a valid envelope: %v", errUnmarshal)
	}
	if env.OK {
		t.Fatal("expected ok=false for unknown method")
	}
	if env.Error == nil || env.Error.Code != "unknown_method" {
		t.Fatalf("expected unknown_method error, got %+v", env.Error)
	}
	if !strings.Contains(env.Error.Message, "no.such.method") {
		t.Fatalf("error message should mention the method, got %q", env.Error.Message)
	}
}

func TestOkEnvelopeRoundTrip(t *testing.T) {
	raw, err := okEnvelope(struct{ Ping string }{Ping: "pong"})
	if err != nil {
		t.Fatalf("okEnvelope: %v", err)
	}
	var env envelope
	if errUnmarshal := json.Unmarshal(raw, &env); errUnmarshal != nil {
		t.Fatalf("unmarshal: %v", errUnmarshal)
	}
	if !env.OK || env.Error != nil {
		t.Fatalf("expected ok envelope, got %+v", env)
	}
	var result struct{ Ping string }
	if errUnmarshal := json.Unmarshal(env.Result, &result); errUnmarshal != nil {
		t.Fatalf("unmarshal result: %v", errUnmarshal)
	}
	if result.Ping != "pong" {
		t.Fatalf("Ping = %q, want pong", result.Ping)
	}
}
```

- [ ] **Step 3: 运行测试确认失败**

```bash
cd /Users/knox/Documents/GitWorkSpace/token-usage-stats
go mod init token-usage-stats 2>/dev/null; go test ./...
```

Expected: FAIL — `undefined: handleMethod` / `undefined: envelope` / `undefined: okEnvelope`（编译错误即失败）

- [ ] **Step 4: 写 go.mod、.gitignore 与 main.go 最小实现**

`go.mod`:

```
module token-usage-stats

go 1.23
```

`.gitignore`:

```
bin/
token-usage-stats-data/
*.test
```

`main.go`（C ABI 样板参照 CPA `examples/plugin/usage/go/main.go`，本插件不用 host 回调故精简）:

```go
package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);
*/
import "C"

import (
	"encoding/json"
	"unsafe"
)

// abiVersion is the C ABI contract version between host and plugin.
const abiVersion uint32 = 1

// schemaVersion is the plugin RPC payload contract version.
const schemaVersion uint32 = 6

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	_ = host // this plugin makes no host callbacks
	plugin.abi_version = C.uint32_t(abiVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, errorEnvelope("invalid_method", "method is required"))
		return 1
	}
	var reqRaw []byte
	if request != nil && requestLen > 0 {
		reqRaw = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	raw, errHandle := handleMethod(C.GoString(method), reqRaw)
	if errHandle != nil {
		writeResponse(response, errorEnvelope("plugin_error", errHandle.Error()))
		return 1
	}
	writeResponse(response, raw)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, length C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
	_ = length
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {
	// Wired to the aggregator in a later task.
}

func handleMethod(method string, request []byte) ([]byte, error) {
	_ = request
	switch method {
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func okEnvelope(result any) ([]byte, error) {
	raw, errMarshal := json.Marshal(result)
	if errMarshal != nil {
		return nil, errMarshal
	}
	return json.Marshal(envelope{OK: true, Result: json.RawMessage(raw)})
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	return raw
}

func writeResponse(response *C.cliproxy_buffer, raw []byte) {
	if response == nil || len(raw) == 0 {
		return
	}
	ptr := C.CBytes(raw)
	if ptr == nil {
		return
	}
	response.ptr = ptr
	response.len = C.size_t(len(raw))
}
```

- [ ] **Step 5: 运行测试确认通过，并验证 c-shared 可编译**

```bash
go test ./...
mkdir -p bin && go build -buildmode=c-shared -o bin/token-usage-stats.dylib . && ls -la bin/ && rm -f bin/token-usage-stats.h
```

Expected: 测试 PASS；`bin/token-usage-stats.dylib` 生成（`-buildmode=c-shared` 可用证明 CGO 链路正常）

- [ ] **Step 6: Commit**

```bash
git add go.mod main.go main_test.go .gitignore
git commit -m "feat: project skeleton with C ABI entrypoint and envelope codec

Co-Authored-By: Claude Code <noreply@anthropic.com>"
```

---

### Task 2: 配置解析（flat-YAML 迷你解析器 + pluginConfig）

插件从 `plugin.register`/`plugin.reconfigure` 收到 `config_yaml`（本插件配置子树的 YAML 字节）。为保持零依赖，解析 flat 标量映射子集（`data_dir`、`retention_days` 均为平铺标量），拒绝嵌套/序列。

**Files:**
- Create: `config.go`
- Create: `config_test.go`

- [ ] **Step 1: 写失败测试**

`config_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestParseFlatYAMLBasic(t *testing.T) {
	raw := []byte("enabled: true\npriority: 1\ndata_dir: ./my-data\nretention_days: 45\n")
	fields, err := parseFlatYAML(raw)
	if err != nil {
		t.Fatalf("parseFlatYAML: %v", err)
	}
	if fields["data_dir"] != "./my-data" {
		t.Fatalf("data_dir = %q", fields["data_dir"])
	}
	if fields["retention_days"] != "45" {
		t.Fatalf("retention_days = %q", fields["retention_days"])
	}
	if fields["enabled"] != "true" {
		t.Fatalf("enabled = %q", fields["enabled"])
	}
}

func TestParseFlatYAMLCommentsAndQuotes(t *testing.T) {
	raw := []byte("# leading comment\ndata_dir: \"./with # hash\"  # trailing comment\nretention_days: '30'\n\n")
	fields, err := parseFlatYAML(raw)
	if err != nil {
		t.Fatalf("parseFlatYAML: %v", err)
	}
	if fields["data_dir"] != "./with # hash" {
		t.Fatalf("data_dir = %q, want ./with # hash", fields["data_dir"])
	}
	if fields["retention_days"] != "30" {
		t.Fatalf("retention_days = %q", fields["retention_days"])
	}
}

func TestParseFlatYAMLUnquotedTrailingComment(t *testing.T) {
	fields, err := parseFlatYAML([]byte("data_dir: ./x # note\n"))
	if err != nil {
		t.Fatalf("parseFlatYAML: %v", err)
	}
	if fields["data_dir"] != "./x" {
		t.Fatalf("data_dir = %q, want ./x", fields["data_dir"])
	}
}

func TestParseFlatYAMLSingleQuoteEscape(t *testing.T) {
	fields, err := parseFlatYAML([]byte("data_dir: 'it''s'\n"))
	if err != nil {
		t.Fatalf("parseFlatYAML: %v", err)
	}
	if fields["data_dir"] != "it's" {
		t.Fatalf("data_dir = %q, want it's", fields["data_dir"])
	}
}

func TestParseFlatYAMLRejectsNested(t *testing.T) {
	_, err := parseFlatYAML([]byte("outer:\n  inner: 1\n"))
	if err == nil || !strings.Contains(err.Error(), "line") {
		t.Fatalf("expected nested rejection error, got %v", err)
	}
	if _, errIndented := parseFlatYAML([]byte("  data_dir: ./x\n")); errIndented == nil {
		t.Fatal("expected indented line rejection")
	}
}

func TestParseFlatYAMLRejectsSequence(t *testing.T) {
	if _, err := parseFlatYAML([]byte("- item\n")); err == nil {
		t.Fatal("expected sequence rejection")
	}
}

func TestParseFlatYAMLRejectsMissingColon(t *testing.T) {
	if _, err := parseFlatYAML([]byte("garbage line\n")); err == nil {
		t.Fatal("expected missing-colon error")
	}
}

func TestParseConfigYAMLDefaults(t *testing.T) {
	cfg, err := parseConfigYAML(nil)
	if err != nil {
		t.Fatalf("parseConfigYAML: %v", err)
	}
	if cfg.DataDir != "./token-usage-stats-data" || cfg.RetentionDays != 30 {
		t.Fatalf("defaults = %+v", cfg)
	}
}

func TestParseConfigYAMLOverride(t *testing.T) {
	cfg, err := parseConfigYAML([]byte("data_dir: /var/lib/stats\nretention_days: 60\nunknown_field: x\n"))
	if err != nil {
		t.Fatalf("parseConfigYAML: %v", err)
	}
	if cfg.DataDir != "/var/lib/stats" {
		t.Fatalf("DataDir = %q", cfg.DataDir)
	}
	if cfg.RetentionDays != 60 {
		t.Fatalf("RetentionDays = %d", cfg.RetentionDays)
	}
}

func TestParseConfigYAMLClampsRetention(t *testing.T) {
	low, err := parseConfigYAML([]byte("retention_days: 0\n"))
	if err != nil || low.RetentionDays != 1 {
		t.Fatalf("low clamp = %+v, %v", low, err)
	}
	high, err := parseConfigYAML([]byte("retention_days: 9999\n"))
	if err != nil || high.RetentionDays != 365 {
		t.Fatalf("high clamp = %+v, %v", high, err)
	}
}

func TestParseConfigYAMLInvalidRetention(t *testing.T) {
	if _, err := parseConfigYAML([]byte("retention_days: abc\n")); err == nil {
		t.Fatal("expected invalid retention_days error")
	}
}

func TestParseConfigYAMLBlankDataDirKeepsDefault(t *testing.T) {
	cfg, err := parseConfigYAML([]byte("data_dir: \"\"\n"))
	if err != nil {
		t.Fatalf("parseConfigYAML: %v", err)
	}
	if cfg.DataDir != "./token-usage-stats-data" {
		t.Fatalf("DataDir = %q, want default", cfg.DataDir)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./... -run 'TestParse' -v
```

Expected: FAIL — `undefined: parseFlatYAML` / `undefined: parseConfigYAML`

- [ ] **Step 3: 实现 config.go**

`config.go`:

```go
package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
)

const (
	defaultDataDir       = "./token-usage-stats-data"
	defaultRetentionDays = 30
	minRetentionDays     = 1
	maxRetentionDays     = 365
)

// stderrLog is the plugin's only logging channel (host process stderr).
var stderrLog = log.New(os.Stderr, "token-usage-stats: ", log.LstdFlags)

// pluginConfig holds the parsed plugin configuration.
type pluginConfig struct {
	DataDir       string
	RetentionDays int
}

func defaultConfig() pluginConfig {
	return pluginConfig{DataDir: defaultDataDir, RetentionDays: defaultRetentionDays}
}

var (
	stateMu       sync.RWMutex
	currentConfig = defaultConfig()
)

func getConfig() pluginConfig {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return currentConfig
}

func setConfig(cfg pluginConfig) {
	stateMu.Lock()
	currentConfig = cfg
	stateMu.Unlock()
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

// parseConfigYAML parses the plugin configuration subtree. Unknown fields
// (enabled, priority, future fields) are ignored.
func parseConfigYAML(raw []byte) (pluginConfig, error) {
	cfg := defaultConfig()
	if len(strings.TrimSpace(string(raw))) == 0 {
		return cfg, nil
	}
	fields, errParse := parseFlatYAML(raw)
	if errParse != nil {
		return cfg, errParse
	}
	for key, value := range fields {
		switch strings.ToLower(key) {
		case "data_dir":
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				cfg.DataDir = trimmed
			}
		case "retention_days":
			n, errAtoi := strconv.Atoi(strings.TrimSpace(value))
			if errAtoi != nil {
				return cfg, fmt.Errorf("invalid retention_days %q: %w", value, errAtoi)
			}
			cfg.RetentionDays = clampInt(n, minRetentionDays, maxRetentionDays)
		default:
			// ignore unknown fields
		}
	}
	return cfg, nil
}

// parseFlatYAML parses a flat YAML mapping of scalar values (the subset used
// for plugin configuration). Nested mappings, sequences, and indented lines
// are rejected with a descriptive error.
func parseFlatYAML(raw []byte) (map[string]string, error) {
	out := make(map[string]string)
	lines := strings.Split(string(raw), "\n")
	for idx, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if line != strings.TrimLeft(line, " \t") {
			return nil, fmt.Errorf("line %d: indented content is not supported: %q", idx+1, line)
		}
		if strings.HasPrefix(trimmed, "-") {
			return nil, fmt.Errorf("line %d: sequences are not supported: %q", idx+1, line)
		}
		key, value, found := strings.Cut(trimmed, ":")
		if !found {
			return nil, fmt.Errorf("line %d: expected key: value: %q", idx+1, line)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", idx+1)
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("line %d: empty value for key %q (nested mappings are not supported)", idx+1, key)
		}
		unquoted, errUnquote := unquoteYAMLScalar(value)
		if errUnquote != nil {
			return nil, fmt.Errorf("line %d: %w", idx+1, errUnquote)
		}
		out[key] = unquoted
	}
	return out, nil
}

// unquoteYAMLScalar decodes one scalar token: double-quoted (with escapes),
// single-quoted ('' escape), or plain (trailing " #" comment stripped).
func unquoteYAMLScalar(value string) (string, error) {
	switch value[0] {
	case '"':
		return scanDoubleQuoted(value)
	case '\'':
		return scanSingleQuoted(value)
	default:
		if idx := strings.Index(value, " #"); idx >= 0 {
			value = strings.TrimSpace(value[:idx])
		}
		return value, nil
	}
}

func scanDoubleQuoted(value string) (string, error) {
	var b strings.Builder
	for i := 1; i < len(value); i++ {
		ch := value[i]
		if ch == '\\' && i+1 < len(value) {
			i++
			switch value[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			case '0':
				b.WriteByte(0)
			default:
				b.WriteByte(value[i])
			}
			continue
		}
		if ch == '"' {
			return b.String(), nil
		}
		b.WriteByte(ch)
	}
	return "", fmt.Errorf("unterminated double-quoted scalar: %q", value)
}

func scanSingleQuoted(value string) (string, error) {
	var b strings.Builder
	for i := 1; i < len(value); i++ {
		ch := value[i]
		if ch == '\'' {
			if i+1 < len(value) && value[i+1] == '\'' {
				b.WriteByte('\'')
				i++
				continue
			}
			return b.String(), nil
		}
		b.WriteByte(ch)
	}
	return "", fmt.Errorf("unterminated single-quoted scalar: %q", value)
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./... -run 'TestParse' -v
```

Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add config.go config_test.go
git commit -m "feat: flat-YAML config parser and plugin configuration state

Co-Authored-By: Claude Code <noreply@anthropic.com>"
```

---

### Task 3: 注册与生命周期（plugin.register / plugin.reconfigure）

**Files:**
- Create: `register.go`
- Create: `register_test.go`
- Modify: `main.go`（`handleMethod` 增加 register 分支）

- [ ] **Step 1: 写失败测试**

`register_test.go`:

```go
package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// resetConfigForTest restores the default global config after the test.
func resetConfigForTest(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { setConfig(defaultConfig()) })
}

func callEnvelope(t *testing.T, method string, request []byte) envelope {
	t.Helper()
	raw, err := handleMethod(method, request)
	if err != nil {
		t.Fatalf("handleMethod(%s): %v", method, err)
	}
	var env envelope
	if errUnmarshal := json.Unmarshal(raw, &env); errUnmarshal != nil {
		t.Fatalf("invalid envelope for %s: %v", method, errUnmarshal)
	}
	return env
}

func TestHandleRegisterDefaults(t *testing.T) {
	resetConfigForTest(t)
	env := callEnvelope(t, "plugin.register", nil)
	if !env.OK {
		t.Fatalf("expected ok envelope, got %+v", env.Error)
	}
	var reg struct {
		SchemaVersion uint32 `json:"schema_version"`
		Metadata      struct {
			Name         string `json:"Name"`
			Version      string `json:"Version"`
			ConfigFields []struct {
				Name string `json:"Name"`
				Type string `json:"Type"`
			} `json:"ConfigFields"`
		} `json:"metadata"`
		Capabilities map[string]bool `json:"capabilities"`
	}
	if errUnmarshal := json.Unmarshal(env.Result, &reg); errUnmarshal != nil {
		t.Fatalf("unmarshal registration: %v", errUnmarshal)
	}
	if reg.SchemaVersion != 6 {
		t.Fatalf("schema_version = %d, want 6", reg.SchemaVersion)
	}
	if reg.Metadata.Name != "token-usage-stats" {
		t.Fatalf("Name = %q", reg.Metadata.Name)
	}
	if !reg.Capabilities["usage_plugin"] || !reg.Capabilities["management_api"] {
		t.Fatalf("capabilities = %v", reg.Capabilities)
	}
	if len(reg.Metadata.ConfigFields) != 2 {
		t.Fatalf("ConfigFields = %v", reg.Metadata.ConfigFields)
	}
	if got := getConfig(); got != defaultConfig() {
		t.Fatalf("currentConfig = %+v, want defaults", got)
	}
}

func TestHandleRegisterAppliesConfig(t *testing.T) {
	resetConfigForTest(t)
	request, errMarshal := json.Marshal(map[string]any{
		"config_yaml":    []byte("data_dir: /tmp/stats\nretention_days: 60\n"),
		"schema_version": 6,
	})
	if errMarshal != nil {
		t.Fatalf("marshal request: %v", errMarshal)
	}
	env := callEnvelope(t, "plugin.reconfigure", request)
	if !env.OK {
		t.Fatalf("expected ok envelope, got %+v", env.Error)
	}
	cfg := getConfig()
	if cfg.DataDir != "/tmp/stats" || cfg.RetentionDays != 60 {
		t.Fatalf("currentConfig = %+v", cfg)
	}
}

func TestHandleRegisterInvalidConfigKeepsOld(t *testing.T) {
	resetConfigForTest(t)
	setConfig(pluginConfig{DataDir: "/keep/me", RetentionDays: 15})
	request, errMarshal := json.Marshal(map[string]any{
		"config_yaml": []byte("nested:\n  inner: 1\n"),
	})
	if errMarshal != nil {
		t.Fatalf("marshal request: %v", errMarshal)
	}
	env := callEnvelope(t, "plugin.reconfigure", request)
	if env.OK {
		t.Fatal("expected error envelope for invalid config")
	}
	if env.Error == nil || env.Error.Code != "invalid_config" {
		t.Fatalf("expected invalid_config, got %+v", env.Error)
	}
	cfg := getConfig()
	if cfg.DataDir != "/keep/me" || cfg.RetentionDays != 15 {
		t.Fatalf("config changed on invalid input: %+v", cfg)
	}
}

func TestHandleRegisterInvalidJSON(t *testing.T) {
	resetConfigForTest(t)
	env := callEnvelope(t, "plugin.register", []byte("{not json"))
	if env.OK || env.Error == nil || env.Error.Code != "invalid_request" {
		t.Fatalf("expected invalid_request, got %+v", env)
	}
	if !strings.Contains(env.Error.Message, "decode") {
		t.Fatalf("message = %q", env.Error.Message)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./... -run 'TestHandleRegister' -v
```

Expected: FAIL — `undefined: handleRegister`（编译错误）

- [ ] **Step 3: 实现 register.go 并接线 handleMethod**

`register.go`:

```go
package main

import (
	"bytes"
	"encoding/json"
)

const (
	pluginName    = "token-usage-stats"
	pluginVersion = "0.1.0"
	pluginAuthor  = "zpvan"
)

// lifecycleRequest is the host payload for plugin.register/plugin.reconfigure.
type lifecycleRequest struct {
	ConfigYAML    []byte `json:"config_yaml"`
	SchemaVersion uint32 `json:"schema_version"`
}

type configField struct {
	Name        string `json:"Name"`
	Type        string `json:"Type"`
	Description string `json:"Description"`
}

type registrationMetadata struct {
	Name             string        `json:"Name"`
	Version          string        `json:"Version"`
	Author           string        `json:"Author"`
	GitHubRepository string        `json:"GitHubRepository"`
	Logo             string        `json:"Logo"`
	ConfigFields     []configField `json:"ConfigFields"`
}

type registration struct {
	SchemaVersion uint32              `json:"schema_version"`
	Metadata      registrationMetadata `json:"metadata"`
	Capabilities  map[string]bool     `json:"capabilities"`
}

func registrationPayload() registration {
	return registration{
		SchemaVersion: schemaVersion,
		Metadata: registrationMetadata{
			Name:    pluginName,
			Version: pluginVersion,
			Author:  pluginAuthor,
			ConfigFields: []configField{
				{Name: "data_dir", Type: "string", Description: "Directory for daily JSON aggregate files (default ./token-usage-stats-data)"},
				{Name: "retention_days", Type: "integer", Description: "Days of daily aggregates to retain (default 30, range 1-365)"},
			},
		},
		Capabilities: map[string]bool{
			"usage_plugin":   true,
			"management_api": true,
		},
	}
}

// applyConfig stores the new configuration. The aggregator is wired in here
// in a later task.
func applyConfig(cfg pluginConfig) {
	setConfig(cfg)
}

func handleRegister(request []byte) ([]byte, error) {
	var req lifecycleRequest
	if len(bytes.TrimSpace(request)) > 0 {
		if errUnmarshal := json.Unmarshal(request, &req); errUnmarshal != nil {
			return errorEnvelope("invalid_request", "failed to decode register request: "+errUnmarshal.Error()), nil
		}
	}
	cfg, errParse := parseConfigYAML(req.ConfigYAML)
	if errParse != nil {
		return errorEnvelope("invalid_config", errParse.Error()), nil
	}
	applyConfig(cfg)
	return okEnvelope(registrationPayload())
}
```

`main.go` 的 `handleMethod` 替换为:

```go
func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		return handleRegister(request)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./...
```

Expected: 全部 PASS（含 Task 1、2 的测试）

- [ ] **Step 5: Commit**

```bash
git add register.go register_test.go main.go
git commit -m "feat: plugin register/reconfigure lifecycle with config application

Co-Authored-By: Claude Code <noreply@anthropic.com>"
```

---

### Task 4: provider 口径换算与内存聚合

**Files:**
- Create: `aggregator.go`
- Create: `aggregator_test.go`

- [ ] **Step 1: 写失败测试**

`aggregator_test.go`:

```go
package main

import (
	"testing"
	"time"
)

func TestSemanticsFor(t *testing.T) {
	cases := []struct {
		provider, executor string
		want               tokenSemantics
	}{
		{"claude", "claudeexecutor", semanticsIndependent},
		{"anthropic", "", semanticsIndependent},
		{"openai", "", semanticsSubset},
		{"", "openaicompatexecutor", semanticsSubset},
		{"openai-compatibility", "", semanticsSubset},
		{"openai-compatible-foo", "", semanticsSubset},
		{"codex", "", semanticsSubset},
		{"kimi", "", semanticsSubset},
		{"openrouter", "", semanticsSubset},
		{"gemini", "", semanticsSeparateReasoning},
		{"aistudio", "", semanticsSeparateReasoning},
		{"vertex", "", semanticsSeparateReasoning},
		{"antigravity", "", semanticsSeparateReasoning},
		{"foo", "bar", semanticsUnknown},
		{"", "", semanticsUnknown},
		{"unknown", "unknown", semanticsUnknown},
	}
	for _, tc := range cases {
		if got := semanticsFor(tc.provider, tc.executor); got != tc.want {
			t.Errorf("semanticsFor(%q, %q) = %d, want %d", tc.provider, tc.executor, got, tc.want)
		}
	}
}

func newTestAggregator() *Aggregator {
	a := newAggregator()
	a.nowFunc = func() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.Local) }
	return a
}

func localTime(y int, m time.Month, d, hh int) time.Time {
	return time.Date(y, m, d, hh, 0, 0, 0, time.Local)
}

func statsOf(t *testing.T, a *Aggregator, day, model string) *ModelStats {
	t.Helper()
	a.mu.RLock()
	defer a.mu.RUnlock()
	dd := a.days[day]
	if dd == nil {
		t.Fatalf("no day %s", day)
	}
	ms := dd.Models[model]
	if ms == nil {
		t.Fatalf("no model %s on %s", model, day)
	}
	return ms
}

func TestAddOpenAISemantics(t *testing.T) {
	a := newTestAggregator()
	a.Add(UsageRecord{
		Provider: "openai", Model: "gpt-5", RequestedAt: localTime(2026, 10, 2, 10),
		Detail: UsageDetail{InputTokens: 100, CacheReadTokens: 40, OutputTokens: 50, ReasoningTokens: 10},
	})
	ms := statsOf(t, a, "2026-10-02", "gpt-5")
	if ms.Requests != 1 || ms.FailedRequests != 0 {
		t.Fatalf("requests = %+v", ms)
	}
	if ms.InputTokens != 100 { // subset: cache already inside input
		t.Fatalf("InputTokens = %d, want 100", ms.InputTokens)
	}
	if ms.CacheReadTokens != 40 || ms.OutputTokens != 50 || ms.ReasoningTokens != 10 {
		t.Fatalf("stats = %+v", ms)
	}
	if ms.TotalTokens != 150 {
		t.Fatalf("TotalTokens = %d, want 150", ms.TotalTokens)
	}
}

func TestAddClaudeSemantics(t *testing.T) {
	a := newTestAggregator()
	a.Add(UsageRecord{
		Provider: "claude", Model: "claude-opus-4-5", RequestedAt: localTime(2026, 10, 2, 10),
		Detail: UsageDetail{InputTokens: 100, CacheReadTokens: 40, CacheCreationTokens: 10, OutputTokens: 50},
	})
	ms := statsOf(t, a, "2026-10-02", "claude-opus-4-5")
	if ms.InputTokens != 150 { // independent: input + cache read + cache creation
		t.Fatalf("InputTokens = %d, want 150", ms.InputTokens)
	}
	if ms.TotalTokens != 200 {
		t.Fatalf("TotalTokens = %d, want 200", ms.TotalTokens)
	}
}

func TestAddGeminiSemantics(t *testing.T) {
	a := newTestAggregator()
	a.Add(UsageRecord{
		Provider: "gemini", Model: "gemini-3-pro", RequestedAt: localTime(2026, 10, 2, 10),
		Detail: UsageDetail{InputTokens: 100, CacheReadTokens: 40, OutputTokens: 50, ReasoningTokens: 20},
	})
	ms := statsOf(t, a, "2026-10-02", "gemini-3-pro")
	if ms.InputTokens != 100 {
		t.Fatalf("InputTokens = %d, want 100", ms.InputTokens)
	}
	if ms.OutputTokens != 70 { // separate reasoning: output + reasoning
		t.Fatalf("OutputTokens = %d, want 70", ms.OutputTokens)
	}
	if ms.TotalTokens != 170 {
		t.Fatalf("TotalTokens = %d, want 170", ms.TotalTokens)
	}
}

func TestAddAccumulatesAndCountsFailures(t *testing.T) {
	a := newTestAggregator()
	rec := UsageRecord{Provider: "openai", Model: "gpt-5", RequestedAt: localTime(2026, 10, 2, 10),
		Detail: UsageDetail{InputTokens: 100, OutputTokens: 50}}
	a.Add(rec)
	a.Add(rec)
	failed := rec
	failed.Failed = true
	a.Add(failed)
	ms := statsOf(t, a, "2026-10-02", "gpt-5")
	if ms.Requests != 3 || ms.FailedRequests != 1 {
		t.Fatalf("requests=%d failed=%d", ms.Requests, ms.FailedRequests)
	}
	if ms.InputTokens != 300 || ms.OutputTokens != 150 {
		t.Fatalf("stats = %+v", ms)
	}
}

func TestAddNegativeClampAndModelFallback(t *testing.T) {
	a := newTestAggregator()
	a.Add(UsageRecord{
		Provider: "openai", ResponseModel: "resp-model", Alias: "alias-model",
		RequestedAt: localTime(2026, 10, 2, 10),
		Detail:      UsageDetail{InputTokens: -5, CacheReadTokens: -1, OutputTokens: -2, ReasoningTokens: -3},
	})
	ms := statsOf(t, a, "2026-10-02", "resp-model")
	if ms.InputTokens != 0 || ms.OutputTokens != 0 || ms.TotalTokens != 0 {
		t.Fatalf("negative values not clamped: %+v", ms)
	}
	a.Add(UsageRecord{Provider: "openai", RequestedAt: localTime(2026, 10, 2, 10)})
	statsOf(t, a, "2026-10-02", "unknown")
}

func TestAddZeroRequestedAtUsesNowFunc(t *testing.T) {
	a := newTestAggregator()
	a.Add(UsageRecord{Provider: "openai", Model: "gpt-5", Detail: UsageDetail{InputTokens: 1}})
	statsOf(t, a, "2026-10-02", "gpt-5")
}

func TestSnapshotRangeSortAndDeepCopy(t *testing.T) {
	a := newTestAggregator()
	for _, day := range []string{"2026-10-02", "2026-10-01", "2026-09-30", "2026-10-05"} {
		parsed, errParse := time.ParseInLocation("2006-01-02", day, time.Local)
		if errParse != nil {
			t.Fatalf("parse %s: %v", day, errParse)
		}
		a.Add(UsageRecord{Provider: "openai", Model: "gpt-5", RequestedAt: parsed,
			Detail: UsageDetail{InputTokens: 1, OutputTokens: 1}})
	}
	days := a.Snapshot("2026-10-01", "2026-10-02")
	if len(days) != 2 || days[0].Day != "2026-10-01" || days[1].Day != "2026-10-02" {
		t.Fatalf("days = %+v", days)
	}
	// mutate the snapshot; the aggregate must not change
	days[0].Models["gpt-5"].Requests = 999
	if got := statsOf(t, a, "2026-10-01", "gpt-5").Requests; got != 1 {
		t.Fatalf("snapshot is not a deep copy, requests = %d", got)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./... -run 'TestSemantics|TestAdd|TestSnapshot' -v
```

Expected: FAIL — `undefined: semanticsFor` / `newAggregator` / `UsageRecord` 等

- [ ] **Step 3: 实现 aggregator.go（仅聚合部分，flush 在 Task 6）**

`aggregator.go`:

```go
package main

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// tokenSemantics classifies how a provider partitions token counters,
// mirroring tokenAccountingSemanticsFor in CPA sdk/cliproxy/usage/accounting.go.
type tokenSemantics int

const (
	semanticsUnknown tokenSemantics = iota
	semanticsSubset
	semanticsIndependent
	semanticsSeparateReasoning
)

// semanticsFor maps provider/executor identifiers to token accounting semantics.
func semanticsFor(provider, executorType string) tokenSemantics {
	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	normalizedExecutor := strings.ToLower(strings.TrimSpace(executorType))
	value := strings.TrimSpace(normalizedProvider + " " + normalizedExecutor)
	if value == "" || value == "unknown" || value == "unknown unknown" {
		return semanticsUnknown
	}
	if normalizedExecutor == "openaicompatexecutor" || normalizedProvider == "openai-compatibility" || strings.HasPrefix(normalizedProvider, "openai-compatible-") {
		return semanticsSubset
	}
	if strings.Contains(value, "claude") || strings.Contains(value, "anthropic") {
		return semanticsIndependent
	}
	for _, marker := range []string{"gemini", "aistudio", "antigravity", "vertex", "interaction"} {
		if strings.Contains(value, marker) {
			return semanticsSeparateReasoning
		}
	}
	for _, marker := range []string{"openai", "codex", "xai", "grok", "kimi", "qwen", "deepseek", "openrouter"} {
		if strings.Contains(value, marker) {
			return semanticsSubset
		}
	}
	return semanticsUnknown
}

// UsageDetail mirrors the token counters of pluginapi.UsageDetail.
type UsageDetail struct {
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
}

// UsageRecord mirrors the fields of pluginapi.UsageRecord consumed by this plugin.
type UsageRecord struct {
	Provider      string
	ExecutorType  string
	Model         string
	Alias         string
	ResponseModel string
	RequestedAt   time.Time
	Failed        bool
	Detail        UsageDetail
}

// ModelStats holds accumulated counters for one model on one day.
type ModelStats struct {
	Requests            int64 `json:"requests"`
	FailedRequests      int64 `json:"failed_requests"`
	InputTokens         int64 `json:"input_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	ReasoningTokens     int64 `json:"reasoning_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
}

// DayData is the per-day aggregate document persisted to disk.
type DayData struct {
	Day    string                 `json:"day"`
	Models map[string]*ModelStats `json:"models"`
}

func dayOf(t time.Time, nowFunc func() time.Time) string {
	if t.IsZero() {
		t = nowFunc()
	}
	return t.Local().Format("2006-01-02")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func nonNegative(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func copyDayData(dd *DayData) DayData {
	out := DayData{Day: dd.Day, Models: make(map[string]*ModelStats, len(dd.Models))}
	for model, stats := range dd.Models {
		copied := *stats
		out.Models[model] = &copied
	}
	return out
}

// agg is the process-wide aggregator instance (plugins run in-process).
var agg = newAggregator()

// Aggregator accumulates usage records in memory and flushes daily
// aggregates to disk. versions/flushed implement dirty tracking:
// a day is dirty when versions[day] > flushed[day].
type Aggregator struct {
	mu       sync.RWMutex
	days     map[string]*DayData
	versions map[string]int64
	flushed  map[string]int64
	cfg      pluginConfig
	nowFunc  func() time.Time

	flushMu     sync.Mutex
	stopFlush   chan struct{}
	flushDone   chan struct{}
	flushActive bool
}

func newAggregator() *Aggregator {
	return &Aggregator{
		days:     make(map[string]*DayData),
		versions: make(map[string]int64),
		flushed:  make(map[string]int64),
		cfg:      defaultConfig(),
		nowFunc:  time.Now,
	}
}

// Add folds one usage record into the in-memory aggregate.
func (a *Aggregator) Add(rec UsageRecord) {
	day := dayOf(rec.RequestedAt, a.nowFunc)
	model := firstNonEmpty(rec.Model, rec.ResponseModel, rec.Alias)
	if model == "" {
		model = "unknown"
	}
	semantics := semanticsFor(rec.Provider, rec.ExecutorType)

	cacheRead := nonNegative(rec.Detail.CacheReadTokens)
	cacheCreate := nonNegative(rec.Detail.CacheCreationTokens)
	reasoning := nonNegative(rec.Detail.ReasoningTokens)
	output := nonNegative(rec.Detail.OutputTokens)
	input := nonNegative(rec.Detail.InputTokens)

	totalInput := input
	if semantics == semanticsIndependent {
		totalInput = input + cacheRead + cacheCreate
	}
	totalOutput := output
	if semantics == semanticsSeparateReasoning {
		totalOutput = output + reasoning
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	dayData := a.days[day]
	if dayData == nil {
		dayData = &DayData{Day: day, Models: make(map[string]*ModelStats)}
		a.days[day] = dayData
	}
	stats := dayData.Models[model]
	if stats == nil {
		stats = &ModelStats{}
		dayData.Models[model] = stats
	}
	stats.Requests++
	if rec.Failed {
		stats.FailedRequests++
	}
	stats.InputTokens += totalInput
	stats.CacheReadTokens += cacheRead
	stats.CacheCreationTokens += cacheCreate
	stats.OutputTokens += totalOutput
	stats.ReasoningTokens += reasoning
	stats.TotalTokens += totalInput + totalOutput
	a.versions[day]++
}

// Snapshot returns deep copies of the aggregated days within [from, to]
// (inclusive, YYYY-MM-DD), sorted ascending by day.
func (a *Aggregator) Snapshot(from, to string) []DayData {
	a.mu.RLock()
	defer a.mu.RUnlock()
	days := make([]string, 0, len(a.days))
	for day := range a.days {
		if day >= from && day <= to {
			days = append(days, day)
		}
	}
	sort.Strings(days)
	out := make([]DayData, 0, len(days))
	for _, day := range days {
		out = append(out, copyDayData(a.days[day]))
	}
	return out
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./...
```

Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add aggregator.go aggregator_test.go
git commit -m "feat: in-memory aggregation with provider token semantics

Co-Authored-By: Claude Code <noreply@anthropic.com>"
```

---

### Task 5: 按天 JSON 存储

**Files:**
- Create: `store.go`
- Create: `store_test.go`

- [ ] **Step 1: 写失败测试**

`store_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMinRetentionDay(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.Local)
	if got := MinRetentionDay(now, 30); got != "2026-09-03" {
		t.Fatalf("MinRetentionDay(30) = %s, want 2026-09-03", got)
	}
	if got := MinRetentionDay(now, 1); got != "2026-10-02" {
		t.Fatalf("MinRetentionDay(1) = %s, want 2026-10-02", got)
	}
	if got := MinRetentionDay(now, 0); got != "2026-10-02" {
		t.Fatalf("MinRetentionDay(0) = %s, want 2026-10-02 (clamped)", got)
	}
}

func sampleDay(day string) DayData {
	return DayData{Day: day, Models: map[string]*ModelStats{
		"gpt-5": {Requests: 2, InputTokens: 100, CacheReadTokens: 40, OutputTokens: 50, TotalTokens: 150},
	}}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SaveDay(dir, sampleDay("2026-10-02")); err != nil {
		t.Fatalf("SaveDay: %v", err)
	}
	if _, errStat := os.Stat(filepath.Join(dir, "2026-10-02.json.tmp")); !os.IsNotExist(errStat) {
		t.Fatal("temp file left behind after atomic write")
	}
	loaded, err := LoadDays(dir, "2026-09-03")
	if err != nil {
		t.Fatalf("LoadDays: %v", err)
	}
	dd := loaded["2026-10-02"]
	if dd == nil {
		t.Fatal("day not loaded")
	}
	ms := dd.Models["gpt-5"]
	if ms == nil || ms.Requests != 2 || ms.InputTokens != 100 || ms.CacheReadTokens != 40 {
		t.Fatalf("loaded stats = %+v", ms)
	}
}

func TestSaveDayRejectsInvalidDay(t *testing.T) {
	dir := t.TempDir()
	if err := SaveDay(dir, sampleDay("2026-13-01")); err == nil {
		t.Fatal("expected error for invalid calendar day")
	}
	if err := SaveDay(dir, sampleDay("../evil")); err == nil {
		t.Fatal("expected error for path traversal day")
	}
}

func TestLoadDaysFiltersAndSkips(t *testing.T) {
	dir := t.TempDir()
	if err := SaveDay(dir, sampleDay("2026-10-02")); err != nil {
		t.Fatalf("SaveDay: %v", err)
	}
	if err := SaveDay(dir, sampleDay("2026-09-01")); err != nil {
		t.Fatalf("SaveDay: %v", err)
	}
	// corrupt file within retention: skipped, not fatal
	if errWrite := os.WriteFile(filepath.Join(dir, "2026-10-01.json"), []byte("{corrupt"), 0o644); errWrite != nil {
		t.Fatalf("write corrupt: %v", errWrite)
	}
	// non-matching files ignored
	if errWrite := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); errWrite != nil {
		t.Fatalf("write notes: %v", errWrite)
	}
	if errWrite := os.WriteFile(filepath.Join(dir, "2026-10-03.json.tmp"), []byte("{}"), 0o644); errWrite != nil {
		t.Fatalf("write tmp: %v", errWrite)
	}
	loaded, err := LoadDays(dir, "2026-09-03")
	if err != nil {
		t.Fatalf("LoadDays: %v", err)
	}
	if len(loaded) != 1 || loaded["2026-10-02"] == nil {
		t.Fatalf("loaded = %v", loaded)
	}
}

func TestLoadDaysMissingDir(t *testing.T) {
	loaded, err := LoadDays(filepath.Join(t.TempDir(), "missing"), "2026-09-03")
	if err != nil {
		t.Fatalf("LoadDays on missing dir: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded = %v, want empty", loaded)
	}
}

func TestCleanupDays(t *testing.T) {
	dir := t.TempDir()
	for _, day := range []string{"2026-09-01", "2026-09-02", "2026-10-01", "2026-10-02"} {
		if err := SaveDay(dir, sampleDay(day)); err != nil {
			t.Fatalf("SaveDay %s: %v", day, err)
		}
	}
	removed, err := CleanupDays(dir, "2026-10-01")
	if err != nil {
		t.Fatalf("CleanupDays: %v", err)
	}
	if len(removed) != 2 || removed[0] != "2026-09-01" || removed[1] != "2026-09-02" {
		t.Fatalf("removed = %v", removed)
	}
	for _, kept := range []string{"2026-10-01.json", "2026-10-02.json"} {
		if _, errStat := os.Stat(filepath.Join(dir, kept)); errStat != nil {
			t.Fatalf("kept file %s missing: %v", kept, errStat)
		}
	}
}

func TestCleanupDaysMissingDir(t *testing.T) {
	removed, err := CleanupDays(filepath.Join(t.TempDir(), "missing"), "2026-10-01")
	if err != nil || removed != nil {
		t.Fatalf("CleanupDays on missing dir = %v, %v", removed, err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./... -run 'TestMinRetention|TestSave|TestLoad|TestCleanup' -v
```

Expected: FAIL — `undefined: MinRetentionDay` / `SaveDay` / `LoadDays` / `CleanupDays`

- [ ] **Step 3: 实现 store.go**

`store.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var dayFilePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.json$`)

// MinRetentionDay returns the oldest retained day (YYYY-MM-DD) for the given
// retention window relative to now. retentionDays < 1 is clamped to 1.
func MinRetentionDay(now time.Time, retentionDays int) string {
	if retentionDays < 1 {
		retentionDays = 1
	}
	return now.Local().AddDate(0, 0, -(retentionDays - 1)).Format("2006-01-02")
}

func validDayFileName(name string) bool {
	if !dayFilePattern.MatchString(name) {
		return false
	}
	_, errParse := time.ParseInLocation("2006-01-02", name[:len(name)-len(".json")], time.Local)
	return errParse == nil
}

// SaveDay atomically writes one day's aggregate as <dir>/<day>.json via a
// temp file + rename.
func SaveDay(dir string, data DayData) error {
	if !validDayFileName(data.Day + ".json") {
		return fmt.Errorf("invalid day %q", data.Day)
	}
	if errMkdir := os.MkdirAll(dir, 0o755); errMkdir != nil {
		return fmt.Errorf("create data dir: %w", errMkdir)
	}
	raw, errMarshal := json.MarshalIndent(data, "", "  ")
	if errMarshal != nil {
		return fmt.Errorf("marshal day %s: %w", data.Day, errMarshal)
	}
	raw = append(raw, '\n')
	tmpPath := filepath.Join(dir, data.Day+".json.tmp")
	if errWrite := os.WriteFile(tmpPath, raw, 0o644); errWrite != nil {
		return fmt.Errorf("write temp file: %w", errWrite)
	}
	if errRename := os.Rename(tmpPath, filepath.Join(dir, data.Day+".json")); errRename != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", errRename)
	}
	return nil
}

// LoadDays reads every retained day file (day >= minDay) from dir. Corrupt or
// unreadable files are skipped with a warning; a missing directory yields an
// empty map without error.
func LoadDays(dir, minDay string) (map[string]*DayData, error) {
	entries, errRead := os.ReadDir(dir)
	if errRead != nil {
		if os.IsNotExist(errRead) {
			return map[string]*DayData{}, nil
		}
		return nil, fmt.Errorf("read data dir: %w", errRead)
	}
	out := make(map[string]*DayData)
	for _, entry := range entries {
		if entry == nil || !entry.Type().IsRegular() || !validDayFileName(entry.Name()) {
			continue
		}
		day := entry.Name()[:len(entry.Name())-len(".json")]
		if day < minDay {
			continue
		}
		raw, errReadFile := os.ReadFile(filepath.Join(dir, entry.Name()))
		if errReadFile != nil {
			stderrLog.Printf("skipping unreadable aggregate file %s: %v", entry.Name(), errReadFile)
			continue
		}
		var data DayData
		if errUnmarshal := json.Unmarshal(raw, &data); errUnmarshal != nil {
			stderrLog.Printf("skipping corrupt aggregate file %s: %v", entry.Name(), errUnmarshal)
			continue
		}
		data.Day = day
		if data.Models == nil {
			data.Models = make(map[string]*ModelStats)
		}
		out[day] = &data
	}
	return out, nil
}

// CleanupDays removes day files older than minDay and returns the removed
// day identifiers in ascending order. A missing directory is not an error.
func CleanupDays(dir, minDay string) ([]string, error) {
	entries, errRead := os.ReadDir(dir)
	if errRead != nil {
		if os.IsNotExist(errRead) {
			return nil, nil
		}
		return nil, fmt.Errorf("read data dir: %w", errRead)
	}
	removed := make([]string, 0)
	for _, entry := range entries {
		if entry == nil || !entry.Type().IsRegular() || !validDayFileName(entry.Name()) {
			continue
		}
		day := entry.Name()[:len(entry.Name())-len(".json")]
		if day >= minDay {
			continue
		}
		if errRemove := os.Remove(filepath.Join(dir, entry.Name())); errRemove != nil && !os.IsNotExist(errRemove) {
			return removed, fmt.Errorf("remove %s: %w", entry.Name(), errRemove)
		}
		removed = append(removed, day)
	}
	sort.Strings(removed)
	return removed, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./...
```

Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add store.go store_test.go
git commit -m "feat: per-day JSON store with atomic writes and retention cleanup

Co-Authored-By: Claude Code <noreply@anthropic.com>"
```

---

### Task 6: flush 生命周期与全局接线

**Files:**
- Modify: `aggregator.go`（追加 configure/loadFromDisk/cleanupExpired/flushDirty/startFlusher/Shutdown）
- Modify: `register.go`（`applyConfig` 接入 aggregator）
- Modify: `main.go`（`cliproxyPluginShutdown` 接入 aggregator）
- Create: `aggregator_flush_test.go`

- [ ] **Step 1: 写失败测试**

`aggregator_flush_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newFlushTestAggregator(dir string) *Aggregator {
	a := newAggregator()
	a.cfg = pluginConfig{DataDir: dir, RetentionDays: 30}
	a.nowFunc = func() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.Local) }
	return a
}

func TestFlushDirtyWritesChangedDays(t *testing.T) {
	dir := t.TempDir()
	a := newFlushTestAggregator(dir)
	a.Add(UsageRecord{Provider: "openai", Model: "gpt-5", RequestedAt: localTime(2026, 10, 2, 10),
		Detail: UsageDetail{InputTokens: 100, OutputTokens: 50}})
	a.flushDirty()
	if _, errStat := os.Stat(filepath.Join(dir, "2026-10-02.json")); errStat != nil {
		t.Fatalf("flushed file missing: %v", errStat)
	}
	if a.flushed["2026-10-02"] != a.versions["2026-10-02"] {
		t.Fatalf("flushed=%d versions=%d", a.flushed["2026-10-02"], a.versions["2026-10-02"])
	}

	// A second flush with no new data is a no-op (versions stay authoritative).
	a.flushDirty()
	if a.flushed["2026-10-02"] != a.versions["2026-10-02"] {
		t.Fatal("version tracking regressed after no-op flush")
	}

	// New data bumps the version and gets flushed again.
	a.Add(UsageRecord{Provider: "openai", Model: "gpt-5", RequestedAt: localTime(2026, 10, 2, 11),
		Detail: UsageDetail{InputTokens: 100, OutputTokens: 50}})
	a.flushDirty()
	loaded, errLoad := LoadDays(dir, "2026-09-03")
	if errLoad != nil {
		t.Fatalf("LoadDays: %v", errLoad)
	}
	if got := loaded["2026-10-02"].Models["gpt-5"].Requests; got != 2 {
		t.Fatalf("reloaded requests = %d, want 2", got)
	}
}

func TestConfigureLoadsDiskDataForMissingDays(t *testing.T) {
	dir := t.TempDir()
	if err := SaveDay(dir, sampleDay("2026-10-01")); err != nil {
		t.Fatalf("SaveDay: %v", err)
	}
	a := newFlushTestAggregator(dir)
	t.Cleanup(a.Shutdown)
	// in-memory day wins over disk for the same day
	a.Add(UsageRecord{Provider: "openai", Model: "mem-only", RequestedAt: localTime(2026, 10, 2, 9),
		Detail: UsageDetail{InputTokens: 1, OutputTokens: 1}})
	a.configure(a.cfg)
	days := a.Snapshot("2026-09-03", "2026-10-02")
	if len(days) != 2 {
		t.Fatalf("days after configure = %+v", days)
	}
	if days[0].Day != "2026-10-01" || days[0].Models["gpt-5"] == nil {
		t.Fatalf("disk day not loaded: %+v", days[0])
	}
	if days[1].Day != "2026-10-02" || days[1].Models["mem-only"] == nil {
		t.Fatalf("memory day lost: %+v", days[1])
	}
}

func TestCleanupExpiredRemovesFilesAndMemory(t *testing.T) {
	dir := t.TempDir()
	a := newFlushTestAggregator(dir)
	a.Add(UsageRecord{Provider: "openai", Model: "old", RequestedAt: localTime(2026, 8, 1, 10),
		Detail: UsageDetail{InputTokens: 1, OutputTokens: 1}})
	a.flushDirty()
	if _, errStat := os.Stat(filepath.Join(dir, "2026-08-01.json")); errStat != nil {
		t.Fatalf("precondition: file missing: %v", errStat)
	}
	a.cleanupExpired() // minDay = 2026-09-03 (nowFunc fixed at 2026-10-02, retention 30)
	if _, errStat := os.Stat(filepath.Join(dir, "2026-08-01.json")); !os.IsNotExist(errStat) {
		t.Fatal("expired file not removed")
	}
	if days := a.Snapshot("2020-01-01", "2030-01-01"); len(days) != 0 {
		t.Fatalf("expired day still in memory: %+v", days)
	}
}

func TestShutdownFlushesAndStopsFlusher(t *testing.T) {
	dir := t.TempDir()
	a := newFlushTestAggregator(dir)
	a.configure(a.cfg) // starts the flusher
	a.Add(UsageRecord{Provider: "openai", Model: "gpt-5", RequestedAt: localTime(2026, 10, 2, 10),
		Detail: UsageDetail{InputTokens: 1, OutputTokens: 1}})
	a.Shutdown()
	if _, errStat := os.Stat(filepath.Join(dir, "2026-10-02.json")); errStat != nil {
		t.Fatalf("shutdown flush missing: %v", errStat)
	}
	a.flushMu.Lock()
	active := a.flushActive
	a.flushMu.Unlock()
	if active {
		t.Fatal("flusher still active after Shutdown")
	}
	// Shutdown again must be safe.
	a.Shutdown()
}

func TestStartFlusherIdempotent(t *testing.T) {
	dir := t.TempDir()
	a := newFlushTestAggregator(dir)
	t.Cleanup(a.Shutdown)
	a.configure(a.cfg)
	a.configure(a.cfg) // must not panic or spawn a second flusher
}

func TestApplyConfigWiresAggregator(t *testing.T) {
	dir := t.TempDir()
	old := agg
	agg = newFlushTestAggregator(dir)
	t.Cleanup(func() {
		agg.Shutdown()
		agg = old
		setConfig(defaultConfig())
	})
	applyConfig(pluginConfig{DataDir: dir, RetentionDays: 10})
	if got := getConfig(); got.DataDir != dir || got.RetentionDays != 10 {
		t.Fatalf("currentConfig = %+v", got)
	}
	agg.Add(UsageRecord{Provider: "openai", Model: "wired", RequestedAt: localTime(2026, 10, 2, 10),
		Detail: UsageDetail{InputTokens: 1, OutputTokens: 1}})
	agg.flushDirty()
	if _, errStat := os.Stat(filepath.Join(dir, "2026-10-02.json")); errStat != nil {
		t.Fatalf("aggregator not configured with new dir: %v", errStat)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./... -run 'TestFlush|TestConfigure|TestCleanupExpired|TestShutdown|TestStartFlusher|TestApplyConfig' -v
```

Expected: FAIL — `undefined: (*Aggregator).configure` / `.flushDirty` / `.cleanupExpired` / `.Shutdown`

- [ ] **Step 3: 实现 flush 生命周期并接线**

在 `aggregator.go` **末尾追加**:

```go
const flushInterval = 30 * time.Second

// configure applies a new plugin configuration: it loads retained days from
// the data directory (days already in memory win), prunes expired data from
// disk and memory, and starts the background flusher. In-memory data is
// preserved across reconfiguration.
func (a *Aggregator) configure(cfg pluginConfig) {
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()

	a.loadFromDisk()
	a.cleanupExpired()
	a.startFlusher()
}

func (a *Aggregator) minRetentionDay() string {
	a.mu.RLock()
	cfg := a.cfg
	a.mu.RUnlock()
	return MinRetentionDay(a.nowFunc(), cfg.RetentionDays)
}

func (a *Aggregator) loadFromDisk() {
	a.mu.RLock()
	cfg := a.cfg
	a.mu.RUnlock()
	loaded, errLoad := LoadDays(cfg.DataDir, a.minRetentionDay())
	if errLoad != nil {
		stderrLog.Printf("failed to load aggregates from %s: %v", cfg.DataDir, errLoad)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for day, data := range loaded {
		if _, exists := a.days[day]; !exists {
			a.days[day] = data
		}
	}
}

func (a *Aggregator) cleanupExpired() {
	minDay := a.minRetentionDay()
	a.mu.RLock()
	cfg := a.cfg
	a.mu.RUnlock()
	removed, errCleanup := CleanupDays(cfg.DataDir, minDay)
	if errCleanup != nil {
		stderrLog.Printf("failed to clean up expired aggregates in %s: %v", cfg.DataDir, errCleanup)
	} else if len(removed) > 0 {
		stderrLog.Printf("removed %d expired aggregate file(s) older than %s", len(removed), minDay)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for day := range a.days {
		if day < minDay {
			delete(a.days, day)
			delete(a.versions, day)
			delete(a.flushed, day)
		}
	}
}

// flushDirty atomically writes every day with unflushed changes, then marks
// the flushed version. Writes that arrive during a flush bump the version and
// are picked up by the next flush.
func (a *Aggregator) flushDirty() {
	type daySnapshot struct {
		data    DayData
		version int64
	}
	a.mu.RLock()
	cfg := a.cfg
	snapshots := make([]daySnapshot, 0)
	for day, data := range a.days {
		if a.versions[day] > a.flushed[day] {
			snapshots = append(snapshots, daySnapshot{data: copyDayData(data), version: a.versions[day]})
		}
	}
	a.mu.RUnlock()

	for _, snapshot := range snapshots {
		if errSave := SaveDay(cfg.DataDir, snapshot.data); errSave != nil {
			stderrLog.Printf("failed to flush aggregate for %s: %v", snapshot.data.Day, errSave)
			continue
		}
		a.mu.Lock()
		if snapshot.version > a.flushed[snapshot.data.Day] {
			a.flushed[snapshot.data.Day] = snapshot.version
		}
		a.mu.Unlock()
	}
}

// startFlusher launches the periodic flush goroutine exactly once.
func (a *Aggregator) startFlusher() {
	a.flushMu.Lock()
	defer a.flushMu.Unlock()
	if a.flushActive {
		return
	}
	a.stopFlush = make(chan struct{})
	a.flushDone = make(chan struct{})
	a.flushActive = true
	go func() {
		defer close(a.flushDone)
		ticker := time.NewTicker(flushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				a.flushDirty()
				a.cleanupExpired()
			case <-a.stopFlush:
				return
			}
		}
	}()
}

// Shutdown stops the background flusher and performs a final flush. It is
// safe to call multiple times and on a never-configured aggregator.
func (a *Aggregator) Shutdown() {
	a.flushMu.Lock()
	if a.flushActive {
		close(a.stopFlush)
		a.flushActive = false
	}
	done := a.flushDone
	a.flushDone = nil
	a.flushMu.Unlock()
	if done != nil {
		<-done
	}
	a.flushDirty()
}
```

`register.go` 的 `applyConfig` 替换为:

```go
// applyConfig stores the new configuration and reconfigures the aggregator.
func applyConfig(cfg pluginConfig) {
	setConfig(cfg)
	agg.configure(cfg)
}
```

`main.go` 的 `cliproxyPluginShutdown` 替换为:

```go
//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {
	agg.Shutdown()
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./...
```

Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add aggregator.go register.go main.go aggregator_flush_test.go
git commit -m "feat: flush lifecycle with versioned dirty tracking and shutdown wiring

Co-Authored-By: Claude Code <noreply@anthropic.com>"
```

---

### Task 7: Management 注册与 JSON API

**Files:**
- Create: `api.go`
- Create: `api_test.go`
- Modify: `main.go`（`handleMethod` 增加 management 分支）

- [ ] **Step 1: 写失败测试**

`api_test.go`:

```go
package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// resetAggregatorForTest swaps in a fresh aggregator with a fixed clock and a
// temp data dir, restoring the original after the test.
func resetAggregatorForTest(t *testing.T) {
	t.Helper()
	old := agg
	a := newAggregator()
	a.cfg = pluginConfig{DataDir: t.TempDir(), RetentionDays: defaultRetentionDays}
	a.nowFunc = func() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.Local) }
	agg = a
	t.Cleanup(func() {
		a.Shutdown()
		agg = old
	})
}

func callManagement(t *testing.T, req managementRequest) managementResponse {
	t.Helper()
	request, errMarshal := json.Marshal(req)
	if errMarshal != nil {
		t.Fatalf("marshal request: %v", errMarshal)
	}
	env := callEnvelope(t, "management.handle", request)
	if !env.OK {
		t.Fatalf("management.handle not ok: %+v", env.Error)
	}
	var resp managementResponse
	if errUnmarshal := json.Unmarshal(env.Result, &resp); errUnmarshal != nil {
		t.Fatalf("unmarshal management response: %v", errUnmarshal)
	}
	return resp
}

func decodeJSONBody(t *testing.T, resp managementResponse) map[string]any {
	t.Helper()
	var out map[string]any
	if errUnmarshal := json.Unmarshal(resp.Body, &out); errUnmarshal != nil {
		t.Fatalf("body is not JSON: %v (%s)", errUnmarshal, resp.Body)
	}
	return out
}

func seedStats(t *testing.T) {
	t.Helper()
	a := agg
	a.Add(UsageRecord{Provider: "openai", Model: "gpt-5", RequestedAt: localTime(2026, 10, 2, 9),
		Detail: UsageDetail{InputTokens: 300, CacheReadTokens: 100, OutputTokens: 100}})
	a.Add(UsageRecord{Provider: "claude", Model: "claude-opus-4-5", RequestedAt: localTime(2026, 10, 2, 10),
		Detail: UsageDetail{InputTokens: 100, CacheReadTokens: 40, CacheCreationTokens: 10, OutputTokens: 50}})
	a.Add(UsageRecord{Provider: "openai", Model: "gpt-5", RequestedAt: localTime(2026, 10, 1, 9),
		Detail: UsageDetail{InputTokens: 60, OutputTokens: 40}})
}

func TestHandleManagementRegister(t *testing.T) {
	env := callEnvelope(t, "management.register", nil)
	if !env.OK {
		t.Fatalf("not ok: %+v", env.Error)
	}
	var reg struct {
		Routes []struct {
			Method string `json:"Method"`
			Path   string `json:"Path"`
			Menu   string `json:"Menu"`
		} `json:"routes"`
		Resources []struct {
			Path string `json:"Path"`
			Menu string `json:"Menu"`
		} `json:"resources"`
	}
	if errUnmarshal := json.Unmarshal(env.Result, &reg); errUnmarshal != nil {
		t.Fatalf("unmarshal: %v", errUnmarshal)
	}
	if len(reg.Routes) != 1 || reg.Routes[0].Method != "GET" || reg.Routes[0].Path != "/usage-stats" {
		t.Fatalf("routes = %+v", reg.Routes)
	}
	if reg.Routes[0].Menu != "" {
		t.Fatalf("management route must not set Menu (would become a resource): %+v", reg.Routes[0])
	}
	if len(reg.Resources) != 1 || reg.Resources[0].Path != "/stats" || reg.Resources[0].Menu == "" {
		t.Fatalf("resources = %+v", reg.Resources)
	}
}

func TestUsageStatsEmpty(t *testing.T) {
	resetAggregatorForTest(t)
	resp := callManagement(t, managementRequest{Method: "GET", Path: "/v0/management/usage-stats"})
	if resp.StatusCode != 200 {
		t.Fatalf("StatusCode = %d", resp.StatusCode)
	}
	body := decodeJSONBody(t, resp)
	days, ok := body["days"].([]any)
	if !ok || len(days) != 0 {
		t.Fatalf("days = %v", body["days"])
	}
	if _, okParse := body["generated_at"].(string); !okParse {
		t.Fatalf("generated_at missing: %v", body)
	}
	totals := body["totals"].(map[string]any)
	if totals["requests"].(float64) != 0 {
		t.Fatalf("totals = %v", totals)
	}
}

func TestUsageStatsAggregationAndSorting(t *testing.T) {
	resetAggregatorForTest(t)
	seedStats(t)
	resp := callManagement(t, managementRequest{Method: "GET", Path: "/v0/management/usage-stats"})
	body := decodeJSONBody(t, resp)
	days := body["days"].([]any)
	if len(days) != 2 {
		t.Fatalf("days = %v", days)
	}
	// days ascending
	day1 := days[0].(map[string]any)
	day2 := days[1].(map[string]any)
	if day1["day"].(string) != "2026-10-01" || day2["day"].(string) != "2026-10-02" {
		t.Fatalf("day order = %s, %s", day1["day"], day2["day"])
	}
	// models sorted by total_tokens desc within a day
	models := day2["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("models = %v", models)
	}
	first := models[0].(map[string]any)
	if first["model"].(string) != "gpt-5" { // 400 total vs claude 200 total
		t.Fatalf("first model = %v", first["model"])
	}
	if first["cache_hit_rate"].(float64) != 0.3333 {
		t.Fatalf("hit rate = %v", first["cache_hit_rate"])
	}
	// claude: input 100+40+10=150
	second := models[1].(map[string]any)
	if second["input_tokens"].(float64) != 150 {
		t.Fatalf("claude input = %v", second["input_tokens"])
	}
	// per-day totals present
	if day2["totals"].(map[string]any)["requests"].(float64) != 2 {
		t.Fatalf("day totals = %v", day2["totals"])
	}
	// overall totals: 3 requests, input 300+150+60=510, output 100+50+40=190
	totals := body["totals"].(map[string]any)
	if totals["requests"].(float64) != 3 || totals["input_tokens"].(float64) != 510 || totals["output_tokens"].(float64) != 190 {
		t.Fatalf("totals = %v", totals)
	}
	// overall hit rate: (100+40)/510 rounded to 4 decimals
	if totals["cache_hit_rate"].(float64) != 0.2745 {
		t.Fatalf("overall hit rate = %v", totals["cache_hit_rate"])
	}
}

func TestUsageStatsRangeFilter(t *testing.T) {
	resetAggregatorForTest(t)
	seedStats(t)
	resp := callManagement(t, managementRequest{Method: "GET", Path: "/v0/management/usage-stats",
		Query: map[string][]string{"from": {"2026-10-02"}, "to": {"2026-10-02"}}})
	body := decodeJSONBody(t, resp)
	days := body["days"].([]any)
	if len(days) != 1 || days[0].(map[string]any)["day"].(string) != "2026-10-02" {
		t.Fatalf("days = %v", days)
	}
}

func TestUsageStatsInvalidDates(t *testing.T) {
	resetAggregatorForTest(t)
	cases := []map[string][]string{
		{"from": {"10/02/2026"}},
		{"to": {"2026-13-40"}},
		{"from": {"2026-10-02"}, "to": {"2026-10-01"}},
	}
	for _, query := range cases {
		resp := callManagement(t, managementRequest{Method: "GET", Path: "/v0/management/usage-stats", Query: query})
		if resp.StatusCode != 400 {
			t.Fatalf("query %v: StatusCode = %d, want 400", query, resp.StatusCode)
		}
		body := decodeJSONBody(t, resp)
		if body["error"] == nil {
			t.Fatalf("query %v: no error message in %v", query, body)
		}
	}
}

func TestManagementNotFound(t *testing.T) {
	resetAggregatorForTest(t)
	resp := callManagement(t, managementRequest{Method: "GET", Path: "/v0/management/elsewhere"})
	if resp.StatusCode != 404 {
		t.Fatalf("StatusCode = %d, want 404", resp.StatusCode)
	}
	body := decodeJSONBody(t, resp)
	if body["error"] == nil {
		t.Fatalf("no error in %v", body)
	}
}

func TestUsageStatsNullHitRate(t *testing.T) {
	resetAggregatorForTest(t)
	agg.Add(UsageRecord{Provider: "openai", Model: "no-input", RequestedAt: localTime(2026, 10, 2, 9)})
	resp := callManagement(t, managementRequest{Method: "GET", Path: "/v0/management/usage-stats"})
	body := decodeJSONBody(t, resp)
	model := body["days"].([]any)[0].(map[string]any)["models"].([]any)[0].(map[string]any)
	rate, exists := model["cache_hit_rate"]
	if !exists || rate != nil {
		t.Fatalf("cache_hit_rate = %v (exists=%v), want explicit null", rate, exists)
	}
	if !strings.Contains(string(resp.Body), `"cache_hit_rate":null`) {
		t.Fatal("null hit rate must serialize explicitly")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./... -run 'TestHandleManagement|TestUsageStats|TestManagement' -v
```

Expected: FAIL — `undefined: managementRequest` / `managementResponse` / `handleManagement` 等

- [ ] **Step 3: 实现 api.go 并接线 handleMethod**

`api.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	managementRoutePath = "/v0/management/usage-stats"
	resourceRoutePath   = "/v0/resource/plugins/" + pluginName + "/stats"
)

type managementRegistration struct {
	Routes    []managementRoute `json:"routes"`
	Resources []resourceRoute   `json:"resources"`
}

type managementRoute struct {
	Method      string `json:"Method"`
	Path        string `json:"Path"`
	Description string `json:"Description,omitempty"`
}

type resourceRoute struct {
	Path        string `json:"Path"`
	Menu        string `json:"Menu"`
	Description string `json:"Description,omitempty"`
}

func handleManagementRegister() ([]byte, error) {
	return okEnvelope(managementRegistration{
		Routes: []managementRoute{
			{Method: http.MethodGet, Path: "/usage-stats", Description: "Daily per-model token usage statistics"},
		},
		Resources: []resourceRoute{
			{Path: "/stats", Menu: "Usage Stats", Description: "Daily per-model token usage page"},
		},
	})
}

type managementRequest struct {
	Method  string              `json:"Method"`
	Path    string              `json:"Path"`
	Headers map[string][]string `json:"Headers"`
	Query   map[string][]string `json:"Query"`
	Body    []byte              `json:"Body"`
}

type managementResponse struct {
	StatusCode int                 `json:"StatusCode"`
	Headers    map[string][]string `json:"Headers,omitempty"`
	Body       []byte              `json:"Body,omitempty"`
}

func handleManagement(request []byte) ([]byte, error) {
	var req managementRequest
	if errUnmarshal := json.Unmarshal(request, &req); errUnmarshal != nil {
		return errorEnvelope("invalid_request", "failed to decode management request: "+errUnmarshal.Error()), nil
	}
	switch {
	case req.Method == http.MethodGet && req.Path == managementRoutePath:
		return okEnvelope(handleUsageStats(req.Query))
	default:
		return okEnvelope(jsonResponse(http.StatusNotFound, map[string]string{"error": "not found"}))
	}
}

// statsCounters is the shared counter block for per-model rows and totals.
type statsCounters struct {
	Requests            int64    `json:"requests"`
	FailedRequests      int64    `json:"failed_requests"`
	InputTokens         int64    `json:"input_tokens"`
	CacheReadTokens     int64    `json:"cache_read_tokens"`
	CacheCreationTokens int64    `json:"cache_creation_tokens"`
	CacheHitRate        *float64 `json:"cache_hit_rate"`
	OutputTokens        int64    `json:"output_tokens"`
	ReasoningTokens     int64    `json:"reasoning_tokens"`
	TotalTokens         int64    `json:"total_tokens"`
}

type modelReport struct {
	Model string `json:"model"`
	statsCounters
}

type totalsReport struct {
	statsCounters
}

type dayReport struct {
	Day    string        `json:"day"`
	Models []modelReport `json:"models"`
	Totals totalsReport  `json:"totals"`
}

type usageStatsResponse struct {
	Days        []dayReport  `json:"days"`
	Totals      totalsReport `json:"totals"`
	GeneratedAt string       `json:"generated_at"`
}

// cacheHitRate returns cacheRead/input rounded to 4 decimals, or nil when
// there is no input.
func cacheHitRate(cacheRead, input int64) *float64 {
	if input <= 0 {
		return nil
	}
	rate := math.Round(float64(cacheRead)/float64(input)*10000) / 10000
	return &rate
}

func countersOf(stats *ModelStats) statsCounters {
	return statsCounters{
		Requests:            stats.Requests,
		FailedRequests:      stats.FailedRequests,
		InputTokens:         stats.InputTokens,
		CacheReadTokens:     stats.CacheReadTokens,
		CacheCreationTokens: stats.CacheCreationTokens,
		CacheHitRate:        cacheHitRate(stats.CacheReadTokens, stats.InputTokens),
		OutputTokens:        stats.OutputTokens,
		ReasoningTokens:     stats.ReasoningTokens,
		TotalTokens:         stats.TotalTokens,
	}
}

func accumulateTotals(dst *statsCounters, src statsCounters) {
	dst.Requests += src.Requests
	dst.FailedRequests += src.FailedRequests
	dst.InputTokens += src.InputTokens
	dst.CacheReadTokens += src.CacheReadTokens
	dst.CacheCreationTokens += src.CacheCreationTokens
	dst.OutputTokens += src.OutputTokens
	dst.ReasoningTokens += src.ReasoningTokens
	dst.TotalTokens += src.TotalTokens
	dst.CacheHitRate = cacheHitRate(dst.CacheReadTokens, dst.InputTokens)
}

// parseDateRange resolves the from/to query parameters (YYYY-MM-DD). The
// default window is the configured retention period ending today.
func parseDateRange(query map[string][]string, cfg pluginConfig, now time.Time) (string, string, error) {
	from := MinRetentionDay(now, cfg.RetentionDays)
	to := now.Local().Format("2006-01-02")
	if values := query["from"]; len(values) > 0 && strings.TrimSpace(values[0]) != "" {
		candidate := strings.TrimSpace(values[0])
		if _, errParse := time.ParseInLocation("2006-01-02", candidate, time.Local); errParse != nil {
			return "", "", fmt.Errorf("invalid from date %q, want YYYY-MM-DD", candidate)
		}
		from = candidate
	}
	if values := query["to"]; len(values) > 0 && strings.TrimSpace(values[0]) != "" {
		candidate := strings.TrimSpace(values[0])
		if _, errParse := time.ParseInLocation("2006-01-02", candidate, time.Local); errParse != nil {
			return "", "", fmt.Errorf("invalid to date %q, want YYYY-MM-DD", candidate)
		}
		to = candidate
	}
	if from > to {
		return "", "", fmt.Errorf("from %s is after to %s", from, to)
	}
	return from, to, nil
}

func handleUsageStats(query map[string][]string) managementResponse {
	cfg := getConfig()
	now := agg.nowFunc()
	from, to, errRange := parseDateRange(query, cfg, now)
	if errRange != nil {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": errRange.Error()})
	}
	days := agg.Snapshot(from, to)
	report := usageStatsResponse{
		Days:        make([]dayReport, 0, len(days)),
		GeneratedAt: now.Format(time.RFC3339),
	}
	overall := statsCounters{}
	for _, day := range days {
		entry := dayReport{Day: day.Day, Models: make([]modelReport, 0, len(day.Models))}
		dayTotals := statsCounters{}
		for model, stats := range day.Models {
			counters := countersOf(stats)
			entry.Models = append(entry.Models, modelReport{Model: model, statsCounters: counters})
			accumulateTotals(&dayTotals, counters)
		}
		sort.Slice(entry.Models, func(i, j int) bool {
			if entry.Models[i].TotalTokens != entry.Models[j].TotalTokens {
				return entry.Models[i].TotalTokens > entry.Models[j].TotalTokens
			}
			return entry.Models[i].Model < entry.Models[j].Model
		})
		entry.Totals = totalsReport{statsCounters: dayTotals}
		accumulateTotals(&overall, dayTotals)
		report.Days = append(report.Days, entry)
	}
	report.Totals = totalsReport{statsCounters: overall}
	return jsonResponse(http.StatusOK, report)
}

func jsonResponse(status int, value any) managementResponse {
	body, errMarshal := json.Marshal(value)
	if errMarshal != nil {
		status = http.StatusInternalServerError
		body = []byte(`{"error":"failed to encode response"}`)
	}
	return managementResponse{
		StatusCode: status,
		Headers:    map[string][]string{"Content-Type": {"application/json; charset=utf-8"}},
		Body:       body,
	}
}
```

`main.go` 的 `handleMethod` 替换为:

```go
func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		return handleRegister(request)
	case "management.register":
		return handleManagementRegister()
	case "management.handle":
		return handleManagement(request)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./...
```

Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add api.go api_test.go main.go
git commit -m "feat: management registration and usage-stats JSON API

Co-Authored-By: Claude Code <noreply@anthropic.com>"
```

---

### Task 8: usage.handle 接线

**Files:**
- Modify: `main.go`（增加 `handleUsage` 与 dispatch 分支）
- Create: `usage_test.go`

- [ ] **Step 1: 写失败测试**

`usage_test.go`:

```go
package main

import (
	"fmt"
	"testing"
	"time"
)

func TestHandleUsageRecord(t *testing.T) {
	resetAggregatorForTest(t)
	requestedAt := time.Date(2026, 10, 2, 12, 0, 0, 0, time.Local).Format(time.RFC3339)
	payload := []byte(fmt.Sprintf(`{
		"Provider": "claude",
		"ExecutorType": "claudeexecutor",
		"Model": "claude-opus-4-5",
		"RequestedAt": %q,
		"Detail": {"InputTokens": 100, "CacheReadTokens": 40, "CacheCreationTokens": 10, "OutputTokens": 50}
	}`, requestedAt))
	env := callEnvelope(t, "usage.handle", payload)
	if !env.OK {
		t.Fatalf("usage.handle not ok: %+v", env.Error)
	}
	days := agg.Snapshot("2026-10-02", "2026-10-02")
	if len(days) != 1 {
		t.Fatalf("days = %+v", days)
	}
	ms := days[0].Models["claude-opus-4-5"]
	if ms == nil {
		t.Fatalf("models = %+v", days[0].Models)
	}
	if ms.Requests != 1 || ms.InputTokens != 150 || ms.OutputTokens != 50 || ms.TotalTokens != 200 {
		t.Fatalf("stats = %+v", ms)
	}
}

func TestHandleUsageInvalidJSON(t *testing.T) {
	resetAggregatorForTest(t)
	env := callEnvelope(t, "usage.handle", []byte("{broken"))
	if env.OK || env.Error == nil || env.Error.Code != "invalid_request" {
		t.Fatalf("expected invalid_request, got %+v", env)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./... -run 'TestHandleUsage' -v
```

Expected: FAIL — `usage.handle` 落到 `unknown_method` 分支（`env.OK == false`），`undefined: handleUsage`

- [ ] **Step 3: 实现 handleUsage 并加入 dispatch**

`main.go` 的 `handleMethod` 替换为:

```go
func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		return handleRegister(request)
	case "usage.handle":
		return handleUsage(request)
	case "management.register":
		return handleManagementRegister()
	case "management.handle":
		return handleManagement(request)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}
```

`main.go` **末尾追加**:

```go
// handleUsage decodes one usage record pushed by the host and folds it into
// the aggregate.
func handleUsage(request []byte) ([]byte, error) {
	var rec UsageRecord
	if errUnmarshal := json.Unmarshal(request, &rec); errUnmarshal != nil {
		return errorEnvelope("invalid_request", "failed to decode usage record: "+errUnmarshal.Error()), nil
	}
	agg.Add(rec)
	return okEnvelope(struct{}{})
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./...
```

Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add main.go usage_test.go
git commit -m "feat: wire usage.handle into the aggregator

Co-Authored-By: Claude Code <noreply@anthropic.com>"
```

---

### Task 9: 浏览器页面

**Files:**
- Create: `page.go`
- Create: `page_test.go`
- Modify: `api.go`（`handleManagement` 增加 resource 分支）

注意：页面是 Go 反引号 raw string，**HTML/JS 内不得出现反引号**（JS 不用模板字符串）。

- [ ] **Step 1: 写失败测试**

`page_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestResourcePageServed(t *testing.T) {
	resetAggregatorForTest(t)
	resp := callManagement(t, managementRequest{Method: "GET", Path: "/v0/resource/plugins/token-usage-stats/stats"})
	if resp.StatusCode != 200 {
		t.Fatalf("StatusCode = %d", resp.StatusCode)
	}
	contentType := resp.Headers["Content-Type"]
	if len(contentType) == 0 || !strings.Contains(contentType[0], "text/html") {
		t.Fatalf("Content-Type = %v", contentType)
	}
	body := string(resp.Body)
	for _, marker := range []string{"<table", "/v0/management/usage-stats", "Bearer", "localStorage", "data-days"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("page missing marker %q", marker)
		}
	}
	if strings.Contains(body, "`") {
		t.Fatal("page must not contain backticks (raw string safety)")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./... -run 'TestResourcePage' -v
```

Expected: FAIL — resource 路径当前落到 404 分支（`StatusCode = 404, want 200`），`undefined: pageHTML`

- [ ] **Step 3: 实现 page.go 并接线 resource 分支**

`page.go`:

```go
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
```

`api.go` 的 `handleManagement` 替换为（新增 resource 分支 + `htmlResponse`）:

```go
func handleManagement(request []byte) ([]byte, error) {
	var req managementRequest
	if errUnmarshal := json.Unmarshal(request, &req); errUnmarshal != nil {
		return errorEnvelope("invalid_request", "failed to decode management request: "+errUnmarshal.Error()), nil
	}
	switch {
	case req.Method == http.MethodGet && req.Path == managementRoutePath:
		return okEnvelope(handleUsageStats(req.Query))
	case req.Method == http.MethodGet && req.Path == resourceRoutePath:
		return okEnvelope(htmlResponse(pageHTML))
	default:
		return okEnvelope(jsonResponse(http.StatusNotFound, map[string]string{"error": "not found"}))
	}
}
```

`api.go` **末尾追加**:

```go
func htmlResponse(html string) managementResponse {
	return managementResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string][]string{"Content-Type": {"text/html; charset=utf-8"}},
		Body:       []byte(html),
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./...
```

Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add page.go page_test.go api.go
git commit -m "feat: self-contained browser page for daily usage stats

Co-Authored-By: Claude Code <noreply@anthropic.com>"
```

---

### Task 10: Makefile、README 与端到端验证

**Files:**
- Create: `Makefile`
- Create: `README.md`

- [ ] **Step 1: 写 Makefile**

`Makefile`:

```makefile
UNAME_S := $(shell uname -s)
ifeq ($(OS),Windows_NT)
EXT := dll
else ifeq ($(UNAME_S),Darwin)
EXT := dylib
else
EXT := so
endif

BIN := bin/token-usage-stats.$(EXT)
CPA_PLUGINS_DIR ?= ../CLIProxyAPI/plugins

.PHONY: build test install clean

build: $(BIN)

$(BIN): $(wildcard *.go)
	mkdir -p bin
	go build -buildmode=c-shared -o $(BIN) .
	rm -f bin/token-usage-stats.h

test:
	go test ./...

install: build
	mkdir -p $(CPA_PLUGINS_DIR)
	cp $(BIN) $(CPA_PLUGINS_DIR)/

clean:
	rm -rf bin
```

- [ ] **Step 2: 写 README.md**

`README.md`:

```markdown
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
```

- [ ] **Step 3: 全量验证**

```bash
cd /Users/knox/Documents/GitWorkSpace/token-usage-stats
go vet ./...
go test ./...
make clean && make build
ls -la bin/
```

Expected: vet 无输出；测试全 PASS；`bin/token-usage-stats.dylib` 存在

- [ ] **Step 4: Commit**

```bash
git add Makefile README.md
git commit -m "docs: makefile and readme with build, deploy and api docs

Co-Authored-By: Claude Code <noreply@anthropic.com>"
```

- [ ] **Step 5: 手动部署验证（交给用户执行，需要运行中的 CPA）**

```bash
make install CPA_PLUGINS_DIR=/Users/knox/Documents/GitWorkSpace/CLIProxyAPI/plugins
# 在 CLIProxyAPI 的 config.yaml 中启用插件后重启 CPA，经代理发几个请求，然后：
curl -H "Authorization: Bearer <management-key>" "http://localhost:8317/v0/management/usage-stats"
# 浏览器打开 http://localhost:8317/v0/resource/plugins/token-usage-stats/stats
```

Expected: JSON 返回当天聚合数据；页面输密码后显示表格

---

## Self-Review 记录

- **Spec 覆盖**：spec §3 机制契约 → Task 1/3/7/8；§4 数据模型口径 → Task 4；§5 存储 → Task 5/6；§6 接口 → Task 7/9；§7 配置 → Task 2/3；§8 并发 → Task 4/6（RWMutex + 版本号脏追踪）；§9 构建 → Task 10；§10 测试 → 各 Task 测试 + Task 10 手动清单。spec §6.2 页面路径在写 plan 前已修正为 `/stats`（host 拒绝根路径资源）。
- **类型一致性**：`envelope`/`okEnvelope`/`errorEnvelope`（Task 1）贯穿全部；`pluginConfig`/`defaultConfig`/`getConfig`/`setConfig`（Task 2）→ `applyConfig`（Task 3→6 演进）；`UsageRecord`/`ModelStats`/`DayData`/`Aggregator`（Task 4）→ store（Task 5）→ flush（Task 6）→ API（Task 7）；`managementRequest/Response`（Task 7）→ page 分支（Task 9）；测试辅助 `callEnvelope`（Task 3 定义）→ `resetAggregatorForTest`（Task 7 定义，Task 8/9 复用）；`localTime`/`newTestAggregator`/`statsOf`（Task 4 定义，Task 6 复用 `localTime`）。
- **已知取舍**：mini YAML 解析器仅支持 flat 标量（spec §7 配置项均为平铺标量，足够）；Task 6 之后 `register_test.go` 中 `TestHandleRegisterAppliesConfig` 会触发真实 `agg.configure`（默认 data_dir 不存在时静默跳过、无文件写入，flusher 随全局 agg 存活——测试进程退出即回收，无副作用）。
