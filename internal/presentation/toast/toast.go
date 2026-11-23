//go:build windows

// Package toast shows notifications as notification-area balloons, which
// Windows 10/11 shows as regular toast notifications for unpackaged desktop
// apps. The tray icon exists only while a notification is up (removed after
// 30 s), so Kota doesn't add a permanent tray icon.
package toast

import (
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"kota/internal/infrastructure/storage"
	"kota/internal/infrastructure/win32"
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
	msgTray                                     = win32.WmApp + 10
	hwndMessage                                 = ^uintptr(2) // HWND_MESSAGE (-3)
	showFor                                     = 30 * time.Second
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

// Toaster shows balloons from its own hidden message window
// (application.Notifier).
type Toaster struct {
	OnClick func() // the user clicked a notification (own goroutine)

	ready   chan struct{}
	host    uintptr // message-only window receiving the tray callbacks
	mu      sync.Mutex
	added   bool
	removal *time.Timer
	icon    uintptr
}

var theToaster *Toaster // for the window procedure

// New starts the toaster's window thread.
func New() *Toaster {
	t := &Toaster{ready: make(chan struct{})}
	theToaster = t
	go t.loop()
	return t
}

func (t *Toaster) loop() {
	runtime.LockOSThread()
	inst, _, _ := win32.GetModuleHandleW.Call(0)
	cls := win32.UTF16Ptr("KotaToastHost")
	wc := win32.WndClassEx{Size: uint32(unsafe.Sizeof(win32.WndClassEx{})), WndProc: syscall.NewCallback(wndProc), Instance: inst, ClassName: cls}
	win32.RegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	t.host, _, _ = win32.CreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), 0, 0, 0, 0, 0, 0, hwndMessage, 0, inst, 0)
	close(t.ready)
	if t.host == 0 {
		storage.Log("toast: no message window")
		return
	}
	var m win32.Msg
	for {
		if r, _, _ := win32.GetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0); int32(r) <= 0 {
			return
		}
		win32.TranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		win32.DispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func wndProc(h, msg, wp, lp uintptr) uintptr {
	// With NOTIFYICON_VERSION_4 the tray event is in the low word of lParam.
	if uint32(msg) == msgTray && lp&0xFFFF == ninBalloonUserClick && theToaster != nil && theToaster.OnClick != nil {
		go theToaster.OnClick()
		return 0
	}
	r, _, _ := win32.DefWindowProcW.Call(h, msg, wp, lp)
	return r
}

func copyUTF16(dst []uint16, s string) {
	u, _ := windows.UTF16FromString(s)
	if len(u) > len(dst) {
		u = append(u[:len(dst)-1], 0)
	}
	copy(dst, u)
}

// Notify displays a notification.
func (t *Toaster) Notify(title, body string) {
	<-t.ready
	if t.host == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.icon == 0 {
		if exe, err := os.Executable(); err == nil {
			t.icon, _, _ = pExtractIconW.Call(0, uintptr(unsafe.Pointer(win32.UTF16Ptr(exe))), 0)
		}
	}
	nid := notifyIconData{Size: uint32(unsafe.Sizeof(notifyIconData{})), Wnd: t.host, ID: trayIconID}
	if !t.added {
		nid.Flags = nifMessage | nifIcon | nifTip
		nid.CallbackMsg = msgTray
		nid.Icon = t.icon
		copyUTF16(nid.Tip[:], "Kota")
		if ok, _, _ := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); ok == 0 {
			storage.Log("notify: NIM_ADD failed")
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
	t.removal = time.AfterFunc(showFor, t.remove)
}

func (t *Toaster) remove() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.added {
		return
	}
	nid := notifyIconData{Size: uint32(unsafe.Sizeof(notifyIconData{})), Wnd: t.host, ID: trayIconID}
	pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	t.added = false
}
