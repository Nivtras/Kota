//go:build windows

package system

import (
	"os"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// snapshot saves Kota's Run and StartupApproved values and returns a func
// that puts them back exactly (or deletes them if they didn't exist).
func snapshot(t *testing.T) func() {
	run, hadRun := runValue()
	var appr []byte
	hadAppr := false
	if k, err := registry.OpenKey(registry.CURRENT_USER, approvedKeyPath, registry.QUERY_VALUE); err == nil {
		appr, _, err = k.GetBinaryValue(runValueName)
		hadAppr = err == nil
		k.Close()
	}
	return func() {
		k, _ := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
		if hadRun {
			_ = k.SetStringValue(runValueName, run)
		} else {
			_ = k.DeleteValue(runValueName)
		}
		k.Close()
		a, _, _ := registry.CreateKey(registry.CURRENT_USER, approvedKeyPath, registry.SET_VALUE)
		if hadAppr {
			_ = a.SetBinaryValue(runValueName, appr)
		} else {
			_ = a.DeleteValue(runValueName)
		}
		a.Close()
	}
}

// Touches the real HKCU keys, so it only runs when asked, and restores both
// values exactly afterwards.
func TestAutostartToggle(t *testing.T) {
	if os.Getenv("AUTOSTART_TEST") == "" {
		t.Skip("set AUTOSTART_TEST=1 to run")
	}
	defer snapshot(t)()

	if err := SetAutostart(true); err != nil || !AutostartEnabled() || readApproval() != approvalEnabled {
		t.Fatalf("enable: err=%v enabled=%v approval=%v", err, AutostartEnabled(), readApproval())
	}
	// switched off in Windows' Startup apps → reported as off, and a repair keeps it off
	if err := writeApproval(false); err != nil || AutostartEnabled() {
		t.Fatalf("disabled in Windows should read as off (err=%v)", err)
	}
	RepairAutostart()
	if readApproval() != approvalDisabled {
		t.Fatal("repair must not re-enable what the user switched off")
	}
	// an entry without a record (older Kota) gets one on repair
	deleteApproval()
	RepairAutostart()
	if readApproval() != approvalEnabled {
		t.Fatal("repair should add the missing record")
	}
	if err := SetAutostart(false); err != nil || AutostartEnabled() || readApproval() != approvalMissing {
		t.Fatalf("disable: err=%v enabled=%v approval=%v", err, AutostartEnabled(), readApproval())
	}
	if err := SetAutostart(false); err != nil { // disabling twice is fine
		t.Fatalf("disable again: %v", err)
	}
}
