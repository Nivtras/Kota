//go:build windows

package system

// "Start with Windows": a value under the per-user Run key, plus its entry in
// Explorer's StartupApproved list. Nothing is written unless the user turns
// the setting on.
//
// StartupApproved is what Settings → Apps → Startup (and Task Manager) show
// and toggle. Current Windows 11 builds start a Run entry at sign-in only
// when it has an "enabled" record there; an entry without one is neither
// listed nor started. So Kota writes both, and treats a record the user
// switched off in Windows as "off".

import (
	"encoding/binary"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"kota/internal/infrastructure/storage"
)

const (
	runKeyPath      = `Software\Microsoft\Windows\CurrentVersion\Run`
	approvedKeyPath = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
	runValueName    = "Kota"
)

// approval is the StartupApproved record: missing, enabled or disabled.
type approval int

const (
	approvalMissing approval = iota
	approvalEnabled
	approvalDisabled
)

func readApproval() approval {
	k, err := registry.OpenKey(registry.CURRENT_USER, approvedKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return approvalMissing
	}
	defer k.Close()
	b, _, err := k.GetBinaryValue(runValueName)
	if err != nil || len(b) == 0 {
		return approvalMissing
	}
	if b[0]&1 == 1 { // 0x03 / 0x07: switched off by the user
		return approvalDisabled
	}
	return approvalEnabled
}

// writeApproval records Kota as enabled (02 …) or disabled (03 + the time
// it was switched off), the same format Settings writes.
func writeApproval(enabled bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, approvedKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	b := make([]byte, 12)
	b[0] = 0x02
	if !enabled {
		b[0] = 0x03
		ft := windows.NsecToFiletime(time.Now().UnixNano())
		binary.LittleEndian.PutUint32(b[4:], ft.LowDateTime)
		binary.LittleEndian.PutUint32(b[8:], ft.HighDateTime)
	}
	return k.SetBinaryValue(runValueName, b)
}

func deleteApproval() {
	if k, err := registry.OpenKey(registry.CURRENT_USER, approvedKeyPath, registry.SET_VALUE); err == nil {
		_ = k.DeleteValue(runValueName)
		k.Close()
	}
}

func runValue() (string, bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runValueName)
	return v, err == nil
}

// AutostartEnabled: there is a Run entry and it isn't switched off in
// Windows' Startup apps.
func AutostartEnabled() bool {
	_, ok := runValue()
	return ok && readApproval() != approvalDisabled
}

// SetAutostart turns "Start with Windows" on (this exe) or off.
func SetAutostart(on bool) error {
	if !on {
		k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer k.Close()
		if err := k.DeleteValue(runValueName); err != nil && err != registry.ErrNotExist {
			return err
		}
		deleteApproval()
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
	return setRunCommand(cmd, true)
}

// setRunCommand writes the Run entry and, when enable is set (the user asked
// for autostart) or there is no record yet, marks it enabled. A record the
// user switched off in Windows is kept as is otherwise.
func setRunCommand(cmd string, enable bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	err = k.SetStringValue(runValueName, cmd)
	k.Close()
	if err != nil {
		return err
	}
	if enable || readApproval() == approvalMissing {
		return writeApproval(true)
	}
	return nil
}

// RepairAutostart keeps an existing "Start with Windows" entry working:
//   - it points at an exe that no longer exists (the folder was moved or
//     renamed) → point it at this exe;
//   - it has no StartupApproved record (written by older Kota versions, so
//     Windows never started it) → add an "enabled" one.
//
// A switched-off record and an entry for another copy that exists are kept.
func RepairAutostart() {
	val, ok := runValue()
	if !ok {
		return // autostart is off
	}
	target := strings.TrimSpace(val)
	if strings.HasPrefix(target, `"`) {
		target, _, _ = strings.Cut(target[1:], `"`)
	} else {
		target, _, _ = strings.Cut(target, " ")
	}
	if _, err := os.Stat(target); err != nil {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		cmd := `"` + exe + `"`
		if !samePath(exe, installedExe()) {
			cmd += " --portable"
		}
		if setRunCommand(cmd, false) == nil {
			storage.Log("autostart: %s was missing; now points at this exe", target)
		}
		return
	}
	if readApproval() == approvalMissing && writeApproval(true) == nil {
		storage.Log("autostart: added the Startup apps record Windows needs to start Kota")
	}
}

// setAutostartPath points "Start with Windows" at exe (after install).
func setAutostartPath(exe string) error { return setRunCommand(`"`+exe+`"`, false) }

// Autostart is "Start with Windows" as an object (application.Autostart).
type Autostart struct{}

func (Autostart) Enabled() bool     { return AutostartEnabled() }
func (Autostart) Set(on bool) error { return SetAutostart(on) }
