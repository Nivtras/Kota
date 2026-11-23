package main

import (
	"path/filepath"
	"testing"
	"time"

	"kota/internal/domain"
)

// The rules are tested in internal/domain; this checks the file round trip.
func TestHistoryPersists(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	data := []domain.Usage{{ID: "claude", Groups: []domain.Group{{Bars: []domain.Bar{
		{Label: "5-hour limit", UsedPct: 10, ResetsAt: now.Add(3 * time.Hour).Format(time.RFC3339), WindowSec: 5 * 3600},
	}}}}}
	path := filepath.Join(t.TempDir(), "history.json")
	newHistory(path).record(data, now)
	got := newHistory(path).forProviders(data, now)
	if len(got) != 1 || len(got[0].Series) != 1 || len(got[0].Series[0].Points) != 1 {
		t.Fatalf("got %+v", got)
	}
}
