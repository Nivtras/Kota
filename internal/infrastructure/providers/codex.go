package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kota/internal/buildinfo"
	"kota/internal/domain"
	"kota/internal/i18n"
)

// Codex reads ChatGPT/Codex rate limits with the token the Codex app keeps in
// ~/.codex/auth.json. The file is only read: the Codex app refreshes the token
// itself, and rotating it here would sign the app out.
type Codex struct{}

func (c *Codex) ID() string   { return "codex" }
func (c *Codex) Name() string { return "Codex" }

const (
	codexUsageURL    = "https://chatgpt.com/backend-api/wham/usage"
	codexMinInterval = 3 * time.Minute
	codexCacheFile   = "codex-cache.json"
)

type codexWindow struct {
	UsedPercent        *float64 `json:"used_percent"`
	LimitWindowSeconds int64    `json:"limit_window_seconds"`
	ResetAt            int64    `json:"reset_at"`
}

type codexRateLimit struct {
	PrimaryWindow   *codexWindow `json:"primary_window"`
	SecondaryWindow *codexWindow `json:"secondary_window"`
}

type codexUsage struct {
	PlanType             string          `json:"plan_type"`
	RateLimit            *codexRateLimit `json:"rate_limit"`
	CodeReviewRateLimit  *codexRateLimit `json:"code_review_rate_limit"`
	AdditionalRateLimits []struct {
		LimitName string          `json:"limit_name"`
		RateLimit *codexRateLimit `json:"rate_limit"`
	} `json:"additional_rate_limits"`
}

type codexCache struct {
	Groups      []domain.Group `json:"groups"`
	Plan        string         `json:"plan"`
	FetchedAt   time.Time      `json:"fetchedAt"`
	NextAllowed time.Time      `json:"nextAllowed"`
	Backoff     time.Duration  `json:"backoff"`
	LastErr     string         `json:"lastErr"`
	RateLimited bool           `json:"rateLimited"`
}

func codexDir() string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".codex")
}

// Installed reports whether Codex has ever been set up on this machine;
// the widget hides the card otherwise.
func (c *Codex) Installed() bool {
	_, err := os.Stat(codexDir())
	return err == nil
}

func (c *Codex) Fetch(ctx context.Context) domain.Usage {
	u := newUsage(c)
	if tally, err := codexLocalTokens(); err == nil {
		setTokens(&u, tally)
	}
	cache := loadJSON[codexCache](codexCacheFile)
	u.Plan = cache.Plan

	stale := func(msg string) domain.Usage {
		if len(cache.Groups) > 0 {
			u.Groups = cache.Groups
			u.Note = i18n.T("%s — showing data from %s.", i18n.T(msg), i18n.DateTime(cache.FetchedAt))
		} else {
			u.Error = msg
		}
		return u
	}

	var cachedBars []domain.Bar
	for _, g := range cache.Groups {
		cachedBars = append(cachedBars, g.Bars...)
	}
	wait := time.Now().Before(cache.NextAllowed) && len(cache.Groups) > 0
	if wait && !cache.RateLimited && anyResetPassed(cachedBars) {
		wait = false // a window reset since the last fetch: refresh now
	}
	if wait {
		u.Groups = cache.Groups
		if cache.LastErr != "" {
			u.Note = i18n.T("%s — retrying in %s.", i18n.T(cache.LastErr), i18n.ShortDuration(time.Until(cache.NextAllowed)))
		}
		return u
	}

	token, account, err := codexToken()
	if err != nil {
		return stale(err.Error())
	}

	data, err := fetchCodexUsage(ctx, token, account)
	if err != nil {
		var he *httpError
		cache.RateLimited = false
		switch {
		case errors.As(err, &he) && he.code == http.StatusTooManyRequests:
			cache.Backoff = nextBackoff(cache.Backoff, he.retryAfter)
			cache.LastErr = "Rate limited by OpenAI"
			cache.RateLimited = true
		case errors.As(err, &he) && he.code == http.StatusUnauthorized:
			cache.Backoff = claudeMinBackoff
			cache.LastErr = "Codex sign-in expired — open the Codex app to refresh it"
		default:
			cache.Backoff = transientRetry
			cache.LastErr = "Usage API unreachable"
		}
		cache.NextAllowed = time.Now().Add(cache.Backoff)
		saveJSON(codexCacheFile, cache)
		return stale(cache.LastErr)
	}

	if data.PlanType != "" {
		u.Plan = strings.ToUpper(data.PlanType[:1]) + data.PlanType[1:]
	}
	u.Groups = codexGroups(data)
	if len(u.Groups) == 0 {
		u.Note = "No rate-limit windows reported for this plan."
	}
	saveJSON(codexCacheFile, codexCache{
		Groups: u.Groups, Plan: u.Plan, FetchedAt: time.Now(),
		NextAllowed: time.Now().Add(codexMinInterval),
	})
	return u
}

func codexToken() (token, account string, err error) {
	b, err := os.ReadFile(filepath.Join(codexDir(), "auth.json"))
	if err != nil {
		return "", "", errors.New("Not signed in — open Codex and sign in with ChatGPT.")
	}
	var a struct {
		APIKey *string `json:"OPENAI_API_KEY"`
		Tokens *struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal(b, &a) != nil {
		return "", "", errors.New("Codex auth file is unreadable")
	}
	if a.Tokens == nil || a.Tokens.AccessToken == "" {
		if a.APIKey != nil && *a.APIKey != "" {
			return "", "", errors.New("Codex uses an API key — plan limits only exist for ChatGPT sign-in.")
		}
		return "", "", errors.New("Not signed in — open Codex and sign in with ChatGPT.")
	}
	if exp, ok := jwtExpiry(a.Tokens.AccessToken); ok && exp.Before(time.Now()) {
		return "", "", errors.New("Codex sign-in expired — open the Codex app to refresh it")
	}
	return a.Tokens.AccessToken, a.Tokens.AccountID, nil
}

// jwtExpiry reads the exp claim without verifying the token.
func jwtExpiry(tok string) (time.Time, bool) {
	parts := strings.Split(tok, ".")
	if len(parts) < 2 {
		return time.Time{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(raw, &claims) != nil || claims.Exp == 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Exp, 0), true
}

func fetchCodexUsage(ctx context.Context, token, account string) (*codexUsage, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, codexUsageURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if account != "" {
		req.Header.Set("ChatGPT-Account-Id", account)
	}
	req.Header.Set("User-Agent", buildinfo.Name+"/"+buildinfo.Version)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		he := &httpError{code: resp.StatusCode}
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
			he.retryAfter = time.Duration(s) * time.Second
		}
		return nil, he
	}
	var u codexUsage
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, err
	}
	return &u, nil
}

func codexGroups(d *codexUsage) []domain.Group {
	var groups []domain.Group
	add := func(name string, rl *codexRateLimit) {
		if rl == nil {
			return
		}
		var bars []domain.Bar
		for _, w := range []*codexWindow{rl.PrimaryWindow, rl.SecondaryWindow} {
			if w == nil || w.UsedPercent == nil {
				continue
			}
			b := domain.Bar{Label: windowLabel(w.LimitWindowSeconds), UsedPct: clamp(*w.UsedPercent), WindowSec: w.LimitWindowSeconds}
			if w.ResetAt > 0 && *w.UsedPercent > 0 {
				b.ResetsAt = time.Unix(w.ResetAt, 0).UTC().Format(time.RFC3339)
			}
			bars = append(bars, b)
		}
		if len(bars) > 0 {
			groups = append(groups, domain.Group{Name: name, Bars: bars})
		}
	}
	add("", d.RateLimit)
	for _, a := range d.AdditionalRateLimits {
		add(a.LimitName, a.RateLimit)
	}
	add("Code review", d.CodeReviewRateLimit)
	// Name the main group only when other groups exist.
	if len(groups) > 1 && groups[0].Name == "" {
		groups[0].Name = "Codex"
	}
	return groups
}

// codexLocalTokens sums token usage from Codex session logs
// (~/.codex/sessions/**/rollout-*.jsonl). Each model response writes a
// token_count event whose last_token_usage is that response's usage.
func codexLocalTokens() (*tokenTally, error) {
	now := time.Now()
	since7 := now.Add(-7 * 24 * time.Hour)
	tally := newTally(now)

	type usage struct {
		Input  int64 `json:"input_tokens"`
		Output int64 `json:"output_tokens"`
		Total  int64 `json:"total_tokens"`
	}
	type line struct {
		Timestamp time.Time `json:"timestamp"`
		Type      string    `json:"type"`
		Payload   struct {
			Type string `json:"type"`
			Info *struct {
				Total usage `json:"total_token_usage"`
				Last  usage `json:"last_token_usage"`
			} `json:"info"`
		} `json:"payload"`
	}

	for _, sub := range []string{"sessions", "archived_sessions"} {
		root := filepath.Join(codexDir(), sub)
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
				return nil
			}
			if info, e := d.Info(); e != nil || info.ModTime().Before(since7) {
				return nil
			}
			f, e := os.Open(path)
			if e != nil {
				return nil
			}
			defer f.Close()
			var prevTotal int64
			r := bufio.NewReaderSize(f, 1<<20)
			for {
				b, e := r.ReadBytes('\n')
				if len(b) > 0 && bytes.Contains(b, []byte(`"token_count"`)) {
					var l line
					if json.Unmarshal(b, &l) == nil && l.Payload.Type == "token_count" && l.Payload.Info != nil {
						// The same totals can be re-emitted; count a response only
						// when the running total moves forward.
						if t := l.Payload.Info.Total.Total; t > prevTotal {
							prevTotal = t
							tally.add(l.Timestamp, l.Payload.Info.Last.Input, l.Payload.Info.Last.Output)
						}
					}
				}
				if e != nil {
					break
				}
			}
			return nil
		})
	}
	return tally, nil
}

func windowLabel(sec int64) string {
	switch {
	case sec <= 0:
		return "Limit"
	case sec == 5*3600:
		return "5-hour limit"
	case sec == 7*24*3600:
		return "Weekly limit"
	case sec >= 28*24*3600 && sec <= 31*24*3600:
		return "Monthly limit"
	case sec%(24*3600) == 0:
		return fmt.Sprintf("%d-day limit", sec/(24*3600))
	case sec%3600 == 0:
		return fmt.Sprintf("%d-hour limit", sec/3600)
	}
	return fmt.Sprintf("%dm limit", sec/60)
}
