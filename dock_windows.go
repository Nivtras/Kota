//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Docking: one Wails window, two modes.
//   compact  – a pill on the taskbar, right next to (left of) the Start button
//   expanded – the panel above the taskbar *plus* the pill, in one window that
//              is clipped (SetWindowRgn) to exactly those two rectangles, so
//              the taskbar between them stays clickable.
// Windows 11 has no API for third-party taskbar widgets, so the window is kept
// topmost and re-positioned on a timer (the same approach as TrafficMonitor).

var (
	user32                    = windows.NewLazySystemDLL("user32.dll")
	gdi32                     = windows.NewLazySystemDLL("gdi32.dll")
	pFindWindowW              = user32.NewProc("FindWindowW")
	pFindWindowExW            = user32.NewProc("FindWindowExW")
	pGetWindowRect            = user32.NewProc("GetWindowRect")
	pSetWindowPos             = user32.NewProc("SetWindowPos")
	pSetWindowRgn             = user32.NewProc("SetWindowRgn")
	pSetParent                = user32.NewProc("SetParent")
	pGetAsyncKeyState         = user32.NewProc("GetAsyncKeyState")
	pIsWindow                 = user32.NewProc("IsWindow")
	pGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	pGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	pAttachThreadInput        = user32.NewProc("AttachThreadInput")
	pBringWindowToTop         = user32.NewProc("BringWindowToTop")
	pGetCursorPos             = user32.NewProc("GetCursorPos")
	pGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	pGetWindowLongPtrW        = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtrW        = user32.NewProc("SetWindowLongPtrW")
	pGetDpiForWindow          = user32.NewProc("GetDpiForWindow")
	pShowWindow               = user32.NewProc("ShowWindow")
	pSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	pCreateRectRgn            = gdi32.NewProc("CreateRectRgn")
	pCombineRgn               = gdi32.NewProc("CombineRgn")
	pDeleteObject             = gdi32.NewProc("DeleteObject")
)

const (
	gwlExStyle       = ^uintptr(19) // -20
	wsExToolWindow   = 0x00000080
	wsExAppWindow    = 0x00040000
	swpNoSize        = 0x0001
	swpNoMove        = 0x0002
	swpNoActivate    = 0x0010
	swShowNoActivate = 4
	hwndTopmost      = ^uintptr(0) // -1
	hwndTop          = 0
	rgnOr            = 2
	gwlStyle         = ^uintptr(15) // -16
	wsChild          = 0x40000000
	wsPopup          = 0x80000000
	swpFrameChanged  = 0x0020
	swpShowWindow    = 0x0040
)

const (
	defaultCompactW = 200 // logical px
	minCompactW     = 180
	maxCompactW     = 320
	compactH        = 40
	expandW         = 360
	expandH         = 480
	gap             = 8
)

type rect struct{ L, T, R, B int32 }

// Box is a rectangle in logical (CSS) pixels, relative to the window.
type Box struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Layout tells the UI where the pill and the panel sit inside the window.
type Layout struct {
	Mode  string `json:"mode"`
	Pill  Box    `json:"pill"`
	Panel *Box   `json:"panel,omitempty"`
}

type dock struct {
	mu     sync.Mutex
	hwnd   uintptr
	mode   string  // "compact" | "expanded"
	width  int     // pill width, logical px
	parent uintptr // taskbar HWND while embedded (compact), else 0
	// Called on their own goroutine for clicks the WebView can't receive.
	onPillClick    func()
	onOutsideClick func()
	onWindowLost   func() // our window was destroyed (e.g. Explorer restart)
	lastPill       rect   // screen px
	lastPanel      rect   // screen px (expanded mode)
	lastPos        rect
	lastMode       string

	startMu        sync.Mutex
	startLeft      int32 // Start button's left edge, physical px (0 = unknown)
	startErrLogged bool
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

// attach finds our window, hides it from the taskbar/Alt-Tab and starts the
// keep-on-top loop and the Start-button tracker.
func (d *dock) attach(title string) {
	var h uintptr
	for i := 0; i < 100 && h == 0; i++ {
		h = findWindow("", title)
		if h == 0 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if h == 0 {
		return
	}
	ex, _, _ := pGetWindowLongPtrW.Call(h, gwlExStyle)
	ex = (ex | wsExToolWindow) &^ wsExAppWindow
	pSetWindowLongPtrW.Call(h, gwlExStyle, ex)

	go d.refreshStart() // ~1s; re-applies the position once Start is known
	d.mu.Lock()
	d.hwnd = h
	d.mu.Unlock()
	d.apply(true)
	pShowWindow.Call(h, swShowNoActivate)

	go func() {
		for range time.Tick(700 * time.Millisecond) {
			// Our window lives inside the taskbar; when Explorer restarts it is
			// destroyed with it, but Wails keeps running window-less. Detect
			// that and hand over to a fresh process.
			if ok, _, _ := pIsWindow.Call(h); ok == 0 {
				dockLog("window lost (Explorer restarted?) — relaunching")
				if d.onWindowLost != nil {
					d.onWindowLost()
				}
				return
			}
			d.apply(false)
		}
	}()
	go d.watchPillClicks()
	// Start moves when pinned/running apps change; native UIA is cheap.
	go func() {
		for range time.Tick(3 * time.Second) {
			d.refreshStart()
		}
	}()
}

var uia = newUIAWorker()

func (d *dock) refreshStart() {
	v, err := uia.startLeft()
	if err != nil {
		d.startMu.Lock()
		first := d.startErrLogged
		d.startErrLogged = true
		d.startMu.Unlock()
		if !first {
			dockLog("start button lookup failed: %v (falling back to left of the tray)", err)
		}
		return
	}
	d.startMu.Lock()
	changed := d.startLeft != int32(v)
	d.startLeft = int32(v)
	d.startMu.Unlock()
	if changed {
		d.apply(true)
	}
}

// dockLog appends a line to %APPDATA%\AIUsageWidget\dock.log (kept small).
func dockLog(format string, args ...any) {
	p := filepath.Join(filepath.Dir(configPath()), "dock.log")
	if fi, err := os.Stat(p); err == nil && fi.Size() > 64<<10 {
		_ = os.Remove(p)
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s "+format+"\n", append([]any{time.Now().Format(time.DateTime)}, args...)...)
}

func (d *dock) setMode(mode string) Layout {
	d.mu.Lock()
	d.mode = mode
	h := d.hwnd
	d.mu.Unlock()
	d.apply(true)
	if mode == "expanded" && h != 0 {
		forceForeground(h)
	}
	return d.layout()
}

func (d *dock) setWidth(w int) Layout {
	d.mu.Lock()
	d.width = clampWidth(w)
	d.mu.Unlock()
	d.apply(true)
	return d.layout()
}

func clampWidth(w int) int {
	if w == 0 {
		return defaultCompactW
	}
	return max(minCompactW, min(maxCompactW, w))
}

type geometry struct {
	tb     uintptr // taskbar HWND
	tbr    rect    // taskbar rect, physical screen px
	scale  float64
	pill   rect // physical, screen
	panel  rect
	window rect // what the window covers in the current mode
}

// geom computes positions in physical pixels. Caller holds d.mu.
func (d *dock) geom() (geometry, bool) {
	var g geometry
	tb := findWindow("Shell_TrayWnd", "")
	tbr, ok := windowRect(tb)
	if !ok {
		return g, false
	}
	g.tb, g.tbr = tb, tbr
	dpi, _, _ := pGetDpiForWindow.Call(d.hwnd)
	if dpi == 0 {
		dpi = 96
	}
	g.scale = float64(dpi) / 96
	px := func(v int) int32 { return int32(float64(v)*g.scale + 0.5) }

	trayLeft := tbr.R - px(320)
	if tn, _, _ := pFindWindowExW.Call(tb, 0, uintptr(unsafe.Pointer(utf16("TrayNotifyWnd"))), 0); tn != 0 {
		if r, ok := windowRect(tn); ok && r.L > tbr.L {
			trayLeft = r.L
		}
	}
	d.startMu.Lock()
	startLeft := d.startLeft
	d.startMu.Unlock()

	w, h := px(clampWidth(d.width)), px(compactH)
	tbH := tbr.B - tbr.T
	if h > tbH-px(4) {
		h = tbH - px(4)
	}
	// Sit right before the Start button. If Start is unknown or at the far
	// left (left-aligned taskbar), fall back to just left of the tray.
	x := startLeft - px(gap) - w
	if startLeft == 0 || x < tbr.L+px(gap) {
		x = trayLeft - px(gap) - w
	}
	y := tbr.T + (tbH-h)/2
	g.pill = rect{x, y, x + w, y + h}

	ew, eh := px(expandW), px(expandH)
	ex := g.pill.R - ew // right-aligned with the pill
	if ex < tbr.L+px(gap) {
		ex = tbr.L + px(gap)
	}
	if ex+ew > tbr.R-px(gap) {
		ex = tbr.R - px(gap) - ew
	}
	ey := tbr.T - px(gap) - eh
	g.panel = rect{ex, ey, ex + ew, ey + eh}

	g.window = g.pill
	if d.mode == "expanded" {
		g.window = rect{min(g.pill.L, g.panel.L), g.panel.T, max(g.pill.R, g.panel.R), g.pill.B}
	}
	return g, true
}

func (d *dock) layout() Layout {
	d.mu.Lock()
	defer d.mu.Unlock()
	g, ok := d.geom()
	l := Layout{Mode: d.mode}
	if !ok {
		return l
	}
	box := func(r rect) Box {
		return Box{
			X: int(float64(r.L-g.window.L)/g.scale + 0.5), Y: int(float64(r.T-g.window.T)/g.scale + 0.5),
			W: int(float64(r.R-r.L)/g.scale + 0.5), H: int(float64(r.B-r.T)/g.scale + 0.5),
		}
	}
	l.Pill = box(g.pill)
	if d.mode == "expanded" {
		p := box(g.panel)
		l.Panel = &p
	}
	return l
}

func (d *dock) apply(force bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.hwnd == 0 {
		return
	}
	g, ok := d.geom()
	if !ok {
		return
	}

	r := g.window
	moved := force || r != d.lastPos || d.mode != d.lastMode

	if d.mode == "compact" {
		// Live inside the taskbar as a child window: shell z-order changes
		// (Start, Search, clicking the taskbar) can't cover us, and we hide
		// with the taskbar for full-screen apps / auto-hide.
		if d.parent != g.tb {
			d.embed(g.tb)
			moved = true
		}
		flags := uintptr(swpNoActivate)
		if !moved {
			flags |= swpNoMove | swpNoSize
		}
		// Position is relative to the taskbar; HWND_TOP keeps us above its
		// XAML content.
		pSetWindowPos.Call(d.hwnd, hwndTop,
			uintptr(r.L-g.tbr.L), uintptr(r.T-g.tbr.T), uintptr(r.R-r.L), uintptr(r.B-r.T), flags)
	} else {
		if d.parent != 0 {
			d.detach()
			moved = true
		}
		flags := uintptr(swpNoActivate)
		if !moved {
			flags |= swpNoMove | swpNoSize
		}
		pSetWindowPos.Call(d.hwnd, hwndTopmost,
			uintptr(r.L), uintptr(r.T), uintptr(r.R-r.L), uintptr(r.B-r.T), flags)
	}
	if moved {
		d.applyRegion(g)
	}
	d.lastPos, d.lastMode = r, d.mode
	d.lastPill, d.lastPanel = g.pill, g.panel
}

func (r rect) has(x, y int32) bool { return x >= r.L && x < r.R && y >= r.T && y < r.B }

// watchPillClicks is the single place that decides when the panel opens and
// closes, because the WebView cannot be relied on for it:
//   - while the pill is a child of the Explorer-owned taskbar the WebView gets
//     no mouse input at all;
//   - while detaching/attaching, the WebView fires spurious blur events.
//
// Rules: a click on the pill toggles the panel; with the panel open, a click
// outside panel+pill closes it, and so does focus moving to another window
// without a click (e.g. Alt+Tab). Clicks inside the panel go to the WebView.
func (d *dock) watchPillClicks() {
	btn := uintptr(0x01)                                           // VK_LBUTTON
	if swapped, _, _ := pGetSystemMetrics.Call(23); swapped != 0 { // SM_SWAPBUTTON
		btn = 0x02
	}
	var pressed int // 0 none, 1 inside pill, 2 outside our UI
	var wasDown, hadFocus bool
	var lastMouse time.Time
	for range time.Tick(40 * time.Millisecond) {
		d.mu.Lock()
		mode, embedded, h := d.mode, d.parent != 0, d.hwnd
		pill, panel := d.lastPill, d.lastPanel
		d.mu.Unlock()

		watching := (mode == "compact" && embedded) || mode == "expanded"

		st, _, _ := pGetAsyncKeyState.Call(btn)
		down := st&0x8000 != 0
		var pt struct{ X, Y int32 }
		pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		where := 0
		switch {
		case !watching:
		case pill.has(pt.X, pt.Y):
			where = 1
		case mode == "expanded" && !panel.has(pt.X, pt.Y):
			where = 2
		}

		switch {
		case down && !wasDown:
			pressed, lastMouse = where, time.Now()
		case !down && wasDown:
			lastMouse = time.Now()
			if pressed != 0 && pressed == where {
				if where == 1 && d.onPillClick != nil {
					go d.onPillClick()
				} else if where == 2 && d.onOutsideClick != nil {
					go d.onOutsideClick()
				}
			}
			pressed = 0
		}
		wasDown = down

		// Keyboard focus change (no click involved) while the panel is open.
		if mode != "expanded" {
			hadFocus = false
			continue
		}
		fg, _, _ := pGetForegroundWindow.Call()
		if fg == h {
			hadFocus = true
		} else if hadFocus && !down && time.Since(lastMouse) > 600*time.Millisecond && d.onOutsideClick != nil {
			hadFocus = false
			go d.onOutsideClick()
		}
	}
}

// forceForeground gives the expanded panel keyboard focus. A click on the
// embedded pill makes the taskbar the foreground window, and Windows'
// foreground lock would block a plain SetForegroundWindow; attaching to the
// foreground thread's input first is the standard way around it.
func forceForeground(h uintptr) {
	fg, _, _ := pGetForegroundWindow.Call()
	if fg == h {
		return
	}
	fgThread, _, _ := pGetWindowThreadProcessId.Call(fg, 0)
	ourThread, _, _ := pGetWindowThreadProcessId.Call(h, 0)
	if fgThread != 0 && fgThread != ourThread {
		pAttachThreadInput.Call(fgThread, ourThread, 1)
		defer pAttachThreadInput.Call(fgThread, ourThread, 0)
	}
	pBringWindowToTop.Call(h)
	pSetForegroundWindow.Call(h)
}

// embed makes our window a child of the taskbar. Caller holds d.mu.
func (d *dock) embed(tb uintptr) {
	st, _, _ := pGetWindowLongPtrW.Call(d.hwnd, gwlStyle)
	st = (st &^ wsPopup) | wsChild
	pSetWindowLongPtrW.Call(d.hwnd, gwlStyle, st)
	pSetParent.Call(d.hwnd, tb)
	pSetWindowPos.Call(d.hwnd, hwndTop, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate|swpFrameChanged|swpShowWindow)
	d.parent = tb
}

// detach turns our window back into a top-level popup. Caller holds d.mu.
func (d *dock) detach() {
	pSetParent.Call(d.hwnd, 0)
	st, _, _ := pGetWindowLongPtrW.Call(d.hwnd, gwlStyle)
	st = (st &^ wsChild) | wsPopup
	pSetWindowLongPtrW.Call(d.hwnd, gwlStyle, st)
	pSetWindowPos.Call(d.hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate|swpFrameChanged|swpShowWindow)
	d.parent = 0
}

// applyRegion clips the expanded window to panel + pill so the strip of
// taskbar between/around them keeps receiving clicks. Caller holds d.mu.
func (d *dock) applyRegion(g geometry) {
	rel := func(r rect) uintptr {
		h, _, _ := pCreateRectRgn.Call(
			uintptr(r.L-g.window.L), uintptr(r.T-g.window.T),
			uintptr(r.R-g.window.L), uintptr(r.B-g.window.T))
		return h
	}
	if d.mode != "expanded" {
		// Explicit pill-sized region (a NULL region did not reliably reset).
		pSetWindowRgn.Call(d.hwnd, rel(g.pill), 1)
		return
	}
	panel, pill := rel(g.panel), rel(g.pill)
	pCombineRgn.Call(panel, panel, pill, rgnOr)
	pDeleteObject.Call(pill)
	pSetWindowRgn.Call(d.hwnd, panel, 1) // the system now owns the region
}
