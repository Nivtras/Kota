//go:build windows

package main

// Delivers notifications as notification-area balloons, which Windows 10/11
// shows as regular toast notifications for unpackaged desktop apps. The
// tray icon exists only while a notification is up (removed after 30 s), so
// the widget doesn't add a permanent tray icon.

import (
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32           = windows.NewLazySystemDLL("shell32.dll")
	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	pExtractIconW     = shell32.NewProc("ExtractIconW")
)

const (
	nimAdd, nimModify, nimDelete, nimSetVersion = 0, 1, 2, 4
	nifMessage, nifIcon, nifTip, nifInfo        = 0x1, 0x2, 0x4, 0x10
	niifUser, niifLargeIcon, niifRespectQuiet   = 0x4, 0x20, 0x80
	notifyVersion4                              = 4
	ninBalloonUserClick                         = 0x0405 // WM_USER + 5
	trayIconID                                  = 1
	msgTray                                     = wmApp + 10
)

type notifyIconData struct {
	Size           uint32
	Wnd            uintptr
	ID             uint32
	Flags          uint32
	CallbackMsg    uint32
	Icon           uintptr
	Tip            [128]uint16
	State          uint32
	StateMask      uint32
	Info           [256]uint16
	TimeoutVersion uint32
	InfoTitle      [64]uint16
	InfoFlags      uint32
	GUIDItem       windows.GUID
	BalloonIcon    uintptr
}

type toaster struct {
	mu      sync.Mutex
	added   bool
	removal *time.Timer
	icon    uintptr
	onClick func()
}

var theToaster = &toaster{}

func copyUTF16(dst []uint16, s string) {
	u, _ := windows.UTF16FromString(s)
	if len(u) > len(dst) {
		u = append(u[:len(dst)-1], 0)
	}
	copy(dst, u)
}

// show displays a notification. host is a window that receives the tray
// callback messages (the pill's hidden host window).
func (t *toaster) show(host uintptr, title, body string) {
	if host == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.icon == 0 {
		if exe, err := os.Executable(); err == nil {
			t.icon, _, _ = pExtractIconW.Call(0, uintptr(unsafe.Pointer(utf16(exe))), 0)
		}
	}
	nid := notifyIconData{Size: uint32(unsafe.Sizeof(notifyIconData{})), Wnd: host, ID: trayIconID}
	if !t.added {
		nid.Flags = nifMessage | nifIcon | nifTip
		nid.CallbackMsg = msgTray
		nid.Icon = t.icon
		copyUTF16(nid.Tip[:], "AI Usage")
		if ok, _, _ := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); ok == 0 {
			dockLog("notify: NIM_ADD failed")
			return
		}
		nid.TimeoutVersion = notifyVersion4
		pShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(&nid)))
		t.added = true
	}
	nid.Flags = nifInfo
	copyUTF16(nid.InfoTitle[:], title)
	copyUTF16(nid.Info[:], body)
	nid.InfoFlags = niifUser | niifLargeIcon | niifRespectQuiet
	nid.BalloonIcon = t.icon
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))

	if t.removal != nil {
		t.removal.Stop()
	}
	t.removal = time.AfterFunc(30*time.Second, func() { t.remove(host) })
}

func (t *toaster) remove(host uintptr) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.added {
		return
	}
	nid := notifyIconData{Size: uint32(unsafe.Sizeof(notifyIconData{})), Wnd: host, ID: trayIconID}
	pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	t.added = false
}

// trayMessage handles the tray icon's callback (pill host window proc).
// With NOTIFYICON_VERSION_4 the event is in the low word of lParam.
func (t *toaster) trayMessage(lp uintptr) {
	if lp&0xFFFF == ninBalloonUserClick && t.onClick != nil {
		go t.onClick()
	}
}

// notifyUser shows a notification (no-op until the pill window exists).
func notifyUser(title, body string) {
	if thePill == nil {
		return
	}
	thePill.mu.Lock()
	host := thePill.host
	thePill.mu.Unlock()
	theToaster.show(host, title, body)
}
