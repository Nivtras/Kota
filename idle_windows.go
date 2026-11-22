//go:build windows

package main

import (
	"time"
	"unsafe"
)

var (
	pGetLastInputInfo = user32.NewProc("GetLastInputInfo")
	pGetTickCount     = kernel32.NewProc("GetTickCount")
)

// userIdle is the time since the last keyboard/mouse input. It keeps
// growing while the session is locked, so a locked PC counts as idle.
func userIdle() time.Duration {
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
