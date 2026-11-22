package main

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"aiusagewidget/providers"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	windowTitle   = "AI Usage Widget"
	repaintEvery  = 30 * time.Second // keep reset countdowns on the pill fresh
	eventUsage    = "usage"          // Go → UI: new provider data
	eventShown    = "panel:shown"    // Go → UI: panel became visible
	eventSettings = "panel:settings" // Go → UI: show the settings view
	fetchDeadline = 30 * time.Second
)

type App struct {
	ctx   context.Context
	cfgMu sync.Mutex // guards cfg writes from the pill's drag callback
	cfg   config
	pill  *pillSet // primary pill + one per secondary taskbar
	panel *panel

	dataMu    sync.Mutex
	last      []providers.Usage
	fetching  chan struct{} // non-nil while a fetch runs; closed when done
	fetchedAt time.Time
	kick      chan struct{} // asks the refresh loop to fetch now

	notifier *notifier
	active   string // provider of the AI tool in use (follow mode); guarded by cfgMu
}

func NewApp() *App {
	path := filepath.Join(filepath.Dir(configPath()), "notify-state.json")
	return &App{cfg: loadConfig(), panel: &panel{}, notifier: newNotifier(path), kick: make(chan struct{}, 1)}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.panel.onShown = func() {
		runtime.EventsEmit(ctx, eventShown)
		a.refreshIfStale(staleOnOpen)
	}
	offset := defaultOffset
	if a.cfg.PillOffset != nil {
		offset = *a.cfg.PillOffset
	}
	primary := newPill(a.cfg.CompactWidth, a.cfg.PillAnchor, offset, loadLogos(),
		func(r rect) { a.panel.toggle(r) },
		func(anchor string, off int) { // the user dragged a pill
			a.cfgMu.Lock()
			a.cfg.PillAnchor, a.cfg.PillOffset = anchor, &off
			saveConfig(a.cfg)
			a.cfgMu.Unlock()
			a.pill.setPosition(anchor, off) // keep the other monitors in step
		})
	primary.onMenu = a.onPillMenu
	a.pill = newPillSet(primary, a.cfg.AllMonitors)
	theToaster.onClick = func() { a.panel.show(a.pill.screenRect()) } // click on a notification
	go a.refreshLoop()
	go watchActiveTool(
		func() bool { a.cfgMu.Lock(); defer a.cfgMu.Unlock(); return a.cfg.FollowActive },
		func(id string) {
			a.cfgMu.Lock()
			a.active = id
			a.cfgMu.Unlock()
			a.updatePill()
		})
}

// SetFollowActive turns follow mode on or off: the pill shows the AI tool
// in use first.
func (a *App) SetFollowActive(on bool) {
	a.cfgMu.Lock()
	a.cfg.FollowActive = on
	saveConfig(a.cfg)
	a.cfgMu.Unlock()
	a.updatePill()
}

// SetAllMonitors shows the pill on every monitor's taskbar, or only on the
// main one.
func (a *App) SetAllMonitors(on bool) {
	a.cfgMu.Lock()
	a.cfg.AllMonitors = on
	saveConfig(a.cfg)
	a.cfgMu.Unlock()
	if a.pill != nil {
		a.pill.setMulti(on)
	}
}

// onPillMenu handles the pill's right-click menu.
func (a *App) onPillMenu(cmd int) {
	switch cmd {
	case MenuOpen:
		a.panel.show(a.pill.screenRect())
	case MenuRefresh:
		a.fetch()
	case MenuSettings:
		a.panel.show(a.pill.screenRect())
		runtime.EventsEmit(a.ctx, eventSettings)
	case MenuResetPosition:
		a.ResetPillPosition()
	case MenuQuit:
		a.Quit()
	}
}

func (a *App) domReady(ctx context.Context) { go a.panel.attach(windowTitle) }

// refreshLoop owns data refresh, so the pill stays current even while the
// panel (web view) is hidden.
//
// The delay adapts (refresh.go): every 2 min while the user is active,
// every 15 min while idle or locked, right after a limit resets, and at
// once when the user comes back from being idle.
func (a *App) refreshLoop() {
	repaint := time.NewTicker(repaintEvery)
	defer repaint.Stop()
	var nextAt time.Time
	fetchNow := func() {
		a.fetch()
		nextAt = time.Now().Add(refreshDelay(userIdle(), a.snapshot(), time.Now()))
	}
	fetchNow()
	wasIdle := false
	for {
		timer := time.NewTimer(max(time.Until(nextAt), 0))
		select {
		case <-timer.C:
			fetchNow()
		case <-a.kick:
			timer.Stop()
			fetchNow()
		case <-repaint.C:
			timer.Stop()
			idle := userIdle() >= idleAfter
			if wasIdle && !idle {
				fetchNow() // back at the keyboard: show fresh numbers
			} else {
				a.updatePill()
			}
			wasIdle = idle
		}
	}
}

// refreshIfStale asks the refresh loop to fetch now when the data is older
// than d (e.g. the panel was just opened).
func (a *App) refreshIfStale(d time.Duration) {
	a.dataMu.Lock()
	stale := time.Since(a.fetchedAt) > d
	a.dataMu.Unlock()
	if stale {
		select {
		case a.kick <- struct{}{}:
		default:
		}
	}
}

// fetch queries all providers in parallel. Concurrent callers share one run.
func (a *App) fetch() []providers.Usage {
	a.dataMu.Lock()
	if ch := a.fetching; ch != nil {
		a.dataMu.Unlock()
		<-ch
		return a.snapshot()
	}
	ch := make(chan struct{})
	a.fetching = ch
	a.dataMu.Unlock()

	ps := providers.All()
	out := make([]providers.Usage, len(ps))
	var wg sync.WaitGroup
	for i, p := range ps {
		wg.Add(1)
		go func(i int, p providers.Provider) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), fetchDeadline)
			defer cancel()
			out[i] = p.Fetch(ctx)
		}(i, p)
	}
	wg.Wait()

	a.dataMu.Lock()
	a.last, a.fetching, a.fetchedAt = out, nil, time.Now()
	a.dataMu.Unlock()
	close(ch)

	a.updatePill()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, eventUsage, out)
	}
	// Alerts are computed every fetch (so state stays current) but only shown
	// when notifications are on.
	alerts := a.notifier.check(out, time.Now())
	a.cfgMu.Lock()
	off := a.cfg.NotifyOff
	a.cfgMu.Unlock()
	if !off {
		for _, al := range alerts {
			notifyUser(al.Title, al.Body)
		}
	}
	return out
}

// SetNotifications turns usage notifications on or off.
func (a *App) SetNotifications(on bool) {
	a.cfgMu.Lock()
	a.cfg.NotifyOff = !on
	saveConfig(a.cfg)
	a.cfgMu.Unlock()
}

// TestNotification shows a sample notification (settings button).
func (a *App) TestNotification() {
	notifyUser("AI Usage notifications are on", "You'll be told at 80% and 95% of a limit, and when it's available again.")
}

func (a *App) snapshot() []providers.Usage {
	a.dataMu.Lock()
	defer a.dataMu.Unlock()
	return a.last
}

func (a *App) updatePill() {
	if a.pill != nil {
		s := a.GetSettings()
		a.cfgMu.Lock()
		s.active = a.active
		a.cfgMu.Unlock()
		a.pill.setRows(buildPillRows(a.snapshot(), s, time.Now()))
	}
}

// ---------- bound to the UI ----------

// GetUsage returns the latest data (fetching once if there is none yet).
func (a *App) GetUsage() []providers.Usage {
	if d := a.snapshot(); d != nil {
		return d
	}
	return a.fetch()
}

// Refresh fetches fresh data now.
func (a *App) Refresh() []providers.Usage { return a.fetch() }

// HidePanel closes the panel (blur, Esc, collapse button).
func (a *App) HidePanel() { a.panel.hideFromUI() }

// SetCompactWidth resizes the taskbar pill (logical px); save=true persists it.
func (a *App) SetCompactWidth(w int, save bool) {
	w = clampWidth(w)
	a.cfgMu.Lock()
	a.cfg.CompactWidth = w
	if save {
		saveConfig(a.cfg)
	}
	a.cfgMu.Unlock()
	if a.pill != nil {
		a.pill.setWidth(w)
	}
}

// ResetPillPosition puts the pill back right before the Start button.
func (a *App) ResetPillPosition() {
	a.cfgMu.Lock()
	a.cfg.PillAnchor, a.cfg.PillOffset = "", nil
	saveConfig(a.cfg)
	a.cfgMu.Unlock()
	if a.pill != nil {
		a.pill.setPosition(anchorStart, defaultOffset)
	}
}

// SetAutostart turns "Start with Windows" on or off; returns the new state.
func (a *App) SetAutostart(on bool) bool {
	if err := setAutostart(on); err != nil {
		dockLog("autostart: %v", err)
	}
	return autostartEnabled()
}

// Settings are the user's display preferences, edited in the panel.
type Settings struct {
	Taskbar   []string `json:"taskbar"`  // provider IDs shown on the taskbar (max 2)
	Display   string   `json:"display"`  // "used" | "remaining"
	Width     int      `json:"width"`    // taskbar pill width, logical px
	MinWidth  int      `json:"minWidth"` // slider bounds (read-only)
	MaxWidth  int      `json:"maxWidth"`
	Autostart bool     `json:"autostart"` // read-only here; changed via SetAutostart
	Notify    bool     `json:"notify"`    // read-only here; changed via SetNotifications
	Follow    bool     `json:"follow"`    // show the tool in use first; changed via SetFollowActive
	Multi     bool     `json:"multi"`     // pill on every monitor's taskbar; changed via SetAllMonitors

	active string // provider of the app in use (follow mode), not sent to the UI
}

func (a *App) GetSettings() Settings {
	s := Settings{
		Taskbar: a.cfg.Taskbar, Display: a.cfg.Display,
		Width: clampWidth(a.cfg.CompactWidth), MinWidth: minCompactW, MaxWidth: maxCompactW,
		Autostart: autostartEnabled(),
		Notify:    !a.cfg.NotifyOff,
		Follow:    a.cfg.FollowActive,
		Multi:     a.cfg.AllMonitors,
	}
	if s.Display != "remaining" {
		s.Display = "used"
	}
	return s
}

// SaveSettings stores the taskbar providers and display mode
// (the width is saved through SetCompactWidth).
func (a *App) SaveSettings(s Settings) {
	if len(s.Taskbar) > 2 {
		s.Taskbar = s.Taskbar[:2]
	}
	if s.Display != "remaining" {
		s.Display = "used"
	}
	a.cfgMu.Lock()
	a.cfg.Taskbar, a.cfg.Display = s.Taskbar, s.Display
	saveConfig(a.cfg)
	a.cfgMu.Unlock()
	a.updatePill()
}

func (a *App) Quit() { runtime.Quit(a.ctx) }

// loadLogos reads the provider icons embedded with the frontend.
func loadLogos() map[string][]byte {
	out := map[string][]byte{}
	for _, id := range []string{"claude", "codex", "antigravity"} {
		if b, err := assets.ReadFile("frontend/dist/logos/" + id + ".png"); err == nil {
			out[id] = b
		}
	}
	return out
}
