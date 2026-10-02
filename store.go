package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var dayFilePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.json$`)

// MinRetentionDay returns the oldest retained day (YYYY-MM-DD) for the given
// retention window relative to now. retentionDays < 1 is clamped to 1.
func MinRetentionDay(now time.Time, retentionDays int) string {
	if retentionDays < 1 {
		retentionDays = 1
	}
	return now.Local().AddDate(0, 0, -(retentionDays - 1)).Format("2006-01-02")
}

func validDayFileName(name string) bool {
	if !dayFilePattern.MatchString(name) {
		return false
	}
	_, errParse := time.ParseInLocation("2006-01-02", name[:len(name)-len(".json")], time.Local)
	return errParse == nil
}

// SaveDay atomically writes one day's aggregate as <dir>/<day>.json via a
// temp file + rename.
func SaveDay(dir string, data DayData) error {
	if !validDayFileName(data.Day + ".json") {
		return fmt.Errorf("invalid day %q", data.Day)
	}
	if errMkdir := os.MkdirAll(dir, 0o755); errMkdir != nil {
		return fmt.Errorf("create data dir: %w", errMkdir)
	}
	raw, errMarshal := json.MarshalIndent(data, "", "  ")
	if errMarshal != nil {
		return fmt.Errorf("marshal day %s: %w", data.Day, errMarshal)
	}
	raw = append(raw, '\n')
	tmpPath := filepath.Join(dir, data.Day+".json.tmp")
	if errWrite := os.WriteFile(tmpPath, raw, 0o644); errWrite != nil {
		return fmt.Errorf("write temp file: %w", errWrite)
	}
	if errRename := os.Rename(tmpPath, filepath.Join(dir, data.Day+".json")); errRename != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", errRename)
	}
	return nil
}

// LoadDays reads every retained day file (day >= minDay) from dir. Corrupt or
// unreadable files are skipped with a warning; a missing directory yields an
// empty map without error.
func LoadDays(dir, minDay string) (map[string]*DayData, error) {
	entries, errRead := os.ReadDir(dir)
	if errRead != nil {
		if os.IsNotExist(errRead) {
			return map[string]*DayData{}, nil
		}
		return nil, fmt.Errorf("read data dir: %w", errRead)
	}
	out := make(map[string]*DayData)
	for _, entry := range entries {
		if entry == nil || !entry.Type().IsRegular() || !validDayFileName(entry.Name()) {
			continue
		}
		day := entry.Name()[:len(entry.Name())-len(".json")]
		if day < minDay {
			continue
		}
		raw, errReadFile := os.ReadFile(filepath.Join(dir, entry.Name()))
		if errReadFile != nil {
			stderrLog.Printf("skipping unreadable aggregate file %s: %v", entry.Name(), errReadFile)
			continue
		}
		var data DayData
		if errUnmarshal := json.Unmarshal(raw, &data); errUnmarshal != nil {
			stderrLog.Printf("skipping corrupt aggregate file %s: %v", entry.Name(), errUnmarshal)
			continue
		}
		data.Day = day
		if data.Models == nil {
			data.Models = make(map[string]*ModelStats)
		}
		out[day] = &data
	}
	return out, nil
}

// CleanupDays removes day files older than minDay and returns the removed
// day identifiers in ascending order. A missing directory is not an error.
func CleanupDays(dir, minDay string) ([]string, error) {
	entries, errRead := os.ReadDir(dir)
	if errRead != nil {
		if os.IsNotExist(errRead) {
			return nil, nil
		}
		return nil, fmt.Errorf("read data dir: %w", errRead)
	}
	removed := make([]string, 0)
	for _, entry := range entries {
		if entry == nil || !entry.Type().IsRegular() || !validDayFileName(entry.Name()) {
			continue
		}
		day := entry.Name()[:len(entry.Name())-len(".json")]
		if day >= minDay {
			continue
		}
		if errRemove := os.Remove(filepath.Join(dir, entry.Name())); errRemove != nil && !os.IsNotExist(errRemove) {
			return removed, fmt.Errorf("remove %s: %w", entry.Name(), errRemove)
		}
		removed = append(removed, day)
	}
	sort.Strings(removed)
	return removed, nil
}
