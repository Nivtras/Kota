//go:build windows

// Package taskbar is the native pill that lives inside the Windows taskbar:
// one on the main taskbar and, optionally, one per secondary taskbar. It
// draws the rows the application layer computes and reports clicks, drags
// and menu choices back.
package taskbar

import (
	"kota/internal/application"
	"kota/internal/infrastructure/win32"
)

// Options configure the pills.
type Options struct {
	Width   int // logical px
	Anchor  string
	Offset  int
	Offset0 bool // true: no stored offset, use the default
	Logos   map[string][]byte
	Multi   bool // a pill on every monitor's taskbar

	OnClick func(pill win32.Rect)           // screen rect of the clicked pill
	OnMoved func(anchor string, offset int) // after the user dragged a pill
	OnMenu  func(cmd int)                   // right-click menu choice
}

// Pills is the set of taskbar pills.
type Pills struct{ set *pillSet }

// Start creates the pills (each on its own window thread).
func Start(o Options) *Pills {
	off := o.Offset
	if o.Offset0 {
		off = defaultOffset
	}
	primary := newPill(o.Width, o.Anchor, off, o.Logos, o.OnClick, o.OnMoved)
	primary.onMenu = o.OnMenu
	return &Pills{newPillSet(primary, o.Multi)}
}

// SetRows shows new rows on every pill.
func (p *Pills) SetRows(rows []application.PillRow) { p.set.setRows(rows) }

// SetWidth resizes every pill (logical px).
func (p *Pills) SetWidth(w int) { p.set.setWidth(w) }

// SetPosition moves every pill to an anchor/offset.
func (p *Pills) SetPosition(anchor string, offset int) { p.set.setPosition(anchor, offset) }

// ResetPosition puts the pills back right before the Start button.
func (p *Pills) ResetPosition() { p.set.setPosition(anchorStart, defaultOffset) }

// SetMulti shows a pill on every monitor's taskbar, or only the main one.
func (p *Pills) SetMulti(on bool) { p.set.setMulti(on) }

// ScreenRect is the pill under the cursor, else the main one.
func (p *Pills) ScreenRect() win32.Rect { return p.set.screenRect() }

// Has reports whether pt is on any pill.
func Has(pt win32.Point) bool { return anyPillHas(pt) }
