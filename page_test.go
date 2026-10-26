package main

import (
	"strings"
	"testing"
)

func TestModelChartUsesSharedModelColors(t *testing.T) {
	start := strings.Index(pageHTML, "function renderModelChart(days) {")
	if start < 0 {
		t.Fatal("model chart renderer not found")
	}
	end := strings.Index(pageHTML[start:], "\nfunction load()")
	if end < 0 {
		t.Fatal("model chart renderer end not found")
	}
	chart := pageHTML[start : start+end]
	for _, marker := range []string{"var colors = slotColors()", "var slot = slotFor(e.model)", "colors[slot]"} {
		if !strings.Contains(chart, marker) {
			t.Errorf("model chart does not use the daily chart's model color mapping: missing %q", marker)
		}
	}
}

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
	for _, marker := range []string{
		"<table", "/v0/management/usage-stats", "Bearer", "localStorage", "data-days",
		"<svg", "--series-1", "--series-8", "renderDailyChart", "renderModelChart", "dailyLegend",
		"slotFor", "modelSlots", "daySegments", "其他",
		"每日 Tokens 直方图", "模型用量排行", "缓存读取", "tooltip", "tabular-nums",
		"fmtTps", "tpsOf", "TPS",
		"--surface-2", "[data-theme=\"white\"]", "[data-theme=\"dark\"]",
		"cli-proxy-theme", "token-usage-stats.theme", "data-theme",
		"THEME_LOCAL_KEY", "THEME_CPA_KEY", "readCpaTheme", "resolveTheme", "applyTheme",
		"themeSel", "onThemeChanged", "lastData",
		"跟随 CPA", "跟随系统", "纯白", "羊毛纸", "暗色",
		"prefers-color-scheme",
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("page missing marker %q", marker)
		}
	}
	if strings.Contains(body, "`") {
		t.Fatal("page must not contain backticks (raw string safety)")
	}
}
