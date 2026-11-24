package application

import (
	"fmt"
	"math"
	"strings"
	"time"

	"kota/internal/domain"
	"kota/internal/i18n"
)

// PillRow is one line of the taskbar pill, ready to draw.
type PillRow struct {
	ID, Name string
	HasBar   bool         // false: show NA text instead of a bar
	NA       string       // "unavailable" / "—"
	Fill     float64      // bar fill 0..100 (used or remaining, per setting)
	Value    int          // number shown, percent
	Word     string       // "used" | "left" (translated)
	Level    domain.Level // by usage, so red always means "near the limit"
	Reset    string       // short reset text ("2h 14m", "Sun 11:00")
	Tooltip  string
	// Pace marker position on the bar (0..100, same orientation as Fill),
	// or -1 when the window length or reset time is unknown.
	Marker float64
}

// PillRows is what the taskbar pill shows for this data: the chosen
// providers (the tool in use first, in follow mode), each with its pill limit.
// Texts are in the current language (i18n).
func PillRows(items []domain.Usage, p domain.Preferences, active string, now time.Time) []PillRow {
	var rows []PillRow
	list := domain.TaskbarProviders(items, p.Taskbar)
	if p.FollowActive {
		list = domain.WithActiveFirst(items, list, active)
	}
	remaining := domain.NormDisplay(p.Display) == domain.DisplayRemaining
	for _, u := range list {
		r := PillRow{ID: u.ID, Name: u.Name}
		b, ok := domain.PickBar(u, now)
		if !ok {
			r.NA = "—"
			if u.Error != "" {
				r.NA = i18n.T("unavailable")
			}
			r.Tooltip = u.Name + "\n" + i18n.T(domain.FirstNonEmpty(u.Error, u.Note, "No data"))
			rows = append(rows, r)
			continue
		}
		used := math.Max(0, math.Min(100, b.UsedPct))
		r.HasBar, r.Level = true, domain.LevelOf(used)
		r.Fill, r.Word = used, i18n.T("used")
		if remaining {
			r.Fill, r.Word = 100-used, i18n.T("left")
		}
		r.Value = int(math.Floor(r.Fill))
		r.Reset = i18n.ResetShort(b.ResetsAt, now)
		r.Marker = -1
		pace := domain.PaceOf(b.Bar, now)
		if pace.OK {
			r.Marker = pace.ElapsedPct
			if remaining {
				r.Marker = 100 - pace.ElapsedPct
			}
		}
		label := i18n.T(b.Label)
		if b.Group != "" {
			label = i18n.T(b.Group) + " · " + label
		}
		tip := []string{u.Name + " · " + label, fmt.Sprintf("%d%% %s", r.Value, r.Word)}
		if t, err := time.Parse(time.RFC3339, b.ResetsAt); err == nil {
			tip = append(tip, i18n.T("Resets %s", i18n.LongDateTime(t)))
		}
		if pace.RunsOut {
			tip = append(tip, i18n.T("At this pace: limit reached in %s", i18n.ShortDuration(pace.RunsOutIn)))
		}
		r.Tooltip = strings.Join(tip, "\n")
		rows = append(rows, r)
	}
	return rows
}
