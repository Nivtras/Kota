package providers

// Shared plumbing for providers that read a remote usage API: keep the last
// good result on disk (a 429, an outage or a restart never blanks the card),
// respect a minimum interval and back off on errors.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"kota/internal/domain"
	"kota/internal/i18n"
)

// remoteResult is what one successful fetch produced.
type remoteResult struct {
	Groups  []domain.Group `json:"groups"`
	Plan    string         `json:"plan"`
	Details []domain.Stat  `json:"details"`
	Note    string         `json:"note"`
	// Per-day history, if the provider has one.
	Daily     []domain.DayAmount `json:"daily,omitempty"`
	DailyUnit string             `json:"dailyUnit,omitempty"`
}

type remoteCache struct {
	remoteResult
	FetchedAt   time.Time     `json:"fetchedAt"`
	NextAllowed time.Time     `json:"nextAllowed"`
	Backoff     time.Duration `json:"backoff"`
	LastErr     string        `json:"lastErr"`
}

// remoteSpec describes one provider's fetch.
type remoteSpec struct {
	cacheFile   string
	minInterval time.Duration
	authHint    string // shown on 401/403
	service     string // "OpenRouter", for messages
	fetch       func(ctx context.Context) (remoteResult, error)
}

// errNotConfigured: no credentials; shown as is, nothing cached.
type errNotConfigured struct{ msg string }

func (e errNotConfigured) Error() string { return e.msg }

func fetchRemote(ctx context.Context, u domain.Usage, s remoteSpec) domain.Usage {
	cache := loadJSON[remoteCache](s.cacheFile)
	apply := func(r remoteResult) {
		u.Groups, u.Plan, u.Details, u.Note = r.Groups, r.Plan, r.Details, r.Note
		u.Daily, u.DailyUnit = r.Daily, r.DailyUnit
		if u.Groups == nil {
			u.Groups = []domain.Group{}
		}
	}
	stale := func(msg string) domain.Usage {
		if len(cache.Groups) > 0 || len(cache.Details) > 0 {
			apply(cache.remoteResult)
			u.Note = i18n.T("%s — showing data from %s.", i18n.T(msg), i18n.DateTime(cache.FetchedAt))
		} else {
			u.Error = msg
		}
		return u
	}

	var bars []domain.Bar
	for _, g := range cache.Groups {
		bars = append(bars, g.Bars...)
	}
	if time.Now().Before(cache.NextAllowed) && !cache.FetchedAt.IsZero() && !anyResetPassed(bars) {
		apply(cache.remoteResult)
		if cache.LastErr != "" {
			u.Note = i18n.T("%s — retrying in %s.", i18n.T(cache.LastErr), i18n.ShortDuration(time.Until(cache.NextAllowed)))
		}
		return u
	}

	r, err := s.fetch(ctx)
	if err != nil {
		var nc errNotConfigured
		if errors.As(err, &nc) {
			u.Error = nc.msg
			return u
		}
		var he *httpError
		switch {
		case errors.As(err, &he) && he.code == http.StatusTooManyRequests:
			cache.Backoff = nextBackoff(cache.Backoff, he.retryAfter)
			cache.LastErr = i18n.T("Rate limited by %s", s.service)
		case errors.As(err, &he) && (he.code == http.StatusUnauthorized || he.code == http.StatusForbidden):
			cache.Backoff = claudeMinBackoff
			cache.LastErr = s.authHint
		default:
			cache.Backoff = transientRetry
			cache.LastErr = i18n.T("%s usage API unreachable", s.service)
		}
		cache.NextAllowed = time.Now().Add(cache.Backoff)
		saveJSON(s.cacheFile, cache)
		return stale(cache.LastErr)
	}
	apply(r)
	saveJSON(s.cacheFile, remoteCache{remoteResult: r, FetchedAt: time.Now(), NextAllowed: time.Now().Add(s.minInterval)})
	return u
}

// doJSON sends req and returns the body of a 2xx response.
func doJSON(req *http.Request) ([]byte, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		he := &httpError{code: resp.StatusCode}
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			he.retryAfter = time.Duration(s) * time.Second
		}
		return nil, he
	}
	return body, err
}

func usd(v float64) string { return fmt.Sprintf("$%.2f", v) }
