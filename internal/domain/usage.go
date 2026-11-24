// Package domain holds Kota's rules: what a provider reports, which limit the
// taskbar shows, pace, notifications, history and the refresh schedule. It
// has no Windows, file or network dependencies, so all of it is unit tested.
package domain

import "time"

// Bar is a single limit line. Standard: show what is USED.
type Bar struct {
	Label    string  `json:"label"`
	UsedPct  float64 `json:"usedPct"` // 0..100
	ResetsAt string  `json:"resetsAt,omitempty"`
	// WindowSec is the length of the limit window (e.g. 18000 for 5 hours);
	// with ResetsAt it tells how much of the window has elapsed (pace).
	WindowSec int64  `json:"windowSec,omitempty"`
	Tooltip   string `json:"tooltip,omitempty"`
}

// Group is a set of bars sharing one quota pool (e.g. "Gemini Models").
// Name may be empty when a provider has a single pool.
type Group struct {
	Name    string `json:"name,omitempty"`
	Tooltip string `json:"tooltip,omitempty"`
	Bars    []Bar  `json:"bars"`
}

// Stat is a key/value shown in the expandable details section.
type Stat struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// TokenWindow is one block in the token dropdown (e.g. "Last 5h").
// Values are pre-formatted (e.g. "45.5M").
type TokenWindow struct {
	Label  string `json:"label"`
	Input  string `json:"input"`
	Output string `json:"output"`
	Total  string `json:"total"`
}

// DayAmount is one calendar day of usage: tokens or spend (USD).
type DayAmount struct {
	Day    string  `json:"day"` // "2026-11-24"
	Tokens int64   `json:"tokens,omitempty"`
	Cost   float64 `json:"cost,omitempty"`
}

// Units of Usage.Daily.
const (
	DailyTokens = "tokens"
	DailyUSD    = "usd"
)

// Usage is what one provider reports.
type Usage struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Plan      string        `json:"plan,omitempty"`
	Groups    []Group       `json:"groups"`
	Details   []Stat        `json:"details,omitempty"`
	Tokens    []TokenWindow `json:"tokens,omitempty"`
	Daily     []DayAmount   `json:"daily,omitempty"`     // last days, oldest first
	DailyUnit string        `json:"dailyUnit,omitempty"` // DailyTokens | DailyUSD
	Note      string        `json:"note,omitempty"`
	Error     string        `json:"error,omitempty"`
	UpdatedAt string        `json:"updatedAt"`
}

// resetTime parses ResetsAt; ok is false when it is missing or malformed.
func (b Bar) resetTime() (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, b.ResetsAt)
	return t, err == nil
}

// Expired reports whether the window has reset since the data was taken:
// such a limit has been refilled, even if the data says otherwise.
func (b Bar) Expired(now time.Time) bool {
	t, ok := b.resetTime()
	return ok && !t.After(now)
}

// LiveUsed is the used percentage, counting an expired window as 0.
func (b Bar) LiveUsed(now time.Time) float64 {
	if b.Expired(now) {
		return 0
	}
	return b.UsedPct
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

// FirstNonEmpty returns the first of ss that is not "".
func FirstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
