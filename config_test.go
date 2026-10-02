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
