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
