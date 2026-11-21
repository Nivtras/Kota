//go:build windows

package main

// The panel is the Wails window: an ordinary top-level popup that is shown
// above the pill and hidden again. It is never re-parented, so focus, input
// and z-order behave like any normal window.

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"
)

const (
	panelW = 360 // logical px
	panelH = 480
)

type panel struct {
	mu       sync.Mutex
	hwnd     uintptr
	visible  bool
	lastHide time.Time
	shownAt  time.Time
	busy     sync.Mutex // serialises toggle()
	onShown  func()
}

// attach finds the Wails window and hides it from the taskbar / Alt-Tab.
func (pn *panel) attach(title string) {
	var h uintptr
	for i := 0; i < 100 && h == 0; i++ {
		if h = findWindow("", title); h == 0 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if h == 0 {
		dockLog("panel: window not found")
		return
	}
	ex, _, _ := pGetWindowLongPtrW.Call(h, gwlExStyle)
	pSetWindowLongPtrW.Call(h, gwlExStyle, (ex|wsExToolWindow)&^wsExAppWindow)
	pn.mu.Lock()
	pn.hwnd = h
	pn.mu.Unlock()
	go pn.watchOutside()
}

// toggle opens the panel above the pill, or closes it. One at a time.
func (pn *panel) toggle(pillRect rect) {
	if !pn.busy.TryLock() {
		return
	}
	defer pn.busy.Unlock()
	pn.mu.Lock()
	visible, recent := pn.visible, time.Since(pn.lastHide) < 400*time.Millisecond
	pn.mu.Unlock()
	switch {
	case visible:
		pn.hide()
	case recent:
		// The click on the pill is what just closed the panel (it lost focus
		// a moment ago); don't re-open it.
	default:
		pn.show(pillRect)
	}
}

func (pn *panel) show(pill rect) {
	pn.mu.Lock()
	h := pn.hwnd
	pn.mu.Unlock()
	if h == 0 {
		return
	}
	tb := findWindow("Shell_TrayWnd", "")
	tbr, ok := windowRect(tb)
	if !ok {
		return
	}
	s := dpiScale(tb)
	px := func(v int) int32 { return int32(float64(v)*s + 0.5) }
	w, ht := px(panelW), px(panelH)
	x := pill.R - w // right-aligned with the pill
	if pill.R == 0 {
		x = tbr.R - w - px(gap)
	}
	x = max(tbr.L+px(gap), min(x, tbr.R-px(gap)-w))
	y := tbr.T - px(gap) - ht
	pn.mu.Lock()
	pn.visible, pn.shownAt = true, time.Now()
	pn.mu.Unlock()
	pSetWindowPos.Call(h, hwndTopmost, uintptr(x), uintptr(y), uintptr(w), uintptr(ht), swpShowWindow)
	forceForeground(h)
	if pn.onShown != nil {
		pn.onShown()
	}
}

// hideFromUI handles the page's close requests (blur, Esc, chevron). A blur
// in the first moment after showing is a side effect of taking focus.
func (pn *panel) hideFromUI() {
	pn.mu.Lock()
	early := time.Since(pn.shownAt) < 500*time.Millisecond
	pn.mu.Unlock()
	if !early {
		pn.hide()
	}
}

func (pn *panel) hide() {
	pn.mu.Lock()
	h, was := pn.hwnd, pn.visible
	pn.visible = false
	if was {
		pn.lastHide = time.Now()
	}
	pn.mu.Unlock()
	if h != 0 && was {
		pShowWindow.Call(h, swHide)
	}
}

// watchOutside closes the panel on a click outside it when it does not have
// focus (if Windows refused to give it focus, no blur event would ever come).
// With focus, the web view's blur handler closes it.
func (pn *panel) watchOutside() {
	btn := uintptr(0x01)                                           // VK_LBUTTON
	if swapped, _, _ := pGetSystemMetrics.Call(23); swapped != 0 { // SM_SWAPBUTTON
		btn = 0x02
	}
	var wasDown, pressedOutside, escWasDown bool
	for range time.Tick(50 * time.Millisecond) {
		pn.mu.Lock()
		h, visible := pn.hwnd, pn.visible
		pn.mu.Unlock()
		st, _, _ := pGetAsyncKeyState.Call(btn)
		down := st&0x8000 != 0
		esc, _, _ := pGetAsyncKeyState.Call(0x1B) // VK_ESCAPE
		escDown := esc&0x8000 != 0
		if !visible {
			wasDown, pressedOutside, escWasDown = down, false, escDown
			continue
		}
		fg, _, _ := pGetForegroundWindow.Call()
		// Esc while the panel has focus closes it (the page may not have
		// keyboard focus inside the web view, so don't rely on its keydown).
		if escDown && !escWasDown && fg == h {
			pn.hide()
		}
		escWasDown = escDown
		var pt point
		pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		r, _ := windowRect(h)
		outside := fg != h && !r.has(pt.X, pt.Y) && !thePillHas(pt)
		switch {
		case down && !wasDown:
			pressedOutside = outside
		case !down && wasDown:
			if pressedOutside && outside {
				pn.hide()
			}
			pressedOutside = false
		}
		wasDown = down
	}
}

func thePillHas(pt point) bool {
	return thePill != nil && thePill.screenRect().has(pt.X, pt.Y)
}

// dockLog appends a line to %APPDATA%\AIUsageWidget\dock.log (kept small).
func dockLog(format string, args ...any) {
	p := filepath.Join(filepath.Dir(configPath()), "dock.log")
	if fi, err := os.Stat(p); err == nil && fi.Size() > 64<<10 {
		_ = os.Remove(p)
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s "+format+"\n", append([]any{time.Now().Format(time.DateTime)}, args...)...)
}
