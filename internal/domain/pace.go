package domain

import (
	"fmt"
	"math"
	"time"
)

// Pace: how much of a limit window has elapsed, and whether the current
// burn rate reaches 100% before the window resets.
type Pace struct {
	ElapsedPct float64       // 0..100
	OK         bool          // window length and reset known
	RunsOut    bool          // at this pace the limit is hit before reset
	RunsOutIn  time.Duration // from now, when RunsOut
}

// MinPaceUsed: below this usage a forecast is mostly noise.
const MinPaceUsed = 20

// PaceOf computes the pace of a limit at now.
func PaceOf(b Bar, now time.Time) Pace {
	reset, ok := b.resetTime()
	if !ok || b.WindowSec <= 0 || !reset.After(now) {
		return Pace{}
	}
	window := time.Duration(b.WindowSec) * time.Second
	left := min(reset.Sub(now), window)
	elapsed := window - left
	p := Pace{ElapsedPct: 100 * float64(elapsed) / float64(window), OK: true}
	if b.UsedPct >= MinPaceUsed && b.UsedPct < 100 && elapsed > 0 {
		perSec := b.UsedPct / elapsed.Seconds()
		toLimit := time.Duration((100 - b.UsedPct) / perSec * float64(time.Second))
		if toLimit < left {
			p.RunsOut, p.RunsOutIn = true, toLimit
		}
	}
	return p
}

// ShortDuration: "45m", "2h 10m", "3d 4h".
func ShortDuration(d time.Duration) string {
	m := int(math.Round(d.Minutes()))
	switch {
	case m < 1:
		return "<1m"
	case m < 60:
		return fmt.Sprintf("%dm", m)
	case m < 24*60:
		return fmt.Sprintf("%dh %dm", m/60, m%60)
	}
	return fmt.Sprintf("%dd %dh", m/(24*60), (m%(24*60))/60)
}

// ResetShort is the short reset text: "now", "45m", "2h 14m", or "Sun 11:00".
func ResetShort(iso string, now time.Time) string {
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
