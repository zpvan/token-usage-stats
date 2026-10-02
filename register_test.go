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
