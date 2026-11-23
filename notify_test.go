package main

import (
	"kota/internal/infrastructure/storage"
	"path/filepath"
	"testing"
	"time"

	"kota/internal/domain"
)

// The rules are tested in internal/domain; this checks that sent alerts
// survive a restart.
func TestNotifierPersists(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	data := []domain.Usage{{ID: "claude", Name: "Claude", Groups: []domain.Group{{Bars: []domain.Bar{
		{Label: "5-hour limit", UsedPct: 82, ResetsAt: now.Add(2 * time.Hour).Format(time.RFC3339)},
	}}}}}
	path := filepath.Join(t.TempDir(), "notify.json")
	if a := newNotifier(storage.NewAlertStore(path)).check(data, now); len(a) != 1 {
		t.Fatalf("first: %v", a)
	}
	if a := newNotifier(storage.NewAlertStore(path)).check(data, now.Add(time.Minute)); len(a) != 0 {
		t.Fatalf("after restart: %v", a)
	}
}
