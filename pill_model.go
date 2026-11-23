package main

// Pill colours: the domain classifies usage (ok / warn / bad), the pill
// paints it.

import (
	"kota/internal/application"
	"kota/internal/domain"
)

// pillRow is one line of the pill (built by the application layer).
type pillRow = application.PillRow

type rgb struct{ R, G, B uint8 }

var (
	colOK   = rgb{61, 220, 132}
	colWarn = rgb{245, 185, 66}
	colBad  = rgb{242, 109, 109}
)

func levelColor(l domain.Level) rgb {
	switch l {
	case domain.LevelBad:
		return colBad
	case domain.LevelWarn:
		return colWarn
	}
	return colOK
}

// colorRef is a GDI COLORREF.
func colorRef(c rgb) uintptr { return uintptr(c.R) | uintptr(c.G)<<8 | uintptr(c.B)<<16 }
