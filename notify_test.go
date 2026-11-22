package main

import (
	"path/filepath"
	"testing"
	"time"

	"aiusagewidget/providers"
)

func TestNotifier(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	reset := now.Add(2 * time.Hour)
	data := func(used float64, resetsAt time.Time) []providers.Usage {
		return []providers.Usage{{ID: "claude", Name: "Claude", Groups: []providers.Group{{Bars: []providers.Bar{
			{Label: "5-hour limit", UsedPct: used, ResetsAt: resetsAt.Format(time.RFC3339Nano)},
		}}}}}
	}
	path := filepath.Join(t.TempDir(), "notify.json")
	n := newNotifier(path)

	if a := n.check(data(50, reset), now); len(a) != 0 {
		t.Fatalf("50%%: %v", a)
	}
	a := n.check(data(82, reset), now)
	if len(a) != 1 || a[0].Title != "Claude · 5-hour limit at 82%" || a[0].Body != "Resets in 2h 0m." {
		t.Fatalf("80%% warning: %+v", a)
	}
	// same window, jittering sub-second reset time: no repeat
	if a := n.check(data(85, reset.Add(300*time.Millisecond)), now.Add(time.Minute)); len(a) != 0 {
		t.Fatalf("repeat 80%%: %v", a)
	}
	if a := n.check(data(96, reset), now.Add(2*time.Minute)); len(a) != 1 || a[0].Title != "Claude · 5-hour limit at 96% — almost out" {
		t.Fatalf("95%% critical: %+v", a)
	}
	// restart: persisted, still no repeat
	n = newNotifier(path)
	n.prev["claude||5-hour limit"] = 96
	if a := n.check(data(97, reset), now.Add(3*time.Minute)); len(a) != 0 {
		t.Fatalf("after restart: %v", a)
	}
	// window resets → "available again"
	next := now.Add(7 * time.Hour)
	a = n.check(data(3, next), now.Add(2*time.Hour+time.Minute))
	if len(a) != 1 || a[0].Title != "Claude · 5-hour limit is available again" {
		t.Fatalf("refilled: %+v", a)
	}
	// stale data (reset already passed) counts as 0%: refilled, no warning
	n2 := newNotifier("")
	n2.prev["claude||5-hour limit"] = 90
	if a := n2.check(data(90, now.Add(-time.Minute)), now); len(a) != 1 || a[0].Title != "Claude · 5-hour limit is available again" {
		t.Fatalf("stale: %+v", a)
	}
}

func TestNotifierLevels(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	data := []providers.Usage{{ID: "c", Name: "C", Groups: []providers.Group{{Bars: []providers.Bar{
		{Label: "L", UsedPct: 72, ResetsAt: now.Add(time.Hour).Format(time.RFC3339)}}}}}}
	n := newNotifier("")
	if a := n.check(data, now); len(a) != 0 {
		t.Fatalf("default 80: %v", a)
	}
	n = newNotifier("")
	n.setLevels(70, 90)
	if a := n.check(data, now); len(a) != 1 || a[0].Title != "C · L at 72%" {
		t.Fatalf("warn 70: %+v", a)
	}
	for _, c := range []struct{ w, c, ww, wc int }{
		{0, 0, 80, 95}, {30, 40, 50, 51}, {95, 90, 95, 96}, {99, 120, 99, 100}, {60, 100, 60, 100},
	} {
		if w, cr := normLevels(c.w, c.c); w != c.ww || cr != c.wc {
			t.Errorf("normLevels(%d,%d) = %d,%d want %d,%d", c.w, c.c, w, cr, c.ww, c.wc)
		}
	}
}
