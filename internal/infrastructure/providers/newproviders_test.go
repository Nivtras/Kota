package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenRouterResult(t *testing.T) {
	now := time.Date(2026, 11, 25, 15, 0, 0, 0, time.UTC) // a Wednesday
	f := func(v float64) *float64 { return &v }
	s := func(v string) *string { return &v }
	var k openRouterKey
	k.Data.Limit, k.Data.LimitRemaining, k.Data.LimitReset = f(50), f(37.6), s("weekly")
	k.Data.Usage, k.Data.UsageDaily, k.Data.UsageWeekly, k.Data.UsageMonthly = 80, f(1.25), f(12.4), f(30)
	c := &openRouterCredits{}
	c.Data.TotalCredits, c.Data.TotalUsage = 100, 25

	r := openRouterResult(k, c, now)
	if r.Plan != "Pay as you go" || len(r.Groups) != 1 || len(r.Groups[0].Bars) != 2 {
		t.Fatalf("result = %+v", r)
	}
	key, credits := r.Groups[0].Bars[0], r.Groups[0].Bars[1]
	if key.Label != "Weekly key limit" || key.UsedPct < 24.7 || key.UsedPct > 24.9 || key.WindowSec != 7*86400 ||
		key.ResetsAt != "2026-11-30T00:00:00Z" {
		t.Errorf("key bar = %+v", key)
	}
	if credits.UsedPct != 25 {
		t.Errorf("credits bar = %+v", credits)
	}
	want := map[string]string{"Balance": "$75.00", "Spent today": "$1.25", "Spent this week": "$12.40", "Spent this month": "$30.00"}
	for _, d := range r.Details {
		if want[d.Label] != d.Value {
			t.Errorf("%s = %s, want %s", d.Label, d.Value, want[d.Label])
		}
		delete(want, d.Label)
	}
	if len(want) != 0 {
		t.Errorf("missing details %v", want)
	}

	// no limit, credits unreadable: a note, no bars
	var free openRouterKey
	free.Data.IsFreeTier = true
	if r := openRouterResult(free, nil, now); r.Plan != "Free tier" || len(r.Groups) != 0 || r.Note == "" {
		t.Errorf("uncapped = %+v", r)
	}
	// credits readable but empty: balance $0.00, still no bar
	if r := openRouterResult(free, &openRouterCredits{}, now); len(r.Groups) != 0 || len(r.Details) != 1 || r.Details[0].Value != "$0.00" {
		t.Errorf("zero credits = %+v", r)
	}
}

func TestOpenRouterFetchHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/key":
			w.Write([]byte(`{"data":{"limit":10,"limit_remaining":5,"limit_reset":null,"usage":5,"usage_daily":0.5}}`))
		case "/credits":
			w.WriteHeader(http.StatusForbidden) // regular keys may not read credits
		}
	}))
	defer srv.Close()
	old := openRouterAPI
	openRouterAPI = srv.URL
	defer func() { openRouterAPI = old }()

	r, err := fetchOpenRouter(context.Background(), "test-key", time.Now())
	if err != nil || len(r.Groups) != 1 || r.Groups[0].Bars[0].UsedPct != 50 || r.Groups[0].Bars[0].ResetsAt != "" {
		t.Fatalf("got %+v, %v", r, err)
	}
	if _, err := fetchOpenRouter(context.Background(), "wrong", time.Now()); err == nil {
		t.Fatal("wrong key should fail")
	}
}

func TestGeminiResult(t *testing.T) {
	var q geminiQuota
	_ = json.Unmarshal([]byte(`{"buckets":[
		{"modelId":"gemini-2.5-pro","remainingFraction":0.6,"resetTime":"2026-11-26T08:00:00Z"},
		{"modelId":"gemini-3-pro-preview","remainingFraction":0.25,"resetTime":"2026-11-26T09:00:00Z"},
		{"modelId":"gemini-2.5-flash","remainingFraction":1},
		{"modelId":"gemini-2.5-flash-lite","remainingFraction":0.9,"resetTime":"2026-11-26T08:00:00Z"},
		{"modelId":"no-fraction"}]}`), &q)
	r := geminiResult(q)
	bars := r.Groups[0].Bars
	if len(bars) != 3 || bars[0].Label != "Pro models" || bars[1].Label != "Flash models" || bars[2].Label != "Flash Lite models" {
		t.Fatalf("bars = %+v", bars)
	}
	if bars[0].UsedPct != 75 || bars[0].ResetsAt != "2026-11-26T09:00:00Z" || bars[0].WindowSec != 86400 {
		t.Errorf("pro = %+v (lowest remaining wins)", bars[0])
	}
	if bars[1].UsedPct != 0 || bars[2].UsedPct < 9.9 || bars[2].UsedPct > 10.1 {
		t.Errorf("flash = %+v, lite = %+v", bars[1], bars[2])
	}
	if geminiProjectID(json.RawMessage(`"p-1"`)) != "p-1" || geminiProjectID(json.RawMessage(`{"id":"p-2"}`)) != "p-2" {
		t.Error("project id")
	}
}

func TestGeminiFetchHTTP(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GEMINI_CLI_HOME", home)
	creds := `{"access_token":"at","refresh_token":"rt","expiry_date":` + jsonInt(time.Now().Add(time.Hour).UnixMilli()) + `}`
	os.WriteFile(filepath.Join(home, "oauth_creds.json"), []byte(creds), 0o600)
	var gotProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1internal:loadCodeAssist":
			w.Write([]byte(`{"cloudaicompanionProject":"proj-9","currentTier":{"id":"free-tier","name":"Gemini Code Assist for individuals"}}`))
		case "/v1internal:retrieveUserQuota":
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			gotProject = b["project"]
			w.Write([]byte(`{"buckets":[{"modelId":"gemini-2.5-pro","remainingFraction":0.5,"resetTime":"2026-11-26T08:00:00Z"}]}`))
		}
	}))
	defer srv.Close()
	old := geminiCodeAssist
	geminiCodeAssist = srv.URL + "/v1internal"
	defer func() { geminiCodeAssist = old }()

	if !(&Gemini{}).Installed() {
		t.Fatal("creds present → installed")
	}
	r, err := fetchGemini(context.Background())
	if err != nil || gotProject != "proj-9" || r.Plan != "Gemini Code Assist for individuals" || r.Groups[0].Bars[0].UsedPct != 50 {
		t.Fatalf("got %+v, %v (project %q)", r, err, gotProject)
	}
	// the credentials file is never modified
	if b, _ := os.ReadFile(filepath.Join(home, "oauth_creds.json")); string(b) != creds {
		t.Error("oauth_creds.json changed")
	}
}
