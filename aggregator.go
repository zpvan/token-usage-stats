package main

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// tokenSemantics classifies how a provider partitions token counters,
// mirroring tokenAccountingSemanticsFor in CPA sdk/cliproxy/usage/accounting.go.
type tokenSemantics int

const (
	semanticsUnknown tokenSemantics = iota
	semanticsSubset
	semanticsIndependent
	semanticsSeparateReasoning
)

// semanticsFor maps provider/executor identifiers to token accounting semantics.
func semanticsFor(provider, executorType string) tokenSemantics {
	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	normalizedExecutor := strings.ToLower(strings.TrimSpace(executorType))
	value := strings.TrimSpace(normalizedProvider + " " + normalizedExecutor)
	if value == "" || value == "unknown" || value == "unknown unknown" {
		return semanticsUnknown
	}
	if normalizedExecutor == "openaicompatexecutor" || normalizedProvider == "openai-compatibility" || strings.HasPrefix(normalizedProvider, "openai-compatible-") {
		return semanticsSubset
	}
	if strings.Contains(value, "claude") || strings.Contains(value, "anthropic") {
		return semanticsIndependent
	}
	for _, marker := range []string{"gemini", "aistudio", "antigravity", "vertex", "interaction"} {
		if strings.Contains(value, marker) {
			return semanticsSeparateReasoning
		}
	}
	for _, marker := range []string{"openai", "codex", "xai", "grok", "kimi", "qwen", "deepseek", "openrouter"} {
		if strings.Contains(value, marker) {
			return semanticsSubset
		}
	}
	return semanticsUnknown
}

// UsageDetail mirrors the token counters of pluginapi.UsageDetail.
type UsageDetail struct {
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
}

// UsageRecord mirrors the fields of pluginapi.UsageRecord consumed by this plugin.
type UsageRecord struct {
	Provider      string
	ExecutorType  string
	Model         string
	Alias         string
	ResponseModel string
	RequestedAt   time.Time
	Failed        bool
	Detail        UsageDetail
}

// ModelStats holds accumulated counters for one model on one day.
type ModelStats struct {
	Requests            int64 `json:"requests"`
	FailedRequests      int64 `json:"failed_requests"`
	InputTokens         int64 `json:"input_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	ReasoningTokens     int64 `json:"reasoning_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
}

// DayData is the per-day aggregate document persisted to disk.
type DayData struct {
	Day    string                 `json:"day"`
	Models map[string]*ModelStats `json:"models"`
}

func dayOf(t time.Time, nowFunc func() time.Time) string {
	if t.IsZero() {
		t = nowFunc()
	}
	return t.Local().Format("2006-01-02")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func nonNegative(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func copyDayData(dd *DayData) DayData {
	out := DayData{Day: dd.Day, Models: make(map[string]*ModelStats, len(dd.Models))}
	for model, stats := range dd.Models {
		copied := *stats
		out.Models[model] = &copied
	}
	return out
}

// agg is the process-wide aggregator instance (plugins run in-process).
var agg = newAggregator()

// Aggregator accumulates usage records in memory and flushes daily
// aggregates to disk. versions/flushed implement dirty tracking:
// a day is dirty when versions[day] > flushed[day].
type Aggregator struct {
	mu       sync.RWMutex
	days     map[string]*DayData
	versions map[string]int64
	flushed  map[string]int64
	cfg      pluginConfig
	nowFunc  func() time.Time

	flushMu     sync.Mutex
	stopFlush   chan struct{}
	flushDone   chan struct{}
	flushActive bool
}

func newAggregator() *Aggregator {
	return &Aggregator{
		days:     make(map[string]*DayData),
		versions: make(map[string]int64),
		flushed:  make(map[string]int64),
		cfg:      defaultConfig(),
		nowFunc:  time.Now,
	}
}

// Add folds one usage record into the in-memory aggregate.
func (a *Aggregator) Add(rec UsageRecord) {
	day := dayOf(rec.RequestedAt, a.nowFunc)
	model := firstNonEmpty(rec.Model, rec.ResponseModel, rec.Alias)
	if model == "" {
		model = "unknown"
	}
	semantics := semanticsFor(rec.Provider, rec.ExecutorType)

	cacheRead := nonNegative(rec.Detail.CacheReadTokens)
	cacheCreate := nonNegative(rec.Detail.CacheCreationTokens)
	reasoning := nonNegative(rec.Detail.ReasoningTokens)
	output := nonNegative(rec.Detail.OutputTokens)
	input := nonNegative(rec.Detail.InputTokens)

	totalInput := input
	if semantics == semanticsIndependent {
		totalInput = input + cacheRead + cacheCreate
	}
	totalOutput := output
	if semantics == semanticsSeparateReasoning {
		totalOutput = output + reasoning
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	dayData := a.days[day]
	if dayData == nil {
		dayData = &DayData{Day: day, Models: make(map[string]*ModelStats)}
		a.days[day] = dayData
	}
	stats := dayData.Models[model]
	if stats == nil {
		stats = &ModelStats{}
		dayData.Models[model] = stats
	}
	stats.Requests++
	if rec.Failed {
		stats.FailedRequests++
	}
	stats.InputTokens += totalInput
	stats.CacheReadTokens += cacheRead
	stats.CacheCreationTokens += cacheCreate
	stats.OutputTokens += totalOutput
	stats.ReasoningTokens += reasoning
	stats.TotalTokens += totalInput + totalOutput
	a.versions[day]++
}

// Snapshot returns deep copies of the aggregated days within [from, to]
// (inclusive, YYYY-MM-DD), sorted ascending by day.
func (a *Aggregator) Snapshot(from, to string) []DayData {
	a.mu.RLock()
	defer a.mu.RUnlock()
	days := make([]string, 0, len(a.days))
	for day := range a.days {
		if day >= from && day <= to {
			days = append(days, day)
		}
	}
	sort.Strings(days)
	out := make([]DayData, 0, len(days))
	for _, day := range days {
		out = append(out, copyDayData(a.days[day]))
	}
	return out
}
