//go:build windows

package system

import (
	"os"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestExeOf(t *testing.T) {
	for in, want := range map[string]string{
		`"C:\a b\Kota.exe" --portable`: `C:\a b\Kota.exe`,
		`"C:\a\Kota.exe"`:              `C:\a\Kota.exe`,
		`C:\a\Kota.exe --portable`:     `C:\a\Kota.exe`,
	} {
		if got := exeOf(in); got != want {
			t.Errorf("exeOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// snapshot saves Kota's Startup shortcut, its record and any old Run entry,
// and returns a func that puts them back exactly.
func snapshot(t *testing.T) func() {
	lnk, lnkErr := os.ReadFile(startupLink())
	readBin := func(key, name string) ([]byte, bool) {
		k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.QUERY_VALUE)
		if err != nil {
			return nil, false
		}
		defer k.Close()
		b, _, err := k.GetBinaryValue(name)
		return b, err == nil
	}
	appr, hadAppr := readBin(approvedFolder, startupLinkName)
	run, hadRun := legacyRunValueOf()
	runAppr, hadRunAppr := readBin(legacyApprovedRun, legacyRunValue)
	putBin := func(key, name string, b []byte, had bool) {
		k, _, _ := registry.CreateKey(registry.CURRENT_USER, key, registry.SET_VALUE)
		if had {
			_ = k.SetBinaryValue(name, b)
		} else {
			_ = k.DeleteValue(name)
		}
		k.Close()
	}
	return func() {
		if lnkErr == nil {
			_ = os.WriteFile(startupLink(), lnk, 0o644)
		} else {
			_ = os.Remove(startupLink())
		}
		putBin(approvedFolder, startupLinkName, appr, hadAppr)
		k, _ := registry.OpenKey(registry.CURRENT_USER, legacyRunKeyPath, registry.SET_VALUE)
		if hadRun {
			_ = k.SetStringValue(legacyRunValue, run)
		} else {
			_ = k.DeleteValue(legacyRunValue)
		}
		k.Close()
		putBin(legacyApprovedRun, legacyRunValue, runAppr, hadRunAppr)
	}
}

// Touches the real Startup folder and HKCU keys, so it only runs when asked,
// and restores everything exactly afterwards.
func TestAutostartToggle(t *testing.T) {
	if os.Getenv("AUTOSTART_TEST") == "" {
		t.Skip("set AUTOSTART_TEST=1 to run")
	}
	defer snapshot(t)()
	self, _ := os.Executable()

	if err := SetAutostart(true); err != nil || !AutostartEnabled() || readApproval() != approvalEnabled {
		t.Fatalf("enable: err=%v enabled=%v approval=%v", err, AutostartEnabled(), readApproval())
	}
	if got := shortcutTarget(startupLink()); !samePath(got, self) {
		t.Fatalf("shortcut points at %q, want %q", got, self)
	}
	// switched off in Windows' Startup apps → reported as off, and a repair keeps it off
	if err := writeApproval(false); err != nil || AutostartEnabled() {
		t.Fatalf("disabled in Windows should read as off (err=%v)", err)
	}
	RepairAutostart()
	if readApproval() != approvalDisabled {
		t.Fatal("repair must not re-enable what the user switched off")
	}
	if err := SetAutostart(false); err != nil || AutostartEnabled() || linkExists() || readApproval() != approvalMissing {
		t.Fatalf("disable: err=%v enabled=%v approval=%v", err, AutostartEnabled(), readApproval())
	}
	if err := SetAutostart(false); err != nil { // disabling twice is fine
		t.Fatalf("disable again: %v", err)
	}

	// an old Run entry (switched off in Windows) moves to the Startup folder, still off
	k, _ := registry.OpenKey(registry.CURRENT_USER, legacyRunKeyPath, registry.SET_VALUE)
	_ = k.SetStringValue(legacyRunValue, `"`+self+`" --portable`)
	k.Close()
	a, _, _ := registry.CreateKey(registry.CURRENT_USER, legacyApprovedRun, registry.SET_VALUE)
	_ = a.SetBinaryValue(legacyRunValue, []byte{3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	a.Close()
	RepairAutostart()
	if _, ok := legacyRunValueOf(); ok || !linkExists() || readApproval() != approvalDisabled {
		t.Fatalf("migration: run left=%v link=%v approval=%v", ok, linkExists(), readApproval())
	}
}
