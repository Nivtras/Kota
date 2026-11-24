package application

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"kota/internal/domain"
)

// ---------- fakes for the ports ----------

type memPrefs struct{ p domain.Preferences }

func (m *memPrefs) Load() domain.Preferences  { return m.p }
func (m *memPrefs) Save(p domain.Preferences) { m.p = p }

type memAlerts struct{ saved int }

func (m *memAlerts) Load() map[string]time.Time { return map[string]time.Time{} }
func (m *memAlerts) Save(map[string]time.Time)  { m.saved++ }

type fakeAutostart struct{ on bool }

func (f *fakeAutostart) Enabled() bool     { return f.on }
func (f *fakeAutostart) Set(on bool) error { f.on = on; return nil }

type fakePresence struct{ busy atomic.Bool }

func (f *fakePresence) Idle() time.Duration { return 0 }
func (f *fakePresence) Busy() bool          { return f.busy.Load() }

type recorder struct {
	mu     sync.Mutex
	titles []string
}

func (r *recorder) Notify(title, _ string) {
	r.mu.Lock()
	r.titles = append(r.titles, title)
	r.mu.Unlock()
}
func (r *recorder) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.titles...)
}

type slowSource struct{ calls atomic.Int32 }

func (s *slowSource) ID() string   { return "x" }
func (s *slowSource) Name() string { return "X" }
func (s *slowSource) Fetch(context.Context) domain.Usage {
	s.calls.Add(1)
	time.Sleep(50 * time.Millisecond)
	return domain.Usage{ID: "x"}
}

// ---------- tests ----------

func TestPillRows(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	items := []domain.Usage{
		{ID: "claude", Name: "Claude", Groups: []domain.Group{{Bars: []domain.Bar{
			{Label: "5-hour limit", UsedPct: 40, ResetsAt: at(2*time.Hour + 14*time.Minute)},
			{Label: "Weekly · all models", UsedPct: 66, ResetsAt: at(80 * time.Hour)},
		}}}},
		{ID: "codex", Name: "Codex", Groups: []domain.Group{{Bars: []domain.Bar{
			{Label: "Monthly limit", UsedPct: 3, ResetsAt: at(300 * time.Hour)},
		}}}},
		{ID: "antigravity", Name: "Antigravity", Groups: []domain.Group{
			{Name: "Gemini Models", Bars: []domain.Bar{
				{Label: "Weekly limit", UsedPct: 21, ResetsAt: at(100 * time.Hour)},
				{Label: "5h limit", UsedPct: 7, ResetsAt: at(3 * time.Hour)},
			}},
			{Name: "Claude and GPT models", Bars: []domain.Bar{{Label: "5h limit", UsedPct: 90, ResetsAt: at(time.Hour)}}},
		}},
	}

	rows := PillRows(items, domain.Preferences{}, "", now)
	if len(rows) != 2 || rows[0].ID != "claude" || rows[1].ID != "codex" {
		t.Fatalf("default providers = %+v", rows)
	}
	if rows[0].Value != 40 || rows[0].Reset != "2h 14m" || rows[0].Word != "used" {
		t.Errorf("claude row = %+v", rows[0])
	}
	if rows[1].Value != 3 {
		t.Errorf("codex should fall back to monthly: %+v", rows[1])
	}

	rows = PillRows(items, domain.Preferences{Taskbar: []string{"antigravity", "claude"}, Display: "remaining"}, "", now)
	if rows[0].ID != "claude" || rows[1].ID != "antigravity" {
		t.Fatalf("chosen order = %s, %s", rows[0].ID, rows[1].ID)
	}
	if rows[1].Value != 93 || rows[1].Word != "left" || rows[1].Level != domain.LevelOK {
		t.Errorf("antigravity row = %+v", rows[1])
	}

	items[0].Groups[0].Bars[0].ResetsAt = at(-time.Minute)
	items[0].Groups[0].Bars[0].UsedPct = 100
	if rows = PillRows(items, domain.Preferences{}, "", now); rows[0].Value != 0 || rows[0].Reset != "" {
		t.Errorf("expired window = %+v", rows[0])
	}

	if rows = PillRows(items[:1], domain.Preferences{}, "", now); rows[0].Marker != -1 {
		t.Errorf("no window length → no marker, got %v", rows[0].Marker)
	}
	items[0].Groups[0].Bars[0] = domain.Bar{Label: "5-hour limit", UsedPct: 40, ResetsAt: at(2*time.Hour + 14*time.Minute), WindowSec: 5 * 3600}
	if m := PillRows(items[:1], domain.Preferences{}, "", now)[0].Marker; m < 55 || m > 56 {
		t.Errorf("marker = %v, want ~55.3", m)
	}

	follow := domain.Preferences{Taskbar: []string{"claude", "codex"}, FollowActive: true}
	if rows = PillRows(items, follow, "antigravity", now); rows[0].ID != "antigravity" || rows[1].ID != "claude" {
		t.Errorf("follow antigravity = %v, %v", rows[0].ID, rows[1].ID)
	}
	follow.FollowActive = false
	if rows = PillRows(items, follow, "antigravity", now); rows[0].ID != "claude" || rows[1].ID != "codex" {
		t.Errorf("follow off = %v, %v", rows[0].ID, rows[1].ID)
	}

	rows = PillRows([]domain.Usage{{ID: "x", Name: "X", Error: "boom"}}, domain.Preferences{}, "", now)
	if rows[0].HasBar || rows[0].NA != "unavailable" {
		t.Errorf("error row = %+v", rows[0])
	}
}

func TestSettingsService(t *testing.T) {
	repo, auto := &memPrefs{}, &fakeAutostart{}
	s := NewSettingsService(repo, auto)
	changes := 0
	s.OnChange(func() { changes++ })

	if w := s.SetWidth(999, false); w != domain.MaxWidth || repo.p.CompactWidth != 0 {
		t.Errorf("unsaved width: %d, repo %d", w, repo.p.CompactWidth)
	}
	s.SetWidth(250, true)
	s.SaveDisplay([]string{"a", "b", "c"}, "weird")
	if repo.p.CompactWidth != 250 || len(repo.p.Taskbar) != 2 || repo.p.Display != "used" {
		t.Errorf("saved prefs = %+v", repo.p)
	}
	if w, c := s.SetNotifyLevels(95, 90); w != 95 || c != 96 {
		t.Errorf("levels = %d, %d", w, c)
	}
	if on, _ := s.SetAutostart(true); !on || !s.View().Autostart {
		t.Error("autostart")
	}
	s.SetActive("codex")
	s.SetActive("codex") // same: no change
	if changes != 5 || s.Active() != "codex" {
		t.Errorf("changes = %d, active = %q", changes, s.Active())
	}
}

func TestNotificationsHeldWhileBusy(t *testing.T) {
	now := time.Now()
	data := []domain.Usage{{ID: "c", Name: "C", Groups: []domain.Group{{Bars: []domain.Bar{
		{Label: "L", UsedPct: 90, ResetsAt: now.Add(time.Hour).Format(time.RFC3339)}}}}}}
	settings := NewSettingsService(&memPrefs{}, &fakeAutostart{})
	out, pres, alerts := &recorder{}, &fakePresence{}, &memAlerts{}
	n := NewNotificationService(settings, alerts, out, pres)

	pres.busy.Store(true)
	n.Check(data)
	if len(out.got()) != 0 || alerts.saved != 1 {
		t.Fatalf("busy: shown %v, saved %d", out.got(), alerts.saved)
	}
	pres.busy.Store(false)
	deadline := time.Now().Add(3 * busyCheckEvery)
	for len(out.got()) == 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if got := out.got(); len(got) != 1 || got[0] != "C · L at 90%" {
		t.Fatalf("after busy: %v", got)
	}
	// notifications off: nothing shown
	settings.SetNotifications(false)
	n.Check([]domain.Usage{{ID: "d", Name: "D", Groups: data[0].Groups}})
	if len(out.got()) != 1 {
		t.Errorf("shown while off: %v", out.got())
	}
}

func TestUsageFetchSharedByConcurrentCallers(t *testing.T) {
	src := &slowSource{}
	s := NewUsageService(func() []UsageSource { return []UsageSource{src} }, &fakePresence{})
	got := 0
	s.OnUsage(func([]domain.Usage) { got++ })
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.Fetch() }()
	}
	wg.Wait()
	if c := src.calls.Load(); c < 1 || c > 2 || got != int(c) {
		t.Errorf("fetches = %d, listener calls = %d", c, got)
	}
	if len(s.Snapshot()) != 1 {
		t.Error("snapshot")
	}
}

type memSecrets map[string]string

func (m memSecrets) Get(n string) string { return m[n] }
func (m memSecrets) Set(n, v string) error {
	if v == "" {
		delete(m, n)
	} else {
		m[n] = v
	}
	return nil
}

func TestCredentials(t *testing.T) {
	store := memSecrets{}
	c := NewCredentialService(store, []KeyDef{{ID: "openrouter", Name: "OpenRouter", Secret: "or"}})
	if l := c.List(); len(l) != 1 || l[0].Set {
		t.Fatalf("list = %+v", l)
	}
	if err := c.Set("openrouter", "  \"sk-or-v1-abc\"\n"); err != nil || store["or"] != "sk-or-v1-abc" {
		t.Fatalf("set: %v, stored %q", err, store["or"])
	}
	if !c.List()[0].Set {
		t.Error("should be set")
	}
	if c.Set("openrouter", "two words") == nil || c.Set("nope", "k") == nil {
		t.Error("bad input accepted")
	}
	if c.Clear("openrouter"); store["or"] != "" {
		t.Error("clear")
	}
}

type panicSource struct{}

func (panicSource) ID() string                         { return "boom" }
func (panicSource) Name() string                       { return "Boom" }
func (panicSource) Fetch(context.Context) domain.Usage { panic("unexpected response") }

func TestPanicsAreContained(t *testing.T) {
	var reports []string
	OnPanic = func(where string, v any, _ []byte) { reports = append(reports, where) }
	defer func() { OnPanic = nil }()

	s := NewUsageService(func() []UsageSource { return []UsageSource{panicSource{}, &slowSource{}} }, &fakePresence{})
	s.OnUsage(func([]domain.Usage) { panic("listener bug") })
	second := false
	s.OnUsage(func([]domain.Usage) { second = true })
	out := s.Fetch()
	if len(out) != 2 || out[0].Error == "" || out[0].ID != "boom" || out[1].ID != "x" {
		t.Fatalf("out = %+v", out)
	}
	if !second || len(reports) != 2 {
		t.Fatalf("second listener ran: %v, reports %v", second, reports)
	}
}
