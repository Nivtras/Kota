//go:build windows

package main

import (
	"testing"
	"time"
)

// Needs a real Windows desktop session with a taskbar.
func TestStartButtonLookup(t *testing.T) {
	w := newUIAWorker()
	t0 := time.Now()
	left, err := w.startLeft()
	if err != nil {
		t.Skipf("no Start button available here: %v", err)
	}
	t.Logf("Start button left edge = %d px (took %v)", left, time.Since(t0))
	if left <= 0 {
		t.Fatalf("unexpected left edge %d", left)
	}
	if _, err := w.startLeft(); err != nil {
		t.Fatalf("second lookup failed: %v", err)
	}
}
