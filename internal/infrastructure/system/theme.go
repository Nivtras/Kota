//go:build windows

package system

import (
	"os"

	"golang.org/x/sys/windows/registry"
)

// TaskbarUsesLightTheme reads Settings → Personalization → Colors →
// "Choose your default Windows mode". KOTA_THEME=light|dark overrides it
// (for testing).
func TaskbarUsesLightTheme() bool {
	if v := os.Getenv("KOTA_THEME"); v == "light" || v == "dark" {
		return v == "light"
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("SystemUsesLightTheme")
	return err == nil && v == 1
}
