//go:build windows

package main

// Win32 bindings shared by the taskbar pill and the panel.

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	msimg32  = windows.NewLazySystemDLL("msimg32.dll")

	pFindWindowW                = user32.NewProc("FindWindowW")
	pFindWindowExW              = user32.NewProc("FindWindowExW")
	pGetWindowRect              = user32.NewProc("GetWindowRect")
	pSetWindowPos               = user32.NewProc("SetWindowPos")
	pSetWindowRgn               = user32.NewProc("SetWindowRgn")
	pSetParent                  = user32.NewProc("SetParent")
	pGetWindowLongPtrW          = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtrW          = user32.NewProc("SetWindowLongPtrW")
	pGetDpiForWindow            = user32.NewProc("GetDpiForWindow")
	pShowWindow                 = user32.NewProc("ShowWindow")
	pIsWindow                   = user32.NewProc("IsWindow")
	pIsWindowVisible            = user32.NewProc("IsWindowVisible")
	pIsChild                    = user32.NewProc("IsChild")
	pSetForegroundWindow        = user32.NewProc("SetForegroundWindow")
	pGetForegroundWindow        = user32.NewProc("GetForegroundWindow")
	pBringWindowToTop           = user32.NewProc("BringWindowToTop")
	pGetWindowThreadProcessId   = user32.NewProc("GetWindowThreadProcessId")
	pAttachThreadInput          = user32.NewProc("AttachThreadInput")
	pGetAsyncKeyState           = user32.NewProc("GetAsyncKeyState")
	pKeybdEvent                 = user32.NewProc("keybd_event")
	pGetCursorPos               = user32.NewProc("GetCursorPos")
	pGetSystemMetrics           = user32.NewProc("GetSystemMetrics")
	pRegisterClassExW           = user32.NewProc("RegisterClassExW")
	pCreateWindowExW            = user32.NewProc("CreateWindowExW")
	pDestroyWindow              = user32.NewProc("DestroyWindow")
	pDefWindowProcW             = user32.NewProc("DefWindowProcW")
	pGetMessageW                = user32.NewProc("GetMessageW")
	pTranslateMessage           = user32.NewProc("TranslateMessage")
	pDispatchMessageW           = user32.NewProc("DispatchMessageW")
	pPostMessageW               = user32.NewProc("PostMessageW")
	pRegisterWindowMessageW     = user32.NewProc("RegisterWindowMessageW")
	pInvalidateRect             = user32.NewProc("InvalidateRect")
	pBeginPaint                 = user32.NewProc("BeginPaint")
	pEndPaint                   = user32.NewProc("EndPaint")
	pSetTimer                   = user32.NewProc("SetTimer")
	pKillTimer                  = user32.NewProc("KillTimer")
	pLoadCursorW                = user32.NewProc("LoadCursorW")
	pSetCursor                  = user32.NewProc("SetCursor")
	pSetCapture                 = user32.NewProc("SetCapture")
	pReleaseCapture             = user32.NewProc("ReleaseCapture")
	pTrackMouseEvent            = user32.NewProc("TrackMouseEvent")
	pDrawTextW                  = user32.NewProc("DrawTextW")
	pFillRect                   = user32.NewProc("FillRect")
	pSetWinEventHook            = user32.NewProc("SetWinEventHook")
	pUnhookWinEvent             = user32.NewProc("UnhookWinEvent")
	pSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	pUpdateLayeredWindow        = user32.NewProc("UpdateLayeredWindow")
	pGetDC                      = user32.NewProc("GetDC")
	pReleaseDC                  = user32.NewProc("ReleaseDC")

	pCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pCreateDIBSection       = gdi32.NewProc("CreateDIBSection")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pDeleteDC               = gdi32.NewProc("DeleteDC")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pBitBlt                 = gdi32.NewProc("BitBlt")
	pCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	pCreatePen              = gdi32.NewProc("CreatePen")
	pRoundRect              = gdi32.NewProc("RoundRect")
	pCreateFontW            = gdi32.NewProc("CreateFontW")
	pSetTextColor           = gdi32.NewProc("SetTextColor")
	pSetBkMode              = gdi32.NewProc("SetBkMode")
	pGetTextExtentPoint32W  = gdi32.NewProc("GetTextExtentPoint32W")
	pGetStockObject         = gdi32.NewProc("GetStockObject")
	pCreateRoundRectRgn     = gdi32.NewProc("CreateRoundRectRgn")

	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	pAlphaBlend       = msimg32.NewProc("AlphaBlend")
)

const (
	gwlStyle   = ^uintptr(15) // -16
	gwlExStyle = ^uintptr(19) // -20

	wsChild        = 0x40000000
	wsPopup        = 0x80000000
	wsVisible      = 0x10000000
	wsClipSiblings = 0x04000000
	wsExToolWindow = 0x00000080
	wsExAppWindow  = 0x00040000
	wsExNoActivate = 0x08000000
	wsExLayered    = 0x00080000
	lwaAlpha       = 0x2

	swpNoSize       = 0x0001
	swpNoMove       = 0x0002
	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020
	swpShowWindow   = 0x0040
	swHide          = 0
	swShowNoActive  = 4
	hwndTop         = 0
	hwndTopmost     = ^uintptr(0) // -1

	wmDestroy        = 0x0002
	wmPaint          = 0x000F
	wmEraseBkgnd     = 0x0014
	wmSetCursor      = 0x0020
	wmMouseActivate  = 0x0021
	wmTimer          = 0x0113
	wmMouseMove      = 0x0200
	wmLButtonDown    = 0x0201
	wmLButtonUp      = 0x0202
	wmCaptureChanged = 0x0215
	wmMouseLeave     = 0x02A3
	wmDisplayChange  = 0x007E
	wmSettingChange  = 0x001A
	wmApp            = 0x8000
	maNoActivate     = 3
	tmeLeave         = 0x00000002
	idcHand          = 32649
	eventLocationChg = 0x800B
)

type rect struct{ L, T, R, B int32 }

func (r rect) has(x, y int32) bool { return x >= r.L && x < r.R && y >= r.T && y < r.B }
func (r rect) w() int32            { return r.R - r.L }
func (r rect) h() int32            { return r.B - r.T }

type point struct{ X, Y int32 }

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

type paintStruct struct {
	Hdc       uintptr
	Erase     int32
	RcPaint   rect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

type trackMouseEvent struct {
	Size      uint32
	Flags     uint32
	HwndTrack uintptr
	HoverTime uint32
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

func utf16(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }

func findWindow(class, title string) uintptr {
	var c, t *uint16
	if class != "" {
		c = utf16(class)
	}
	if title != "" {
		t = utf16(title)
	}
	h, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(t)))
	return h
}

func windowRect(h uintptr) (rect, bool) {
	var r rect
	ok, _, _ := pGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
	return r, ok != 0
}

func dpiScale(h uintptr) float64 {
	dpi, _, _ := pGetDpiForWindow.Call(h)
	if dpi == 0 {
		return 1
	}
	return float64(dpi) / 96
}

func colorRef(c rgb) uintptr { return uintptr(c.R) | uintptr(c.G)<<8 | uintptr(c.B)<<16 }

// forceForeground gives a window keyboard focus. The click that triggers it
// lands on our own pill window, so Windows normally allows it. If the
// foreground lock still refuses, a synthetic Alt tap (the well-known
// workaround) makes the next SetForegroundWindow succeed. AttachThreadInput
// is deliberately not used: the pill shares Explorer's input queue, and
// attaching more threads to it broke later clicks.
func forceForeground(h uintptr) {
	pBringWindowToTop.Call(h)
	pSetForegroundWindow.Call(h)
	if fg, _, _ := pGetForegroundWindow.Call(); fg == h {
		return
	}
	const vkMenu, keyUp = 0x12, 0x0002
	pKeybdEvent.Call(vkMenu, 0, 0, 0)
	pSetForegroundWindow.Call(h)
	pKeybdEvent.Call(vkMenu, 0, keyUp, 0)
}
