package main

import (
	"testing"
	"time"
)

func TestSemanticsFor(t *testing.T) {
	cases := []struct {
		provider, executor string
		want               tokenSemantics
	}{
		{"claude", "claudeexecutor", semanticsIndependent},
		{"anthropic", "", semanticsIndependent},
		{"openai", "", semanticsSubset},
		{"", "openaicompatexecutor", semanticsSubset},
		{"openai-compatibility", "", semanticsSubset},
		{"openai-compatible-foo", "", semanticsSubset},
		{"codex", "", semanticsSubset},
		{"kimi", "", semanticsSubset},
		{"openrouter", "", semanticsSubset},
		{"gemini", "", semanticsSeparateReasoning},
		{"aistudio", "", semanticsSeparateReasoning},
		{"vertex", "", semanticsSeparateReasoning},
		{"antigravity", "", semanticsSeparateReasoning},
		{"foo", "bar", semanticsUnknown},
		{"", "", semanticsUnknown},
		{"unknown", "unknown", semanticsUnknown},
	}
	for _, tc := range cases {
		if got := semanticsFor(tc.provider, tc.executor); got != tc.want {
			t.Errorf("semanticsFor(%q, %q) = %d, want %d", tc.provider, tc.executor, got, tc.want)
		}
	}
}

func newTestAggregator() *Aggregator {
	a := newAggregator()
	a.nowFunc = func() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.Local) }
	return a
}

func localTime(y int, m time.Month, d, hh int) time.Time {
	return time.Date(y, m, d, hh, 0, 0, 0, time.Local)
}

func statsOf(t *testing.T, a *Aggregator, day, model string) *ModelStats {
	t.Helper()
	a.mu.RLock()
	defer a.mu.RUnlock()
	dd := a.days[day]
	if dd == nil {
		t.Fatalf("no day %s", day)
	}
	ms := dd.Models[model]
	if ms == nil {
		t.Fatalf("no model %s on %s", model, day)
	}
	return ms
}

func TestAddOpenAISemantics(t *testing.T) {
	a := newTestAggregator()
	a.Add(UsageRecord{
		Provider: "openai", Model: "gpt-5", RequestedAt: localTime(2026, 10, 2, 10),
		Detail: UsageDetail{InputTokens: 100, CacheReadTokens: 40, OutputTokens: 50, ReasoningTokens: 10},
	})
	ms := statsOf(t, a, "2026-10-02", "gpt-5")
	if ms.Requests != 1 || ms.FailedRequests != 0 {
		t.Fatalf("requests = %+v", ms)
	}
	if ms.InputTokens != 100 { // subset: cache already inside input
		t.Fatalf("InputTokens = %d, want 100", ms.InputTokens)
	}
	if ms.CacheReadTokens != 40 || ms.OutputTokens != 50 || ms.ReasoningTokens != 10 {
		t.Fatalf("stats = %+v", ms)
	}
	if ms.TotalTokens != 150 {
		t.Fatalf("TotalTokens = %d, want 150", ms.TotalTokens)
	}
}

func TestAddClaudeSemantics(t *testing.T) {
	a := newTestAggregator()
	a.Add(UsageRecord{
		Provider: "claude", Model: "claude-opus-4-5", RequestedAt: localTime(2026, 10, 2, 10),
		Detail: UsageDetail{InputTokens: 100, CacheReadTokens: 40, CacheCreationTokens: 10, OutputTokens: 50},
	})
	ms := statsOf(t, a, "2026-10-02", "claude-opus-4-5")
	if ms.InputTokens != 150 { // independent: input + cache read + cache creation
		t.Fatalf("InputTokens = %d, want 150", ms.InputTokens)
	}
	if ms.TotalTokens != 200 {
		t.Fatalf("TotalTokens = %d, want 200", ms.TotalTokens)
	}
}

func TestAddGeminiSemantics(t *testing.T) {
	a := newTestAggregator()
	a.Add(UsageRecord{
		Provider: "gemini", Model: "gemini-3-pro", RequestedAt: localTime(2026, 10, 2, 10),
		Detail: UsageDetail{InputTokens: 100, CacheReadTokens: 40, OutputTokens: 50, ReasoningTokens: 20},
	})
	ms := statsOf(t, a, "2026-10-02", "gemini-3-pro")
	if ms.InputTokens != 100 {
		t.Fatalf("InputTokens = %d, want 100", ms.InputTokens)
	}
	if ms.OutputTokens != 70 { // separate reasoning: output + reasoning
		t.Fatalf("OutputTokens = %d, want 70", ms.OutputTokens)
	}
	if ms.TotalTokens != 170 {
		t.Fatalf("TotalTokens = %d, want 170", ms.TotalTokens)
	}
}

func TestAddAccumulatesAndCountsFailures(t *testing.T) {
	a := newTestAggregator()
	rec := UsageRecord{Provider: "openai", Model: "gpt-5", RequestedAt: localTime(2026, 10, 2, 10),
		Detail: UsageDetail{InputTokens: 100, OutputTokens: 50}}
	a.Add(rec)
	a.Add(rec)
	failed := rec
	failed.Failed = true
	a.Add(failed)
	ms := statsOf(t, a, "2026-10-02", "gpt-5")
	if ms.Requests != 3 || ms.FailedRequests != 1 {
		t.Fatalf("requests=%d failed=%d", ms.Requests, ms.FailedRequests)
	}
	if ms.InputTokens != 300 || ms.OutputTokens != 150 {
		t.Fatalf("stats = %+v", ms)
	}
}

func TestAddNegativeClampAndModelFallback(t *testing.T) {
	a := newTestAggregator()
	a.Add(UsageRecord{
		Provider: "openai", ResponseModel: "resp-model", Alias: "alias-model",
		RequestedAt: localTime(2026, 10, 2, 10),
		Detail:      UsageDetail{InputTokens: -5, CacheReadTokens: -1, OutputTokens: -2, ReasoningTokens: -3},
	})
	ms := statsOf(t, a, "2026-10-02", "resp-model")
	if ms.InputTokens != 0 || ms.OutputTokens != 0 || ms.TotalTokens != 0 {
		t.Fatalf("negative values not clamped: %+v", ms)
	}
	a.Add(UsageRecord{Provider: "openai", RequestedAt: localTime(2026, 10, 2, 10)})
	statsOf(t, a, "2026-10-02", "unknown")
}

func TestAddZeroRequestedAtUsesNowFunc(t *testing.T) {
	a := newTestAggregator()
	a.Add(UsageRecord{Provider: "openai", Model: "gpt-5", Detail: UsageDetail{InputTokens: 1}})
	statsOf(t, a, "2026-10-02", "gpt-5")
}

func TestSnapshotRangeSortAndDeepCopy(t *testing.T) {
	a := newTestAggregator()
	for _, day := range []string{"2026-10-02", "2026-10-01", "2026-09-30", "2026-10-05"} {
		parsed, errParse := time.ParseInLocation("2006-01-02", day, time.Local)
		if errParse != nil {
			t.Fatalf("parse %s: %v", day, errParse)
		}
		a.Add(UsageRecord{Provider: "openai", Model: "gpt-5", RequestedAt: parsed,
			Detail: UsageDetail{InputTokens: 1, OutputTokens: 1}})
	}
	days := a.Snapshot("2026-10-01", "2026-10-02")
	if len(days) != 2 || days[0].Day != "2026-10-01" || days[1].Day != "2026-10-02" {
		t.Fatalf("days = %+v", days)
	}
	// mutate the snapshot; the aggregate must not change
	days[0].Models["gpt-5"].Requests = 999
	if got := statsOf(t, a, "2026-10-01", "gpt-5").Requests; got != 1 {
		t.Fatalf("snapshot is not a deep copy, requests = %d", got)
	}
}
