package main

// Usage history: the used % of each limit, sampled from the data the app
// already fetches (no extra requests) and kept for 7 days in
// %APPDATA%\AIUsageWidget\history.json. The panel charts, per provider, the
// limit the pill shows (usually 5-hour) and its longest limit (usually weekly).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"aiusagewidget/providers"
)

const (
	historyKeep  = 7 * 24 * time.Hour
	historyEvery = 10 * time.Minute // at most one sample per limit per 10 min…
	historyMove  = 1.0              // …unless the value moved by this many points
)

type histPoint struct {
	T int64   `json:"t"` // unix seconds
	U float64 `json:"u"` // % used
}

type history struct {
	mu     sync.Mutex
	series map[string][]histPoint // "<provider>|<group>|<label>"
	path   string                 // "" = don't persist (tests)
}

func newHistory(path string) *history {
	h := &history{series: map[string][]histPoint{}, path: path}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, &h.series)
		}
	}
	return h
}

func histKey(provider, group, label string) string { return provider + "|" + group + "|" + label }

// record adds a sample for every limit in the data.
func (h *history) record(items []providers.Usage, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	changed := false
	for _, u := range items {
		if u.Error != "" {
			continue
		}
		for _, g := range u.Groups {
			for _, b := range g.Bars {
				used := b.UsedPct
				if t, err := time.Parse(time.RFC3339, b.ResetsAt); err == nil && !t.After(now) {
					used = 0 // the window reset since this data was taken
				}
				k := histKey(u.ID, g.Name, b.Label)
				pts := h.series[k]
				if n := len(pts); n > 0 {
					last := pts[n-1]
					d := used - last.U
					if now.Unix()-last.T < int64(historyEvery/time.Second) && d < historyMove && d > -historyMove {
						continue
					}
				}
				h.series[k] = append(pts, histPoint{now.Unix(), used})
				changed = true
			}
		}
	}
	cut := now.Add(-historyKeep).Unix()
	for k, pts := range h.series {
		i := sort.Search(len(pts), func(i int) bool { return pts[i].T >= cut })
		if i > 0 {
			changed = true
			if i == len(pts) {
				delete(h.series, k)
			} else {
				h.series[k] = append([]histPoint(nil), pts[i:]...)
			}
		}
	}
	if changed && h.path != "" {
		_ = os.MkdirAll(filepath.Dir(h.path), 0o755)
		if b, err := json.Marshal(h.series); err == nil {
			tmp := h.path + ".tmp"
			if os.WriteFile(tmp, b, 0o644) == nil {
				_ = os.Rename(tmp, h.path)
			}
		}
	}
}

// HistorySeries is one charted limit.
type HistorySeries struct {
	Label  string      `json:"label"`
	Points [][]float64 `json:"points"` // [unix seconds, % used]
}

// ProviderHistory is what the panel charts for one provider.
type ProviderHistory struct {
	ID     string          `json:"id"`
	Series []HistorySeries `json:"series"`
}

// chartBars picks up to two limits to chart for a provider: the one the
// pill shows and the longest other window in the same pool.
func chartBars(u providers.Usage, now time.Time) []pillBar {
	first, ok := pickBar(u, now)
	if !ok {
		return nil
	}
	out := []pillBar{first}
	var long *pillBar
	for _, g := range u.Groups {
		if g.Name != first.group {
			continue
		}
		for _, b := range g.Bars {
			if b.Label == first.Label || b.WindowSec <= first.WindowSec {
				continue
			}
			if long == nil || b.WindowSec > long.WindowSec {
				long = &pillBar{b, g.Name}
			}
		}
	}
	if long != nil {
		out = append(out, *long)
	}
	return out
}

func (h *history) forProviders(items []providers.Usage, now time.Time) []ProviderHistory {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []ProviderHistory
	for _, u := range items {
		ph := ProviderHistory{ID: u.ID}
		for _, b := range chartBars(u, now) {
			label := b.Label
			if b.group != "" && len(u.Groups) > 1 {
				label = b.group + " · " + b.Label
			}
			s := HistorySeries{Label: label, Points: [][]float64{}}
			for _, p := range h.series[histKey(u.ID, b.group, b.Label)] {
				s.Points = append(s.Points, []float64{float64(p.T), p.U})
			}
			if len(s.Points) > 0 {
				ph.Series = append(ph.Series, s)
			}
		}
		if len(ph.Series) > 0 {
			out = append(out, ph)
		}
	}
	return out
}
