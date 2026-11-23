//go:build windows

package system

// "Start with Windows": a value under the per-user Run key. Nothing is
// written unless the user turns the setting on.

import (
	"kota/internal/infrastructure/storage"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "Kota"
)

func AutostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValueName)
	return err == nil
}

func SetAutostart(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue(runValueName); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := `"` + exe + `"`
	if !samePath(exe, installedExe()) {
		cmd += " --portable" // a copy run from elsewhere: don't ask to install at login
	}
	return k.SetStringValue(runValueName, cmd)
}

// repairAutostart re-points "Start with Windows" at this exe when the entry
// names an exe that no longer exists (the folder was moved or renamed).
// Windows silently skips a Run entry whose file is missing, so without this
// the app would just stop starting at login. An entry pointing at another
// copy that still exists is left alone.
func RepairAutostart() {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	val, _, err := k.GetStringValue(runValueName)
	k.Close()
	if err != nil {
		return // autostart is off
	}
	target := strings.TrimSpace(val)
	if strings.HasPrefix(target, `"`) {
		target, _, _ = strings.Cut(target[1:], `"`)
	} else {
		target, _, _ = strings.Cut(target, " ")
	}
	if _, err := os.Stat(target); err == nil {
		return
	}
	if err := SetAutostart(true); err == nil {
		storage.Log("autostart: %s was missing; now points at this exe", target)
	}
}

// setAutostartPath points "Start with Windows" at exe (after install).
func setAutostartPath(exe string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(runValueName, `"`+exe+`"`)
}

// Autostart is "Start with Windows" as an object (application.Autostart).
type Autostart struct{}

func (Autostart) Enabled() bool     { return AutostartEnabled() }
func (Autostart) Set(on bool) error { return SetAutostart(on) }
