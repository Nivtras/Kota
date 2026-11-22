package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Uses a temp CLAUDE_CONFIG_DIR and a fake token server; real credentials
// are never touched.
func setupCreds(t *testing.T, expiresAt int64) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	creds := `{"claudeAiOauth":{"accessToken":"old-access","refreshToken":"old-refresh","expiresAt":` +
		jsonInt(expiresAt) + `,"scopes":["user:inference"],"subscriptionType":"max","rateLimitTier":"x","big":12345678901234567890},"other":{"keep":true}}`
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(creds), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func jsonInt(v int64) string { b, _ := json.Marshal(v); return string(b) }

func fakeTokenServer(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	old := claudeTokenURL
	claudeTokenURL = srv.URL
	t.Cleanup(func() { claudeTokenURL = old })
}

func TestClaudeTokenFreshNoRefresh(t *testing.T) {
	setupCreds(t, time.Now().Add(time.Hour).UnixMilli())
	fakeTokenServer(t, func(w http.ResponseWriter, r *http.Request) { t.Error("should not refresh") })
	o, err := claudeToken(context.Background())
	if err != nil || o.AccessToken != "old-access" {
		t.Fatalf("got %v %v", o, err)
	}
}

func TestClaudeTokenRefreshWritesBack(t *testing.T) {
	dir := setupCreds(t, time.Now().Add(-time.Hour).UnixMilli())
	fakeTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["grant_type"] != "refresh_token" || body["refresh_token"] != "old-refresh" {
			t.Errorf("bad request body")
		}
		w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`))
	})
	o, err := claudeToken(context.Background())
	if err != nil || o.AccessToken != "new-access" {
		t.Fatalf("got %v %v", o, err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".credentials.json"))
	s := string(raw)
	for _, want := range []string{`"new-access"`, `"new-refresh"`, `"subscriptionType":"max"`, `"rateLimitTier":"x"`,
		`"scopes":["user:inference"]`, `"big":12345678901234567890`, `"other":{"keep":true}`} {
		if !strings.Contains(s, want) {
			t.Errorf("written file lacks %s", want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".credentials.json.tmp")); err == nil {
		t.Error("tmp file left behind")
	}
	// Now fresh: a second call must not hit the server.
	claudeTokenURL = "http://127.0.0.1:1"
	if o, err := claudeToken(context.Background()); err != nil || o.AccessToken != "new-access" {
		t.Fatalf("second call: %v %v", o, err)
	}
}

// Claude Code refreshed first (rotating the refresh token): our refresh is
// rejected, but the file now holds a valid token, which we use.
func TestClaudeTokenRefreshRaceWithCLI(t *testing.T) {
	dir := setupCreds(t, time.Now().Add(-time.Hour).UnixMilli())
	fakeTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		cli := `{"claudeAiOauth":{"accessToken":"cli-access","refreshToken":"cli-refresh","expiresAt":` +
			jsonInt(time.Now().Add(time.Hour).UnixMilli()) + `}}`
		_ = os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(cli), 0o600)
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	})
	o, err := claudeToken(context.Background())
	if err != nil || o.AccessToken != "cli-access" {
		t.Fatalf("got %v %v", o, err)
	}
}

func TestClaudeTokenRefreshFails(t *testing.T) {
	setupCreds(t, time.Now().Add(-time.Hour).UnixMilli())
	fakeTokenServer(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusBadRequest) })
	if _, err := claudeToken(context.Background()); err == nil || !strings.Contains(err.Error(), "/login") {
		t.Fatalf("want login error, got %v", err)
	}
}
