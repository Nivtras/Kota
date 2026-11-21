package main

// What the taskbar pill shows, computed in Go so the native pill window does
// not depend on the (hidden) web view. Mirrors the panel's display rules.

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"aiusagewidget/providers"
)

type rgb struct{ R, G, B uint8 }

var (
	colOK   = rgb{61, 220, 132}
	colWarn = rgb{245, 185, 66}
	colBad  = rgb{242, 109, 109}
)

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
}

var (
	fiveHourRe = regexp.MustCompile(`(?i)\b5[\s-]?h(our)?\b`)
	// Provider → quota group the pill should use (Antigravity: Gemini Models).
	pillGroup = map[string]*regexp.Regexp{"antigravity": regexp.MustCompile(`(?i)gemini`)}
)

func usageColor(used float64) rgb {
	switch {
	case used >= 85:
		return colBad
	case used >= 60:
		return colWarn
	}
	return colOK
}

// resetShort mirrors the panel: "now", "45m", "2h 14m", or "Sun 11:00".
func resetShort(iso string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return ""
	}
	d := t.Sub(now)
	if d <= 0 {
		return "now"
	}
	m := int(math.Round(d.Minutes()))
	switch {
	case m < 60:
		return fmt.Sprintf("%dm", m)
	case m < 24*60:
		return fmt.Sprintf("%dh %dm", m/60, m%60)
	}
	return t.Local().Format("Mon 15:04")
}

type pillBar struct {
	providers.Bar
	group string
}

// pickBar returns the bar the pill shows for a provider: its 5-hour limit
// (most used one if several), else its most-used limit. Windows whose reset
// time has passed count as 0% used.
func pickBar(u providers.Usage, now time.Time) (pillBar, bool) {
	groups := u.Groups
	if re := pillGroup[u.ID]; re != nil {
		var only []providers.Group
		for _, g := range groups {
			if re.MatchString(g.Name) {
				only = append(only, g)
			}
		}
		if len(only) > 0 {
			groups = only
		}
	}
	var all, fiveH []pillBar
	for _, g := range groups {
		for _, b := range g.Bars {
			if t, err := time.Parse(time.RFC3339, b.ResetsAt); err == nil && !t.After(now) {
				b.UsedPct, b.ResetsAt = 0, ""
			}
			pb := pillBar{b, g.Name}
			all = append(all, pb)
			if fiveHourRe.MatchString(b.Label) {
				fiveH = append(fiveH, pb)
			}
		}
	}
	pool := all
	if len(fiveH) > 0 {
		pool = fiveH
	}
	if len(pool) == 0 {
		return pillBar{}, false
	}
	best := pool[0]
	for _, b := range pool[1:] {
		if b.UsedPct > best.UsedPct {
			best = b
		}
	}
	return best, true
}

// taskbarProviders: the user's choice (max 2, in provider order), else the
// first two providers.
func taskbarProviders(items []providers.Usage, chosen []string) []providers.Usage {
	var out []providers.Usage
	if len(chosen) > 0 {
		for _, u := range items {
			for _, id := range chosen {
				if u.ID == id {
					out = append(out, u)
				}
			}
		}
	}
	if len(out) == 0 {
		out = items
	}
	if len(out) > 2 {
		out = out[:2]
	}
	return out
}

func buildPillRows(items []providers.Usage, s Settings, now time.Time) []pillRow {
	var rows []pillRow
	for _, u := range taskbarProviders(items, s.Taskbar) {
		r := pillRow{ID: u.ID, Name: u.Name}
		b, ok := pickBar(u, now)
		if !ok {
			r.NA = "—"
			if u.Error != "" {
				r.NA = "unavailable"
			}
			r.Tooltip = u.Name + "\n" + firstNonEmpty(u.Error, u.Note, "No data")
			rows = append(rows, r)
			continue
		}
		used := math.Max(0, math.Min(100, b.UsedPct))
		r.HasBar, r.Color = true, usageColor(used)
		r.Fill, r.Word = used, "used"
		if s.Display == "remaining" {
			r.Fill, r.Word = 100-used, "left"
		}
		r.Value = int(math.Floor(r.Fill))
		r.Reset = resetShort(b.ResetsAt, now)
		label := strings.Join(nonEmpty(b.group, b.Label), " · ")
		tip := []string{u.Name + " · " + label, fmt.Sprintf("%d%% %s", r.Value, r.Word)}
		if t, err := time.Parse(time.RFC3339, b.ResetsAt); err == nil {
			tip = append(tip, "Resets "+t.Local().Format("Mon 2 Jan 15:04"))
		}
		r.Tooltip = strings.Join(tip, "\n")
		rows = append(rows, r)
	}
	return rows
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
