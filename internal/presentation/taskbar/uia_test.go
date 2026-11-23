//go:build windows

package taskbar

import (
	"testing"
	"time"
)

// Needs a real Windows desktop session with a taskbar.
func TestStartButtonLookup(t *testing.T) {
	w := newUIAWorker()
	t0 := time.Now()
	left, err := w.startLeft(0)
	if err != nil {
		t.Skipf("no Start button available here: %v", err)
	}
	t.Logf("Start button left edge = %d px (took %v)", left, time.Since(t0))
	if left <= 0 {
		t.Fatalf("unexpected left edge %d", left)
	}
	if _, err := w.startLeft(0); err != nil {
		t.Fatalf("second lookup failed: %v", err)
	}
}
