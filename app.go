package main

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"aiusagewidget/providers"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const windowTitle = "AI Usage Widget"

type App struct {
	ctx     context.Context
	dock    *dock
	cfg     config
	modeMu  sync.Mutex
	modeSeq int
	// set by Quit(); otherwise an exit is treated as unexpected (Explorer
	// restart destroyed our taskbar-embedded window) and we relaunch.
	quitRequested atomic.Bool
}

func NewApp() *App {
	cfg := loadConfig()
	return &App{cfg: cfg, dock: &dock{mode: "compact", width: clampWidth(cfg.CompactWidth)}}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.dock.onPillClick = func() { runtime.EventsEmit(ctx, "pill:click") }
	a.dock.onOutsideClick = func() { runtime.EventsEmit(ctx, "outside:click") }
	a.dock.onWindowLost = func() {
		if !a.quitRequested.Load() {
			relaunchSelf()
		}
		os.Exit(0)
	}
}

func (a *App) domReady(ctx context.Context) { go a.dock.attach(windowTitle) }

// GetUsage queries all providers in parallel.
func (a *App) GetUsage() []providers.Usage {
	ps := providers.All()
	out := make([]providers.Usage, len(ps))
	var wg sync.WaitGroup
	for i, p := range ps {
		wg.Add(1)
		go func(i int, p providers.Provider) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			out[i] = p.Fetch(ctx)
		}(i, p)
	}
	wg.Wait()
	return out
}

// SetMode switches between "compact" (taskbar pill) and "expanded" (panel
// plus pill) and returns where each part sits in the window.
// seq increases with every request from the UI; a request that arrives after
// a newer one (calls can be reordered) is ignored, so the window size always
// matches the UI's latest decision. A stale request gets a nil layout.
func (a *App) SetMode(mode string, seq int) *Layout {
	a.modeMu.Lock()
	defer a.modeMu.Unlock()
	if seq <= a.modeSeq {
		return nil
	}
	a.modeSeq = seq
	if mode != "expanded" {
		mode = "compact"
	}
	l := a.dock.setMode(mode)
	return &l
}

// GetLayout returns the current window layout.
func (a *App) GetLayout() Layout { return a.dock.layout() }

// SetCompactWidth resizes the taskbar pill (logical px); save=true persists it.
func (a *App) SetCompactWidth(w int, save bool) Layout {
	w = clampWidth(w)
	a.cfg.CompactWidth = w
	l := a.dock.setWidth(w)
	if save {
		saveConfig(a.cfg)
	}
	return l
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
}

// Quit closes the widget for good (no automatic relaunch).
func (a *App) Quit() {
	a.quitRequested.Store(true)
	runtime.Quit(a.ctx)
}
