package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	migrateFromOldName() // AI Usage Widget → Kota (install_windows.go)
	if handleInstall() { // install / update / uninstall (install_windows.go)
		return
	}
	app := NewApp()
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
