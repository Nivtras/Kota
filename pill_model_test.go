package main

import (
	"testing"
	"time"

	"aiusagewidget/providers"
)

func TestBarPace(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	bar := func(used float64, left time.Duration) providers.Bar {
		return providers.Bar{UsedPct: used, WindowSec: 5 * 3600, ResetsAt: now.Add(left).Format(time.RFC3339)}
	}
	// 60% used after 2h of 5h → 30%/h → 100% after 3h20m total, i.e. in 1h20m,
	// before the reset in 3h.
	p := barPace(bar(60, 3*time.Hour), now)
	if !p.OK || p.ElapsedPct != 40 || !p.RunsOut || p.RunsOutIn != 80*time.Minute {
		t.Errorf("fast burn = %+v", p)
	}
	// 30% used after 2h → 15%/h → 70% left needs 4h40m > 3h left: fine.
	if p := barPace(bar(30, 3*time.Hour), now); p.RunsOut {
		t.Errorf("slow burn should not run out: %+v", p)
	}
	// low usage: no forecast noise
	if p := barPace(bar(10, 4*time.Hour+50*time.Minute), now); p.RunsOut {
		t.Errorf("tiny usage should not forecast: %+v", p)
	}
	// unknown window
	if p := barPace(providers.Bar{UsedPct: 50, ResetsAt: now.Add(time.Hour).Format(time.RFC3339)}, now); p.OK {
		t.Errorf("no window → not OK: %+v", p)
	}
}

func TestBuildPillRows(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	items := []providers.Usage{
		{ID: "claude", Name: "Claude", Groups: []providers.Group{{Bars: []providers.Bar{
			{Label: "5-hour limit", UsedPct: 40, ResetsAt: at(2*time.Hour + 14*time.Minute)},
			{Label: "Weekly · all models", UsedPct: 66, ResetsAt: at(80 * time.Hour)},
		}}}},
		{ID: "codex", Name: "Codex", Groups: []providers.Group{{Bars: []providers.Bar{
			{Label: "Monthly limit", UsedPct: 3, ResetsAt: at(300 * time.Hour)},
		}}}},
		{ID: "antigravity", Name: "Antigravity", Groups: []providers.Group{
			{Name: "Gemini Models", Bars: []providers.Bar{
				{Label: "Weekly limit", UsedPct: 21, ResetsAt: at(100 * time.Hour)},
				{Label: "5h limit", UsedPct: 7, ResetsAt: at(3 * time.Hour)},
			}},
			{Name: "Claude and GPT models", Bars: []providers.Bar{{Label: "5h limit", UsedPct: 90, ResetsAt: at(time.Hour)}}},
		}},
	}

	// default: first two providers, 5-hour preferred, monthly fallback
	rows := buildPillRows(items, Settings{}, now)
	if len(rows) != 2 || rows[0].ID != "claude" || rows[1].ID != "codex" {
		t.Fatalf("default providers = %+v", rows)
	}
	if rows[0].Value != 40 || rows[0].Reset != "2h 14m" || rows[0].Word != "used" {
		t.Errorf("claude row = %+v", rows[0])
	}
	if rows[1].Value != 3 {
		t.Errorf("codex should fall back to monthly: %+v", rows[1])
	}

	// chosen providers + Gemini group only + remaining display
	rows = buildPillRows(items, Settings{Taskbar: []string{"antigravity", "claude"}, Display: "remaining"}, now)
	if rows[0].ID != "claude" || rows[1].ID != "antigravity" {
		t.Fatalf("chosen order = %s, %s", rows[0].ID, rows[1].ID)
	}
	if rows[1].Value != 93 || rows[1].Word != "left" || rows[1].Color != colOK {
		t.Errorf("antigravity row = %+v", rows[1])
	}

	// a window whose reset passed shows 0%
	items[0].Groups[0].Bars[0].ResetsAt = at(-time.Minute)
	items[0].Groups[0].Bars[0].UsedPct = 100
	rows = buildPillRows(items, Settings{}, now)
	if rows[0].Value != 0 || rows[0].Reset != "" {
		t.Errorf("expired window = %+v", rows[0])
	}

	// pace marker: 5h window, 2h14m left → 2h46m of 5h elapsed ≈ 55%
	rows = buildPillRows(items[:1], Settings{}, now)
	if rows[0].Marker != -1 {
		t.Errorf("no window length → no marker, got %v", rows[0].Marker)
	}
	items[0].Groups[0].Bars[0] = providers.Bar{Label: "5-hour limit", UsedPct: 40, ResetsAt: at(2*time.Hour + 14*time.Minute), WindowSec: 5 * 3600}
	rows = buildPillRows(items[:1], Settings{}, now)
	if m := rows[0].Marker; m < 55 || m > 56 {
		t.Errorf("marker = %v, want ~55.3", m)
	}

	// provider without data
	rows = buildPillRows([]providers.Usage{{ID: "x", Name: "X", Error: "boom"}}, Settings{}, now)
	if rows[0].HasBar || rows[0].NA != "unavailable" {
		t.Errorf("error row = %+v", rows[0])
	}
}
