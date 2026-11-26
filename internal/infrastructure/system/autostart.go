//go:build windows

package system

// "Start with Windows": a Kota.lnk shortcut in the user's Startup folder,
// plus its record in Explorer's StartupApproved\StartupFolder list (what
// Settings → Apps → Startup and Task Manager show and toggle). Nothing is
// written unless the user turns the setting on.
//
// Earlier versions used a value under the per-user Run key. On current
// Windows 11 builds Explorer skipped that entry at sign-in even with an
// "enabled" StartupApproved record, while Startup folder shortcuts start
// reliably; RepairAutostart moves an old Run entry over, keeping a
// "switched off in Windows" choice.

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"kota/internal/infrastructure/storage"
)

const (
	startupLinkName   = "Kota.lnk"
	approvedRoot      = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\`
	approvedFolder    = approvedRoot + `StartupFolder`
	legacyRunKeyPath  = `Software\Microsoft\Windows\CurrentVersion\Run`
	legacyApprovedRun = approvedRoot + `Run`
	legacyRunValue    = "Kota"
)

// startupLink is %APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\Kota.lnk.
func startupLink() string {
	return filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs\Startup`, startupLinkName)
}

// approval is a StartupApproved record: missing, enabled or disabled.
type approval int

const (
	approvalMissing approval = iota
	approvalEnabled
	approvalDisabled
)

func readApprovalAt(key, name string) approval {
	k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.QUERY_VALUE)
	if err != nil {
		return approvalMissing
	}
	defer k.Close()
	b, _, err := k.GetBinaryValue(name)
	if err != nil || len(b) == 0 {
		return approvalMissing
	}
	if b[0]&1 == 1 { // 0x03 / 0x07: switched off by the user
		return approvalDisabled
	}
	return approvalEnabled
}

func readApproval() approval { return readApprovalAt(approvedFolder, startupLinkName) }

// writeApproval records Kota as enabled (02 …) or disabled (03 + the time
// it was switched off), the same format Settings writes.
func writeApproval(enabled bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, approvedFolder, registry.SET_VALUE)
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
	return k.SetBinaryValue(startupLinkName, b)
}

func deleteValue(key, name string) {
	if k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.SET_VALUE); err == nil {
		_ = k.DeleteValue(name)
		k.Close()
	}
}

func linkExists() bool {
	_, err := os.Stat(startupLink())
	return err == nil
}

// AutostartEnabled: the Startup shortcut exists and isn't switched off in
// Windows' Startup apps.
func AutostartEnabled() bool {
	return linkExists() && readApproval() != approvalDisabled
}

// SetAutostart turns "Start with Windows" on (this exe) or off.
func SetAutostart(on bool) error {
	removeLegacyRun()
	if !on {
		if err := os.Remove(startupLink()); err != nil && !os.IsNotExist(err) {
			return err
		}
		deleteValue(approvedFolder, startupLinkName)
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return writeLink(exe, true)
}

// writeLink points the Startup shortcut at exe. When enable is set (the user
// asked for autostart) or there is no record yet, it is marked enabled; a
// record the user switched off in Windows is kept as is otherwise.
func writeLink(exe string, enable bool) error {
	args := ""
	if !samePath(exe, installedExe()) {
		args = "--portable" // a copy run from elsewhere: don't ask to install at login
	}
	if err := createShortcut(startupLink(), exe, args); err != nil {
		return err
	}
	if enable || readApproval() == approvalMissing {
		return writeApproval(true)
	}
	return nil
}

func legacyRunValueOf() (string, bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, legacyRunKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(legacyRunValue)
	return v, err == nil
}

func removeLegacyRun() {
	deleteValue(legacyRunKeyPath, legacyRunValue)
	deleteValue(legacyApprovedRun, legacyRunValue)
}

// exeOf is the program in a command line: "C:\x\Kota.exe" --portable → C:\x\Kota.exe.
func exeOf(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, `"`) {
		exe, _, _ := strings.Cut(cmd[1:], `"`)
		return exe
	}
	exe, _, _ := strings.Cut(cmd, " ")
	return exe
}

// RepairAutostart keeps "Start with Windows" working:
//   - an old Run entry becomes the Startup shortcut (switched off stays off);
//   - a shortcut pointing at an exe that no longer exists (the folder was
//     moved or renamed) is pointed at this exe.
//
// A shortcut for another copy that exists is kept.
func RepairAutostart() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	if cmd, ok := legacyRunValueOf(); ok {
		off := readApprovalAt(legacyApprovedRun, legacyRunValue) == approvalDisabled
		target := exeOf(cmd)
		if _, err := os.Stat(target); err != nil {
			target = self
		}
		if err := writeLink(target, false); err != nil {
			storage.Log("autostart: moving the Run entry to the Startup folder failed: %v", err)
			return
		}
		if off {
			_ = writeApproval(false)
		}
		removeLegacyRun()
		storage.Log("autostart: moved to the Startup folder (%s)", target)
		return
	}
	if !linkExists() {
		return // autostart is off
	}
	if target := shortcutTarget(startupLink()); target != "" {
		if _, err := os.Stat(target); err != nil && writeLink(self, false) == nil {
			storage.Log("autostart: %s was missing; now points at this exe", target)
		}
	}
}

// setAutostartPath points "Start with Windows" at exe (after install).
func setAutostartPath(exe string) error { return writeLink(exe, false) }

// Autostart is "Start with Windows" as an object (application.Autostart).
type Autostart struct{}

func (Autostart) Enabled() bool     { return AutostartEnabled() }
func (Autostart) Set(on bool) error { return SetAutostart(on) }
