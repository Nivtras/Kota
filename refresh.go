package main

// Adaptive refresh: often while the user is active, rarely while idle or
// locked, and right after a limit window resets.

import (
	"time"

	"kota/providers"
)

const (
	activeEvery = 2 * time.Minute
	idleEvery   = 15 * time.Minute
	idleAfter   = 10 * time.Minute // no keyboard/mouse input for this long = idle
	afterReset  = 30 * time.Second // fetch this long after a window resets
	minDelay    = 20 * time.Second
	staleOnOpen = time.Minute // opening the panel refreshes older data
)

// refreshDelay returns how long to wait before the next fetch.
func refreshDelay(idle time.Duration, items []providers.Usage, now time.Time) time.Duration {
	d := activeEvery
	if idle >= idleAfter {
		d = idleEvery
	}
	// Bring it forward to just after the soonest upcoming reset.
	for _, u := range items {
		for _, g := range u.Groups {
			for _, b := range g.Bars {
				t, err := time.Parse(time.RFC3339, b.ResetsAt)
				if err != nil || !t.After(now) {
					continue
				}
				if r := t.Sub(now) + afterReset; r < d {
					d = r
				}
			}
		}
	}
	return max(d, minDelay)
}
