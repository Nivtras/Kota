package main

import (
	"embed"
	"os"
	"os/exec"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

const relaunchFlag = "--relaunch"

func main() {
	if len(os.Args) > 1 && os.Args[1] == relaunchFlag {
		waitForTaskbar()
	}

	app := NewApp()
	err := wails.Run(&options.App{
		Title:            windowTitle,
		Width:            defaultCompactW,
		Height:           compactH,
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
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: "aiusagewidget-5c1f2e"},
		Windows: &windows.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
			BackdropType:         windows.None,
			DisableWindowIcon:    true,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}

	// The pill lives inside the taskbar window, so an Explorer restart
	// destroys it. Unless the user quit, start again once the taskbar is back.
	if !app.quitRequested.Load() {
		relaunchSelf()
	}
}

// relaunchSelf starts a new copy that waits for the taskbar before showing.
// The caller exits right after, releasing the single-instance lock well
// before the new copy (which first waits for the taskbar) takes it.
func relaunchSelf() {
	if exe, err := os.Executable(); err == nil {
		_ = exec.Command(exe, relaunchFlag).Start()
	}
}

// waitForTaskbar blocks until Explorer's taskbar exists again (max ~2 min).
func waitForTaskbar() {
	time.Sleep(2 * time.Second)
	for i := 0; i < 120 && findWindow("Shell_TrayWnd", "") == 0; i++ {
		time.Sleep(time.Second)
	}
	time.Sleep(2 * time.Second) // let the taskbar finish laying out
}
