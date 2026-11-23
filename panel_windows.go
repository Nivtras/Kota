//go:build windows

package main

// The panel is the Wails window: an ordinary top-level popup that is shown
// above the pill and hidden again. It is never re-parented, so focus, input
// and z-order behave like any normal window.

import (
	"kota/internal/infrastructure/storage"
	"kota/internal/infrastructure/win32"
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
		if h = win32.FindWindow("", title); h == 0 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if h == 0 {
		storage.Log("panel: window not found")
		return
	}
	ex, _, _ := win32.GetWindowLongPtrW.Call(h, win32.GwlExStyle)
	win32.SetWindowLongPtrW.Call(h, win32.GwlExStyle, (ex|win32.WsExToolWindow)&^win32.WsExAppWindow)
	pn.mu.Lock()
	pn.hwnd = h
	pn.mu.Unlock()
	go pn.watchOutside()
}

// toggle opens the panel above the pill, or closes it. One at a time.
func (pn *panel) toggle(pillRect win32.Rect) {
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

func (pn *panel) show(pill win32.Rect) {
	pn.mu.Lock()
	h := pn.hwnd
	pn.mu.Unlock()
	if h == 0 {
		return
	}
	// The taskbar of the monitor whose pill was clicked.
	tb := taskbarAt(win32.Pt((pill.L+pill.R)/2, (pill.T+pill.B)/2))
	tbr, ok := win32.WindowRect(tb)
	if !ok {
		return
	}
	s := win32.DPIScale(tb)
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
	win32.SetWindowPos.Call(h, win32.HwndTopmost, uintptr(x), uintptr(y), uintptr(w), uintptr(ht), win32.SwpShowWindow)
	win32.ForceForeground(h)
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
		win32.ShowWindow.Call(h, win32.SwHide)
	}
}

// watchOutside closes the panel on a click outside it when it does not have
// focus (if Windows refused to give it focus, no blur event would ever come).
// With focus, the web view's blur handler closes it.
func (pn *panel) watchOutside() {
	btn := uintptr(0x01)                                                // VK_LBUTTON
	if swapped, _, _ := win32.GetSystemMetrics.Call(23); swapped != 0 { // SM_SWAPBUTTON
		btn = 0x02
	}
	var wasDown, pressedOutside, escWasDown bool
	for range time.Tick(50 * time.Millisecond) {
		pn.mu.Lock()
		h, visible := pn.hwnd, pn.visible
		pn.mu.Unlock()
		st, _, _ := win32.GetAsyncKeyState.Call(btn)
		down := st&0x8000 != 0
		esc, _, _ := win32.GetAsyncKeyState.Call(0x1B) // VK_ESCAPE
		escDown := esc&0x8000 != 0
		if !visible {
			wasDown, pressedOutside, escWasDown = down, false, escDown
			continue
		}
		fg, _, _ := win32.GetForegroundWindow.Call()
		// Esc while the panel has focus closes it (the page may not have
		// keyboard focus inside the web view, so don't rely on its keydown).
		if escDown && !escWasDown && fg == h {
			pn.hide()
		}
		escWasDown = escDown
		var pt win32.Point
		win32.GetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		r, _ := win32.WindowRect(h)
		outside := fg != h && !r.Has(pt.X, pt.Y) && !anyPillHas(pt)
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
