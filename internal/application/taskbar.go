package application

import (
	"fmt"
	"math"
	"strings"
	"time"

	"kota/internal/domain"
)

// PillRow is one line of the taskbar pill, ready to draw.
type PillRow struct {
	ID, Name string
	HasBar   bool         // false: show NA text instead of a bar
	NA       string       // "unavailable" / "—"
	Fill     float64      // bar fill 0..100 (used or remaining, per setting)
	Value    int          // number shown, percent
	Word     string       // "used" | "left"
	Level    domain.Level // by usage, so red always means "near the limit"
	Reset    string       // short reset text ("2h 14m", "Sun 11:00")
	Tooltip  string
	// Pace marker position on the bar (0..100, same orientation as Fill),
	// or -1 when the window length or reset time is unknown.
	Marker float64
}

// PillRows is what the taskbar pill shows for this data: the chosen
// providers (the tool in use first, in follow mode), each with its pill limit.
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
				r.NA = "unavailable"
			}
			r.Tooltip = u.Name + "\n" + domain.FirstNonEmpty(u.Error, u.Note, "No data")
			rows = append(rows, r)
			continue
		}
		used := math.Max(0, math.Min(100, b.UsedPct))
		r.HasBar, r.Level = true, domain.LevelOf(used)
		r.Fill, r.Word = used, "used"
		if remaining {
			r.Fill, r.Word = 100-used, "left"
		}
		r.Value = int(math.Floor(r.Fill))
		r.Reset = domain.ResetShort(b.ResetsAt, now)
		r.Marker = -1
		pace := domain.PaceOf(b.Bar, now)
		if pace.OK {
			r.Marker = pace.ElapsedPct
			if remaining {
				r.Marker = 100 - pace.ElapsedPct
			}
		}
		label := b.Label
		if b.Group != "" {
			label = b.Group + " · " + b.Label
		}
		tip := []string{u.Name + " · " + label, fmt.Sprintf("%d%% %s", r.Value, r.Word)}
		if t, err := time.Parse(time.RFC3339, b.ResetsAt); err == nil {
			tip = append(tip, "Resets "+t.Local().Format("Mon 2 Jan 15:04"))
		}
		if pace.RunsOut {
			tip = append(tip, "At this pace: limit reached in "+domain.ShortDuration(pace.RunsOutIn))
		}
		r.Tooltip = strings.Join(tip, "\n")
		rows = append(rows, r)
	}
	return rows
}
