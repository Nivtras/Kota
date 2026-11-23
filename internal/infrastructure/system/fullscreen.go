//go:build windows

package system

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var pSHQueryUserNotificationState = windows.NewLazySystemDLL("shell32.dll").NewProc("SHQueryUserNotificationState")

// UserBusy reports fullscreen / presentation / game mode
// (QUNS_BUSY, QUNS_RUNNING_D3D_FULL_SCREEN, QUNS_PRESENTATION_MODE).
func UserBusy() bool {
	var state int32
	if hr, _, _ := pSHQueryUserNotificationState.Call(uintptr(unsafe.Pointer(&state))); hr != 0 {
		return false
	}
	return state == 2 || state == 3 || state == 4
}
