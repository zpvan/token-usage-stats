package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMinRetentionDay(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.Local)
	if got := MinRetentionDay(now, 30); got != "2026-09-03" {
		t.Fatalf("MinRetentionDay(30) = %s, want 2026-09-03", got)
	}
	if got := MinRetentionDay(now, 1); got != "2026-10-02" {
		t.Fatalf("MinRetentionDay(1) = %s, want 2026-10-02", got)
	}
	if got := MinRetentionDay(now, 0); got != "2026-10-02" {
		t.Fatalf("MinRetentionDay(0) = %s, want 2026-10-02 (clamped)", got)
	}
}

func sampleDay(day string) DayData {
	return DayData{Day: day, Models: map[string]*ModelStats{
		"gpt-5": {Requests: 2, InputTokens: 100, CacheReadTokens: 40, OutputTokens: 50, TotalTokens: 150},
	}}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SaveDay(dir, sampleDay("2026-10-02")); err != nil {
		t.Fatalf("SaveDay: %v", err)
	}
	if _, errStat := os.Stat(filepath.Join(dir, "2026-10-02.json.tmp")); !os.IsNotExist(errStat) {
		t.Fatal("temp file left behind after atomic write")
	}
	loaded, err := LoadDays(dir, "2026-09-03")
	if err != nil {
		t.Fatalf("LoadDays: %v", err)
	}
	dd := loaded["2026-10-02"]
	if dd == nil {
		t.Fatal("day not loaded")
	}
	ms := dd.Models["gpt-5"]
	if ms == nil || ms.Requests != 2 || ms.InputTokens != 100 || ms.CacheReadTokens != 40 {
		t.Fatalf("loaded stats = %+v", ms)
	}
}

func TestSaveDayRejectsInvalidDay(t *testing.T) {
	dir := t.TempDir()
	if err := SaveDay(dir, sampleDay("2026-13-01")); err == nil {
		t.Fatal("expected error for invalid calendar day")
	}
	if err := SaveDay(dir, sampleDay("../evil")); err == nil {
		t.Fatal("expected error for path traversal day")
	}
}

func TestLoadDaysFiltersAndSkips(t *testing.T) {
	dir := t.TempDir()
	if err := SaveDay(dir, sampleDay("2026-10-02")); err != nil {
		t.Fatalf("SaveDay: %v", err)
	}
	if err := SaveDay(dir, sampleDay("2026-09-01")); err != nil {
		t.Fatalf("SaveDay: %v", err)
	}
	// corrupt file within retention: skipped, not fatal
	if errWrite := os.WriteFile(filepath.Join(dir, "2026-10-01.json"), []byte("{corrupt"), 0o644); errWrite != nil {
		t.Fatalf("write corrupt: %v", errWrite)
	}
	// non-matching files ignored
	if errWrite := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); errWrite != nil {
		t.Fatalf("write notes: %v", errWrite)
	}
	if errWrite := os.WriteFile(filepath.Join(dir, "2026-10-03.json.tmp"), []byte("{}"), 0o644); errWrite != nil {
		t.Fatalf("write tmp: %v", errWrite)
	}
	loaded, err := LoadDays(dir, "2026-09-03")
	if err != nil {
		t.Fatalf("LoadDays: %v", err)
	}
	if len(loaded) != 1 || loaded["2026-10-02"] == nil {
		t.Fatalf("loaded = %v", loaded)
	}
}

func TestLoadDaysMissingDir(t *testing.T) {
	loaded, err := LoadDays(filepath.Join(t.TempDir(), "missing"), "2026-09-03")
	if err != nil {
		t.Fatalf("LoadDays on missing dir: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded = %v, want empty", loaded)
	}
}

func TestCleanupDays(t *testing.T) {
	dir := t.TempDir()
	for _, day := range []string{"2026-09-01", "2026-09-02", "2026-10-01", "2026-10-02"} {
		if err := SaveDay(dir, sampleDay(day)); err != nil {
			t.Fatalf("SaveDay %s: %v", day, err)
		}
	}
	removed, err := CleanupDays(dir, "2026-10-01")
	if err != nil {
		t.Fatalf("CleanupDays: %v", err)
	}
	if len(removed) != 2 || removed[0] != "2026-09-01" || removed[1] != "2026-09-02" {
		t.Fatalf("removed = %v", removed)
	}
	for _, kept := range []string{"2026-10-01.json", "2026-10-02.json"} {
		if _, errStat := os.Stat(filepath.Join(dir, kept)); errStat != nil {
			t.Fatalf("kept file %s missing: %v", kept, errStat)
		}
	}
}

func TestCleanupDaysMissingDir(t *testing.T) {
	removed, err := CleanupDays(filepath.Join(t.TempDir(), "missing"), "2026-10-01")
	if err != nil || removed != nil {
		t.Fatalf("CleanupDays on missing dir = %v, %v", removed, err)
	}
}
