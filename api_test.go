package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// resetAggregatorForTest swaps in a fresh aggregator with a fixed clock and a
// temp data dir, restoring the original after the test.
func resetAggregatorForTest(t *testing.T) {
	t.Helper()
	old := agg
	a := newAggregator()
	a.cfg = pluginConfig{DataDir: t.TempDir(), RetentionDays: defaultRetentionDays}
	a.nowFunc = func() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.Local) }
	agg = a
	t.Cleanup(func() {
		a.Shutdown()
		agg = old
	})
}

func callManagement(t *testing.T, req managementRequest) managementResponse {
	t.Helper()
	request, errMarshal := json.Marshal(req)
	if errMarshal != nil {
		t.Fatalf("marshal request: %v", errMarshal)
	}
	env := callEnvelope(t, "management.handle", request)
	if !env.OK {
		t.Fatalf("management.handle not ok: %+v", env.Error)
	}
	var resp managementResponse
	if errUnmarshal := json.Unmarshal(env.Result, &resp); errUnmarshal != nil {
		t.Fatalf("unmarshal management response: %v", errUnmarshal)
	}
	return resp
}

func decodeJSONBody(t *testing.T, resp managementResponse) map[string]any {
	t.Helper()
	var out map[string]any
	if errUnmarshal := json.Unmarshal(resp.Body, &out); errUnmarshal != nil {
		t.Fatalf("body is not JSON: %v (%s)", errUnmarshal, resp.Body)
	}
	return out
}

func seedStats(t *testing.T) {
	t.Helper()
	a := agg
	a.Add(UsageRecord{Provider: "openai", Model: "gpt-5", RequestedAt: localTime(2026, 10, 2, 9),
		Detail: UsageDetail{InputTokens: 300, CacheReadTokens: 100, OutputTokens: 100}})
	a.Add(UsageRecord{Provider: "claude", Model: "claude-opus-4-5", RequestedAt: localTime(2026, 10, 2, 10),
		Detail: UsageDetail{InputTokens: 100, CacheReadTokens: 40, CacheCreationTokens: 10, OutputTokens: 50}})
	a.Add(UsageRecord{Provider: "openai", Model: "gpt-5", RequestedAt: localTime(2026, 10, 1, 9),
		Detail: UsageDetail{InputTokens: 60, OutputTokens: 40}})
}

func TestHandleManagementRegister(t *testing.T) {
	env := callEnvelope(t, "management.register", nil)
	if !env.OK {
		t.Fatalf("not ok: %+v", env.Error)
	}
	var reg struct {
		Routes []struct {
			Method string `json:"Method"`
			Path   string `json:"Path"`
			Menu   string `json:"Menu"`
		} `json:"routes"`
		Resources []struct {
			Path string `json:"Path"`
			Menu string `json:"Menu"`
		} `json:"resources"`
	}
	if errUnmarshal := json.Unmarshal(env.Result, &reg); errUnmarshal != nil {
		t.Fatalf("unmarshal: %v", errUnmarshal)
	}
	if len(reg.Routes) != 1 || reg.Routes[0].Method != "GET" || reg.Routes[0].Path != "/usage-stats" {
		t.Fatalf("routes = %+v", reg.Routes)
	}
	if reg.Routes[0].Menu != "" {
		t.Fatalf("management route must not set Menu (would become a resource): %+v", reg.Routes[0])
	}
	if len(reg.Resources) != 1 || reg.Resources[0].Path != "/stats" || reg.Resources[0].Menu == "" {
		t.Fatalf("resources = %+v", reg.Resources)
	}
}

func TestUsageStatsEmpty(t *testing.T) {
	resetAggregatorForTest(t)
	resp := callManagement(t, managementRequest{Method: "GET", Path: "/v0/management/usage-stats"})
	if resp.StatusCode != 200 {
		t.Fatalf("StatusCode = %d", resp.StatusCode)
	}
	body := decodeJSONBody(t, resp)
	days, ok := body["days"].([]any)
	if !ok || len(days) != 0 {
		t.Fatalf("days = %v", body["days"])
	}
	if _, okParse := body["generated_at"].(string); !okParse {
		t.Fatalf("generated_at missing: %v", body)
	}
	totals := body["totals"].(map[string]any)
	if totals["requests"].(float64) != 0 {
		t.Fatalf("totals = %v", totals)
	}
}

func TestUsageStatsAggregationAndSorting(t *testing.T) {
	resetAggregatorForTest(t)
	seedStats(t)
	resp := callManagement(t, managementRequest{Method: "GET", Path: "/v0/management/usage-stats"})
	body := decodeJSONBody(t, resp)
	days := body["days"].([]any)
	if len(days) != 2 {
		t.Fatalf("days = %v", days)
	}
	// days ascending
	day1 := days[0].(map[string]any)
	day2 := days[1].(map[string]any)
	if day1["day"].(string) != "2026-10-01" || day2["day"].(string) != "2026-10-02" {
		t.Fatalf("day order = %s, %s", day1["day"], day2["day"])
	}
	// models sorted by total_tokens desc within a day
	models := day2["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("models = %v", models)
	}
	first := models[0].(map[string]any)
	if first["model"].(string) != "gpt-5" { // 400 total vs claude 200 total
		t.Fatalf("first model = %v", first["model"])
	}
	if first["cache_hit_rate"].(float64) != 0.3333 {
		t.Fatalf("hit rate = %v", first["cache_hit_rate"])
	}
	// claude: input 100+40+10=150
	second := models[1].(map[string]any)
	if second["input_tokens"].(float64) != 150 {
		t.Fatalf("claude input = %v", second["input_tokens"])
	}
	// per-day totals present
	if day2["totals"].(map[string]any)["requests"].(float64) != 2 {
		t.Fatalf("day totals = %v", day2["totals"])
	}
	// overall totals: 3 requests, input 300+150+60=510, output 100+50+40=190
	totals := body["totals"].(map[string]any)
	if totals["requests"].(float64) != 3 || totals["input_tokens"].(float64) != 510 || totals["output_tokens"].(float64) != 190 {
		t.Fatalf("totals = %v", totals)
	}
	// overall hit rate: (100+40)/510 rounded to 4 decimals
	if totals["cache_hit_rate"].(float64) != 0.2745 {
		t.Fatalf("overall hit rate = %v", totals["cache_hit_rate"])
	}
}

func TestUsageStatsRangeFilter(t *testing.T) {
	resetAggregatorForTest(t)
	seedStats(t)
	resp := callManagement(t, managementRequest{Method: "GET", Path: "/v0/management/usage-stats",
		Query: map[string][]string{"from": {"2026-10-02"}, "to": {"2026-10-02"}}})
	body := decodeJSONBody(t, resp)
	days := body["days"].([]any)
	if len(days) != 1 || days[0].(map[string]any)["day"].(string) != "2026-10-02" {
		t.Fatalf("days = %v", days)
	}
}

func TestUsageStatsInvalidDates(t *testing.T) {
	resetAggregatorForTest(t)
	cases := []map[string][]string{
		{"from": {"10/02/2026"}},
		{"to": {"2026-13-40"}},
		{"from": {"2026-10-02"}, "to": {"2026-10-01"}},
	}
	for _, query := range cases {
		resp := callManagement(t, managementRequest{Method: "GET", Path: "/v0/management/usage-stats", Query: query})
		if resp.StatusCode != 400 {
			t.Fatalf("query %v: StatusCode = %d, want 400", query, resp.StatusCode)
		}
		body := decodeJSONBody(t, resp)
		if body["error"] == nil {
			t.Fatalf("query %v: no error message in %v", query, body)
		}
	}
}

func TestManagementNotFound(t *testing.T) {
	resetAggregatorForTest(t)
	resp := callManagement(t, managementRequest{Method: "GET", Path: "/v0/management/elsewhere"})
	if resp.StatusCode != 404 {
		t.Fatalf("StatusCode = %d, want 404", resp.StatusCode)
	}
	body := decodeJSONBody(t, resp)
	if body["error"] == nil {
		t.Fatalf("no error in %v", body)
	}
}

func TestUsageStatsNullHitRate(t *testing.T) {
	resetAggregatorForTest(t)
	agg.Add(UsageRecord{Provider: "openai", Model: "no-input", RequestedAt: localTime(2026, 10, 2, 9)})
	resp := callManagement(t, managementRequest{Method: "GET", Path: "/v0/management/usage-stats"})
	body := decodeJSONBody(t, resp)
	model := body["days"].([]any)[0].(map[string]any)["models"].([]any)[0].(map[string]any)
	rate, exists := model["cache_hit_rate"]
	if !exists || rate != nil {
		t.Fatalf("cache_hit_rate = %v (exists=%v), want explicit null", rate, exists)
	}
	if !strings.Contains(string(resp.Body), `"cache_hit_rate":null`) {
		t.Fatal("null hit rate must serialize explicitly")
	}
}
