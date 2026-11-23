//go:build windows

package system

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
	was := AutostartEnabled()
	defer SetAutostart(was)

	if err := SetAutostart(true); err != nil || !AutostartEnabled() {
		t.Fatalf("enable: err=%v enabled=%v", err, AutostartEnabled())
	}
	if err := SetAutostart(false); err != nil || AutostartEnabled() {
		t.Fatalf("disable: err=%v enabled=%v", err, AutostartEnabled())
	}
	if err := SetAutostart(false); err != nil { // disabling twice is fine
		t.Fatalf("disable again: %v", err)
	}
}
