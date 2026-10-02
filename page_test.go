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
	for _, marker := range []string{
		"<table", "/v0/management/usage-stats", "Bearer", "localStorage", "data-days",
		"<svg", "--series-1", "renderDailyChart", "renderModelChart", "dailyLegend",
		"每日 Tokens 直方图", "模型用量排行", "缓存读取", "tooltip", "tabular-nums",
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("page missing marker %q", marker)
		}
	}
	if strings.Contains(body, "`") {
		t.Fatal("page must not contain backticks (raw string safety)")
	}
}
