package domain

// Usage notifications: warn at a warning and a critical threshold of a limit
// (once per window), and say when a limit that was nearly used up is
// available again. Uses the data already fetched — no extra requests.

import "time"

const (
	DefaultWarn     = 80
	DefaultCritical = 95
	refilledMax     = 20 // "available again" when it drops to this or below
	sentTTL         = 8 * 24 * time.Hour
)

// NormLevels keeps thresholds sane: 50 ≤ warn < critical ≤ 100; 0 = default.
func NormLevels(warn, critical int) (int, int) {
	if warn == 0 {
		warn = DefaultWarn
	}
	if critical == 0 {
		critical = DefaultCritical
	}
	warn = max(50, min(warn, 99))
	critical = max(warn+1, min(critical, 100))
	return warn, critical
}

// AlertKind is what an alert says.
type AlertKind int

const (
	AlertWarn      AlertKind = iota // crossed the warning threshold
	AlertCritical                   // crossed the critical threshold
	AlertAvailable                  // a nearly used-up limit is available again
)

// Alert is one notification to show; the application layer words it in the
// user's language.
type Alert struct {
	Kind            AlertKind
	Provider, Group string // e.g. "Antigravity", "Gemini Models" (may be empty)
	Label           string // the limit, e.g. "5-hour limit"
	Used            float64
	ResetIn         time.Duration // AlertWarn / AlertCritical
}

// AlertState is what the policy remembers between checks.
type AlertState struct {
	// Sent: "<bar id>|<window>|80" → when that alert went out. Persisted, so
	// a restart doesn't repeat alerts. The "80"/"95" suffixes are historic
	// names for the warning / critical alert.
	Sent map[string]time.Time
	// Prev: last seen used % per bar, this run only.
	Prev map[string]float64
}

// NewAlertState returns an empty state.
func NewAlertState() *AlertState {
	return &AlertState{Sent: map[string]time.Time{}, Prev: map[string]float64{}}
}

// NotificationPolicy decides which alerts are due.
type NotificationPolicy struct {
	Warn, Critical float64 // % used
}

// NewNotificationPolicy builds a policy from (possibly unset) thresholds.
func NewNotificationPolicy(warn, critical int) NotificationPolicy {
	w, c := NormLevels(warn, critical)
	return NotificationPolicy{float64(w), float64(c)}
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

// Check returns the alerts due for this data and updates st. sentChanged
// tells whether st.Sent changed (and should be persisted).
func (p NotificationPolicy) Check(st *AlertState, items []Usage, now time.Time) (alerts []Alert, sentChanged bool) {
	for _, u := range items {
		for _, g := range u.Groups {
			for _, b := range g.Bars {
				id := u.ID + "|" + g.Name + "|" + b.Label
				base := Alert{Provider: u.Name, Group: g.Name, Label: b.Label}
				used := b.LiveUsed(now)

				if prev, ok := st.Prev[id]; ok && prev >= p.Warn && used <= refilledMax {
					a := base
					a.Kind, a.Used = AlertAvailable, used
					alerts = append(alerts, a)
				}
				st.Prev[id] = used

				reset, ok := b.resetTime()
				wk := windowKey(b.ResetsAt)
				if wk == "" || !ok || !reset.After(now) {
					continue // unknown window: can't tell "once per window"
				}
				a := base
				a.Used, a.ResetIn = used, reset.Sub(now)
				warnKey, critKey := id+"|"+wk+"|80", id+"|"+wk+"|95"
				switch {
				case used >= p.Critical && st.Sent[critKey].IsZero():
					st.Sent[critKey], st.Sent[warnKey] = now, now
					a.Kind = AlertCritical
					alerts = append(alerts, a)
					sentChanged = true
				case used >= p.Warn && used < p.Critical && st.Sent[warnKey].IsZero():
					st.Sent[warnKey] = now
					a.Kind = AlertWarn
					alerts = append(alerts, a)
					sentChanged = true
				}
			}
		}
	}
	for k, t := range st.Sent {
		if now.Sub(t) > sentTTL {
			delete(st.Sent, k)
			sentChanged = true
		}
	}
	return alerts, sentChanged
}
