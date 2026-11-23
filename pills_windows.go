//go:build windows

package main

// pillSet: the primary pill plus, when "all monitors" is on, one pill per
// secondary taskbar (Shell_SecondaryTrayWnd). It keeps the set in sync as
// monitors come and go and when Explorer restarts.

import (
	"kota/internal/infrastructure/win32"
	"sync"
	"time"
	"unsafe"
)

type pillSet struct {
	primary *pill

	mu          sync.Mutex
	multi       bool
	secondaries map[uintptr]*pill // by secondary taskbar HWND
}

func newPillSet(primary *pill, multi bool) *pillSet {
	ps := &pillSet{primary: primary, multi: multi, secondaries: map[uintptr]*pill{}}
	go func() {
		ps.sync()
		for range time.Tick(3 * time.Second) {
			ps.sync()
		}
	}()
	return ps
}

// secondaryTaskbars lists the taskbars of the other monitors.
func secondaryTaskbars() []uintptr {
	var out []uintptr
	cls := win32.UTF16Ptr("Shell_SecondaryTrayWnd")
	var prev uintptr
	for i := 0; i < 16; i++ {
		h, _, _ := win32.FindWindowExW.Call(0, prev, uintptr(unsafe.Pointer(cls)), 0)
		if h == 0 {
			break
		}
		out = append(out, h)
		prev = h
	}
	return out
}

func (ps *pillSet) sync() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	want := map[uintptr]bool{}
	if ps.multi {
		for _, tb := range secondaryTaskbars() {
			want[tb] = true
		}
	}
	for tb, p := range ps.secondaries {
		if !want[tb] || p.closed.Load() {
			p.close()
			delete(ps.secondaries, tb)
		}
	}
	for tb := range want {
		if ps.secondaries[tb] == nil {
			ps.secondaries[tb] = newSecondaryPill(ps.primary, tb)
		}
	}
}

func (ps *pillSet) each(f func(*pill)) {
	f(ps.primary)
	ps.mu.Lock()
	secs := make([]*pill, 0, len(ps.secondaries))
	for _, p := range ps.secondaries {
		secs = append(secs, p)
	}
	ps.mu.Unlock()
	for _, p := range secs {
		f(p)
	}
}

func (ps *pillSet) setRows(rows []pillRow) { ps.each(func(p *pill) { p.setRows(rows) }) }
func (ps *pillSet) setWidth(w int)         { ps.each(func(p *pill) { p.setWidth(w) }) }
func (ps *pillSet) setPosition(anchor string, off int) {
	ps.each(func(p *pill) { p.setPosition(anchor, off) })
}

// screenRect is the pill under the cursor (e.g. the one right-clicked),
// else the primary one.
func (ps *pillSet) screenRect() win32.Rect {
	var pt win32.Point
	win32.GetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	for _, p := range allPills() {
		if r := p.screenRect(); r.Has(pt.X, pt.Y) {
			return r
		}
	}
	return ps.primary.screenRect()
}

func (ps *pillSet) setMulti(on bool) {
	ps.mu.Lock()
	ps.multi = on
	ps.mu.Unlock()
	ps.sync()
}

// anyPillHas reports whether pt is on any pill (screen px).
func anyPillHas(pt win32.Point) bool {
	for _, p := range allPills() {
		if p.screenRect().Has(pt.X, pt.Y) {
			return true
		}
	}
	return false
}

// taskbarAt returns the taskbar (primary or secondary) containing pt,
// falling back to the primary one.
func taskbarAt(pt win32.Point) uintptr {
	primary := win32.FindWindow("Shell_TrayWnd", "")
	for _, tb := range append([]uintptr{primary}, secondaryTaskbars()...) {
		if r, ok := win32.WindowRect(tb); ok && r.Has(pt.X, pt.Y) {
			return tb
		}
	}
	return primary
}
