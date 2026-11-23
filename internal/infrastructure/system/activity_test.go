//go:build windows

package system

import (
	"kota/internal/infrastructure/win32"
	"os"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Diagnostic: what follow mode sees on this machine right now.
func TestActiveToolDiag(t *testing.T) {
	if os.Getenv("ACTIVE_DIAG") == "" {
		t.Skip("set ACTIVE_DIAG=1")
	}
	if v := os.Getenv("HOST_PID"); v != "" { // a host pid with a CLI underneath
		var hp uint32
		for _, c := range v {
			hp = hp*10 + uint32(c-'0')
		}
		t.Logf("cliUnder(%d) = %q", hp, cliUnder(hp, ""))
	}
	fg, _, _ := win32.GetForegroundWindow.Call()
	var pid uint32
	win32.GetWindowThreadProcessId.Call(fg, uintptr(unsafe.Pointer(&pid)))
	name := exeName(pid)
	t.Logf("foreground: %s (pid %d) → tool %q", name, pid, toolExes[name])

	snap, _ := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		n := strings.ToLower(windows.UTF16ToString(e.ExeFile[:]))
		if cliHosts[n] {
			if id := cliUnder(e.ProcessID, ""); id != "" {
				t.Logf("host %s (pid %d) runs a CLI → %s", n, e.ProcessID, id)
			}
		}
	}
}
