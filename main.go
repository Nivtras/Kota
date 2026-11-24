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
	"kota/internal/buildinfo"
	"kota/internal/i18n"
	"kota/internal/infrastructure/github"
	"kota/internal/infrastructure/providers"
	"kota/internal/infrastructure/storage"
	"kota/internal/infrastructure/system"
	"kota/internal/presentation/panel"
	"kota/internal/presentation/toast"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	storage.StartCrashLog(buildinfo.Version) // crash.log in %APPDATA%\Kota
	application.OnPanic = storage.LogPanic
	system.MigrateOldName() // AI Usage Widget → Kota
	// The language first: the install prompts are already translated.
	prefs := storage.NewPreferencesStore(storage.Path("config.json")).Load()
	i18n.Set(i18n.Resolve(prefs.Language, system.UILanguage()))
	if system.HandleInstall() { // install / update / uninstall
		return
	}
	system.RepairAutostart() // the exe may have moved since autostart was turned on

	toaster := toast.New()
	api := panel.NewAPI(newServices(toaster), loadLogos(), toaster)
	onStartup, onDomReady := panel.Hooks(api)
	// The Wails window is the panel only; the taskbar pill is a separate
	// native window (internal/presentation/taskbar).
	err := wails.Run(&options.App{
		Title:            panel.Title,
		Width:            panel.Width,
		Height:           panel.Height,
		Frameless:        true,
		AlwaysOnTop:      true,
		StartHidden:      true,
		DisableResize:    true,
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        onStartup,
		OnDomReady:       onDomReady,
		Bind:             []interface{}{api},
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

func newServices(notifier application.Notifier) panel.Services {
	presence := system.Presence{}
	settings := application.NewSettingsService(
		storage.NewPreferencesStore(storage.Path("config.json")), system.Autostart{})
	// Asked on every fetch, so a provider appears as soon as it is set up
	// (e.g. an API key was added) and disappears when it goes away.
	sources := func() []application.UsageSource {
		var out []application.UsageSource
		for _, p := range providers.All() {
			out = append(out, p)
		}
		return out
	}
	usage := application.NewUsageService(sources, presence)
	notes := application.NewNotificationService(settings,
		storage.NewAlertStore(storage.Path("notify-state.json")), notifier, presence)
	hist := application.NewHistoryService(storage.NewHistoryStore(storage.Path("history.json")))
	usage.OnUsage(hist.Record)
	usage.OnUsage(notes.Check)
	return panel.Services{
		Settings:      settings,
		Usage:         usage,
		Notifications: notes,
		History:       hist,
		Updates:       application.NewUpdateService(github.NewReleases(), settings),
		Credentials:   application.NewCredentialService(storage.Secrets{}, keyProviders),
	}
}

// keyProviders are the providers that work with a key the user enters.
var keyProviders = []application.KeyDef{
	{ID: "openrouter", Name: "OpenRouter", Secret: providers.OpenRouterSecret,
		Hint: "An API key from openrouter.ai → Keys (starts with sk-or-).", URL: "https://openrouter.ai/settings/keys"},
	{ID: "copilot", Name: "GitHub Copilot", Secret: providers.CopilotSecret,
		Hint: "A GitHub token: the output of `gh auth token`, or a token with the read:user scope. Not needed if the GitHub CLI is signed in.",
		URL:  "https://github.com/settings/tokens/new?scopes=read:user&description=Kota"},
}

// loadLogos reads the provider icons embedded with the frontend.
func loadLogos() map[string][]byte {
	out := map[string][]byte{}
	for _, id := range []string{"claude", "codex", "antigravity", "gemini", "openrouter", "copilot", "cursor"} {
		if b, err := assets.ReadFile("frontend/dist/logos/" + id + ".png"); err == nil {
			out[id] = b
		}
	}
	return out
}
