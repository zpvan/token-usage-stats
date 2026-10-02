# Draft: upstream issue for router-for-me/CLIProxyAPI

Copy everything below the line into https://github.com/router-for-me/CLIProxyAPI/issues/new

---

**Title:** Official release binaries crash loading any self-compiled Go plugin (cross-runtime cgo ABI incompatibility)

## Summary

Every official release binary I tested (v7.2.151 and v8.0.11, both darwin/amd64) crashes with a fatal runtime error the moment it `dlopen`s and calls into **any** self-compiled Go C ABI plugin — including a minimal plugin built exactly after the `examples/plugin/usage/go` skeleton. The same plugin files register and run correctly on a core built from source on the local machine, so this is specifically a **release-binary ↔ plugin-runtime ABI incompatibility**, not a plugin bug.

## Environment

- Host: macOS 26 (darwin/arm64 machine, release binaries run under Rosetta as amd64)
- CPA release binaries tested: `v7.2.151` (commit 5208aec7, built with go1.26.4) and `v8.0.11` (commit e2bff010, built with go1.26.4)
- Plugin toolchains tested: go1.27.1 and go1.26.4 (both crash identically, so this is not simply a Go-version mismatch)
- Control: self-built core from `main` source (`go build -o cli-proxy-api ./cmd/server`, go1.27.1, darwin/arm64) loads and registers the same plugins without issues

## Minimal reproduction

1. Build a minimal plugin (skeleton per the official example):

```go
package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct { void* ptr; size_t len; } cliproxy_buffer;
typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);
typedef struct { uint32_t abi_version; void* host_ctx; cliproxy_host_call_fn call; cliproxy_host_free_fn free_buffer; } cliproxy_host_api;
typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);
typedef struct { uint32_t abi_version; cliproxy_plugin_call_fn call; cliproxy_plugin_free_fn free_buffer; cliproxy_plugin_shutdown_fn shutdown; } cliproxy_plugin_api;

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);
*/
import "C"
import "unsafe"

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	plugin.abi_version = 1
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, length C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {}
```

```bash
go build -buildmode=c-shared -o plugins/minplugin.dylib .
```

2. Enable it in `config.yaml`:

```yaml
plugins:
  enabled: true
  configs:
    minplugin:
      enabled: true
```

3. Start an **official release binary** (v7.2.151 or v8.0.11).

## Observed crash (100% reproducible)

Variant A (during `cliproxy_plugin_init`):

```
runtime: g 52: unexpected return pc for runtime.cgocallback called from 0x1008cd208
...
goroutine 52 [copystack, locked to thread]:
_cgoexp_401ea695ea28_cliproxy_plugin_init(0x30e8c3eb0)
    _cgo_gotypes.go:118 +0x3d
fatal error: unknown caller pc
```

Variant B (SIGSEGV while the plugin runtime grows a stack):

```
SIGSEGV: segmentation violation
PC=0x1591ffec3 m=13 sigcode=1 addr=0x0
runtime.(*mheap).allocNeedsZero(...)
runtime.stackalloc(0x2000)
runtime.copystack(...)
runtime.morestack()
goroutine 23 [copystack, locked to thread]:
_cgoexp_504fe58b7857_cliproxy_plugin_init(0x31227aeb0)
```

Both crash inside the plugin's Go runtime while handling the very first host→plugin call via `dlsym`. The crash happens in `copystack`/`morestack` stack-frame unwinding, which suggests the host process and the plugin runtime disagree about stack/frame layout — i.e., the two Go runtimes (host's CI-built runtime vs the plugin's runtime) are not ABI-compatible across the `dlopen` boundary in the way the loader assumes.

Also reproducible with `-gcflags="all=-l -N"` (no inlining, no optimizations), so it is not an inlining artifact.

## Expected behavior

A plugin built per the documented examples loads and registers on release binaries, or the documentation states the exact required toolchain/build flags for plugin authors (and the loader rejects incompatible plugins with a clean error instead of crashing the whole process).

## Secondary findings (documentation gaps found while debugging)

1. `validPlugin()` rejects registration when `Metadata.GitHubRepository` is empty (`host.go:1116-1128`), returning only "invalid metadata or no capabilities". The examples don't show this field as required — worth documenting or relaxing.
2. Storing Go-exported function addresses directly in `plugin->call/free/shutdown` (as the Go examples do) produces empty response reads in some host↔plugin runtime combinations; routing the struct's function pointers through C-side trampolines (`static` C functions that then call the Go exports) made responses reliable. If trampolines are the intended pattern, the Go example should demonstrate it.

## Suggested directions

- Document the exact Go version + build flags plugin authors must use to stay ABI-compatible with release binaries, and enforce/verify it in the loader (fail with a clear error rather than a process crash), or
- Ship an official plugin SDK/base image pinning a compatible toolchain, or
- Change the loading boundary (e.g., subprocess plugins) so host and plugin runtimes never share a stack-unwinding contract.
