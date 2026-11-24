// Package i18n translates Kota's interface. Keys are the English texts
// themselves (with fmt verbs for the variable parts), so a missing
// translation just shows English. The Go side and the panel page use the same
// dictionary (the page fetches it through the panel API).
package i18n

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"
)

// Languages.
const (
	English = "en"
	Turkish = "tr"
)

//go:embed tr.json
var trJSON []byte

var (
	tr   = mustLoad(trJSON)
	lang atomic.Value // string
)

func init() { lang.Store(English) }

func mustLoad(b []byte) map[string]string {
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		panic("i18n: bad tr.json: " + err.Error())
	}
	return m
}

// Set switches the language ("en" or "tr"; anything else is English).
func Set(l string) {
	if l != Turkish {
		l = English
	}
	lang.Store(l)
}

// Lang is the current language.
func Lang() string { return lang.Load().(string) }

// Resolve picks the language for a preference ("" = follow Windows' display
// language, given as system).
func Resolve(pref, system string) string {
	switch pref {
	case English, Turkish:
		return pref
	}
	if strings.HasPrefix(strings.ToLower(system), Turkish) {
		return Turkish
	}
	return English
}

// T translates key and formats it with args (fmt verbs).
func T(key string, args ...any) string {
	s := key
	if Lang() == Turkish {
		if v, ok := tr[key]; ok && v != "" {
			s = v
		}
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// Dict is the dictionary of the current language (empty for English).
func Dict() map[string]string {
	if Lang() != Turkish {
		return map[string]string{}
	}
	return tr
}

// Keys lists the translated keys (for tests).
func Keys() map[string]string { return tr }

// ShortDuration: "45m", "2h 10m", "3d 4h" (localised units).
func ShortDuration(d time.Duration) string {
	m := int(math.Round(d.Minutes()))
	switch {
	case m < 1:
		return T("<1m")
	case m < 60:
		return T("%dm", m)
	case m < 24*60:
		return T("%dh %dm", m/60, m%60)
	}
	return T("%dd %dh", m/(24*60), (m%(24*60))/60)
}

var (
	weekdays = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	months   = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
)

// Weekday is the short day name ("Sun" / "Paz").
func Weekday(t time.Time) string { return T(weekdays[t.Weekday()]) }

// DayTime: "Sun 11:00" (local time).
func DayTime(t time.Time) string {
	t = t.Local()
	return Weekday(t) + " " + t.Format("15:04")
}

// DateTime: "Jan 2 15:04" / "2 Oca 15:04" (local time).
func DateTime(t time.Time) string {
	t = t.Local()
	return T("%[1]s %[2]d %[3]s", T(months[t.Month()-1]), t.Day(), t.Format("15:04"))
}

// LongDateTime: "Mon 2 Jan 15:04" / "Pzt 2 Oca 15:04" (local time).
func LongDateTime(t time.Time) string {
	t = t.Local()
	return fmt.Sprintf("%s %d %s %s", Weekday(t), t.Day(), T(months[t.Month()-1]), t.Format("15:04"))
}

// ResetShort: "now", "45m", "2h 14m", or "Sun 11:00".
func ResetShort(iso string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return ""
	}
	d := t.Sub(now)
	if d <= 0 {
		return T("now")
	}
	if d < 24*time.Hour {
		return ShortDuration(d)
	}
	return DayTime(t)
}
