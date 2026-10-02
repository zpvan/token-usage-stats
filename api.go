package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	managementRoutePath = "/v0/management/usage-stats"
	resourceRoutePath   = "/v0/resource/plugins/" + pluginName + "/stats"
)

type managementRegistration struct {
	Routes    []managementRoute `json:"routes"`
	Resources []resourceRoute   `json:"resources"`
}

type managementRoute struct {
	Method      string `json:"Method"`
	Path        string `json:"Path"`
	Description string `json:"Description,omitempty"`
}

type resourceRoute struct {
	Path        string `json:"Path"`
	Menu        string `json:"Menu"`
	Description string `json:"Description,omitempty"`
}

func handleManagementRegister() ([]byte, error) {
	return okEnvelope(managementRegistration{
		Routes: []managementRoute{
			{Method: http.MethodGet, Path: "/usage-stats", Description: "Daily per-model token usage statistics"},
		},
		Resources: []resourceRoute{
			{Path: "/stats", Menu: "Usage Stats", Description: "Daily per-model token usage page"},
		},
	})
}

type managementRequest struct {
	Method  string              `json:"Method"`
	Path    string              `json:"Path"`
	Headers map[string][]string `json:"Headers"`
	Query   map[string][]string `json:"Query"`
	Body    []byte              `json:"Body"`
}

type managementResponse struct {
	StatusCode int                 `json:"StatusCode"`
	Headers    map[string][]string `json:"Headers,omitempty"`
	Body       []byte              `json:"Body,omitempty"`
}

func handleManagement(request []byte) ([]byte, error) {
	var req managementRequest
	if errUnmarshal := json.Unmarshal(request, &req); errUnmarshal != nil {
		return errorEnvelope("invalid_request", "failed to decode management request: "+errUnmarshal.Error()), nil
	}
	switch {
	case req.Method == http.MethodGet && req.Path == managementRoutePath:
		return okEnvelope(handleUsageStats(req.Query))
	default:
		return okEnvelope(jsonResponse(http.StatusNotFound, map[string]string{"error": "not found"}))
	}
}

// statsCounters is the shared counter block for per-model rows and totals.
type statsCounters struct {
	Requests            int64    `json:"requests"`
	FailedRequests      int64    `json:"failed_requests"`
	InputTokens         int64    `json:"input_tokens"`
	CacheReadTokens     int64    `json:"cache_read_tokens"`
	CacheCreationTokens int64    `json:"cache_creation_tokens"`
	CacheHitRate        *float64 `json:"cache_hit_rate"`
	OutputTokens        int64    `json:"output_tokens"`
	ReasoningTokens     int64    `json:"reasoning_tokens"`
	TotalTokens         int64    `json:"total_tokens"`
}

type modelReport struct {
	Model string `json:"model"`
	statsCounters
}

type totalsReport struct {
	statsCounters
}

type dayReport struct {
	Day    string        `json:"day"`
	Models []modelReport `json:"models"`
	Totals totalsReport  `json:"totals"`
}

type usageStatsResponse struct {
	Days        []dayReport  `json:"days"`
	Totals      totalsReport `json:"totals"`
	GeneratedAt string       `json:"generated_at"`
}

// cacheHitRate returns cacheRead/input rounded to 4 decimals, or nil when
// there is no input.
func cacheHitRate(cacheRead, input int64) *float64 {
	if input <= 0 {
		return nil
	}
	rate := math.Round(float64(cacheRead)/float64(input)*10000) / 10000
	return &rate
}

func countersOf(stats *ModelStats) statsCounters {
	return statsCounters{
		Requests:            stats.Requests,
		FailedRequests:      stats.FailedRequests,
		InputTokens:         stats.InputTokens,
		CacheReadTokens:     stats.CacheReadTokens,
		CacheCreationTokens: stats.CacheCreationTokens,
		CacheHitRate:        cacheHitRate(stats.CacheReadTokens, stats.InputTokens),
		OutputTokens:        stats.OutputTokens,
		ReasoningTokens:     stats.ReasoningTokens,
		TotalTokens:         stats.TotalTokens,
	}
}

func accumulateTotals(dst *statsCounters, src statsCounters) {
	dst.Requests += src.Requests
	dst.FailedRequests += src.FailedRequests
	dst.InputTokens += src.InputTokens
	dst.CacheReadTokens += src.CacheReadTokens
	dst.CacheCreationTokens += src.CacheCreationTokens
	dst.OutputTokens += src.OutputTokens
	dst.ReasoningTokens += src.ReasoningTokens
	dst.TotalTokens += src.TotalTokens
	dst.CacheHitRate = cacheHitRate(dst.CacheReadTokens, dst.InputTokens)
}

// parseDateRange resolves the from/to query parameters (YYYY-MM-DD). The
// default window is the configured retention period ending today.
func parseDateRange(query map[string][]string, cfg pluginConfig, now time.Time) (string, string, error) {
	from := MinRetentionDay(now, cfg.RetentionDays)
	to := now.Local().Format("2006-01-02")
	if values := query["from"]; len(values) > 0 && strings.TrimSpace(values[0]) != "" {
		candidate := strings.TrimSpace(values[0])
		if _, errParse := time.ParseInLocation("2006-01-02", candidate, time.Local); errParse != nil {
			return "", "", fmt.Errorf("invalid from date %q, want YYYY-MM-DD", candidate)
		}
		from = candidate
	}
	if values := query["to"]; len(values) > 0 && strings.TrimSpace(values[0]) != "" {
		candidate := strings.TrimSpace(values[0])
		if _, errParse := time.ParseInLocation("2006-01-02", candidate, time.Local); errParse != nil {
			return "", "", fmt.Errorf("invalid to date %q, want YYYY-MM-DD", candidate)
		}
		to = candidate
	}
	if from > to {
		return "", "", fmt.Errorf("from %s is after to %s", from, to)
	}
	return from, to, nil
}

func handleUsageStats(query map[string][]string) managementResponse {
	cfg := getConfig()
	now := agg.nowFunc()
	from, to, errRange := parseDateRange(query, cfg, now)
	if errRange != nil {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": errRange.Error()})
	}
	days := agg.Snapshot(from, to)
	report := usageStatsResponse{
		Days:        make([]dayReport, 0, len(days)),
		GeneratedAt: now.Format(time.RFC3339),
	}
	overall := statsCounters{}
	for _, day := range days {
		entry := dayReport{Day: day.Day, Models: make([]modelReport, 0, len(day.Models))}
		dayTotals := statsCounters{}
		for model, stats := range day.Models {
			counters := countersOf(stats)
			entry.Models = append(entry.Models, modelReport{Model: model, statsCounters: counters})
			accumulateTotals(&dayTotals, counters)
		}
		sort.Slice(entry.Models, func(i, j int) bool {
			if entry.Models[i].TotalTokens != entry.Models[j].TotalTokens {
				return entry.Models[i].TotalTokens > entry.Models[j].TotalTokens
			}
			return entry.Models[i].Model < entry.Models[j].Model
		})
		entry.Totals = totalsReport{statsCounters: dayTotals}
		accumulateTotals(&overall, dayTotals)
		report.Days = append(report.Days, entry)
	}
	report.Totals = totalsReport{statsCounters: overall}
	return jsonResponse(http.StatusOK, report)
}

func jsonResponse(status int, value any) managementResponse {
	body, errMarshal := json.Marshal(value)
	if errMarshal != nil {
		status = http.StatusInternalServerError
		body = []byte(`{"error":"failed to encode response"}`)
	}
	return managementResponse{
		StatusCode: status,
		Headers:    map[string][]string{"Content-Type": {"application/json; charset=utf-8"}},
		Body:       body,
	}
}
