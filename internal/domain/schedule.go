package domain

// Adaptive refresh: often while the user is active, rarely while idle or
// locked, and right after a limit window resets.

import "time"

const (
	ActiveEvery = 2 * time.Minute
	IdleEvery   = 15 * time.Minute
	IdleAfter   = 10 * time.Minute // no keyboard/mouse input for this long = idle
	AfterReset  = 30 * time.Second // fetch this long after a window resets
	MinDelay    = 20 * time.Second
	StaleOnOpen = time.Minute // opening the panel refreshes older data
)

// RefreshDelay returns how long to wait before the next fetch.
func RefreshDelay(idle time.Duration, items []Usage, now time.Time) time.Duration {
	d := ActiveEvery
	if idle >= IdleAfter {
		d = IdleEvery
	}
	// Bring it forward to just after the soonest upcoming reset.
	for _, u := range items {
		for _, g := range u.Groups {
			for _, b := range g.Bars {
				t, ok := b.resetTime()
				if !ok || !t.After(now) {
					continue
				}
				if r := t.Sub(now) + AfterReset; r < d {
					d = r
				}
			}
		}
	}
	return max(d, MinDelay)
}
