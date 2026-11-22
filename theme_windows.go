//go:build windows

package main

// Pill colours follow the taskbar's theme (Settings → Personalization →
// Colors → "Choose your default Windows mode").

import (
	"os"
	"sync/atomic"

	"golang.org/x/sys/windows/registry"
)

type pillTheme struct {
	Bg, Border, BorderHi, Track, Text, Muted, Marker rgb
}

var (
	darkTheme = pillTheme{
		Bg: rgb{28, 28, 31}, Border: rgb{52, 52, 56}, BorderHi: rgb{92, 92, 98}, Track: rgb{54, 54, 58},
		Text: rgb{236, 236, 239}, Muted: rgb{142, 142, 150}, Marker: rgb{210, 210, 216},
	}
	lightTheme = pillTheme{
		Bg: rgb{251, 251, 252}, Border: rgb{214, 214, 220}, BorderHi: rgb{150, 150, 160}, Track: rgb{222, 222, 228},
		Text: rgb{26, 26, 30}, Muted: rgb{96, 96, 106}, Marker: rgb{60, 60, 70},
	}
	lightTaskbar atomic.Bool
)

// refreshTheme re-reads the taskbar theme; reports whether it changed.
func refreshTheme() bool {
	light := false
	if v := os.Getenv("AIW_THEME"); v == "light" || v == "dark" { // testing override
		return lightTaskbar.Swap(v == "light") != (v == "light")
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE); err == nil {
		if v, _, err := k.GetIntegerValue("SystemUsesLightTheme"); err == nil {
			light = v == 1
		}
		k.Close()
	}
	return lightTaskbar.Swap(light) != light
}

func currentTheme() pillTheme {
	if lightTaskbar.Load() {
		return lightTheme
	}
	return darkTheme
}
