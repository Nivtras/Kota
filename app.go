package main

import (
	"context"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"kota/internal/application"
	"kota/internal/domain"
	"kota/internal/infrastructure/storage"
	"kota/internal/infrastructure/system"
	"kota/internal/infrastructure/win32"
)

const (
	windowTitle   = "Kota"
	eventUsage    = "usage"          // Go → UI: new provider data
	eventShown    = "panel:shown"    // Go → UI: panel became visible
	eventSettings = "panel:settings" // Go → UI: show the settings view
	eventUpdate   = "update"         // Go → UI: a newer release (or null)
)

// Services are the application layer's use cases the UI works with.
type Services struct {
	Settings      *application.SettingsService
	Usage         *application.UsageService
	Notifications *application.NotificationService
	History       *application.HistoryService
	Updates       *application.UpdateService
}

// App connects the services to the taskbar pill and the panel; the methods
// below are bound to the panel page.
type App struct {
	ctx   context.Context
	svc   Services
	logos map[string][]byte
	pill  *pillSet // primary pill + one per secondary taskbar
	panel *panel
}

func NewApp(svc Services, logos map[string][]byte) *App {
	return &App{svc: svc, logos: logos, panel: &panel{}}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.panel.onShown = func() {
		runtime.EventsEmit(ctx, eventShown)
		a.svc.Usage.RefreshIfStale(domain.StaleOnOpen)
	}
	p := a.svc.Settings.Prefs()
	offset := defaultOffset
	if p.PillOffset != nil {
		offset = *p.PillOffset
	}
	primary := newPill(domain.ClampWidth(p.CompactWidth), p.PillAnchor, offset, a.logos,
		func(r win32.Rect) { a.panel.toggle(r) },
		func(anchor string, off int) { // the user dragged a pill
			a.svc.Settings.SetPosition(anchor, &off)
			a.pill.setPosition(anchor, off) // keep the other monitors in step
		})
	primary.onMenu = a.onPillMenu
	a.pill = newPillSet(primary, p.AllMonitors)
	theToaster.onClick = func() { a.panel.show(a.pill.screenRect()) } // click on a notification

	a.svc.Usage.OnUsage(func(items []domain.Usage) {
		a.updatePill()
		runtime.EventsEmit(ctx, eventUsage, items)
	})
	a.svc.Usage.OnTick(a.updatePill)
	a.svc.Settings.OnChange(a.updatePill)
	a.svc.Updates.OnChange(func(r *domain.Release) { runtime.EventsEmit(ctx, eventUpdate, r) })
	go a.svc.Usage.Run()
	go a.svc.Updates.Run()
	go system.WatchActiveTool(
		func() bool { return a.svc.Settings.Prefs().FollowActive },
		a.svc.Settings.SetActive)
}

func (a *App) domReady(ctx context.Context) { go a.panel.attach(windowTitle) }

func (a *App) updatePill() {
	if a.pill != nil {
		rows := application.PillRows(a.svc.Usage.Snapshot(), a.svc.Settings.Prefs(), a.svc.Settings.Active(), time.Now())
		a.pill.setRows(rows)
	}
}

// onPillMenu handles the pill's right-click menu.
func (a *App) onPillMenu(cmd int) {
	switch cmd {
	case MenuOpen:
		a.panel.show(a.pill.screenRect())
	case MenuRefresh:
		a.svc.Usage.Fetch()
	case MenuSettings:
		a.panel.show(a.pill.screenRect())
		runtime.EventsEmit(a.ctx, eventSettings)
	case MenuResetPosition:
		a.ResetPillPosition()
	case MenuQuit:
		a.Quit()
	}
}

// ---------- bound to the panel page ----------

// GetUsage returns the latest data (fetching once if there is none yet).
func (a *App) GetUsage() []domain.Usage { return a.svc.Usage.Latest() }

// Refresh fetches fresh data now.
func (a *App) Refresh() []domain.Usage { return a.svc.Usage.Fetch() }

// GetHistory returns the last 7 days of the charted limits per provider.
func (a *App) GetHistory() []domain.ProviderHistory {
	return a.svc.History.Charts(a.svc.Usage.Snapshot())
}

// GetUpdate returns a newer release, or nil.
func (a *App) GetUpdate() *domain.Release { return a.svc.Updates.Current() }

// OpenUpdate opens the newer release's page in the browser.
func (a *App) OpenUpdate() {
	if u := a.svc.Updates.Current(); u != nil {
		runtime.BrowserOpenURL(a.ctx, u.URL)
		a.panel.hide()
	}
}

// SetUpdateCheck turns the daily release check on or off.
func (a *App) SetUpdateCheck(on bool) {
	a.svc.Settings.SetUpdateCheck(on)
	if !on {
		a.svc.Updates.Clear()
	}
}

// GetSettings returns the settings view.
func (a *App) GetSettings() application.Settings { return a.svc.Settings.View() }

// SaveSettings stores the taskbar providers and display mode (the width is
// saved through SetCompactWidth).
func (a *App) SaveSettings(s application.Settings) { a.svc.Settings.SaveDisplay(s.Taskbar, s.Display) }

// SetCompactWidth resizes the taskbar pill (logical px); save=true persists it.
func (a *App) SetCompactWidth(w int, save bool) {
	w = a.svc.Settings.SetWidth(w, save)
	if a.pill != nil {
		a.pill.setWidth(w)
	}
}

// ResetPillPosition puts the pill back right before the Start button.
func (a *App) ResetPillPosition() {
	a.svc.Settings.SetPosition("", nil)
	if a.pill != nil {
		a.pill.setPosition(anchorStart, defaultOffset)
	}
}

// SetAutostart turns "Start with Windows" on or off; returns the new state.
func (a *App) SetAutostart(on bool) bool {
	state, err := a.svc.Settings.SetAutostart(on)
	if err != nil {
		storage.Log("autostart: %v", err)
	}
	return state
}

// SetNotifications turns usage notifications on or off.
func (a *App) SetNotifications(on bool) { a.svc.Settings.SetNotifications(on) }

// SetNotifyLevels sets the warning and critical thresholds (% used) and
// returns the values kept.
func (a *App) SetNotifyLevels(warn, critical int) []int {
	w, c := a.svc.Settings.SetNotifyLevels(warn, critical)
	return []int{w, c}
}

// TestNotification shows a sample notification (settings button).
func (a *App) TestNotification() { a.svc.Notifications.Test() }

// SetFollowActive turns follow mode on or off.
func (a *App) SetFollowActive(on bool) { a.svc.Settings.SetFollowActive(on) }

// SetAllMonitors shows the pill on every monitor's taskbar, or only the main one.
func (a *App) SetAllMonitors(on bool) {
	a.svc.Settings.SetAllMonitors(on)
	if a.pill != nil {
		a.pill.setMulti(on)
	}
}

// HidePanel closes the panel (blur, Esc, collapse button).
func (a *App) HidePanel() { a.panel.hideFromUI() }

func (a *App) Quit() { runtime.Quit(a.ctx) }
