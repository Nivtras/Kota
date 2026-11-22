package main

// Usage notifications: warn at 80% and 95% of a limit by default, both
// adjustable (once per window),
// and say when a limit that was nearly used up is available again. Uses
// the data the app already fetches — no extra requests.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"kota/providers"
)

const (
	defaultWarn     = 80
	defaultCritical = 95
	refilledMax     = 20 // "available again" when it drops to this or below
	sentTTL         = 8 * 24 * time.Hour
)

type alert struct {
	Title, Body string
}

type notifier struct {
	mu   sync.Mutex
	sent map[string]time.Time // "<bar id>|<window>|80" → when sent (persisted)
	prev map[string]float64   // last seen used% per bar (this run only)
	path string               // "" = don't persist (tests)

	warn, critical float64 // thresholds in % used
}

// normLevels keeps thresholds sane: 50 ≤ warn < critical ≤ 100; 0 = default.
func normLevels(warn, critical int) (int, int) {
	if warn == 0 {
		warn = defaultWarn
	}
	if critical == 0 {
		critical = defaultCritical
	}
	warn = max(50, min(warn, 99))
	critical = max(warn+1, min(critical, 100))
	return warn, critical
}

func (n *notifier) setLevels(warn, critical int) {
	w, c := normLevels(warn, critical)
	n.mu.Lock()
	n.warn, n.critical = float64(w), float64(c)
	n.mu.Unlock()
}

func newNotifier(path string) *notifier {
	n := &notifier{sent: map[string]time.Time{}, prev: map[string]float64{}, path: path,
		warn: defaultWarn, critical: defaultCritical}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, &n.sent)
		}
	}
	return n
}

// windowKey identifies one instance of a limit window. Reset times are
// rounded to 10 minutes: some APIs return them with jittering sub-seconds.
func windowKey(resetsAt string) string {
	t, err := time.Parse(time.RFC3339, resetsAt)
	if err != nil {
		return ""
	}
	return t.UTC().Round(10 * time.Minute).Format(time.RFC3339)
}

// check returns the notifications due for this data.
func (n *notifier) check(items []providers.Usage, now time.Time) []alert {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []alert
	changed := false
	for _, u := range items {
		for _, g := range u.Groups {
			for _, b := range g.Bars {
				id := u.ID + "|" + g.Name + "|" + b.Label
				name := u.Name + " · " + strings.Join(nonEmpty(g.Name, b.Label), " · ")
				used := b.UsedPct
				reset, err := time.Parse(time.RFC3339, b.ResetsAt)
				if err == nil && !reset.After(now) {
					used = 0 // window already reset since this data was taken
				}

				if prev, ok := n.prev[id]; ok && prev >= n.warn && used <= refilledMax {
					out = append(out, alert{name + " is available again", fmt.Sprintf("Now %.0f%% used.", used)})
				}
				n.prev[id] = used

				wk := windowKey(b.ResetsAt)
				if wk == "" || err != nil || !reset.After(now) {
					continue // unknown window: can't tell "once per window"
				}
				in := "Resets in " + shortDuration(reset.Sub(now)) + "."
				switch {
				// Sent-keys keep their historic "80"/"95" names: warning / critical.
				case used >= n.critical && n.sent[id+"|"+wk+"|95"].IsZero():
					n.sent[id+"|"+wk+"|95"], n.sent[id+"|"+wk+"|80"] = now, now
					out = append(out, alert{fmt.Sprintf("%s at %.0f%% — almost out", name, used), in})
					changed = true
				case used >= n.warn && used < n.critical && n.sent[id+"|"+wk+"|80"].IsZero():
					n.sent[id+"|"+wk+"|80"] = now
					out = append(out, alert{fmt.Sprintf("%s at %.0f%%", name, used), in})
					changed = true
				}
			}
		}
	}
	for k, t := range n.sent {
		if now.Sub(t) > sentTTL {
			delete(n.sent, k)
			changed = true
		}
	}
	if changed && n.path != "" {
		_ = os.MkdirAll(filepath.Dir(n.path), 0o755)
		if b, err := json.Marshal(n.sent); err == nil {
			_ = os.WriteFile(n.path, b, 0o644)
		}
	}
	return out
}
