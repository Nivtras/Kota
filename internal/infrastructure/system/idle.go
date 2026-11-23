//go:build windows

package system

import (
	"kota/internal/infrastructure/win32"
	"time"
	"unsafe"
)

var (
	pGetLastInputInfo = win32.User32.NewProc("GetLastInputInfo")
	pGetTickCount     = win32.Kernel32.NewProc("GetTickCount")
)

// userIdle is the time since the last keyboard/mouse input. It keeps
// growing while the session is locked, so a locked PC counts as idle.
func UserIdle() time.Duration {
	var lii struct {
		Size uint32
		Time uint32
	}
	lii.Size = uint32(unsafe.Sizeof(lii))
	if ok, _, _ := pGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii))); ok == 0 {
		return 0
	}
	now, _, _ := pGetTickCount.Call()
	return time.Duration(uint32(now)-lii.Time) * time.Millisecond // uint32 wrap-safe
}

// Presence reports what the user is doing (application.Presence).
type Presence struct{}

func (Presence) Idle() time.Duration { return UserIdle() }
func (Presence) Busy() bool          { return UserBusy() }
