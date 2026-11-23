//go:build windows

package panel

import (
	"context"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"kota/internal/application"
	"kota/internal/domain"
	"kota/internal/infrastructure/storage"
	"kota/internal/infrastructure/system"
	"kota/internal/presentation/taskbar"
	"kota/internal/presentation/toast"
)

const (
	// Title is the Wails window title (the panel finds its window by it).
	Title         = "Kota"
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

// API connects the services to the taskbar pills and the panel. Its
// exported methods are what the panel page calls (window.go.panel.API).
type API struct {
	ctx     context.Context
	svc     Services
	logos   map[string][]byte
	toaster *toast.Toaster
	pill    *taskbar.Pills
	panel   *panel
}

// NewAPI builds the panel's API; the pills start with the Wails window.
func NewAPI(svc Services, logos map[string][]byte, toaster *toast.Toaster) *API {
	return &API{svc: svc, logos: logos, toaster: toaster, panel: &panel{}}
}

// Hooks are the Wails OnStartup / OnDomReady callbacks.
func Hooks(a *API) (onStartup, onDomReady func(context.Context)) { return a.startup, a.domReady }

func (a *API) startup(ctx context.Context) {
	a.ctx = ctx
	a.panel.onShown = func() {
		runtime.EventsEmit(ctx, eventShown)
		a.svc.Usage.RefreshIfStale(domain.StaleOnOpen)
	}
	p := a.svc.Settings.Prefs()
	o := taskbar.Options{
		Width: domain.ClampWidth(p.CompactWidth), Anchor: p.PillAnchor, Offset0: p.PillOffset == nil,
		Logos: a.logos, Multi: p.AllMonitors,
		OnClick: a.panel.toggle,
		OnMoved: func(anchor string, off int) { // the user dragged a pill
			a.svc.Settings.SetPosition(anchor, &off)
			a.pill.SetPosition(anchor, off) // keep the other monitors in step
		},
		OnMenu: a.onPillMenu,
	}
	if p.PillOffset != nil {
		o.Offset = *p.PillOffset
	}
	a.pill = taskbar.Start(o)
	a.toaster.OnClick = func() { a.panel.show(a.pill.ScreenRect()) } // click on a notification

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

func (a *API) domReady(ctx context.Context) { go a.panel.attach(Title) }

func (a *API) updatePill() {
	if a.pill != nil {
		rows := application.PillRows(a.svc.Usage.Snapshot(), a.svc.Settings.Prefs(), a.svc.Settings.Active(), time.Now())
		a.pill.SetRows(rows)
	}
}

// onPillMenu handles the pill's right-click menu.
func (a *API) onPillMenu(cmd int) {
	switch cmd {
	case taskbar.MenuOpen:
		a.panel.show(a.pill.ScreenRect())
	case taskbar.MenuRefresh:
		a.svc.Usage.Fetch()
	case taskbar.MenuSettings:
		a.panel.show(a.pill.ScreenRect())
		runtime.EventsEmit(a.ctx, eventSettings)
	case taskbar.MenuResetPosition:
		a.ResetPillPosition()
	case taskbar.MenuQuit:
		a.Quit()
	}
}

// ---------- bound to the panel page ----------

// GetUsage returns the latest data (fetching once if there is none yet).
func (a *API) GetUsage() []domain.Usage { return a.svc.Usage.Latest() }

// Refresh fetches fresh data now.
func (a *API) Refresh() []domain.Usage { return a.svc.Usage.Fetch() }

// GetHistory returns the last 7 days of the charted limits per provider.
func (a *API) GetHistory() []domain.ProviderHistory {
	return a.svc.History.Charts(a.svc.Usage.Snapshot())
}

// GetUpdate returns a newer release, or nil.
func (a *API) GetUpdate() *domain.Release { return a.svc.Updates.Current() }

// OpenUpdate opens the newer release's page in the browser.
func (a *API) OpenUpdate() {
	if u := a.svc.Updates.Current(); u != nil {
		runtime.BrowserOpenURL(a.ctx, u.URL)
		a.panel.hide()
	}
}

// SetUpdateCheck turns the daily release check on or off.
func (a *API) SetUpdateCheck(on bool) {
	a.svc.Settings.SetUpdateCheck(on)
	if !on {
		a.svc.Updates.Clear()
	}
}

// GetSettings returns the settings view.
func (a *API) GetSettings() application.Settings { return a.svc.Settings.View() }

// SaveSettings stores the taskbar providers and display mode (the width is
// saved through SetCompactWidth).
func (a *API) SaveSettings(s application.Settings) { a.svc.Settings.SaveDisplay(s.Taskbar, s.Display) }

// SetCompactWidth resizes the taskbar pill (logical px); save=true persists it.
func (a *API) SetCompactWidth(w int, save bool) {
	w = a.svc.Settings.SetWidth(w, save)
	if a.pill != nil {
		a.pill.SetWidth(w)
	}
}

// ResetPillPosition puts the pill back right before the Start button.
func (a *API) ResetPillPosition() {
	a.svc.Settings.SetPosition("", nil)
	if a.pill != nil {
		a.pill.ResetPosition()
	}
}

// SetAutostart turns "Start with Windows" on or off; returns the new state.
func (a *API) SetAutostart(on bool) bool {
	state, err := a.svc.Settings.SetAutostart(on)
	if err != nil {
		storage.Log("autostart: %v", err)
	}
	return state
}

// SetNotifications turns usage notifications on or off.
func (a *API) SetNotifications(on bool) { a.svc.Settings.SetNotifications(on) }

// SetNotifyLevels sets the warning and critical thresholds (% used) and
// returns the values kept.
func (a *API) SetNotifyLevels(warn, critical int) []int {
	w, c := a.svc.Settings.SetNotifyLevels(warn, critical)
	return []int{w, c}
}

// TestNotification shows a sample notification (settings button).
func (a *API) TestNotification() { a.svc.Notifications.Test() }

// SetFollowActive turns follow mode on or off.
func (a *API) SetFollowActive(on bool) { a.svc.Settings.SetFollowActive(on) }

// SetAllMonitors shows the pill on every monitor's taskbar, or only the main one.
func (a *API) SetAllMonitors(on bool) {
	a.svc.Settings.SetAllMonitors(on)
	if a.pill != nil {
		a.pill.SetMulti(on)
	}
}

// HidePanel closes the panel (blur, Esc, collapse button).
func (a *API) HidePanel() { a.panel.hideFromUI() }

func (a *API) Quit() { runtime.Quit(a.ctx) }
