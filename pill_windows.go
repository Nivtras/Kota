//go:build windows

package main

// The taskbar pill: a native window (GDI-drawn) living inside the taskbar.
//
// Windows 11 has no API for third-party taskbar widgets. Like TrafficMonitor,
// the pill is a child window of Explorer's Shell_TrayWnd, so it is always
// drawn with the taskbar (Start/Search/clicks can't cover it, it hides with
// the taskbar for full-screen apps). A native window — unlike a web view —
// receives mouse input normally in that position.
//
// It sits just left of the Start button. Start's position comes from UI
// Automation (uia_windows.go), refreshed when Explorer reports a layout
// change on the taskbar (WinEvent hook) plus a slow fallback timer. When
// Explorer restarts it broadcasts "TaskbarCreated" and the pill is recreated.

import (
	"bytes"
	"image"
	"image/png"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	defaultCompactW = 200 // logical px
	minCompactW     = 180
	maxCompactW     = 320
	compactH        = 40
	gap             = 8

	timerFallback = 1 // periodic re-layout
	timerRelayout = 2 // debounced re-layout after a taskbar change
	timerSettle   = 3 // second look after Start's slide animation

	msgRelayout = wmApp + 1 // re-layout + repaint (posted from any thread)
	msgRepaint  = wmApp + 2 // repaint only
)

func clampWidth(w int) int {
	if w == 0 {
		return defaultCompactW
	}
	return max(minCompactW, min(maxCompactW, w))
}

type pill struct {
	mu        sync.Mutex
	rows      []pillRow
	width     int // logical px
	hover     bool
	startLeft int32 // Start button left edge, physical px (0 = unknown)
	screen    rect  // pill rect in screen px (for the panel)

	host, hwnd, tb uintptr
	hook           uintptr
	taskbarCreated uint32
	lastClick      time.Time // window thread only
	lookingUp      atomic.Bool

	// Position: the pill's right edge sits `offset` logical px left of the
	// anchor ("start": Start button's left edge, "tray": notification area).
	anchor string
	offset int

	// Drag state (window thread only).
	pressed, dragging bool
	downX, downLeft   int32 // cursor x and pill left at mouse-down (screen px)
	dragLeft          int32 // pill left while dragging
	downAt            time.Time

	onClick func(screen rect)               // called on its own goroutine
	onMoved func(anchor string, offset int) // after a drag; own goroutine
	onMenu  func(cmd int)                   // right-click menu choice; own goroutine
	logos   map[string][]byte               // provider id → PNG
	icons   map[iconKey]uintptr
}

const (
	anchorStart   = "start"
	anchorTray    = "tray"
	defaultOffset = gap
)

type iconKey struct {
	id   string
	size int32
}

var thePill *pill // the window procedures need a package-level receiver

// Created once: syscall.NewCallback slots are limited and never freed.
var winEventCallback = syscall.NewCallback(winEventProc)

func newPill(width int, anchor string, offset int, logos map[string][]byte,
	onClick func(rect), onMoved func(string, int)) *pill {
	refreshTheme()
	if anchor != anchorTray {
		anchor = anchorStart
	}
	p := &pill{width: clampWidth(width), anchor: anchor, offset: offset, logos: logos,
		onClick: onClick, onMoved: onMoved, icons: map[iconKey]uintptr{}}
	thePill = p
	go p.loop()
	return p
}

// setPosition moves the pill to an anchor/offset (e.g. "reset position").
func (p *pill) setPosition(anchor string, offset int) {
	p.mu.Lock()
	p.anchor, p.offset = anchor, offset
	h := p.host
	p.mu.Unlock()
	if h != 0 {
		pPostMessageW.Call(h, msgRelayout, 0, 0)
	}
}

// ---------- public API (any goroutine) ----------

func (p *pill) setRows(rows []pillRow) {
	p.mu.Lock()
	p.rows = rows
	h := p.host
	p.mu.Unlock()
	if h != 0 {
		pPostMessageW.Call(h, msgRepaint, 0, 0)
	}
}

func (p *pill) setWidth(w int) {
	p.mu.Lock()
	p.width = clampWidth(w)
	h := p.host
	p.mu.Unlock()
	if h != 0 {
		pPostMessageW.Call(h, msgRelayout, 0, 0)
	}
}

// screenRect is where the pill currently is (physical screen px).
func (p *pill) screenRect() rect {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.screen
}

// ---------- window thread ----------

var uia = newUIAWorker()

func (p *pill) loop() {
	runtime.LockOSThread()
	inst, _, _ := pGetModuleHandleW.Call(0)
	cursor, _, _ := pLoadCursorW.Call(0, idcHand)

	hostClass := utf16("AIUsageWidgetPillHost")
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wndClassEx{
		Size: uint32(unsafe.Sizeof(wndClassEx{})), WndProc: syscall.NewCallback(hostProc),
		Instance: inst, ClassName: hostClass,
	})))
	pillClass := utf16("AIUsageWidgetPill")
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wndClassEx{
		Size: uint32(unsafe.Sizeof(wndClassEx{})), WndProc: syscall.NewCallback(pillProc),
		Instance: inst, ClassName: pillClass, Cursor: cursor,
	})))

	// A hidden top-level window: receives the TaskbarCreated broadcast (child
	// windows don't) and owns the timers.
	host, _, _ := pCreateWindowExW.Call(wsExToolWindow, uintptr(unsafe.Pointer(hostClass)), 0,
		wsPopup, 0, 0, 0, 0, 0, 0, inst, 0)
	tc, _, _ := pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(utf16("TaskbarCreated"))))
	p.mu.Lock()
	p.host, p.taskbarCreated = host, uint32(tc)
	p.mu.Unlock()

	p.createPill(inst, pillClass)
	pSetTimer.Call(host, timerFallback, 5000, 0)

	var m msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

var (
	pillInst  uintptr
	pillClass *uint16
)

// createPill (re)creates the pill inside the current taskbar. Window thread.
func (p *pill) createPill(inst uintptr, class *uint16) {
	pillInst, pillClass = inst, class
	tb := findWindow("Shell_TrayWnd", "")
	if tb == 0 {
		return // taskbar not there yet; the fallback timer retries
	}
	// Create as a hidden popup, then re-parent into the taskbar. It must be
	// layered: the Win11 taskbar is DirectComposition-based, and a plain GDI
	// child window is not composited (it stays invisible).
	h, _, _ := pCreateWindowExW.Call(wsExToolWindow|wsExNoActivate|wsExLayered, uintptr(unsafe.Pointer(class)), 0,
		wsPopup, 0, 0, 1, 1, 0, 0, inst, 0)
	if h == 0 {
		dockLog("pill: CreateWindowEx failed")
		return
	}
	pSetLayeredWindowAttributes.Call(h, 0, 255, lwaAlpha)
	pSetWindowLongPtrW.Call(h, gwlStyle, wsChild|wsClipSiblings)
	pSetParent.Call(h, tb)

	// Watch the taskbar for layout changes (pinned/running apps change where
	// Start is). Out-of-context hooks are delivered to this thread's loop.
	if p.hook != 0 {
		pUnhookWinEvent.Call(p.hook)
	}
	var pid uint32
	pGetWindowThreadProcessId.Call(tb, uintptr(unsafe.Pointer(&pid)))
	hook, _, _ := pSetWinEventHook.Call(eventLocationChg, eventLocationChg, 0,
		winEventCallback, uintptr(pid), 0, 0)

	p.mu.Lock()
	p.hwnd, p.tb, p.hook = h, tb, hook
	p.screen = rect{} // new window: force the first layout to size/move it
	rows, host := p.rows, p.host
	p.mu.Unlock()
	attachTooltip(host, h, inst)
	updateTooltip(rows)
	p.lookupStart()
	p.layout()
	pShowWindow.Call(h, swShowNoActive)
}

// lookupStart asks UI Automation where Start is, off the window thread, and
// posts a re-layout when it changed. UIA calls into Explorer, and Explorer
// sends messages to our child window: doing it on the window thread made
// both sides wait on each other (delayed clicks).
func (p *pill) lookupStart() {
	if !p.lookingUp.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer p.lookingUp.Store(false)
		v, err := uia.startLeft()
		if err != nil {
			return
		}
		p.mu.Lock()
		changed := v != p.startLeft
		p.startLeft = v
		host := p.host
		p.mu.Unlock()
		if changed && host != 0 {
			pPostMessageW.Call(host, msgRelayout, 0, 0)
		}
	}()
}

// trayLeft is the left edge of the notification area (screen px).
func trayLeftOf(tb uintptr, tbr rect, s float64) int32 {
	left := tbr.R - int32(320*s)
	if tn, _, _ := pFindWindowExW.Call(tb, 0, uintptr(unsafe.Pointer(utf16("TrayNotifyWnd"))), 0); tn != 0 {
		if r, ok := windowRect(tn); ok && r.L > tbr.L {
			left = r.L
		}
	}
	return left
}

// layout positions the pill relative to its anchor (or where it is being
// dragged). Window thread.
func (p *pill) layout() {
	p.mu.Lock()
	h, tb, width, startLeft := p.hwnd, p.tb, p.width, p.startLeft
	anchor, offset, dragging, dragLeft := p.anchor, p.offset, p.dragging, p.dragLeft
	p.mu.Unlock()
	if h == 0 {
		return
	}
	tbr, ok := windowRect(tb)
	if !ok {
		return
	}
	s := dpiScale(tb)
	px := func(v int) int32 { return int32(float64(v)*s + 0.5) }

	trayLeft := trayLeftOf(tb, tbr, s)
	w, ht := px(width), px(compactH)
	if ht > tbr.h()-px(4) {
		ht = tbr.h() - px(4)
	}
	var x int32
	switch {
	case dragging:
		x = dragLeft
	case anchor == anchorStart && startLeft > 0:
		x = startLeft - px(offset) - w
	case anchor == anchorStart:
		x = trayLeft - px(defaultOffset) - w // Start unknown: next to the tray
	default:
		x = trayLeft - px(offset) - w
	}
	// Stay on the taskbar and off the notification area.
	x = max(tbr.L+px(4), min(x, trayLeft-px(4)-w))
	y := tbr.T + (tbr.h()-ht)/2
	scr := rect{x, y, x + w, y + ht}

	p.mu.Lock()
	changed := scr != p.screen
	p.screen = scr
	p.mu.Unlock()

	// HWND_TOP keeps us above the taskbar's own XAML content.
	flags := uintptr(swpNoActivate)
	if !changed {
		flags |= swpNoMove | swpNoSize
	}
	pSetWindowPos.Call(h, hwndTop, uintptr(x-tbr.L), uintptr(y-tbr.T), uintptr(w), uintptr(ht), flags)
	if changed {
		r := px(8)
		rgn, _, _ := pCreateRoundRectRgn.Call(0, 0, uintptr(w+1), uintptr(ht+1), uintptr(r), uintptr(r))
		pSetWindowRgn.Call(h, rgn, 1)
		pInvalidateRect.Call(h, 0, 0)
	}
}

func hostProc(hwnd, m, wp, lp uintptr) uintptr {
	p := thePill
	switch {
	case p != nil && uint32(m) == p.taskbarCreated && p.taskbarCreated != 0:
		// Explorer restarted: the old pill died with the old taskbar.
		dockLog("TaskbarCreated: recreating pill")
		p.mu.Lock()
		old := p.hwnd
		p.hwnd = 0
		p.mu.Unlock()
		if old != 0 {
			pDestroyWindow.Call(old)
		}
		p.createPill(pillInst, pillClass)
		return 0
	case m == msgRelayout:
		p.layout()
		p.repaint()
		return 0
	case m == msgTray:
		theToaster.trayMessage(lp)
		return 0
	case m == msgRepaint:
		p.repaint()
		p.mu.Lock()
		rows := p.rows
		p.mu.Unlock()
		updateTooltip(rows)
		return 0
	case m == wmTimer && wp == timerRelayout:
		pKillTimer.Call(hwnd, timerRelayout)
		p.lookupStart()
		p.layout()
		// Start slides into place; look again once the animation settled.
		pSetTimer.Call(hwnd, timerSettle, 700, 0)
		return 0
	case m == wmTimer && wp == timerSettle:
		pKillTimer.Call(hwnd, timerSettle)
		p.lookupStart()
		return 0
	case m == wmTimer && wp == timerFallback:
		p.mu.Lock()
		alive := p.hwnd
		p.mu.Unlock()
		if ok, _, _ := pIsWindow.Call(alive); alive == 0 || ok == 0 {
			p.createPill(pillInst, pillClass) // lost without a broadcast
		} else {
			p.lookupStart()
			p.layout()
		}
		return 0
	case m == wmDisplayChange || m == wmSettingChange:
		// Theme switches arrive as WM_SETTINGCHANGE("ImmersiveColorSet").
		if refreshTheme() {
			p.repaint()
		}
		pSetTimer.Call(hwnd, timerRelayout, 300, 0)
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}

// winEventProc: a window in Explorer moved. If it's the taskbar or one of
// its children, Start may have moved too — re-layout shortly (debounced).
func winEventProc(hook, event, hwnd, idObject, idChild, thread, ms uintptr) uintptr {
	p := thePill
	if p == nil {
		return 0
	}
	p.mu.Lock()
	tb, host := p.tb, p.host
	p.mu.Unlock()
	if hwnd == tb {
		pSetTimer.Call(host, timerRelayout, 150, 0)
	} else if ok, _, _ := pIsChild.Call(tb, hwnd); ok != 0 {
		pSetTimer.Call(host, timerRelayout, 150, 0)
	}
	return 0
}

// msgScreenX converts a mouse message's client x (low word of lParam,
// signed: it can be negative while captured) to screen px.
func msgScreenX(hwnd, lp uintptr) int32 {
	r, _ := windowRect(hwnd)
	return r.L + int32(int16(uint16(lp&0xFFFF)))
}

// primaryButtonDown reports whether the primary mouse button is physically
// held right now (honours swapped buttons).
func primaryButtonDown() bool {
	vk := uintptr(0x01) // VK_LBUTTON
	if swapped, _, _ := pGetSystemMetrics.Call(23); swapped != 0 {
		vk = 0x02
	}
	st, _, _ := pGetAsyncKeyState.Call(vk)
	return st&0x8000 != 0
}

// finishDrag turns the dropped position into an anchor + offset: left of
// Start follows Start (it moves as apps open/close); right of it follows the
// notification area. Window thread.
func (p *pill) finishDrag() {
	p.mu.Lock()
	tb, startLeft, scr := p.tb, p.startLeft, p.screen
	p.mu.Unlock()
	tbr, ok := windowRect(tb)
	if !ok {
		return
	}
	s := dpiScale(tb)
	logical := func(v int32) int { return int(float64(v)/s + 0.5) }
	anchor, offset := anchorTray, logical(trayLeftOf(tb, tbr, s)-scr.R)
	if startLeft > 0 && (scr.L+scr.R)/2 < startLeft {
		anchor, offset = anchorStart, logical(startLeft-scr.R)
	}
	dockLog("pill moved: %s offset %d (pill %d..%d, start %d)", anchor, offset, scr.L, scr.R, startLeft)
	p.mu.Lock()
	p.anchor, p.offset, p.dragging = anchor, offset, false
	p.mu.Unlock()
	pReleaseCapture.Call()
	p.layout()
	if p.onMoved != nil {
		go p.onMoved(anchor, offset)
	}
}

func (p *pill) repaint() {
	p.mu.Lock()
	h := p.hwnd
	p.mu.Unlock()
	if h != 0 {
		pInvalidateRect.Call(h, 0, 0)
	}
}

func pillProc(hwnd, m, wp, lp uintptr) uintptr {
	p := thePill
	switch m {
	case wmPaint:
		p.paint(hwnd)
		return 0
	case wmEraseBkgnd:
		return 1
	case wmMouseActivate:
		return maNoActivate
	case wmLButtonDown:
		// Use the coordinates carried by the message, not the cursor "now":
		// input routed through Explorer's queue can be handled late.
		x := msgScreenX(hwnd, lp)
		p.mu.Lock()
		p.pressed, p.dragging = true, false
		p.downX, p.downLeft, p.downAt = x, p.screen.L, time.Now()
		p.mu.Unlock()
		pSetCapture.Call(hwnd)
		return 0
	case wmMouseMove:
		p.mu.Lock()
		was := p.hover
		p.hover = true
		pressed := p.pressed
		p.mu.Unlock()
		if !was {
			tme := trackMouseEvent{Size: uint32(unsafe.Sizeof(trackMouseEvent{})), Flags: tmeLeave, HwndTrack: hwnd}
			pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			pInvalidateRect.Call(hwnd, 0, 0)
		}
		if pressed && !primaryButtonDown() {
			// The button was released without us seeing WM_LBUTTONUP: no drag.
			p.mu.Lock()
			wasDrag := p.dragging
			p.pressed, p.dragging = false, false
			p.mu.Unlock()
			pReleaseCapture.Call()
			if wasDrag {
				p.layout()
			}
			return 0
		}
		if pressed {
			x := msgScreenX(hwnd, lp)
			p.mu.Lock()
			dx := x - p.downX
			// A drag needs real movement while the button is held for a
			// moment; a click (even a slightly shaky one) never moves the pill.
			threshold := int32(6 * dpiScale(hwnd))
			if !p.dragging && (dx > threshold || dx < -threshold) && time.Since(p.downAt) > 150*time.Millisecond {
				p.dragging = true
			}
			if p.dragging {
				p.dragLeft = p.downLeft + dx
			}
			drag := p.dragging
			p.mu.Unlock()
			if drag {
				p.layout()
			}
		}
		return 0
	case wmCaptureChanged:
		// Capture taken away mid-drag (e.g. Start opened): snap back.
		p.mu.Lock()
		wasDrag := p.dragging
		p.pressed, p.dragging = false, false
		p.mu.Unlock()
		if wasDrag {
			p.layout()
		}
		return 0
	case wmMouseLeave:
		p.mu.Lock()
		p.hover = false
		p.mu.Unlock()
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmRButtonUp:
		p.mu.Lock()
		host := p.host
		p.mu.Unlock()
		if cmd := showPillMenu(host); cmd != 0 && p.onMenu != nil {
			go p.onMenu(cmd)
		}
		return 0
	case wmLButtonUp:
		p.mu.Lock()
		pressed, dragging := p.pressed, p.dragging
		p.pressed = false
		p.mu.Unlock()
		if dragging {
			p.finishDrag() // releases capture → WM_CAPTURECHANGED clears state
			return 0
		}
		pReleaseCapture.Call()
		// The same click can arrive twice (input is routed through Explorer's
		// queue); one click per 250 ms is plenty.
		if pressed && time.Since(p.lastClick) > 250*time.Millisecond && p.onClick != nil {
			p.lastClick = time.Now()
			go p.onClick(p.screenRect())
		}
		return 0
	case wmDestroy:
		p.mu.Lock()
		if p.hwnd == hwnd {
			p.hwnd = 0
		}
		p.mu.Unlock()
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}

// ---------- painting (window thread) ----------

// Current palette, set from the taskbar theme at the start of each paint
// (window thread only).
var colBg, colBorder, colBorderHi, colTrack, colText, colMuted, colMarker rgb

func useTheme(t pillTheme) {
	colBg, colBorder, colBorderHi, colTrack = t.Bg, t.Border, t.BorderHi, t.Track
	colText, colMuted, colMarker = t.Text, t.Muted, t.Marker
}

func (p *pill) paint(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	useTheme(currentTheme())

	p.mu.Lock()
	rows, hover, width := p.rows, p.hover, p.width
	p.mu.Unlock()

	wr, _ := windowRect(hwnd)
	W, H := wr.w(), wr.h()
	s := dpiScale(hwnd)
	px := func(v float64) int32 { return int32(v*s + 0.5) }

	// Double buffer.
	mdc, _, _ := pCreateCompatibleDC.Call(hdc)
	bmp, _, _ := pCreateCompatibleBitmap.Call(hdc, uintptr(W), uintptr(H))
	oldBmp, _, _ := pSelectObject.Call(mdc, bmp)
	defer func() {
		pSelectObject.Call(mdc, oldBmp)
		pDeleteObject.Call(bmp)
		pDeleteDC.Call(mdc)
	}()

	// Background + border.
	border := colBorder
	if hover {
		border = colBorderHi
	}
	fillRound(mdc, rect{0, 0, W, H}, colBg, border, px(8))
	pSetBkMode.Call(mdc, 1) // TRANSPARENT

	fName := makeFont(px(11), 600)
	fSmall := makeFont(px(10.5), 400)
	fPct := makeFont(px(11), 700)
	fWord := makeFont(px(9.5), 500)
	defer func() {
		for _, f := range []uintptr{fName, fSmall, fPct, fWord} {
			pDeleteObject.Call(f)
		}
	}()

	n := int32(len(rows))
	if n == 0 {
		drawText(mdc, fSmall, colMuted, "Loading…", rect{0, 0, W, H}, dtCenter)
		pBitBlt.Call(hdc, 0, 0, uintptr(W), uintptr(H), mdc, 0, 0, 0x00CC0020)
		return
	}
	rowH, rowGap := px(14), px(3)
	y0 := (H - (n*rowH + (n-1)*rowGap)) / 2

	pad, logo, cg := px(10), px(14), px(6)
	pctW, resetW, nameW := px(54), px(46), int32(0)
	if width >= 260 {
		nameW = px(70)
	}
	for i, r := range rows {
		y := y0 + int32(i)*(rowH+rowGap)
		x := pad
		p.drawLogo(mdc, r.ID, r.Name, x, y, logo)
		x += logo + cg
		if nameW > 0 {
			drawText(mdc, fName, colText, r.Name, rect{x, y, x + nameW, y + rowH}, dtLeft)
			x += nameW + cg
		}
		pctR := W - pad
		resetR := pctR - pctW - cg
		if !r.HasBar {
			drawText(mdc, fSmall, colMuted, r.NA, rect{x, y, pctR, y + rowH}, dtLeft)
			continue
		}
		barR := resetR - resetW - cg
		if barR > x {
			th := max(px(4), 2)
			ty := y + (rowH-th)/2
			fillRound(mdc, rect{x, ty, barR, ty + th}, colTrack, colTrack, th)
			if fw := int32(float64(barR-x) * r.Fill / 100); fw > 0 {
				fillRound(mdc, rect{x, ty, x + max(fw, th), ty + th}, r.Color, r.Color, th)
			}
			// Pace marker: how much of the window's time has passed.
			if r.Marker >= 0 {
				mx := x + int32(float64(barR-x-px(2))*r.Marker/100)
				fillRect(mdc, rect{mx, ty - px(3), mx + max(px(2), 1), ty + th + px(3)}, colMarker)
			}
		}
		drawText(mdc, fSmall, colMuted, r.Reset, rect{resetR - resetW, y, resetR, y + rowH}, dtRight)
		// "40%" bold + " used" small, right-aligned together.
		wordW := textWidth(mdc, fWord, r.Word)
		drawText(mdc, fWord, colMuted, r.Word, rect{pctR - wordW, y + px(1), pctR, y + rowH}, dtRight)
		drawText(mdc, fPct, colText, itoaPct(r.Value), rect{pctR - pctW, y, pctR - wordW - px(2), y + rowH}, dtRight)
	}
	pBitBlt.Call(hdc, 0, 0, uintptr(W), uintptr(H), mdc, 0, 0, 0x00CC0020) // SRCCOPY
}

func itoaPct(v int) string { return strconv.Itoa(v) + "%" }

const (
	dtLeft   = 0x0000 | 0x0004 | 0x0020 | 0x0800 | 0x8000 // VCENTER|SINGLELINE|NOPREFIX|END_ELLIPSIS
	dtRight  = 0x0002 | 0x0004 | 0x0020 | 0x0800
	dtCenter = 0x0001 | 0x0004 | 0x0020 | 0x0800
)

func makeFont(px int32, weight int) uintptr {
	f, _, _ := pCreateFontW.Call(uintptr(-px), 0, 0, 0, uintptr(weight), 0, 0, 0,
		1 /*DEFAULT_CHARSET*/, 0, 0, 5 /*CLEARTYPE_QUALITY*/, 0, uintptr(unsafe.Pointer(utf16("Segoe UI"))))
	return f
}

func drawText(dc, font uintptr, c rgb, s string, r rect, flags uintptr) {
	if s == "" {
		return
	}
	old, _, _ := pSelectObject.Call(dc, font)
	pSetTextColor.Call(dc, colorRef(c))
	pDrawTextW.Call(dc, uintptr(unsafe.Pointer(utf16(s))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), flags)
	pSelectObject.Call(dc, old)
}

func textWidth(dc, font uintptr, s string) int32 {
	old, _, _ := pSelectObject.Call(dc, font)
	defer pSelectObject.Call(dc, old)
	u := utf16(s)
	n := 0
	for p := unsafe.Pointer(u); *(*uint16)(p) != 0; p = unsafe.Add(p, 2) {
		n++
	}
	var sz struct{ CX, CY int32 }
	pGetTextExtentPoint32W.Call(dc, uintptr(unsafe.Pointer(u)), uintptr(n), uintptr(unsafe.Pointer(&sz)))
	return sz.CX
}

func fillRect(dc uintptr, r rect, c rgb) {
	brush, _, _ := pCreateSolidBrush.Call(colorRef(c))
	pFillRect.Call(dc, uintptr(unsafe.Pointer(&r)), brush)
	pDeleteObject.Call(brush)
}

func fillRound(dc uintptr, r rect, fill, line rgb, radius int32) {
	brush, _, _ := pCreateSolidBrush.Call(colorRef(fill))
	pen, _, _ := pCreatePen.Call(0, 1, colorRef(line))
	ob, _, _ := pSelectObject.Call(dc, brush)
	op, _, _ := pSelectObject.Call(dc, pen)
	pRoundRect.Call(dc, uintptr(r.L), uintptr(r.T), uintptr(r.R), uintptr(r.B), uintptr(radius), uintptr(radius))
	pSelectObject.Call(dc, ob)
	pSelectObject.Call(dc, op)
	pDeleteObject.Call(brush)
	pDeleteObject.Call(pen)
}

// drawLogo draws the provider's PNG icon (alpha-blended), or a letter badge.
func (p *pill) drawLogo(dc uintptr, id, name string, x, y, size int32) {
	bmp := p.icon(id, size)
	if bmp == 0 {
		fillRound(dc, rect{x, y, x + size, y + size}, rgb{85, 85, 90}, rgb{85, 85, 90}, size/4)
		letter := "?"
		if name != "" {
			letter = name[:1]
		}
		f := makeFont(size*2/3, 700)
		drawText(dc, f, colText, letter, rect{x, y, x + size, y + size}, dtCenter)
		pDeleteObject.Call(f)
		return
	}
	src, _, _ := pCreateCompatibleDC.Call(dc)
	old, _, _ := pSelectObject.Call(src, bmp)
	blend := uintptr(255<<16 | 1<<24) // SourceConstantAlpha=255, AC_SRC_ALPHA
	pAlphaBlend.Call(dc, uintptr(x), uintptr(y), uintptr(size), uintptr(size), src, 0, 0, uintptr(size), uintptr(size), blend)
	pSelectObject.Call(src, old)
	pDeleteDC.Call(src)
}

// icon returns a cached premultiplied 32-bit DIB of the provider logo.
func (p *pill) icon(id string, size int32) uintptr {
	k := iconKey{id, size}
	if h, ok := p.icons[k]; ok {
		return h
	}
	var h uintptr
	if data, ok := p.logos[id]; ok {
		if img, err := png.Decode(bytes.NewReader(data)); err == nil {
			h = makeDIB(img, int(size))
		}
	}
	p.icons[k] = h
	return h
}

func makeDIB(img image.Image, size int) uintptr {
	bi := bitmapInfoHeader{Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(size), Height: -int32(size), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	h, _, _ := pCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if h == 0 || bits == nil {
		return 0
	}
	buf := unsafe.Slice((*byte)(bits), size*size*4)
	b := img.Bounds()
	sw, sh := float64(b.Dx())/float64(size), float64(b.Dy())/float64(size)
	for dy := 0; dy < size; dy++ {
		for dx := 0; dx < size; dx++ {
			// Box filter over the source area covered by this pixel.
			x0, x1 := b.Min.X+int(float64(dx)*sw), b.Min.X+max(int(float64(dx+1)*sw), int(float64(dx)*sw)+1)
			y0, y1 := b.Min.Y+int(float64(dy)*sh), b.Min.Y+max(int(float64(dy+1)*sh), int(float64(dy)*sh)+1)
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := img.At(sx, sy).RGBA() // premultiplied, 16-bit
					r, g, bl, a, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), n+1
				}
			}
			if n == 0 {
				continue
			}
			i := (dy*size + dx) * 4
			buf[i+0] = byte(bl / n >> 8)
			buf[i+1] = byte(g / n >> 8)
			buf[i+2] = byte(r / n >> 8)
			buf[i+3] = byte(a / n >> 8)
		}
	}
	return h
}
