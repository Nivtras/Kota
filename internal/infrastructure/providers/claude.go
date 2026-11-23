package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"kota/internal/buildinfo"
	"kota/internal/domain"
	"kota/internal/infrastructure/storage"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Claude reads plan limits via the Claude Code CLI OAuth token
// (~/.claude/.credentials.json, written by `claude` → /login) and always adds
// local token totals from Claude Code session logs as details.
type Claude struct{}

func (c *Claude) ID() string   { return "claude" }
func (c *Claude) Name() string { return "Claude" }

const claudeClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"

// claudeTokenURL is a var so tests can point it at a fake server.
var claudeTokenURL = "https://console.anthropic.com/v1/oauth/token"

const (
	claudeUsageURL = "https://api.anthropic.com/api/oauth/usage"
)

var credsMu sync.Mutex

type claudeOauth struct {
	AccessToken      string `json:"accessToken"`
	RefreshToken     string `json:"refreshToken"`
	ExpiresAt        int64  `json:"expiresAt"`
	SubscriptionType string `json:"subscriptionType"`
}

type claudeWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

func claudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".claude")
}

func credsPath() string { return filepath.Join(claudeDir(), ".credentials.json") }

func (c *Claude) Fetch(ctx context.Context) domain.Usage {
	u := newUsage(c)

	if tw, err := claudeLocalStats(); err == nil {
		u.Tokens = tw
	}

	cache := loadClaudeCache()
	showCache := func(note string) {
		if len(cache.Bars) > 0 {
			u.Groups = []domain.Group{{Bars: cache.Bars}}
			u.Note = note
		} else {
			u.Error = note
		}
	}

	// The usage endpoint is rate-limited; don't hit it more often than needed —
	// unless a cached window has already reset (then the data is known stale)
	// and we are not being rate limited.
	wait := time.Now().Before(cache.NextAllowed)
	if wait && !cache.RateLimited && anyResetPassed(cache.Bars) {
		wait = false
	}
	if wait {
		u.Plan = cache.Plan
		if len(cache.Bars) == 0 {
			u.Error = fmt.Sprintf("%s — retrying in %s.", cache.LastErr, shortDur(time.Until(cache.NextAllowed)))
		} else if cache.LastErr != "" {
			showCache(fmt.Sprintf("%s — showing data from %s, retrying in %s.",
				cache.LastErr, cache.FetchedAt.Local().Format("15:04"), shortDur(time.Until(cache.NextAllowed))))
		} else {
			u.Groups = []domain.Group{{Bars: cache.Bars}}
		}
		return u
	}

	o, err := claudeToken(ctx)
	if o != nil && o.SubscriptionType != "" {
		u.Plan = strings.ToUpper(o.SubscriptionType[:1]) + o.SubscriptionType[1:]
	}
	if err != nil {
		showCache(err.Error())
		return u
	}
	bars, err := fetchClaudeLimits(ctx, o.AccessToken)
	if err != nil {
		var he *httpError
		if errors.As(err, &he) && he.code == http.StatusTooManyRequests {
			cache.Backoff = nextBackoff(cache.Backoff, he.retryAfter)
			cache.LastErr = "Rate limited by Anthropic"
			cache.RateLimited = true
		} else {
			// Timeouts / network blips: retry soon, they are not our fault.
			cache.Backoff = transientRetry
			cache.LastErr = "Limit API unreachable"
			cache.RateLimited = false
		}
		cache.NextAllowed = time.Now().Add(cache.Backoff)
		saveClaudeCache(cache)
		if len(cache.Bars) > 0 {
			showCache(fmt.Sprintf("%s — showing data from %s, retrying in %s.",
				cache.LastErr, cache.FetchedAt.Local().Format("15:04"), shortDur(cache.Backoff)))
		} else {
			u.Error = fmt.Sprintf("%s — retrying in %s.", cache.LastErr, shortDur(cache.Backoff))
		}
		return u
	}
	cache = claudeCache{Bars: bars, Plan: u.Plan, FetchedAt: time.Now(), NextAllowed: time.Now().Add(claudeMinInterval)}
	saveClaudeCache(cache)
	u.Groups = []domain.Group{{Bars: bars}}
	return u
}

const (
	claudeMinInterval = 3 * time.Minute
	claudeMinBackoff  = 5 * time.Minute
	claudeMaxBackoff  = 30 * time.Minute
)

// claudeCache keeps the last good limits on disk so a 429 or restart never
// blanks the widget, and so we respect backoff across restarts.
type claudeCache struct {
	Bars        []domain.Bar  `json:"bars"`
	Plan        string        `json:"plan"`
	FetchedAt   time.Time     `json:"fetchedAt"`
	NextAllowed time.Time     `json:"nextAllowed"`
	Backoff     time.Duration `json:"backoff"`
	LastErr     string        `json:"lastErr"`
	RateLimited bool          `json:"rateLimited"`
}

// transientRetry is the wait after a timeout or network error (not a 429).
const transientRetry = time.Minute

// anyResetPassed reports whether a bar's window has reset since it was fetched.
func anyResetPassed(bars []domain.Bar) bool {
	now := time.Now()
	for _, b := range bars {
		if t, err := time.Parse(time.RFC3339, b.ResetsAt); err == nil && !t.After(now) {
			return true
		}
	}
	return false
}

var claudeCacheMu sync.Mutex

func claudeCachePath() string { return storage.Path("claude-cache.json") }

func loadClaudeCache() claudeCache {
	claudeCacheMu.Lock()
	defer claudeCacheMu.Unlock()
	var c claudeCache
	if b, err := os.ReadFile(claudeCachePath()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func saveClaudeCache(c claudeCache) {
	claudeCacheMu.Lock()
	defer claudeCacheMu.Unlock()
	p := claudeCachePath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if b, err := json.Marshal(c); err == nil {
		_ = os.WriteFile(p, b, 0o644)
	}
}

func nextBackoff(prev, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return max(retryAfter, time.Minute)
	}
	if prev < claudeMinBackoff {
		return claudeMinBackoff
	}
	return min(prev*2, claudeMaxBackoff)
}

func shortDur(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	return fmt.Sprintf("%dm", int(d.Round(time.Minute)/time.Minute))
}

type httpError struct {
	code       int
	retryAfter time.Duration
}

func (e *httpError) Error() string { return fmt.Sprintf("HTTP %d", e.code) }

// claudeToken returns a valid OAuth token, refreshing and saving it if needed.
func claudeToken(ctx context.Context) (*claudeOauth, error) {
	credsMu.Lock()
	defer credsMu.Unlock()

	file, o, err := readClaudeCreds()
	if err != nil {
		return nil, err
	}
	if o.fresh() {
		return o, nil
	}
	if o.RefreshToken == "" {
		return o, errNotSignedIn
	}
	if err := refreshClaude(ctx, o); err != nil {
		// Claude Code may have refreshed (and rotated the refresh token) in
		// the meantime; if the file now holds a valid token, use it.
		if _, again, rerr := readClaudeCreds(); rerr == nil && again.fresh() {
			return again, nil
		}
		return o, fmt.Errorf("token refresh failed (%v) — run /login in `claude` again.", err)
	}
	writeClaudeCreds(file, o)
	return o, nil
}

var errNotSignedIn = errors.New("Not signed in — run `claude` in a terminal and use /login to see plan limits.")

func (o *claudeOauth) fresh() bool {
	return o.AccessToken != "" && time.UnixMilli(o.ExpiresAt).After(time.Now().Add(time.Minute))
}

func readClaudeCreds() (map[string]json.RawMessage, *claudeOauth, error) {
	raw, err := os.ReadFile(credsPath())
	if err != nil {
		return nil, nil, errNotSignedIn
	}
	var file map[string]json.RawMessage
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, nil, errors.New("credentials file is unreadable")
	}
	var o claudeOauth
	_ = json.Unmarshal(file["claudeAiOauth"], &o)
	return file, &o, nil
}

// writeClaudeCreds saves the refreshed token, preserving every other field
// (numbers are kept verbatim, not round-tripped through float64).
func writeClaudeCreds(file map[string]json.RawMessage, o *claudeOauth) {
	var oauthMap map[string]json.RawMessage
	_ = json.Unmarshal(file["claudeAiOauth"], &oauthMap)
	if oauthMap == nil {
		oauthMap = map[string]json.RawMessage{}
	}
	set := func(k string, v any) { b, _ := json.Marshal(v); oauthMap[k] = b }
	set("accessToken", o.AccessToken)
	set("refreshToken", o.RefreshToken)
	set("expiresAt", o.ExpiresAt)
	b, _ := json.Marshal(oauthMap)
	file["claudeAiOauth"] = b
	out, err := json.Marshal(file)
	if err != nil {
		return
	}
	tmp := credsPath() + ".tmp"
	if os.WriteFile(tmp, out, 0o600) == nil {
		_ = os.Rename(tmp, credsPath())
	}
}

func refreshClaude(ctx context.Context, o *claudeOauth) error {
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": o.RefreshToken,
		"client_id":     claudeClientID,
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, claudeTokenURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var r struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil || r.AccessToken == "" {
		return errors.New("bad refresh response")
	}
	o.AccessToken = r.AccessToken
	if r.RefreshToken != "" {
		o.RefreshToken = r.RefreshToken
	}
	o.ExpiresAt = time.Now().Add(time.Duration(r.ExpiresIn) * time.Second).UnixMilli()
	return nil
}

func fetchClaudeLimits(ctx context.Context, token string) ([]domain.Bar, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, claudeUsageURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
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
	var data map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	const fiveHours, week = 5 * 3600, 7 * 24 * 3600
	order := []struct {
		key, label string
		window     int64
	}{
		{"five_hour", "5-hour limit", fiveHours},
		{"seven_day", "Weekly · all models", week},
		{"seven_day_opus", "Weekly · Opus", week},
		{"seven_day_sonnet", "Weekly · Sonnet", week},
	}
	var bars []domain.Bar
	for _, w := range order {
		var cw claudeWindow
		if json.Unmarshal(data[w.key], &cw) != nil || cw.Utilization == nil {
			continue
		}
		b := domain.Bar{Label: w.label, UsedPct: clamp(*cw.Utilization), WindowSec: w.window}
		if cw.ResetsAt != nil {
			b.ResetsAt = *cw.ResetsAt
		}
		bars = append(bars, b)
	}
	if len(bars) == 0 {
		return nil, errors.New("no limit data in response")
	}
	return bars, nil
}

type claudeLogLine struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	RequestID string    `json:"requestId"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			Input       int64 `json:"input_tokens"`
			Output      int64 `json:"output_tokens"`
			CacheCreate int64 `json:"cache_creation_input_tokens"`
			CacheRead   int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// claudeRecord is one assistant response's usage from a session log.
type claudeRecord struct {
	key     string
	ts      time.Time
	in, out int64
}

type claudeFileCache struct {
	mod     time.Time
	size    int64
	records []claudeRecord
}

var (
	claudeScanMu    sync.Mutex
	claudeScanCache = map[string]claudeFileCache{}
)

// parseClaudeLog reads one session log. Only called when the file changed.
func parseClaudeLog(path string) []claudeRecord {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []claudeRecord
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, e := r.ReadBytes('\n')
		if len(line) > 0 && bytes.Contains(line[:min(len(line), 4096)], []byte(`"assistant"`)) {
			var l claudeLogLine
			if json.Unmarshal(line, &l) == nil && l.Type == "assistant" && l.Message.Usage != nil {
				us := l.Message.Usage
				out = append(out, claudeRecord{
					key: l.Message.ID + ":" + l.RequestID, ts: l.Timestamp,
					// Input counts everything sent to the model, cached or not.
					in: us.Input + us.CacheCreate + us.CacheRead, out: us.Output,
				})
			}
		}
		if e != nil {
			break
		}
	}
	return out
}

func claudeLocalStats() ([]domain.TokenWindow, error) {
	root := filepath.Join(claudeDir(), "projects")
	now := time.Now()
	since7 := now.Add(-7 * 24 * time.Hour)
	since5h := now.Add(-5 * time.Hour)
	seen := map[string]bool{}
	var w5, w7 struct{ in, out int64 }

	claudeScanMu.Lock()
	defer claudeScanMu.Unlock()
	live := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, e := d.Info()
		if e != nil || info.ModTime().Before(since7) {
			return nil
		}
		live[path] = true
		c, ok := claudeScanCache[path]
		if !ok || !c.mod.Equal(info.ModTime()) || c.size != info.Size() {
			c = claudeFileCache{mod: info.ModTime(), size: info.Size(), records: parseClaudeLog(path)}
			claudeScanCache[path] = c
		}
		for _, rec := range c.records {
			if !rec.ts.After(since7) || seen[rec.key] {
				continue
			}
			seen[rec.key] = true
			w7.in += rec.in
			w7.out += rec.out
			if rec.ts.After(since5h) {
				w5.in += rec.in
				w5.out += rec.out
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for p := range claudeScanCache {
		if !live[p] {
			delete(claudeScanCache, p)
		}
	}
	return []domain.TokenWindow{
		tokenWindow("Last 5h", w5.in, w5.out),
		tokenWindow("Last 7d", w7.in, w7.out),
	}, nil
}

func clamp(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

func human(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}
