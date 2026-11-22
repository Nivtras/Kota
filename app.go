package main

import (
	"context"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"aiusagewidget/providers"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	windowTitle   = "AI Usage Widget"
	refreshEvery  = 2 * time.Minute
	repaintEvery  = 30 * time.Second // keep reset countdowns on the pill fresh
	eventUsage    = "usage"          // Go → UI: new provider data
	eventShown    = "panel:shown"    // Go → UI: panel became visible
	fetchDeadline = 30 * time.Second
)

type App struct {
	ctx   context.Context
	cfgMu sync.Mutex // guards cfg writes from the pill's drag callback
	cfg   config
	pill  *pill
	panel *panel

	dataMu   sync.Mutex
	last     []providers.Usage
	fetching chan struct{} // non-nil while a fetch runs; closed when done

	notifier *notifier
}

func NewApp() *App {
	path := filepath.Join(filepath.Dir(configPath()), "notify-state.json")
	return &App{cfg: loadConfig(), panel: &panel{}, notifier: newNotifier(path)}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.panel.onShown = func() { runtime.EventsEmit(ctx, eventShown) }
	offset := defaultOffset
	if a.cfg.PillOffset != nil {
		offset = *a.cfg.PillOffset
	}
	a.pill = newPill(a.cfg.CompactWidth, a.cfg.PillAnchor, offset, loadLogos(),
		func(r rect) { a.panel.toggle(r) },
		func(anchor string, off int) { // the user dragged the pill
			a.cfgMu.Lock()
			a.cfg.PillAnchor, a.cfg.PillOffset = anchor, &off
			saveConfig(a.cfg)
			a.cfgMu.Unlock()
		})
	theToaster.onClick = func() { a.panel.show(a.pill.screenRect()) } // click on a notification
	// Buttons style: open the panel centred on where the button was clicked.
	theButtons.onClick = func(string) {
		var pt point
		pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		half := int32(float64(panelW) * dpiScale(findWindow("Shell_TrayWnd", "")) / 2)
		a.panel.toggle(rect{L: pt.X - half, R: pt.X + half})
	}
	theButtons.onClose = a.Quit
	a.pill.setStyle(a.cfg.TaskbarStyle)
	go a.refreshLoop()
}

func (a *App) domReady(ctx context.Context) { go a.panel.attach(windowTitle) }

// refreshLoop owns data refresh, so the pill stays current even while the
// panel (web view) is hidden.
func (a *App) refreshLoop() {
	a.fetch()
	refresh := time.NewTicker(refreshEvery)
	repaint := time.NewTicker(repaintEvery)
	for {
		select {
		case <-refresh.C:
			a.fetch()
		case <-repaint.C:
			a.updatePill()
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
	a.last, a.fetching = out, nil
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

// SetTaskbarStyle switches between the pill ("pill") and one taskbar button
// per provider ("buttons").
func (a *App) SetTaskbarStyle(style string) {
	if style != styleButtons {
		style = stylePill
	}
	a.cfgMu.Lock()
	a.cfg.TaskbarStyle = style
	saveConfig(a.cfg)
	a.cfgMu.Unlock()
	a.pill.setStyle(style)
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
		a.pill.setRows(buildPillRows(a.snapshot(), a.GetSettings(), time.Now()))
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
	Style     string   `json:"style"`     // "pill" | "buttons"; changed via SetTaskbarStyle
}

func (a *App) GetSettings() Settings {
	s := Settings{
		Taskbar: a.cfg.Taskbar, Display: a.cfg.Display,
		Width: clampWidth(a.cfg.CompactWidth), MinWidth: minCompactW, MaxWidth: maxCompactW,
		Autostart: autostartEnabled(),
		Notify:    !a.cfg.NotifyOff,
		Style:     a.cfg.TaskbarStyle,
	}
	if s.Style != styleButtons {
		s.Style = stylePill
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
