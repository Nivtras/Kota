package providers

// Gemini CLI: the sign-in the CLI keeps in ~/.gemini/oauth_creds.json →
// Code Assist's retrieveUserQuota (per-model daily quota). The credentials
// file is only read. When its access token has expired, a fresh one is
// fetched with the OAuth client of the installed Gemini CLI and kept in
// memory only, so the CLI's own sign-in is never touched.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"kota/internal/domain"
)

var (
	geminiCodeAssist = "https://cloudcode-pa.googleapis.com/v1internal"
	geminiTokenURL   = "https://oauth2.googleapis.com/token"
)

type Gemini struct{}

func (g *Gemini) ID() string   { return "gemini" }
func (g *Gemini) Name() string { return "Gemini CLI" }

func geminiDir() string {
	if d := os.Getenv("GEMINI_CLI_HOME"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".gemini")
}

func geminiCredsPath() string { return filepath.Join(geminiDir(), "oauth_creds.json") }

// Installed: the Gemini CLI has been signed in with Google on this machine.
func (g *Gemini) Installed() bool {
	_, err := os.Stat(geminiCredsPath())
	return err == nil
}

func (g *Gemini) Fetch(ctx context.Context) domain.Usage {
	return fetchRemote(ctx, newUsage(g), remoteSpec{
		cacheFile:   "gemini-cache.json",
		minInterval: 3 * time.Minute,
		service:     "Gemini",
		authHint:    "Gemini CLI sign-in expired — run `gemini` and sign in again",
		fetch:       fetchGemini,
	})
}

type geminiCreds struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiryDate   int64  `json:"expiry_date"` // unix ms
}

var geminiMem struct {
	sync.Mutex
	token  string
	expiry time.Time
}

// geminiToken returns a usable access token without writing anything.
func geminiToken(ctx context.Context) (string, error) {
	b, err := os.ReadFile(geminiCredsPath())
	if err != nil {
		return "", errNotConfigured{"Not signed in — run `gemini` in a terminal and sign in with Google."}
	}
	var c geminiCreds
	if json.Unmarshal(b, &c) != nil || (c.AccessToken == "" && c.RefreshToken == "") {
		return "", errNotConfigured{"Gemini CLI credentials are unreadable — sign in again with `gemini`."}
	}
	if c.AccessToken != "" && time.UnixMilli(c.ExpiryDate).After(time.Now().Add(time.Minute)) {
		return c.AccessToken, nil
	}
	geminiMem.Lock()
	defer geminiMem.Unlock()
	if geminiMem.token != "" && geminiMem.expiry.After(time.Now().Add(time.Minute)) {
		return geminiMem.token, nil
	}
	if c.RefreshToken == "" {
		return "", &httpError{code: http.StatusUnauthorized}
	}
	id, secret := geminiOAuthClient()
	if id == "" {
		return "", errNotConfigured{"Gemini CLI token expired — run `gemini` once to refresh it."}
	}
	form := url.Values{"client_id": {id}, "client_secret": {secret}, "refresh_token": {c.RefreshToken}, "grant_type": {"refresh_token"}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, geminiTokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	body, err := doJSON(req)
	if err != nil {
		return "", err
	}
	var r struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if json.Unmarshal(body, &r) != nil || r.AccessToken == "" {
		return "", &httpError{code: http.StatusUnauthorized}
	}
	geminiMem.token, geminiMem.expiry = r.AccessToken, time.Now().Add(time.Duration(r.ExpiresIn)*time.Second)
	return r.AccessToken, nil
}

var (
	geminiClientIDRe     = regexp.MustCompile(`OAUTH_CLIENT_ID\s*=\s*['"]([^'"]+)['"]`)
	geminiClientSecretRe = regexp.MustCompile(`OAUTH_CLIENT_SECRET\s*=\s*['"]([^'"]+)['"]`)
)

// geminiOAuthClient reads the OAuth client the installed Gemini CLI uses
// (it is public in the CLI's source), from its npm package.
func geminiOAuthClient() (id, secret string) {
	if id, secret = os.Getenv("GEMINI_OAUTH_CLIENT_ID"), os.Getenv("GEMINI_OAUTH_CLIENT_SECRET"); id != "" {
		return id, secret
	}
	var roots []string
	if d := os.Getenv("APPDATA"); d != "" {
		roots = append(roots, filepath.Join(d, "npm", "node_modules"))
	}
	if d := os.Getenv("LOCALAPPDATA"); d != "" {
		roots = append(roots, filepath.Join(d, "pnpm", "global"), filepath.Join(d, "Volta", "tools", "image", "packages"))
	}
	for _, root := range roots {
		for _, rel := range []string{
			`@google\gemini-cli\node_modules\@google\gemini-cli-core\dist\src\code_assist\oauth2.js`,
			`@google\gemini-cli-core\dist\src\code_assist\oauth2.js`,
		} {
			if b, err := os.ReadFile(filepath.Join(root, rel)); err == nil {
				if m, n := geminiClientIDRe.FindSubmatch(b), geminiClientSecretRe.FindSubmatch(b); m != nil && n != nil {
					return string(m[1]), string(n[1])
				}
			}
		}
		// Newer releases ship one bundle: look through the package's .js files.
		pkg := filepath.Join(root, "@google", "gemini-cli")
		var found [2]string
		_ = filepath.WalkDir(pkg, func(p string, d os.DirEntry, err error) error {
			if err != nil || found[0] != "" || d.IsDir() || !strings.HasSuffix(p, ".js") {
				return nil
			}
			if info, e := d.Info(); e != nil || info.Size() > 64<<20 {
				return nil
			}
			if b, e := os.ReadFile(p); e == nil {
				if m, n := geminiClientIDRe.FindSubmatch(b), geminiClientSecretRe.FindSubmatch(b); m != nil && n != nil {
					found = [2]string{string(m[1]), string(n[1])}
				}
			}
			return nil
		})
		if found[0] != "" {
			return found[0], found[1]
		}
	}
	return "", ""
}

func geminiPost(ctx context.Context, token, method string, in, out any) error {
	b, _ := json.Marshal(in)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, geminiCodeAssist+":"+method, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	body, err := doJSON(req)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

type geminiQuota struct {
	Buckets []struct {
		ModelID           string   `json:"modelId"`
		RemainingFraction *float64 `json:"remainingFraction"`
		ResetTime         string   `json:"resetTime"`
		TokenType         string   `json:"tokenType"`
	} `json:"buckets"`
}

func fetchGemini(ctx context.Context) (remoteResult, error) {
	token, err := geminiToken(ctx)
	if err != nil {
		return remoteResult{}, err
	}
	var assist struct {
		Project     json.RawMessage `json:"cloudaicompanionProject"`
		CurrentTier struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"currentTier"`
	}
	_ = geminiPost(ctx, token, "loadCodeAssist", map[string]any{
		"metadata": map[string]string{"ideType": "GEMINI_CLI", "pluginType": "GEMINI"},
	}, &assist) // optional: plan name + quota project
	body := map[string]string{}
	if p := geminiProjectID(assist.Project); p != "" {
		body["project"] = p
	}
	var q geminiQuota
	if err := geminiPost(ctx, token, "retrieveUserQuota", body, &q); err != nil {
		return remoteResult{}, err
	}
	r := geminiResult(q)
	r.Plan = assist.CurrentTier.Name
	return r, nil
}

// geminiProjectID accepts "projects-id" or {"id": "..."}.
func geminiProjectID(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &o)
	return o.ID
}

// geminiResult groups the per-model buckets into Pro / Flash / Flash Lite,
// each showing its most-used model (lowest remaining fraction).
func geminiResult(q geminiQuota) remoteResult {
	type tier struct {
		name      string
		remaining float64
		reset     string
		models    []string
	}
	tiers := map[string]*tier{}
	order := []string{"Pro models", "Flash models", "Flash Lite models", "Other models"}
	for _, b := range q.Buckets {
		if b.RemainingFraction == nil {
			continue
		}
		id := strings.ToLower(b.ModelID)
		name := "Other models"
		switch {
		case strings.Contains(id, "flash-lite") || strings.Contains(id, "flash_lite"):
			name = "Flash Lite models"
		case strings.Contains(id, "flash"):
			name = "Flash models"
		case strings.Contains(id, "pro"):
			name = "Pro models"
		}
		t := tiers[name]
		if t == nil {
			t = &tier{name: name, remaining: 2}
			tiers[name] = t
		}
		t.models = append(t.models, b.ModelID)
		if *b.RemainingFraction < t.remaining {
			t.remaining, t.reset = *b.RemainingFraction, b.ResetTime
		}
	}
	var bars []domain.Bar
	for _, name := range order {
		t := tiers[name]
		if t == nil {
			continue
		}
		sort.Strings(t.models)
		b := domain.Bar{Label: name, UsedPct: clamp(100 * (1 - t.remaining)), WindowSec: 24 * 3600,
			Tooltip: "Daily quota · " + strings.Join(t.models, ", ")}
		if rt, err := time.Parse(time.RFC3339, t.reset); err == nil {
			b.ResetsAt = rt.UTC().Format(time.RFC3339)
		}
		bars = append(bars, b)
	}
	r := remoteResult{}
	if len(bars) > 0 {
		r.Groups = []domain.Group{{Bars: bars}}
	} else {
		r.Note = "No quota reported for this account."
	}
	return r
}
