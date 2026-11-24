package providers

// OpenRouter: the user's API key (stored encrypted, see storage.Secret) →
// the documented /api/v1/key (key limit, daily/weekly/monthly spend) and
// /api/v1/credits (prepaid credits) endpoints.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"kota/internal/domain"
	"kota/internal/infrastructure/storage"
)

// OpenRouterSecret is the name the API key is stored under.
const OpenRouterSecret = "openrouter"

var openRouterAPI = "https://openrouter.ai/api/v1"

type OpenRouter struct{}

func (o *OpenRouter) ID() string      { return "openrouter" }
func (o *OpenRouter) Name() string    { return "OpenRouter" }
func (o *OpenRouter) Installed() bool { return storage.Secret(OpenRouterSecret) != "" }

type openRouterKey struct {
	Data struct {
		Label          string   `json:"label"`
		Limit          *float64 `json:"limit"`
		LimitRemaining *float64 `json:"limit_remaining"`
		LimitReset     *string  `json:"limit_reset"`
		Usage          float64  `json:"usage"`
		UsageDaily     *float64 `json:"usage_daily"`
		UsageWeekly    *float64 `json:"usage_weekly"`
		UsageMonthly   *float64 `json:"usage_monthly"`
		IsFreeTier     bool     `json:"is_free_tier"`
		// Requests to :free models today (UTC) against the account's daily
		// cap: 50, or 1000 once at least $10 of credits were ever bought.
		FreeModelDaily *struct {
			Used      float64 `json:"used"`
			Limit     float64 `json:"limit"`
			Remaining float64 `json:"remaining"`
		} `json:"free_model_daily_requests"`
	} `json:"data"`
}

type openRouterCredits struct {
	Data struct {
		TotalCredits float64 `json:"total_credits"`
		TotalUsage   float64 `json:"total_usage"`
	} `json:"data"`
}

func (o *OpenRouter) Fetch(ctx context.Context) domain.Usage {
	return fetchRemote(ctx, newUsage(o), remoteSpec{
		cacheFile:   "openrouter-cache.json",
		minInterval: 3 * time.Minute,
		service:     "OpenRouter",
		authHint:    "OpenRouter rejected the API key — check it in Settings → API keys",
		fetch: func(ctx context.Context) (remoteResult, error) {
			key := storage.Secret(OpenRouterSecret)
			if key == "" {
				return remoteResult{}, errNotConfigured{"Add your OpenRouter API key in Settings → API keys."}
			}
			return fetchOpenRouter(ctx, key, time.Now())
		},
	})
}

func openRouterGet(ctx context.Context, key, path string, v any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, openRouterAPI+path, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	body, err := doJSON(req)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

func fetchOpenRouter(ctx context.Context, key string, now time.Time) (remoteResult, error) {
	var k openRouterKey
	if err := openRouterGet(ctx, key, "/key", &k); err != nil {
		return remoteResult{}, err
	}
	var c *openRouterCredits
	var cr openRouterCredits
	if openRouterGet(ctx, key, "/credits", &cr) == nil { // optional: some keys can't read credits
		c = &cr
	}
	return openRouterResult(k, c, now), nil
}

// openRouterResult maps the API answers to bars and stats.
func openRouterResult(k openRouterKey, c *openRouterCredits, now time.Time) remoteResult {
	d := k.Data
	r := remoteResult{Plan: "Pay as you go"}
	if d.IsFreeTier {
		r.Plan = "Free tier"
	}
	var bars []domain.Bar
	if d.Limit != nil && *d.Limit > 0 {
		used := d.Usage
		if d.LimitRemaining != nil {
			used = *d.Limit - *d.LimitRemaining
		}
		b := domain.Bar{Label: "Key limit", UsedPct: clamp(100 * used / *d.Limit),
			Tooltip: usd(used) + " of " + usd(*d.Limit) + " (spending cap on this key, not your balance)"}
		if d.LimitReset != nil {
			if reset, window := openRouterReset(*d.LimitReset, now); !reset.IsZero() {
				b.ResetsAt, b.WindowSec = reset.Format(time.RFC3339), int64(window/time.Second)
				b.Label = strings.ToUpper((*d.LimitReset)[:1]) + (*d.LimitReset)[1:] + " key limit"
			}
		}
		bars = append(bars, b)
	}
	if f := d.FreeModelDaily; f != nil && f.Limit > 0 {
		reset, window := openRouterReset("daily", now)
		bars = append(bars, domain.Bar{Label: "Free requests", UsedPct: clamp(100 * f.Used / f.Limit),
			ResetsAt: reset.Format(time.RFC3339), WindowSec: int64(window / time.Second),
			Tooltip: fmt.Sprintf("%.0f of %.0f requests to :free models today (UTC) · also max 20 per minute", f.Used, f.Limit)})
	}
	if c != nil {
		if c.Data.TotalCredits > 0 {
			bars = append(bars, domain.Bar{Label: "Credits", UsedPct: clamp(100 * c.Data.TotalUsage / c.Data.TotalCredits),
				Tooltip: usd(c.Data.TotalUsage) + " of " + usd(c.Data.TotalCredits) + " in credits used"})
		}
		r.Details = append(r.Details, domain.Stat{Label: "Balance", Value: usd(c.Data.TotalCredits - c.Data.TotalUsage)})
	}
	for _, s := range []struct {
		label string
		v     *float64
	}{{"Spent today", d.UsageDaily}, {"Spent this week", d.UsageWeekly}, {"Spent this month", d.UsageMonthly}} {
		if s.v != nil {
			r.Details = append(r.Details, domain.Stat{Label: s.label, Value: usd(*s.v)})
		}
	}
	if len(bars) > 0 {
		r.Groups = []domain.Group{{Bars: bars}}
	} else {
		r.Note = "Nothing to measure against yet: add credits, or set a credit limit on this key at openrouter.ai to get a bar. Spend is under the arrow."
	}
	return r
}

// openRouterReset returns when a "daily" / "weekly" / "monthly" key limit
// resets (UTC boundaries) and the window length.
func openRouterReset(kind string, now time.Time) (time.Time, time.Duration) {
	n := now.UTC()
	day := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
	switch kind {
	case "daily":
		return day.AddDate(0, 0, 1), 24 * time.Hour
	case "weekly": // ISO weeks start on Monday
		start := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
		return start.AddDate(0, 0, 7), 7 * 24 * time.Hour
	case "monthly":
		start := time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 1, 0)
		return end, end.Sub(start)
	}
	return time.Time{}, 0
}
