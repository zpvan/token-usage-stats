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
	SchemaVersion uint32               `json:"schema_version"`
	Metadata      registrationMetadata `json:"metadata"`
	Capabilities  map[string]bool      `json:"capabilities"`
}

func registrationPayload() registration {
	return registration{
		SchemaVersion: schemaVersion,
		Metadata: registrationMetadata{
			Name:             pluginName,
			Version:          pluginVersion,
			Author:           pluginAuthor,
			GitHubRepository: "https://github.com/zpvan/token-usage-stats",
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

// applyConfig stores the new configuration and reconfigures the aggregator.
func applyConfig(cfg pluginConfig) {
	setConfig(cfg)
	agg.configure(cfg)
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
