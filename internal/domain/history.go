package domain

// Usage history: the used % of each limit, sampled from the data already
// fetched and kept for 7 days. The panel charts, per provider, the limit
// the taskbar shows (usually 5-hour) and its longest limit (usually weekly).

import (
	"sort"
	"time"
)

const (
	HistoryKeep  = 7 * 24 * time.Hour
	historyEvery = 10 * time.Minute // at most one sample per limit per 10 min…
	historyMove  = 1.0              // …unless the value moved by this many points
)

// HistoryPoint is one sample.
type HistoryPoint struct {
	T int64   `json:"t"` // unix seconds
	U float64 `json:"u"` // % used
}

// History is the samples per limit, keyed "<provider>|<group>|<label>".
type History map[string][]HistoryPoint

func historyKey(provider, group, label string) string { return provider + "|" + group + "|" + label }

// Record adds a sample for every limit in the data and drops samples older
// than HistoryKeep. It reports whether anything changed.
func (h History) Record(items []Usage, now time.Time) (changed bool) {
	for _, u := range items {
		if u.Error != "" {
			continue
		}
		for _, g := range u.Groups {
			for _, b := range g.Bars {
				used := b.LiveUsed(now)
				k := historyKey(u.ID, g.Name, b.Label)
				pts := h[k]
				if n := len(pts); n > 0 {
					last := pts[n-1]
					d := used - last.U
					if now.Unix()-last.T < int64(historyEvery/time.Second) && d < historyMove && d > -historyMove {
						continue
					}
				}
				h[k] = append(pts, HistoryPoint{now.Unix(), used})
				changed = true
			}
		}
	}
	cut := now.Add(-HistoryKeep).Unix()
	for k, pts := range h {
		i := sort.Search(len(pts), func(i int) bool { return pts[i].T >= cut })
		if i > 0 {
			changed = true
			if i == len(pts) {
				delete(h, k)
			} else {
				h[k] = append([]HistoryPoint(nil), pts[i:]...)
			}
		}
	}
	return changed
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

// Charts returns, per provider, the series of its charted limits (ChartBars).
func (h History) Charts(items []Usage, now time.Time) []ProviderHistory {
	var out []ProviderHistory
	for _, u := range items {
		ph := ProviderHistory{ID: u.ID}
		for _, b := range ChartBars(u, now) {
			label := b.Label
			if b.Group != "" && len(u.Groups) > 1 {
				label = b.Group + " · " + b.Label
			}
			s := HistorySeries{Label: label, Points: [][]float64{}}
			for _, p := range h[historyKey(u.ID, b.Group, b.Label)] {
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
