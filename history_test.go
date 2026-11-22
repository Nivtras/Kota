package main

import (
	"path/filepath"
	"testing"
	"time"

	"aiusagewidget/providers"
)

func TestHistory(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	data := func(five, week float64, reset time.Time) []providers.Usage {
		return []providers.Usage{{ID: "claude", Groups: []providers.Group{{Bars: []providers.Bar{
			{Label: "5-hour limit", UsedPct: five, ResetsAt: reset.Format(time.RFC3339), WindowSec: 5 * 3600},
			{Label: "Weekly limit", UsedPct: week, ResetsAt: reset.Add(72 * time.Hour).Format(time.RFC3339), WindowSec: 7 * 86400},
		}}}}, {ID: "codex", Error: "offline", Groups: []providers.Group{{Bars: []providers.Bar{{Label: "x", UsedPct: 5}}}}}}
	}
	path := filepath.Join(t.TempDir(), "history.json")
	h := newHistory(path)
	reset := now.Add(3 * time.Hour)

	h.record(data(10, 40, reset), now)
	h.record(data(10.4, 40, reset), now.Add(2*time.Minute)) // small move, too soon: skipped
	h.record(data(13, 40, reset), now.Add(4*time.Minute))   // moved ≥1: kept for 5h only
	h.record(data(13, 40, reset), now.Add(15*time.Minute))  // 10 min passed: kept for both
	h.record(data(50, 60, reset), now.Add(4*time.Hour))     // reset passed: 5h counts as 0

	h = newHistory(path) // persisted
	got := h.forProviders(data(0, 60, now.Add(9*time.Hour)), now.Add(4*time.Hour))
	if len(got) != 1 || got[0].ID != "claude" || len(got[0].Series) != 2 {
		t.Fatalf("got %+v", got)
	}
	five, week := got[0].Series[0], got[0].Series[1]
	if five.Label != "5-hour limit" || week.Label != "Weekly limit" {
		t.Fatalf("labels %q %q", five.Label, week.Label)
	}
	wantFive := []float64{10, 13, 13, 0}
	if len(five.Points) != len(wantFive) {
		t.Fatalf("5h points %v", five.Points)
	}
	for i, w := range wantFive {
		if five.Points[i][1] != w {
			t.Errorf("5h[%d] = %v want %v", i, five.Points[i][1], w)
		}
	}
	if len(week.Points) != 3 {
		t.Fatalf("weekly points %v", week.Points)
	}

	// older than 7 days is dropped
	h.record(data(1, 1, now.Add(9*24*time.Hour)), now.Add(8*24*time.Hour))
	got = h.forProviders(data(1, 1, now.Add(9*24*time.Hour)), now.Add(8*24*time.Hour))
	if n := len(got[0].Series[0].Points); n != 1 {
		t.Fatalf("after prune: %d points", n)
	}
}
