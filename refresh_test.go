package main

import (
	"testing"
	"time"

	"aiusagewidget/providers"
)

func TestRefreshDelay(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	withReset := func(in time.Duration) []providers.Usage {
		return []providers.Usage{{Groups: []providers.Group{{Bars: []providers.Bar{
			{ResetsAt: now.Add(in).Format(time.RFC3339)},
		}}}}}
	}
	cases := []struct {
		name  string
		idle  time.Duration
		items []providers.Usage
		want  time.Duration
	}{
		{"active, no resets", time.Minute, nil, 2 * time.Minute},
		{"idle", 20 * time.Minute, nil, 15 * time.Minute},
		{"reset soon while active", 0, withReset(45 * time.Second), 75 * time.Second},
		{"reset soon while idle", time.Hour, withReset(5 * time.Minute), 5*time.Minute + 30*time.Second},
		{"reset later than interval", 0, withReset(time.Hour), 2 * time.Minute},
		{"reset already passed", 0, withReset(-time.Minute), 2 * time.Minute},
		{"never below minimum", 0, withReset(time.Second), 31 * time.Second},
	}
	for _, c := range cases {
		if got := refreshDelay(c.idle, c.items, now); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
