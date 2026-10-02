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
