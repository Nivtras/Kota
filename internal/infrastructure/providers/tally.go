package providers

import (
	"time"

	"kota/internal/domain"
)

// dailyDays is how many calendar days the per-day history covers.
const dailyDays = 7

// tokenTally sums token usage from local logs into the token dropdown's
// windows (last 5 h, last 7 days) and per-day totals (local calendar days).
type tokenTally struct {
	now             time.Time
	since5h, since7 time.Time
	w5, w7          struct{ in, out int64 }
	days            map[string]int64
}

func newTally(now time.Time) *tokenTally {
	return &tokenTally{now: now, since5h: now.Add(-5 * time.Hour), since7: now.Add(-7 * 24 * time.Hour), days: map[string]int64{}}
}

func dayKey(t time.Time) string { return t.Local().Format("2006-01-02") }

// add counts one response.
func (t *tokenTally) add(ts time.Time, in, out int64) {
	if ts.Before(t.since7) {
		return
	}
	t.w7.in += in
	t.w7.out += out
	if !ts.Before(t.since5h) {
		t.w5.in += in
		t.w5.out += out
	}
	t.days[dayKey(ts)] += in + out
}

func (t *tokenTally) windows() []domain.TokenWindow {
	return []domain.TokenWindow{
		tokenWindow("Last 5h", t.w5.in, t.w5.out),
		tokenWindow("Last 7d", t.w7.in, t.w7.out),
	}
}

// daily is the last dailyDays calendar days, oldest first, zeros included.
func (t *tokenTally) daily() []domain.DayAmount {
	out := make([]domain.DayAmount, 0, dailyDays)
	for i := dailyDays - 1; i >= 0; i-- {
		k := dayKey(t.now.AddDate(0, 0, -i))
		out = append(out, domain.DayAmount{Day: k, Tokens: t.days[k]})
	}
	return out
}

// setTokens puts a tally on a usage card.
func setTokens(u *domain.Usage, t *tokenTally) {
	u.Tokens = t.windows()
	u.Daily, u.DailyUnit = t.daily(), domain.DailyTokens
}
