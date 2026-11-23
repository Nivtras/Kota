package providers

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCopilotResult(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	var u copilotUser
	_ = json.Unmarshal([]byte(`{"copilot_plan":"individual_pro","quota_reset_date":"2026-12-01",
		"quota_snapshots":{
			"premium_interactions":{"entitlement":300,"remaining":120,"percent_remaining":40,"unlimited":false},
			"chat":{"unlimited":true},
			"completions":{"entitlement":2000,"remaining":2000,"percent_remaining":100}}}`), &u)
	r := copilotResult(u, now)
	if r.Plan != "Individual Pro" || len(r.Groups) != 1 {
		t.Fatalf("result = %+v", r)
	}
	bars := r.Groups[0].Bars
	if len(bars) != 2 || bars[0].Label != "Premium requests" || bars[0].UsedPct != 60 || bars[0].Tooltip != "180 of 300 used this month" {
		t.Fatalf("bars = %+v", bars)
	}
	if bars[0].ResetsAt != "2026-12-01T00:00:00Z" || bars[0].WindowSec != 30*86400 {
		t.Errorf("reset = %s, window %d", bars[0].ResetsAt, bars[0].WindowSec)
	}
	if bars[1].Label != "Completions" || bars[1].UsedPct != 0 {
		t.Errorf("completions = %+v", bars[1])
	}
	if r := copilotResult(copilotUser{AccessTypeSKU: "subscription_ended"}, now); r.Note != "Copilot subscription ended." {
		t.Errorf("ended = %+v", r)
	}
}

func TestCopilotFetchHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token gho_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"copilot_plan":"free","quota_snapshots":{"premium_interactions":{"entitlement":50,"remaining":45,"percent_remaining":90}}}`))
	}))
	defer srv.Close()
	old := copilotAPI
	copilotAPI = srv.URL
	defer func() { copilotAPI = old }()
	r, err := fetchCopilot(context.Background(), "gho_test", time.Now())
	if err != nil || r.Groups[0].Bars[0].UsedPct != 10 {
		t.Fatalf("got %+v, %v", r, err)
	}
	if _, err := fetchCopilot(context.Background(), "bad", time.Now()); err == nil {
		t.Fatal("bad token should fail")
	}
}

func fakeJWT(sub string, exp time.Time) string {
	enc := base64.RawURLEncoding.EncodeToString
	payload, _ := json.Marshal(map[string]any{"sub": sub, "exp": exp.Unix()})
	return enc([]byte(`{"alg":"HS256"}`)) + "." + enc(payload) + ".sig"
}

func TestCursorTokenAndSession(t *testing.T) {
	now := time.Now()
	tok := fakeJWT("auth0|user_ABC123", now.Add(24*time.Hour))
	dbPath := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`CREATE TABLE ItemTable (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB)`)
	db.Exec(`INSERT INTO ItemTable VALUES ('cursorAuth/accessToken', ?)`, []byte(tok))
	db.Close()
	t.Setenv("CURSOR_STATE_DB", dbPath)

	if !(&Cursor{}).Installed() {
		t.Fatal("token present → installed")
	}
	got, err := cursorToken()
	if err != nil || got != tok {
		t.Fatalf("token read: %v", err)
	}
	s, err := cursorSession(tok, now)
	if err != nil || s != "user_ABC123%3A%3A"+tok {
		t.Fatalf("session = %q, %v", s, err)
	}
	if _, err := cursorSession(fakeJWT("auth0|u", now.Add(-time.Hour)), now); err == nil {
		t.Error("expired token accepted")
	}
	if _, err := cursorSession("cursor-safe-storage:v1:xyz", now); err == nil {
		t.Error("encrypted token accepted")
	}
	// UTF-16LE values
	u16 := []byte{}
	for _, r := range "eyJ.a.b" {
		u16 = append(u16, byte(r), 0)
	}
	if decodeStateValue(u16) != "eyJ.a.b" {
		t.Error("utf-16 value")
	}
}

func TestCursorFetchHTTP(t *testing.T) {
	now := time.Now()
	tok := fakeJWT("github|user_9", now.Add(time.Hour))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usage-summary" || !strings.Contains(r.Header.Get("Cookie"), "WorkosCursorSessionToken=user_9%3A%3A") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"billingCycleStart":"2026-11-03T10:00:00.000Z","billingCycleEnd":"2026-12-03T10:00:00.000Z",
			"membershipType":"pro","individualUsage":{
				"plan":{"enabled":true,"used":1500,"limit":2000,"totalPercentUsed":75,"autoPercentUsed":20,"apiPercentUsed":55},
				"onDemand":{"enabled":true,"used":350,"limit":5000}}}`))
	}))
	defer srv.Close()
	old := cursorAPI
	cursorAPI = srv.URL
	defer func() { cursorAPI = old }()

	r, err := fetchCursor(context.Background(), tok, now)
	if err != nil || r.Plan != "Pro" || len(r.Groups[0].Bars) != 3 {
		t.Fatalf("got %+v, %v", r, err)
	}
	inc := r.Groups[0].Bars[0]
	if inc.UsedPct != 75 || inc.ResetsAt != "2026-12-03T10:00:00Z" || inc.WindowSec != 30*86400 || inc.Tooltip != "$15.00 of $20.00 included usage this billing cycle" {
		t.Errorf("included = %+v", inc)
	}
	if len(r.Details) != 1 || r.Details[0].Value != "$3.50 of $50.00" {
		t.Errorf("on-demand = %+v", r.Details)
	}
}
