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
CPA_SRC_DIR ?= ../CLIProxyAPI
CPA_CORE_DIR ?= $(HOME)/Library/Application Support/com.cpa.gui/cpa-core

.PHONY: build test install install-local-core clean

build: $(BIN)

$(BIN): $(wildcard *.go)
	mkdir -p bin
	go build -buildmode=c-shared -gcflags="all=-l -N" -o $(BIN) .
	rm -f bin/token-usage-stats.h

test:
	go test ./...

install: build
	mkdir -p $(CPA_PLUGINS_DIR)
	cp $(BIN) $(CPA_PLUGINS_DIR)/

# install-local-core restores a locally-built CPA core binary plus this plugin
# into a CPA core directory (default: EasyCLIProxyAPI's bundled core dir).
# Needed because official release binaries crash/fail to load self-compiled
# plugins (cross-runtime cgo ABI issue) and GUI auto-updates can overwrite a
# locally-built core. After running, restart the core from your GUI/launcher.
install-local-core: build
	@test -d "$(CPA_SRC_DIR)" || { echo "CPA source not found: $(CPA_SRC_DIR) (override with CPA_SRC_DIR=...)"; exit 1; }
	@test -d "$(CPA_CORE_DIR)" || { echo "CPA core dir not found: $(CPA_CORE_DIR) (override with CPA_CORE_DIR=...)"; exit 1; }
	cd "$(CPA_SRC_DIR)" && go build -o cli-proxy-api-dev ./cmd/server
	@TS=$$(date +%Y%m%d-%H%M%S); \
	if [ -f "$(CPA_CORE_DIR)/cli-proxy-api" ]; then \
		cp "$(CPA_CORE_DIR)/cli-proxy-api" "$(CPA_CORE_DIR)/cli-proxy-api.bak-$$TS"; \
		echo "backed up existing core binary -> cli-proxy-api.bak-$$TS"; \
	fi
	cp "$(CPA_SRC_DIR)/cli-proxy-api-dev" "$(CPA_CORE_DIR)/cli-proxy-api"
	rm -f "$(CPA_SRC_DIR)/cli-proxy-api-dev"
	mkdir -p "$(CPA_CORE_DIR)/plugins"
	cp $(BIN) "$(CPA_CORE_DIR)/plugins/token-usage-stats.$(EXT)"
	@if pgrep -f "cpa-core/cli-proxy-api" >/dev/null 2>&1; then \
		echo "note: a CPA core is currently running — restart it (e.g. from the GUI) to load the new binary + plugin"; \
	else \
		echo "done. Start the core from your GUI/launcher."; \
	fi

clean:
	rm -rf bin
