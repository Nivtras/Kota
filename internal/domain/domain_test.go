package domain

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func TestPaceOf(t *testing.T) {
	bar := func(used float64, left time.Duration) Bar {
		return Bar{UsedPct: used, WindowSec: 5 * 3600, ResetsAt: t0.Add(left).Format(time.RFC3339)}
	}
	// 60% used after 2h of 5h → 30%/h → 100% in 1h20m, before the reset in 3h.
	p := PaceOf(bar(60, 3*time.Hour), t0)
	if !p.OK || p.ElapsedPct != 40 || !p.RunsOut || p.RunsOutIn != 80*time.Minute {
		t.Errorf("fast burn = %+v", p)
	}
	if p := PaceOf(bar(30, 3*time.Hour), t0); p.RunsOut {
		t.Errorf("slow burn should not run out: %+v", p)
	}
	if p := PaceOf(bar(10, 4*time.Hour+50*time.Minute), t0); p.RunsOut {
		t.Errorf("tiny usage should not forecast: %+v", p)
	}
	if p := PaceOf(Bar{UsedPct: 50, ResetsAt: t0.Add(time.Hour).Format(time.RFC3339)}, t0); p.OK {
		t.Errorf("no window → not OK: %+v", p)
	}
}

func TestSelection(t *testing.T) {
	at := func(d time.Duration) string { return t0.Add(d).Format(time.RFC3339) }
	items := []Usage{
		{ID: "claude", Groups: []Group{{Bars: []Bar{
			{Label: "5-hour limit", UsedPct: 40, ResetsAt: at(2 * time.Hour)},
			{Label: "Weekly · all models", UsedPct: 66, ResetsAt: at(80 * time.Hour)},
		}}}},
		{ID: "codex", Groups: []Group{{Bars: []Bar{{Label: "Monthly limit", UsedPct: 3, ResetsAt: at(300 * time.Hour)}}}}},
		{ID: "antigravity", Groups: []Group{
			{Name: "Gemini Models", Bars: []Bar{
				{Label: "Weekly limit", UsedPct: 21, ResetsAt: at(100 * time.Hour)},
				{Label: "5h limit", UsedPct: 7, ResetsAt: at(3 * time.Hour)},
			}},
			{Name: "Claude and GPT models", Bars: []Bar{{Label: "5h limit", UsedPct: 90, ResetsAt: at(time.Hour)}}},
		}},
	}
	if b, _ := PickBar(items[0], t0); b.Label != "5-hour limit" {
		t.Errorf("claude: 5-hour preferred, got %q", b.Label)
	}
	if b, _ := PickBar(items[1], t0); b.Label != "Monthly limit" {
		t.Errorf("codex: falls back to the only limit, got %q", b.Label)
	}
	if b, _ := PickBar(items[2], t0); b.Group != "Gemini Models" || b.UsedPct != 7 {
		t.Errorf("antigravity: Gemini group only, got %+v", b)
	}
	if ids := idsOf(TaskbarProviders(items, []string{"antigravity", "claude"})); ids != "claude,antigravity" {
		t.Errorf("chosen keep provider order, got %s", ids)
	}
	if ids := idsOf(TaskbarProviders(items, nil)); ids != "claude,codex" {
		t.Errorf("default first two, got %s", ids)
	}
	chosen := TaskbarProviders(items, []string{"claude", "codex"})
	if ids := idsOf(WithActiveFirst(items, chosen, "antigravity")); ids != "antigravity,claude" {
		t.Errorf("follow antigravity, got %s", ids)
	}
	if ids := idsOf(WithActiveFirst(items, chosen, "codex")); ids != "codex,claude" {
		t.Errorf("follow codex, got %s", ids)
	}
	if LevelOf(59) != LevelOK || LevelOf(60) != LevelWarn || LevelOf(85) != LevelBad {
		t.Error("levels")
	}
}

func idsOf(us []Usage) string {
	s := ""
	for i, u := range us {
		if i > 0 {
			s += ","
		}
		s += u.ID
	}
	return s
}

func TestNotificationPolicy(t *testing.T) {
	reset := t0.Add(2 * time.Hour)
	data := func(used float64, resetsAt time.Time) []Usage {
		return []Usage{{ID: "claude", Name: "Claude", Groups: []Group{{Bars: []Bar{
			{Label: "5-hour limit", UsedPct: used, ResetsAt: resetsAt.Format(time.RFC3339Nano)},
		}}}}}
	}
	p, st := NewNotificationPolicy(0, 0), NewAlertState()

	if a, _ := p.Check(st, data(50, reset), t0); len(a) != 0 {
		t.Fatalf("50%%: %v", a)
	}
	a, changed := p.Check(st, data(82, reset), t0)
	if len(a) != 1 || a[0].Title != "Claude · 5-hour limit at 82%" || a[0].Body != "Resets in 2h 0m." || !changed {
		t.Fatalf("80%% warning: %+v", a)
	}
	// same window, jittering sub-second reset time: no repeat
	if a, _ := p.Check(st, data(85, reset.Add(300*time.Millisecond)), t0.Add(time.Minute)); len(a) != 0 {
		t.Fatalf("repeat 80%%: %v", a)
	}
	if a, _ := p.Check(st, data(96, reset), t0.Add(2*time.Minute)); len(a) != 1 || a[0].Title != "Claude · 5-hour limit at 96% — almost out" {
		t.Fatalf("95%% critical: %+v", a)
	}
	// restart: Sent survives, Prev is new
	st = &AlertState{Sent: st.Sent, Prev: map[string]float64{"claude||5-hour limit": 96}}
	if a, _ := p.Check(st, data(97, reset), t0.Add(3*time.Minute)); len(a) != 0 {
		t.Fatalf("after restart: %v", a)
	}
	// window resets → "available again"
	a, _ = p.Check(st, data(3, t0.Add(7*time.Hour)), t0.Add(2*time.Hour+time.Minute))
	if len(a) != 1 || a[0].Title != "Claude · 5-hour limit is available again" {
		t.Fatalf("refilled: %+v", a)
	}
	// stale data (reset already passed) counts as 0%: refilled, no warning
	st2 := NewAlertState()
	st2.Prev["claude||5-hour limit"] = 90
	if a, _ := p.Check(st2, data(90, t0.Add(-time.Minute)), t0); len(a) != 1 || a[0].Title != "Claude · 5-hour limit is available again" {
		t.Fatalf("stale: %+v", a)
	}
}

func TestNotificationLevels(t *testing.T) {
	data := []Usage{{ID: "c", Name: "C", Groups: []Group{{Bars: []Bar{
		{Label: "L", UsedPct: 72, ResetsAt: t0.Add(time.Hour).Format(time.RFC3339)}}}}}}
	if a, _ := NewNotificationPolicy(0, 0).Check(NewAlertState(), data, t0); len(a) != 0 {
		t.Fatalf("default 80: %v", a)
	}
	if a, _ := NewNotificationPolicy(70, 90).Check(NewAlertState(), data, t0); len(a) != 1 || a[0].Title != "C · L at 72%" {
		t.Fatalf("warn 70: %+v", a)
	}
	for _, c := range []struct{ w, c, ww, wc int }{
		{0, 0, 80, 95}, {30, 40, 50, 51}, {95, 90, 95, 96}, {99, 120, 99, 100}, {60, 100, 60, 100},
	} {
		if w, cr := NormLevels(c.w, c.c); w != c.ww || cr != c.wc {
			t.Errorf("NormLevels(%d,%d) = %d,%d want %d,%d", c.w, c.c, w, cr, c.ww, c.wc)
		}
	}
}

func TestHistory(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	data := func(five, week float64, reset time.Time) []Usage {
		return []Usage{{ID: "claude", Groups: []Group{{Bars: []Bar{
			{Label: "5-hour limit", UsedPct: five, ResetsAt: reset.Format(time.RFC3339), WindowSec: 5 * 3600},
			{Label: "Weekly limit", UsedPct: week, ResetsAt: reset.Add(72 * time.Hour).Format(time.RFC3339), WindowSec: 7 * 86400},
		}}}}, {ID: "codex", Error: "offline", Groups: []Group{{Bars: []Bar{{Label: "x", UsedPct: 5}}}}}}
	}
	h := History{}
	reset := now.Add(3 * time.Hour)
	h.Record(data(10, 40, reset), now)
	h.Record(data(10.4, 40, reset), now.Add(2*time.Minute)) // small move, too soon: skipped
	h.Record(data(13, 40, reset), now.Add(4*time.Minute))   // moved ≥1: kept for 5h only
	h.Record(data(13, 40, reset), now.Add(15*time.Minute))  // 10 min passed: kept for both
	h.Record(data(50, 60, reset), now.Add(4*time.Hour))     // reset passed: 5h counts as 0

	got := h.Charts(data(0, 60, now.Add(9*time.Hour)), now.Add(4*time.Hour))
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
	h.Record(data(1, 1, now.Add(9*24*time.Hour)), now.Add(8*24*time.Hour))
	got = h.Charts(data(1, 1, now.Add(9*24*time.Hour)), now.Add(8*24*time.Hour))
	if n := len(got[0].Series[0].Points); n != 1 {
		t.Fatalf("after prune: %d points", n)
	}
}

func TestRefreshDelay(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	withReset := func(in time.Duration) []Usage {
		return []Usage{{Groups: []Group{{Bars: []Bar{{ResetsAt: now.Add(in).Format(time.RFC3339)}}}}}}
	}
	cases := []struct {
		name  string
		idle  time.Duration
		items []Usage
		want  time.Duration
	}{
		{"active, no resets", time.Minute, nil, 2 * time.Minute},
		{"idle", 20 * time.Minute, nil, 15 * time.Minute},
		{"reset soon while active", 0, withReset(45 * time.Second), 75 * time.Second},
		{"reset soon while idle", time.Hour, withReset(5 * time.Minute), 5*time.Minute + 30*time.Second},
		{"reset later than interval", 0, withReset(time.Hour), 2 * time.Minute},
		{"reset already passed", 0, withReset(-time.Minute), 2 * time.Minute},
		{"never below minimum", 0, withReset(time.Second), 31 * time.Second},
	}
	for _, c := range cases {
		if got := RefreshDelay(c.idle, c.items, now); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		tag, cur string
		want     bool
	}{
		{"v1.2.0", "1.1.0", true}, {"1.1.1", "1.1.0", true}, {"v2.0", "1.9.9", true},
		{"v1.1.0", "1.1.0", false}, {"v1.0.9", "1.1.0", false}, {"v1.2.0-beta", "1.2.0", false}, {"", "1.1.0", false},
	} {
		if got := NewerVersion(c.tag, c.cur); got != c.want {
			t.Errorf("NewerVersion(%q, %q) = %v", c.tag, c.cur, got)
		}
	}
}

func TestPreferences(t *testing.T) {
	if ClampWidth(0) != DefaultWidth || ClampWidth(100) != MinWidth || ClampWidth(999) != MaxWidth || ClampWidth(250) != 250 {
		t.Error("ClampWidth")
	}
	if NormDisplay("remaining") != "remaining" || NormDisplay("x") != "used" {
		t.Error("NormDisplay")
	}
	if len(NormTaskbar([]string{"a", "b", "c"})) != 2 {
		t.Error("NormTaskbar")
	}
}
