//go:build windows

package toast

import (
	"os"
	"testing"
	"time"
)

// Shows one real notification. Run with KOTA_TOAST_DIAG=1.
func TestToastDiag(t *testing.T) {
	if os.Getenv("KOTA_TOAST_DIAG") == "" {
		t.Skip("set KOTA_TOAST_DIAG=1")
	}
	tt := New()
	tt.Notify("Kota", "Test notification from the new toast window.")
	time.Sleep(time.Second)
	tt.mu.Lock()
	added := tt.added
	tt.mu.Unlock()
	if tt.host == 0 || !added {
		t.Fatalf("host=%x added=%v", tt.host, added)
	}
	t.Logf("host window %x, tray icon added", tt.host)
	tt.remove()
}
