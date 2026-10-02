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
// single-quoted (” escape), or plain (trailing " #" comment stripped).
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
