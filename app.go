package main

import (
	"context"
	"sync"
	"time"

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
	cfg   config
	pill  *pill
	panel *panel

	dataMu   sync.Mutex
	last     []providers.Usage
	fetching chan struct{} // non-nil while a fetch runs; closed when done
}

func NewApp() *App { return &App{cfg: loadConfig(), panel: &panel{}} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.panel.onShown = func() { runtime.EventsEmit(ctx, eventShown) }
	a.pill = newPill(a.cfg.CompactWidth, loadLogos(), func(r rect) { a.panel.toggle(r) })
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
	return out
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
	a.cfg.CompactWidth = w
	if a.pill != nil {
		a.pill.setWidth(w)
	}
	if save {
		saveConfig(a.cfg)
	}
}

// Settings are the user's display preferences, edited in the panel.
type Settings struct {
	Taskbar  []string `json:"taskbar"`  // provider IDs shown on the taskbar (max 2)
	Display  string   `json:"display"`  // "used" | "remaining"
	Width    int      `json:"width"`    // taskbar pill width, logical px
	MinWidth int      `json:"minWidth"` // slider bounds (read-only)
	MaxWidth int      `json:"maxWidth"`
}

func (a *App) GetSettings() Settings {
	s := Settings{
		Taskbar: a.cfg.Taskbar, Display: a.cfg.Display,
		Width: clampWidth(a.cfg.CompactWidth), MinWidth: minCompactW, MaxWidth: maxCompactW,
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
	a.cfg.Taskbar, a.cfg.Display = s.Taskbar, s.Display
	saveConfig(a.cfg)
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
