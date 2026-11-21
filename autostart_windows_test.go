//go:build windows

package main

import (
	"os"
	"testing"
)

// Touches the real HKCU Run key, so it only runs when asked and restores the
// previous state.
func TestAutostartToggle(t *testing.T) {
	if os.Getenv("AUTOSTART_TEST") == "" {
		t.Skip("set AUTOSTART_TEST=1 to run")
	}
	was := autostartEnabled()
	defer setAutostart(was)

	if err := setAutostart(true); err != nil || !autostartEnabled() {
		t.Fatalf("enable: err=%v enabled=%v", err, autostartEnabled())
	}
	if err := setAutostart(false); err != nil || autostartEnabled() {
		t.Fatalf("disable: err=%v enabled=%v", err, autostartEnabled())
	}
	if err := setAutostart(false); err != nil { // disabling twice is fine
		t.Fatalf("disable again: %v", err)
	}
}
