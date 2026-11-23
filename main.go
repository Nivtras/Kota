package main

// The composition root: builds the infrastructure, the application services
// on top of it, and the presentation (taskbar pill + Wails panel).

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"kota/internal/application"
	"kota/internal/infrastructure/github"
	"kota/internal/infrastructure/providers"
	"kota/internal/infrastructure/storage"
	"kota/internal/infrastructure/system"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	system.MigrateOldName()     // AI Usage Widget → Kota
	if system.HandleInstall() { // install / update / uninstall
		return
	}
	system.RepairAutostart() // the exe may have moved since autostart was turned on

	app := NewApp(newServices(), loadLogos())
	// The Wails window is the panel only; the taskbar pill is a separate
	// native window (pill_windows.go).
	err := wails.Run(&options.App{
		Title:            windowTitle,
		Width:            panelW,
		Height:           panelH,
		Frameless:        true,
		AlwaysOnTop:      true,
		StartHidden:      true,
		DisableResize:    true,
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		OnDomReady:       app.domReady,
		Bind:             []interface{}{app},
		// A second launch (double-click, startup + manual) just exits.
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: "kota-5c1f2e"},
		Windows: &windows.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
			BackdropType:         windows.None,
			DisableWindowIcon:    true,
			// The panel is a small static page: software rendering is plenty
			// and saves ~50 MB (measured: 225 → 177 MB private). Wails' Go
			// WebView2 loader ignores WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS,
			// so this is the only Chromium switch available.
			WebviewGpuIsDisabled: true,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}

func newServices() Services {
	presence := system.Presence{}
	settings := application.NewSettingsService(
		storage.NewPreferencesStore(storage.Path("config.json")), system.Autostart{})
	var sources []application.UsageSource
	for _, p := range providers.All() {
		sources = append(sources, p)
	}
	usage := application.NewUsageService(sources, presence)
	notes := application.NewNotificationService(settings,
		storage.NewAlertStore(storage.Path("notify-state.json")), balloons{}, presence)
	hist := application.NewHistoryService(storage.NewHistoryStore(storage.Path("history.json")))
	usage.OnUsage(hist.Record)
	usage.OnUsage(notes.Check)
	return Services{
		Settings:      settings,
		Usage:         usage,
		Notifications: notes,
		History:       hist,
		Updates:       application.NewUpdateService(github.NewReleases(), settings),
	}
}

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
