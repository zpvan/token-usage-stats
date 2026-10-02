package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newFlushTestAggregator(dir string) *Aggregator {
	a := newAggregator()
	a.cfg = pluginConfig{DataDir: dir, RetentionDays: 30}
	a.nowFunc = func() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.Local) }
	return a
}

func TestFlushDirtyWritesChangedDays(t *testing.T) {
	dir := t.TempDir()
	a := newFlushTestAggregator(dir)
	a.Add(UsageRecord{Provider: "openai", Model: "gpt-5", RequestedAt: localTime(2026, 10, 2, 10),
		Detail: UsageDetail{InputTokens: 100, OutputTokens: 50}})
	a.flushDirty()
	if _, errStat := os.Stat(filepath.Join(dir, "2026-10-02.json")); errStat != nil {
		t.Fatalf("flushed file missing: %v", errStat)
	}
	if a.flushed["2026-10-02"] != a.versions["2026-10-02"] {
		t.Fatalf("flushed=%d versions=%d", a.flushed["2026-10-02"], a.versions["2026-10-02"])
	}

	// A second flush with no new data is a no-op (versions stay authoritative).
	a.flushDirty()
	if a.flushed["2026-10-02"] != a.versions["2026-10-02"] {
		t.Fatal("version tracking regressed after no-op flush")
	}

	// New data bumps the version and gets flushed again.
	a.Add(UsageRecord{Provider: "openai", Model: "gpt-5", RequestedAt: localTime(2026, 10, 2, 11),
		Detail: UsageDetail{InputTokens: 100, OutputTokens: 50}})
	a.flushDirty()
	loaded, errLoad := LoadDays(dir, "2026-09-03")
	if errLoad != nil {
		t.Fatalf("LoadDays: %v", errLoad)
	}
	if got := loaded["2026-10-02"].Models["gpt-5"].Requests; got != 2 {
		t.Fatalf("reloaded requests = %d, want 2", got)
	}
}

func TestConfigureLoadsDiskDataForMissingDays(t *testing.T) {
	dir := t.TempDir()
	if err := SaveDay(dir, sampleDay("2026-10-01")); err != nil {
		t.Fatalf("SaveDay: %v", err)
	}
	a := newFlushTestAggregator(dir)
	t.Cleanup(a.Shutdown)
	// in-memory day wins over disk for the same day
	a.Add(UsageRecord{Provider: "openai", Model: "mem-only", RequestedAt: localTime(2026, 10, 2, 9),
		Detail: UsageDetail{InputTokens: 1, OutputTokens: 1}})
	a.configure(a.cfg)
	days := a.Snapshot("2026-09-03", "2026-10-02")
	if len(days) != 2 {
		t.Fatalf("days after configure = %+v", days)
	}
	if days[0].Day != "2026-10-01" || days[0].Models["gpt-5"] == nil {
		t.Fatalf("disk day not loaded: %+v", days[0])
	}
	if days[1].Day != "2026-10-02" || days[1].Models["mem-only"] == nil {
		t.Fatalf("memory day lost: %+v", days[1])
	}
}

func TestCleanupExpiredRemovesFilesAndMemory(t *testing.T) {
	dir := t.TempDir()
	a := newFlushTestAggregator(dir)
	a.Add(UsageRecord{Provider: "openai", Model: "old", RequestedAt: localTime(2026, 8, 1, 10),
		Detail: UsageDetail{InputTokens: 1, OutputTokens: 1}})
	a.flushDirty()
	if _, errStat := os.Stat(filepath.Join(dir, "2026-08-01.json")); errStat != nil {
		t.Fatalf("precondition: file missing: %v", errStat)
	}
	a.cleanupExpired() // minDay = 2026-09-03 (nowFunc fixed at 2026-10-02, retention 30)
	if _, errStat := os.Stat(filepath.Join(dir, "2026-08-01.json")); !os.IsNotExist(errStat) {
		t.Fatal("expired file not removed")
	}
	if days := a.Snapshot("2020-01-01", "2030-01-01"); len(days) != 0 {
		t.Fatalf("expired day still in memory: %+v", days)
	}
}

func TestShutdownFlushesAndStopsFlusher(t *testing.T) {
	dir := t.TempDir()
	a := newFlushTestAggregator(dir)
	a.configure(a.cfg) // starts the flusher
	a.Add(UsageRecord{Provider: "openai", Model: "gpt-5", RequestedAt: localTime(2026, 10, 2, 10),
		Detail: UsageDetail{InputTokens: 1, OutputTokens: 1}})
	a.Shutdown()
	if _, errStat := os.Stat(filepath.Join(dir, "2026-10-02.json")); errStat != nil {
		t.Fatalf("shutdown flush missing: %v", errStat)
	}
	a.flushMu.Lock()
	active := a.flushActive
	a.flushMu.Unlock()
	if active {
		t.Fatal("flusher still active after Shutdown")
	}
	// Shutdown again must be safe.
	a.Shutdown()
}

func TestStartFlusherIdempotent(t *testing.T) {
	dir := t.TempDir()
	a := newFlushTestAggregator(dir)
	t.Cleanup(a.Shutdown)
	a.configure(a.cfg)
	a.configure(a.cfg) // must not panic or spawn a second flusher
}

func TestApplyConfigWiresAggregator(t *testing.T) {
	dir := t.TempDir()
	old := agg
	agg = newFlushTestAggregator(dir)
	t.Cleanup(func() {
		agg.Shutdown()
		agg = old
		setConfig(defaultConfig())
	})
	applyConfig(pluginConfig{DataDir: dir, RetentionDays: 10})
	if got := getConfig(); got.DataDir != dir || got.RetentionDays != 10 {
		t.Fatalf("currentConfig = %+v", got)
	}
	agg.Add(UsageRecord{Provider: "openai", Model: "wired", RequestedAt: localTime(2026, 10, 2, 10),
		Detail: UsageDetail{InputTokens: 1, OutputTokens: 1}})
	agg.flushDirty()
	if _, errStat := os.Stat(filepath.Join(dir, "2026-10-02.json")); errStat != nil {
		t.Fatalf("aggregator not configured with new dir: %v", errStat)
	}
}
