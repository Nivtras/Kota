//go:build windows

package win32

// Package win32 holds the Win32 and COM bindings shared by Kota's taskbar
// pill, panel and system integration.

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	User32   = windows.NewLazySystemDLL("user32.dll")
	Gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	Kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	Msimg32  = windows.NewLazySystemDLL("msimg32.dll")

	FindWindowW                = User32.NewProc("FindWindowW")
	FindWindowExW              = User32.NewProc("FindWindowExW")
	GetWindowRect              = User32.NewProc("GetWindowRect")
	SetWindowPos               = User32.NewProc("SetWindowPos")
	SetWindowRgn               = User32.NewProc("SetWindowRgn")
	SetParent                  = User32.NewProc("SetParent")
	GetWindowLongPtrW          = User32.NewProc("GetWindowLongPtrW")
	SetWindowLongPtrW          = User32.NewProc("SetWindowLongPtrW")
	GetDpiForWindow            = User32.NewProc("GetDpiForWindow")
	ShowWindow                 = User32.NewProc("ShowWindow")
	IsWindow                   = User32.NewProc("IsWindow")
	IsChild                    = User32.NewProc("IsChild")
	SetForegroundWindow        = User32.NewProc("SetForegroundWindow")
	GetForegroundWindow        = User32.NewProc("GetForegroundWindow")
	BringWindowToTop           = User32.NewProc("BringWindowToTop")
	GetWindowThreadProcessId   = User32.NewProc("GetWindowThreadProcessId")
	GetAsyncKeyState           = User32.NewProc("GetAsyncKeyState")
	KeybdEvent                 = User32.NewProc("keybd_event")
	GetCursorPos               = User32.NewProc("GetCursorPos")
	GetSystemMetrics           = User32.NewProc("GetSystemMetrics")
	RegisterClassExW           = User32.NewProc("RegisterClassExW")
	CreateWindowExW            = User32.NewProc("CreateWindowExW")
	DestroyWindow              = User32.NewProc("DestroyWindow")
	DefWindowProcW             = User32.NewProc("DefWindowProcW")
	GetMessageW                = User32.NewProc("GetMessageW")
	PeekMessageW               = User32.NewProc("PeekMessageW")
	TranslateMessage           = User32.NewProc("TranslateMessage")
	DispatchMessageW           = User32.NewProc("DispatchMessageW")
	PostMessageW               = User32.NewProc("PostMessageW")
	PostQuitMessage            = User32.NewProc("PostQuitMessage")
	RegisterWindowMessageW     = User32.NewProc("RegisterWindowMessageW")
	InvalidateRect             = User32.NewProc("InvalidateRect")
	BeginPaint                 = User32.NewProc("BeginPaint")
	EndPaint                   = User32.NewProc("EndPaint")
	SetTimer                   = User32.NewProc("SetTimer")
	KillTimer                  = User32.NewProc("KillTimer")
	LoadCursorW                = User32.NewProc("LoadCursorW")
	SetCapture                 = User32.NewProc("SetCapture")
	ReleaseCapture             = User32.NewProc("ReleaseCapture")
	TrackMouseEvent            = User32.NewProc("TrackMouseEvent")
	DrawTextW                  = User32.NewProc("DrawTextW")
	FillRect                   = User32.NewProc("FillRect")
	SetWinEventHook            = User32.NewProc("SetWinEventHook")
	UnhookWinEvent             = User32.NewProc("UnhookWinEvent")
	SetLayeredWindowAttributes = User32.NewProc("SetLayeredWindowAttributes")

	CreateCompatibleDC     = Gdi32.NewProc("CreateCompatibleDC")
	CreateCompatibleBitmap = Gdi32.NewProc("CreateCompatibleBitmap")
	CreateDIBSection       = Gdi32.NewProc("CreateDIBSection")
	SelectObject           = Gdi32.NewProc("SelectObject")
	DeleteDC               = Gdi32.NewProc("DeleteDC")
	DeleteObject           = Gdi32.NewProc("DeleteObject")
	BitBlt                 = Gdi32.NewProc("BitBlt")
	CreateSolidBrush       = Gdi32.NewProc("CreateSolidBrush")
	CreatePen              = Gdi32.NewProc("CreatePen")
	RoundRect              = Gdi32.NewProc("RoundRect")
	CreateFontW            = Gdi32.NewProc("CreateFontW")
	SetTextColor           = Gdi32.NewProc("SetTextColor")
	SetBkMode              = Gdi32.NewProc("SetBkMode")
	GetTextExtentPoint32W  = Gdi32.NewProc("GetTextExtentPoint32W")
	CreateRoundRectRgn     = Gdi32.NewProc("CreateRoundRectRgn")

	GetModuleHandleW = Kernel32.NewProc("GetModuleHandleW")
	AlphaBlend       = Msimg32.NewProc("AlphaBlend")
)

const (
	GwlStyle   = ^uintptr(15) // -16
	GwlExStyle = ^uintptr(19) // -20

	WsChild        = 0x40000000
	WsPopup        = 0x80000000
	WsClipSiblings = 0x04000000
	WsExToolWindow = 0x00000080
	WsExAppWindow  = 0x00040000
	WsExNoActivate = 0x08000000
	WsExLayered    = 0x00080000
	LwaAlpha       = 0x2

	SwpNoSize      = 0x0001
	SwpNoMove      = 0x0002
	SwpNoActivate  = 0x0010
	SwpShowWindow  = 0x0040
	SwHide         = 0
	SwShowNoActive = 4
	HwndTop        = 0
	HwndTopmost    = ^uintptr(0) // -1

	WmDestroy        = 0x0002
	WmClose          = 0x0010
	WmPaint          = 0x000F
	WmEraseBkgnd     = 0x0014
	WmMouseActivate  = 0x0021
	WmTimer          = 0x0113
	WmMouseMove      = 0x0200
	WmLButtonDown    = 0x0201
	WmLButtonUp      = 0x0202
	WmCaptureChanged = 0x0215
	WmMouseLeave     = 0x02A3
	WmDisplayChange  = 0x007E
	WmSettingChange  = 0x001A
	WmApp            = 0x8000
	MaNoActivate     = 3
	TmeLeave         = 0x00000002
	IdcHand          = 32649
	EventLocationChg = 0x800B
)

type Rect struct{ L, T, R, B int32 }

func (r Rect) Has(x, y int32) bool { return x >= r.L && x < r.R && y >= r.T && y < r.B }
func (r Rect) W() int32            { return r.R - r.L }
func (r Rect) H() int32            { return r.B - r.T }

type Point struct{ X, Y int32 }

type WndClassEx struct {
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

type Msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      Point
	Private uint32
}

type PaintStruct struct {
	Hdc       uintptr
	Erase     int32
	RcPaint   Rect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

type TrackMouseEventArgs struct {
	Size      uint32
	Flags     uint32
	HwndTrack uintptr
	HoverTime uint32
}

type BitmapInfoHeader struct {
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

func UTF16Ptr(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }

func FindWindow(class, title string) uintptr {
	var c, t *uint16
	if class != "" {
		c = UTF16Ptr(class)
	}
	if title != "" {
		t = UTF16Ptr(title)
	}
	h, _, _ := FindWindowW.Call(uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(t)))
	return h
}

func WindowRect(h uintptr) (Rect, bool) {
	var r Rect
	ok, _, _ := GetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
	return r, ok != 0
}

func DPIScale(h uintptr) float64 {
	dpi, _, _ := GetDpiForWindow.Call(h)
	if dpi == 0 {
		return 1
	}
	return float64(dpi) / 96
}

// ForceForeground gives a window keyboard focus. The click that triggers it
// lands on our own pill window, so Windows normally allows it. If the
// foreground lock still refuses, a synthetic Alt tap (the well-known
// workaround) makes the next SetForegroundWindow succeed. AttachThreadInput
// is deliberately not used: the pill shares Explorer's input queue, and
// attaching more threads to it broke later clicks.
func ForceForeground(h uintptr) {
	BringWindowToTop.Call(h)
	SetForegroundWindow.Call(h)
	if fg, _, _ := GetForegroundWindow.Call(); fg == h {
		return
	}
	const vkMenu, keyUp = 0x12, 0x0002
	KeybdEvent.Call(vkMenu, 0, 0, 0)
	SetForegroundWindow.Call(h)
	KeybdEvent.Call(vkMenu, 0, keyUp, 0)
}

// ---------- COM ----------

var (
	Ole32            = windows.NewLazySystemDLL("ole32.dll")
	CoInitializeEx   = Ole32.NewProc("CoInitializeEx")
	CoCreateInstance = Ole32.NewProc("CoCreateInstance")
	CoUninitialize   = Ole32.NewProc("CoUninitialize")
)

const (
	ClsctxInprocServer = 1
	vtRelease          = 2
)

// comCall invokes method #idx of the COM object obj (memory owned by COM).
func ComCall(obj unsafe.Pointer, idx int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, uintptr(idx)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

// comRelease releases a COM object (nil is ignored).
func ComRelease(obj unsafe.Pointer) {
	if obj != nil {
		ComCall(obj, vtRelease)
	}
}

// R builds a Rect from its edges.
func R(l, t, r, b int32) Rect { return Rect{L: l, T: t, R: r, B: b} }

// Pt builds a Point.
func Pt(x, y int32) Point { return Point{X: x, Y: y} }

// ---------- taskbars ----------

// SecondaryTaskbars lists the taskbars of the other monitors.
func SecondaryTaskbars() []uintptr {
	var out []uintptr
	cls := UTF16Ptr("Shell_SecondaryTrayWnd")
	var prev uintptr
	for i := 0; i < 16; i++ {
		h, _, _ := FindWindowExW.Call(0, prev, uintptr(unsafe.Pointer(cls)), 0)
		if h == 0 {
			break
		}
		out = append(out, h)
		prev = h
	}
	return out
}

// TaskbarAt returns the taskbar (primary or secondary) containing pt,
// falling back to the primary one.
func TaskbarAt(pt Point) uintptr {
	primary := FindWindow("Shell_TrayWnd", "")
	for _, tb := range append([]uintptr{primary}, SecondaryTaskbars()...) {
		if r, ok := WindowRect(tb); ok && r.Has(pt.X, pt.Y) {
			return tb
		}
	}
	return primary
}

// ---------- monitors ----------

var (
	pMonitorFromPoint = User32.NewProc("MonitorFromPoint")
	pGetDpiForMonitor = windows.NewLazySystemDLL("shcore.dll").NewProc("GetDpiForMonitor")
)

// MonitorScale is the scale factor (1.0 = 96 DPI) of the monitor at pt.
// Prefer it over DPIScale for windows of other processes: Explorer's
// secondary taskbars report 96 DPI whatever their monitor's scale is.
func MonitorScale(pt Point) float64 {
	const monitorDefaultToNearest = 2
	m, _, _ := pMonitorFromPoint.Call(uintptr(uint32(pt.X))|uintptr(uint32(pt.Y))<<32, monitorDefaultToNearest)
	var x, y uint32
	if hr, _, _ := pGetDpiForMonitor.Call(m, 0, uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y))); hr != 0 || x == 0 {
		return 1
	}
	return float64(x) / 96
}
