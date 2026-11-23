package main

// What the taskbar pill shows, computed in Go so the native pill window does
// not depend on the (hidden) web view. Mirrors the panel's display rules.

import (
	"fmt"
	"math"
	"strings"
	"time"

	"kota/internal/domain"
)

type rgb struct{ R, G, B uint8 }

var (
	colOK   = rgb{61, 220, 132}
	colWarn = rgb{245, 185, 66}
	colBad  = rgb{242, 109, 109}
)

// colorRef is a GDI COLORREF.
func colorRef(c rgb) uintptr { return uintptr(c.R) | uintptr(c.G)<<8 | uintptr(c.B)<<16 }

func usageColor(used float64) rgb {
	switch domain.LevelOf(used) {
	case domain.LevelBad:
		return colBad
	case domain.LevelWarn:
		return colWarn
	}
	return colOK
}

// pillRow is one line of the pill.
type pillRow struct {
	ID, Name string
	HasBar   bool    // false: show NA text instead of a bar
	NA       string  // "unavailable" / "—"
	Fill     float64 // bar fill 0..100 (used or remaining, per setting)
	Value    int     // number shown, percent
	Word     string  // "used" | "left"
	Color    rgb     // by usage, so red always means "near the limit"
	Reset    string  // short reset text ("2h 14m", "Sun 11:00")
	Tooltip  string
	// Pace marker position on the bar (0..100, same orientation as Fill),
	// or -1 when the window length or reset time is unknown.
	Marker float64
}

func buildPillRows(items []domain.Usage, s Settings, now time.Time) []pillRow {
	var rows []pillRow
	list := domain.TaskbarProviders(items, s.Taskbar)
	if s.Follow {
		list = domain.WithActiveFirst(items, list, s.active)
	}
	remaining := s.Display == domain.DisplayRemaining
	for _, u := range list {
		r := pillRow{ID: u.ID, Name: u.Name}
		b, ok := domain.PickBar(u, now)
		if !ok {
			r.NA = "—"
			if u.Error != "" {
				r.NA = "unavailable"
			}
			r.Tooltip = u.Name + "\n" + domain.FirstNonEmpty(u.Error, u.Note, "No data")
			rows = append(rows, r)
			continue
		}
		used := math.Max(0, math.Min(100, b.UsedPct))
		r.HasBar, r.Color = true, usageColor(used)
		r.Fill, r.Word = used, "used"
		if remaining {
			r.Fill, r.Word = 100-used, "left"
		}
		r.Value = int(math.Floor(r.Fill))
		r.Reset = domain.ResetShort(b.ResetsAt, now)
		r.Marker = -1
		pace := domain.PaceOf(b.Bar, now)
		if pace.OK {
			r.Marker = pace.ElapsedPct
			if remaining {
				r.Marker = 100 - pace.ElapsedPct
			}
		}
		label := b.Label
		if b.Group != "" {
			label = b.Group + " · " + b.Label
		}
		tip := []string{u.Name + " · " + label, fmt.Sprintf("%d%% %s", r.Value, r.Word)}
		if t, err := time.Parse(time.RFC3339, b.ResetsAt); err == nil {
			tip = append(tip, "Resets "+t.Local().Format("Mon 2 Jan 15:04"))
		}
		if pace.RunsOut {
			tip = append(tip, "At this pace: limit reached in "+domain.ShortDuration(pace.RunsOutIn))
		}
		r.Tooltip = strings.Join(tip, "\n")
		rows = append(rows, r)
	}
	return rows
}
