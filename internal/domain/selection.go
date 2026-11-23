package domain

import (
	"regexp"
	"time"
)

// Level classifies how close a limit is, so every view can colour it the
// same way: red always means "near the limit".
type Level int

const (
	LevelOK Level = iota
	LevelWarn
	LevelBad
)

// LevelOf returns the level for a used percentage.
func LevelOf(used float64) Level {
	switch {
	case used >= 85:
		return LevelBad
	case used >= 60:
		return LevelWarn
	}
	return LevelOK
}

var (
	fiveHourRe = regexp.MustCompile(`(?i)\b5[\s-]?h(our)?\b`)
	// Provider → quota group the pill should use (Antigravity: Gemini Models).
	pillGroup = map[string]*regexp.Regexp{"antigravity": regexp.MustCompile(`(?i)gemini`)}
)

// PickedBar is a limit together with the quota group it belongs to.
type PickedBar struct {
	Bar
	Group string
}

// PickBar returns the limit the taskbar shows for a provider: its 5-hour
// limit (the most used one if several), else its most-used limit. Expired
// windows count as 0% used.
func PickBar(u Usage, now time.Time) (PickedBar, bool) {
	groups := u.Groups
	if re := pillGroup[u.ID]; re != nil {
		var only []Group
		for _, g := range groups {
			if re.MatchString(g.Name) {
				only = append(only, g)
			}
		}
		if len(only) > 0 {
			groups = only
		}
	}
	var all, fiveH []PickedBar
	for _, g := range groups {
		for _, b := range g.Bars {
			if b.Expired(now) {
				b.UsedPct, b.ResetsAt = 0, ""
			}
			pb := PickedBar{b, g.Name}
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
		return PickedBar{}, false
	}
	best := pool[0]
	for _, b := range pool[1:] {
		if b.UsedPct > best.UsedPct {
			best = b
		}
	}
	return best, true
}

// ChartBars picks up to two limits to chart for a provider: the one the
// taskbar shows and the longest other window in the same pool.
func ChartBars(u Usage, now time.Time) []PickedBar {
	first, ok := PickBar(u, now)
	if !ok {
		return nil
	}
	out := []PickedBar{first}
	var long *PickedBar
	for _, g := range u.Groups {
		if g.Name != first.Group {
			continue
		}
		for _, b := range g.Bars {
			if b.Label == first.Label || b.WindowSec <= first.WindowSec {
				continue
			}
			if long == nil || b.WindowSec > long.WindowSec {
				long = &PickedBar{b, g.Name}
			}
		}
	}
	if long != nil {
		out = append(out, *long)
	}
	return out
}

// MaxTaskbarProviders is how many providers fit on the taskbar pill.
const MaxTaskbarProviders = 2

// TaskbarProviders: the user's choice (in provider order), else the first
// providers, at most MaxTaskbarProviders.
func TaskbarProviders(items []Usage, chosen []string) []Usage {
	var out []Usage
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
	if len(out) > MaxTaskbarProviders {
		out = out[:MaxTaskbarProviders]
	}
	return out
}

// WithActiveFirst puts the provider of the tool in use first (follow mode),
// keeping the user's other choice second.
func WithActiveFirst(items, chosen []Usage, active string) []Usage {
	if active == "" {
		return chosen
	}
	var out []Usage
	for _, u := range items {
		if u.ID == active {
			out = append(out, u)
			break
		}
	}
	if len(out) == 0 {
		return chosen // the tool in use has no provider data
	}
	for _, u := range chosen {
		if u.ID != active && len(out) < MaxTaskbarProviders {
			out = append(out, u)
		}
	}
	return out
}
