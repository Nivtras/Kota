package providers

// GitHub Copilot: a GitHub token → api.github.com/copilot_internal/user,
// the endpoint Copilot's own clients use for the monthly premium-request
// quota. Not officially documented, so it may change. The token comes from
// the key the user entered in Kota, else GH_TOKEN / GITHUB_TOKEN, else the
// GitHub CLI (`gh auth token`) when it is installed.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"kota/internal/domain"
	"kota/internal/infrastructure/storage"
)

// CopilotSecret is the name the GitHub token is stored under.
const CopilotSecret = "copilot"

var copilotAPI = "https://api.github.com/copilot_internal/user"

type Copilot struct{}

func (c *Copilot) ID() string      { return "copilot" }
func (c *Copilot) Name() string    { return "GitHub Copilot" }
func (c *Copilot) Installed() bool { return copilotToken() != "" }

// copilotToken: the stored key, the environment, then the GitHub CLI.
func copilotToken() string {
	if t := storage.Secret(CopilotSecret); t != "" {
		return t
	}
	for _, v := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if t := strings.TrimSpace(os.Getenv(v)); t != "" {
			return t
		}
	}
	return ghCLIToken()
}

var ghToken struct {
	checked time.Time
	token   string
}

// ghCLIToken asks an installed GitHub CLI for its token (at most every 10 min).
func ghCLIToken() string {
	if time.Since(ghToken.checked) < 10*time.Minute {
		return ghToken.token
	}
	ghToken.checked, ghToken.token = time.Now(), ""
	path, err := exec.LookPath("gh")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "auth", "token")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	if out, err := cmd.Output(); err == nil {
		ghToken.token = strings.TrimSpace(string(out))
	}
	return ghToken.token
}

func (c *Copilot) Fetch(ctx context.Context) domain.Usage {
	return fetchRemote(ctx, newUsage(c), remoteSpec{
		cacheFile:   "copilot-cache.json",
		minInterval: 5 * time.Minute,
		service:     "GitHub",
		authHint:    "GitHub rejected the token — check it in Settings → API keys",
		fetch: func(ctx context.Context) (remoteResult, error) {
			t := copilotToken()
			if t == "" {
				return remoteResult{}, errNotConfigured{"Add a GitHub token in Settings → API keys."}
			}
			return fetchCopilot(ctx, t, time.Now())
		},
	})
}

type copilotQuota struct {
	Entitlement      float64 `json:"entitlement"`
	Remaining        float64 `json:"remaining"`
	PercentRemaining float64 `json:"percent_remaining"`
	Unlimited        bool    `json:"unlimited"`
}

type copilotUser struct {
	Plan           string                   `json:"copilot_plan"`
	AccessTypeSKU  string                   `json:"access_type_sku"`
	QuotaResetDate string                   `json:"quota_reset_date"`
	Snapshots      map[string]*copilotQuota `json:"quota_snapshots"`
}

func fetchCopilot(ctx context.Context, token string, now time.Time) (remoteResult, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, copilotAPI, nil)
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Github-Api-Version", "2025-04-01")
	req.Header.Set("User-Agent", "Kota")
	body, err := doJSON(req)
	if err != nil {
		return remoteResult{}, err
	}
	var u copilotUser
	if err := json.Unmarshal(body, &u); err != nil {
		return remoteResult{}, err
	}
	return copilotResult(u, now), nil
}

func copilotResult(u copilotUser, now time.Time) remoteResult {
	r := remoteResult{Plan: titleWords(u.Plan)}
	if u.AccessTypeSKU == "subscription_ended" {
		r.Note = "Copilot subscription ended."
		return r
	}
	reset, start := copilotPeriod(u.QuotaResetDate, now)
	var bars []domain.Bar
	for _, k := range []struct{ key, label string }{
		{"premium_interactions", "Premium requests"}, {"chat", "Chat"}, {"completions", "Completions"},
	} {
		q := u.Snapshots[k.key]
		if q == nil || q.Unlimited {
			continue
		}
		b := domain.Bar{Label: k.label, UsedPct: clamp(100 - q.PercentRemaining)}
		if q.Entitlement > 0 {
			b.Tooltip = fmt.Sprintf("%.0f of %.0f used this month", q.Entitlement-q.Remaining, q.Entitlement)
		}
		if !reset.IsZero() {
			b.ResetsAt, b.WindowSec = reset.Format(time.RFC3339), int64(reset.Sub(start)/time.Second)
		}
		bars = append(bars, b)
	}
	if len(bars) > 0 {
		r.Groups = []domain.Group{{Bars: bars}}
	} else {
		r.Note = "Unlimited on this plan."
	}
	return r
}

// copilotPeriod: the monthly window ending at quota_reset_date
// ("2026-12-01" or RFC 3339).
func copilotPeriod(s string, now time.Time) (reset, start time.Time) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		if t, err = time.Parse("2006-01-02", s); err != nil {
			return time.Time{}, time.Time{}
		}
	}
	t = t.UTC()
	return t, t.AddDate(0, -1, 0)
}

// titleWords: "individual_pro" → "Individual Pro".
func titleWords(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' || r == ' ' })
	for i, p := range parts {
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}
