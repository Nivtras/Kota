package main

// Usage notifications: warn at 80% and 95% of a limit (once per window),
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

	"aiusagewidget/providers"
)

const (
	warnAt      = 80
	criticalAt  = 95
	refilledMax = 20 // "available again" when it drops to this or below
	sentTTL     = 8 * 24 * time.Hour
)

type alert struct {
	Title, Body string
}

type notifier struct {
	mu   sync.Mutex
	sent map[string]time.Time // "<bar id>|<window>|80" → when sent (persisted)
	prev map[string]float64   // last seen used% per bar (this run only)
	path string               // "" = don't persist (tests)
}

func newNotifier(path string) *notifier {
	n := &notifier{sent: map[string]time.Time{}, prev: map[string]float64{}, path: path}
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

				if prev, ok := n.prev[id]; ok && prev >= warnAt && used <= refilledMax {
					out = append(out, alert{name + " is available again", fmt.Sprintf("Now %.0f%% used.", used)})
				}
				n.prev[id] = used

				wk := windowKey(b.ResetsAt)
				if wk == "" || err != nil || !reset.After(now) {
					continue // unknown window: can't tell "once per window"
				}
				in := "Resets in " + shortDuration(reset.Sub(now)) + "."
				switch {
				case used >= criticalAt && n.sent[id+"|"+wk+"|95"].IsZero():
					n.sent[id+"|"+wk+"|95"], n.sent[id+"|"+wk+"|80"] = now, now
					out = append(out, alert{fmt.Sprintf("%s at %.0f%% — almost out", name, used), in})
					changed = true
				case used >= warnAt && used < criticalAt && n.sent[id+"|"+wk+"|80"].IsZero():
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
