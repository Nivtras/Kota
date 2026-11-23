package providers

// Cursor: the sign-in the Cursor app keeps in its state database
// (%APPDATA%\Cursor\User\globalStorage\state.vscdb, key
// cursorAuth/accessToken) → cursor.com/api/usage-summary, the endpoint
// Cursor's dashboard uses. Not officially documented, so it may change. The
// database is opened read-only and the token is never refreshed or written.

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"kota/internal/domain"
)

var cursorAPI = "https://cursor.com/api"

type Cursor struct{}

func (c *Cursor) ID() string   { return "cursor" }
func (c *Cursor) Name() string { return "Cursor" }

func cursorDB() string {
	if p := os.Getenv("CURSOR_STATE_DB"); p != "" {
		return p
	}
	return filepath.Join(os.Getenv("APPDATA"), "Cursor", "User", "globalStorage", "state.vscdb")
}

// Installed: the Cursor app is signed in on this machine.
func (c *Cursor) Installed() bool {
	t, err := cursorToken()
	return err == nil && t != ""
}

// cursorToken reads the app's access token (read-only).
func cursorToken() (string, error) {
	p := cursorDB()
	if _, err := os.Stat(p); err != nil {
		return "", err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(p)+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return "", err
	}
	defer db.Close()
	var raw []byte
	if err := db.QueryRow(`SELECT value FROM ItemTable WHERE key = 'cursorAuth/accessToken'`).Scan(&raw); err != nil {
		return "", err
	}
	return decodeStateValue(raw), nil
}

// decodeStateValue: values are UTF-8 text, or UTF-16LE in some builds.
func decodeStateValue(b []byte) string {
	if len(b) >= 2 && len(b)%2 == 0 && b[1] == 0 {
		u := make([]uint16, len(b)/2)
		for i := range u {
			u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		}
		return strings.TrimSpace(string(utf16.Decode(u)))
	}
	return strings.Trim(strings.TrimSpace(string(b)), `"`)
}

// cursorSession turns the token into the dashboard's session cookie value.
func cursorSession(token string, now time.Time) (string, error) {
	if strings.HasPrefix(token, "cursor-safe-storage:") {
		return "", errNotConfigured{"This Cursor version encrypts its sign-in; Kota can't read it yet."}
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errNotConfigured{"Cursor sign-in not found — open Cursor and sign in."}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errNotConfigured{"Cursor sign-in is unreadable — sign in again in Cursor."}
	}
	var claims struct {
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	_ = json.Unmarshal(payload, &claims)
	if claims.Exp > 0 && time.Unix(claims.Exp, 0).Before(now.Add(time.Minute)) {
		return "", errNotConfigured{"Cursor sign-in expired — open Cursor to refresh it."}
	}
	user := claims.Sub
	if i := strings.LastIndex(user, "|"); i >= 0 {
		user = user[i+1:]
	}
	if user == "" {
		return "", errNotConfigured{"Cursor sign-in is unreadable — sign in again in Cursor."}
	}
	return user + "%3A%3A" + token, nil
}

func (c *Cursor) Fetch(ctx context.Context) domain.Usage {
	return fetchRemote(ctx, newUsage(c), remoteSpec{
		cacheFile:   "cursor-cache.json",
		minInterval: 5 * time.Minute,
		service:     "Cursor",
		authHint:    "Cursor rejected the sign-in — open Cursor and sign in again",
		fetch: func(ctx context.Context) (remoteResult, error) {
			t, err := cursorToken()
			if err != nil || t == "" {
				return remoteResult{}, errNotConfigured{"Not signed in — open Cursor and sign in."}
			}
			return fetchCursor(ctx, t, time.Now())
		},
	})
}

type cursorSummary struct {
	BillingCycleStart string `json:"billingCycleStart"`
	BillingCycleEnd   string `json:"billingCycleEnd"`
	MembershipType    string `json:"membershipType"`
	IndividualUsage   struct {
		Plan *struct {
			Enabled          *bool    `json:"enabled"`
			Used             float64  `json:"used"`
			Limit            float64  `json:"limit"`
			TotalPercentUsed *float64 `json:"totalPercentUsed"`
			AutoPercentUsed  *float64 `json:"autoPercentUsed"`
			APIPercentUsed   *float64 `json:"apiPercentUsed"`
		} `json:"plan"`
		OnDemand *struct {
			Enabled bool     `json:"enabled"`
			Used    float64  `json:"used"`
			Limit   *float64 `json:"limit"`
		} `json:"onDemand"`
	} `json:"individualUsage"`
}

func fetchCursor(ctx context.Context, token string, now time.Time) (remoteResult, error) {
	session, err := cursorSession(token, now)
	if err != nil {
		return remoteResult{}, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, cursorAPI+"/usage-summary", nil)
	req.Header.Set("Cookie", "WorkosCursorSessionToken="+session)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://cursor.com")
	body, err := doJSON(req)
	if err != nil {
		return remoteResult{}, err
	}
	var s cursorSummary
	if err := json.Unmarshal(body, &s); err != nil {
		return remoteResult{}, err
	}
	return cursorResult(s), nil
}

// cursorTime accepts RFC 3339 or epoch milliseconds (as a string).
func cursorTime(s string) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	var ms int64
	if _, err := fmt.Sscanf(s, "%d", &ms); err == nil && ms > 0 {
		return time.UnixMilli(ms).UTC()
	}
	return time.Time{}
}

func cursorResult(s cursorSummary) remoteResult {
	r := remoteResult{Plan: titleWords(s.MembershipType)}
	start, end := cursorTime(s.BillingCycleStart), cursorTime(s.BillingCycleEnd)
	withCycle := func(b domain.Bar) domain.Bar {
		if !end.IsZero() {
			b.ResetsAt = end.Format(time.RFC3339)
			if !start.IsZero() && end.After(start) {
				b.WindowSec = int64(end.Sub(start) / time.Second)
			}
		}
		return b
	}
	var bars []domain.Bar
	if p := s.IndividualUsage.Plan; p != nil && (p.Enabled == nil || *p.Enabled) {
		used := 0.0
		switch {
		case p.TotalPercentUsed != nil:
			used = *p.TotalPercentUsed
		case p.Limit > 0:
			used = 100 * p.Used / p.Limit
		}
		b := withCycle(domain.Bar{Label: "Included usage", UsedPct: clamp(used)})
		if p.Limit > 0 {
			b.Tooltip = fmt.Sprintf("%s of %s included usage this billing cycle", usd(p.Used/100), usd(p.Limit/100))
		}
		bars = append(bars, b)
		if p.AutoPercentUsed != nil {
			bars = append(bars, withCycle(domain.Bar{Label: "Auto", UsedPct: clamp(*p.AutoPercentUsed)}))
		}
		if p.APIPercentUsed != nil {
			bars = append(bars, withCycle(domain.Bar{Label: "API models", UsedPct: clamp(*p.APIPercentUsed)}))
		}
	}
	if od := s.IndividualUsage.OnDemand; od != nil && od.Enabled {
		v := usd(od.Used / 100)
		if od.Limit != nil && *od.Limit > 0 {
			v += " of " + usd(*od.Limit/100)
		}
		r.Details = append(r.Details, domain.Stat{Label: "On-demand", Value: v})
	}
	if len(bars) > 0 {
		r.Groups = []domain.Group{{Bars: bars}}
	} else {
		r.Note = "No included usage reported for this plan."
	}
	return r
}
