//go:build windows

package taskbar

// Pill colours follow the taskbar's theme (Settings → Personalization →
// Colors → "Choose your default Windows mode").

import (
	"sync/atomic"

	"kota/internal/infrastructure/system"
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
	light := system.TaskbarUsesLightTheme()
	return lightTaskbar.Swap(light) != light
}

func currentTheme() pillTheme {
	if lightTaskbar.Load() {
		return lightTheme
	}
	return darkTheme
}
