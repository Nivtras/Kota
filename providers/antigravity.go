package providers

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

// localClient talks to the Antigravity language server on 127.0.0.1, which
// uses a self-signed certificate.
var localClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // 127.0.0.1 only
	},
}

// Antigravity reads quota groups from the running Antigravity language server.
type Antigravity struct{}

func (a *Antigravity) ID() string   { return "antigravity" }
func (a *Antigravity) Name() string { return "Antigravity" }

type agProcess struct {
	PID   int    `json:"pid"`
	Cmd   string `json:"cmd"`
	Ports []int  `json:"ports"`
}

type agEndpoint struct {
	base string // e.g. http://127.0.0.1:54320
	csrf string
}

type agBucket struct {
	DisplayName       string   `json:"displayName"`
	Description       string   `json:"description"`
	Window            string   `json:"window"`
	RemainingFraction *float64 `json:"remainingFraction"`
	ResetTime         string   `json:"resetTime"`
}

type agGroup struct {
	DisplayName string     `json:"displayName"`
	Description string     `json:"description"`
	Buckets     []agBucket `json:"buckets"`
}

type agQuotaSummary struct {
	Response struct {
		Groups []agGroup `json:"groups"`
	} `json:"response"`
}

type agUserStatus struct {
	UserStatus struct {
		UserTier struct {
			Description string `json:"description"`
		} `json:"userTier"`
	} `json:"userStatus"`
}

var csrfRe = regexp.MustCompile(`--csrf_token[= ](\S+)`)

const agProcScript = `$ErrorActionPreference='SilentlyContinue'
@(Get-CimInstance Win32_Process -Filter "Name like 'language_server%'" | Where-Object { $_.CommandLine -match 'antigravity' } | ForEach-Object {
  [pscustomobject]@{ pid=$_.ProcessId; cmd=$_.CommandLine; ports=@(Get-NetTCPConnection -OwningProcess $_.ProcessId -State Listen | Select-Object -ExpandProperty LocalPort -Unique) }
}) | ConvertTo-Json -Depth 3 -Compress`

const agMeta = `{"metadata":{"ideName":"antigravity","extensionName":"antigravity","locale":"en"}}`

// cached endpoint from the previous successful fetch
var agCached *agEndpoint

// Fetch order: running app (language server) → `agy` CLI (works with the app
// closed, read-only, no quota spent) → last saved data.
func (a *Antigravity) Fetch(ctx context.Context) Usage {
	u := newUsage(a)
	u.Tokens = antigravityLocalTokens()
	cache := loadJSON[agCache]("antigravity-cache.json")

	qs, plan, lsErr := fromLanguageServer(ctx)
	switch {
	case lsErr == nil:
		cache = agCache{Summary: qs, Plan: plan, FetchedAt: time.Now(), NextCLI: cache.NextCLI}
		saveJSON("antigravity-cache.json", cache)
	case time.Now().Before(cache.NextCLI) && cache.Summary != nil:
		// CLI was asked recently; reuse its answer instead of spawning it again.
		qs = cache.Summary
		u.Note = "App closed · via agy CLI"
	default:
		cliQs, cliErr := fromCLI(ctx)
		if cliErr == nil {
			qs = cliQs
			cache.Summary, cache.FetchedAt = cliQs, time.Now()
			cache.NextCLI = time.Now().Add(agCLIInterval)
			saveJSON("antigravity-cache.json", cache)
			u.Note = "App closed · via agy CLI"
		} else if cache.Summary != nil {
			qs = cache.Summary
			u.Note = fmt.Sprintf("Antigravity closed and agy CLI unavailable — showing data from %s.",
				cache.FetchedAt.Local().Format("Jan 2 15:04"))
		} else {
			u.Error = lsErr.Error()
			if errors.Is(cliErr, errNoCLI) {
				u.Error += " (agy CLI not found either)"
			}
			return u
		}
	}
	if u.Plan = plan; u.Plan == "" {
		u.Plan = cache.Plan
	}

	now := time.Now()
	for _, g := range qs.Response.Groups {
		grp := Group{Name: g.DisplayName, Tooltip: g.Description}
		for _, b := range g.Buckets {
			if b.RemainingFraction == nil {
				continue
			}
			used := clamp((1 - *b.RemainingFraction) * 100)
			reset := b.ResetTime
			if t, err := time.Parse(time.RFC3339, reset); err == nil && t.Before(now) {
				used = 0 // window already reset since this data was taken
			}
			if used < 0.5 {
				reset = "" // untouched window: reset time is meaningless
			}
			grp.Bars = append(grp.Bars, Bar{
				Label:     bucketLabel(b.Window, b.DisplayName),
				UsedPct:   used,
				ResetsAt:  reset,
				WindowSec: bucketWindowSec(b.Window),
				Tooltip:   b.Description,
			})
		}
		if len(grp.Bars) > 0 {
			u.Groups = append(u.Groups, grp)
		}
	}
	if len(u.Groups) == 0 {
		u.Note = "No quota data returned."
	}
	return u
}

const agCLIInterval = 3 * time.Minute

type agCache struct {
	Summary   *agQuotaSummary `json:"summary"`
	Plan      string          `json:"plan"`
	FetchedAt time.Time       `json:"fetchedAt"`
	NextCLI   time.Time       `json:"nextCli"`
}

func fromLanguageServer(ctx context.Context) (*agQuotaSummary, string, error) {
	var qs agQuotaSummary
	ep := agCached
	if ep == nil || agCall(ctx, ep, "RetrieveUserQuotaSummary", &qs) != nil {
		var err error
		ep, err = agDiscover(ctx, &qs)
		if err != nil {
			agCached = nil
			return nil, "", err
		}
		agCached = ep
	}
	plan := ""
	var us agUserStatus
	if agCall(ctx, ep, "GetUserStatus", &us) == nil {
		plan = us.UserStatus.UserTier.Description
	}
	return &qs, plan, nil
}

var errNoCLI = errors.New("agy CLI not found")

func agyPath() string {
	if h, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(h, ".gemini", "bin", "agy.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("agy"); err == nil {
		return p
	}
	return ""
}

// fromCLI runs `agy -p /usage --output-format json`, a read-only command that
// does not start an agent turn or spend quota.
func fromCLI(ctx context.Context) (*agQuotaSummary, error) {
	bin := agyPath()
	if bin == "" {
		return nil, errNoCLI
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	out, err := runHidden(ctx, bin, "-p", "/usage", "--output-format", "json")
	if err != nil {
		return nil, fmt.Errorf("agy: %w", err)
	}
	var r struct {
		Status  string `json:"status"`
		Command struct {
			Data struct {
				Groups []struct {
					Name        string `json:"name"`
					Description string `json:"description"`
					Buckets     []struct {
						Name              string   `json:"name"`
						Window            string   `json:"window"`
						RemainingFraction *float64 `json:"remaining_fraction"`
						ResetTime         string   `json:"reset_time"`
					} `json:"buckets"`
				} `json:"groups"`
			} `json:"data"`
		} `json:"command"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &r); err != nil {
		return nil, fmt.Errorf("agy: unexpected output")
	}
	if len(r.Command.Data.Groups) == 0 {
		return nil, fmt.Errorf("agy: no quota data (status %s)", r.Status)
	}
	var qs agQuotaSummary
	for _, g := range r.Command.Data.Groups {
		grp := agGroup{DisplayName: g.Name, Description: g.Description}
		for _, b := range g.Buckets {
			grp.Buckets = append(grp.Buckets, agBucket{
				DisplayName: b.Name, Window: b.Window,
				RemainingFraction: b.RemainingFraction, ResetTime: b.ResetTime,
			})
		}
		qs.Response.Groups = append(qs.Response.Groups, grp)
	}
	return &qs, nil
}

func bucketWindowSec(window string) int64 {
	switch window {
	case "weekly":
		return 7 * 24 * 3600
	case "5h":
		return 5 * 3600
	}
	return 0
}

func bucketLabel(window, name string) string {
	switch window {
	case "weekly":
		return "Weekly limit"
	case "5h":
		return "5h limit"
	}
	return strings.TrimSuffix(name, " Remaining")
}

func agDiscover(ctx context.Context, out *agQuotaSummary) (*agEndpoint, error) {
	raw, err := runHidden(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", agProcScript)
	if err != nil {
		return nil, errors.New("could not list processes: " + err.Error())
	}
	procs := parseProcs(raw)
	if len(procs) == 0 {
		return nil, errors.New("Antigravity is not running — open it to see quotas.")
	}
	lastErr := errors.New("could not reach the Antigravity language server")
	for _, p := range procs {
		m := csrfRe.FindStringSubmatch(p.Cmd)
		if m == nil {
			continue
		}
		for _, port := range p.Ports {
			for _, scheme := range []string{"http", "https"} {
				ep := &agEndpoint{base: fmt.Sprintf("%s://127.0.0.1:%d", scheme, port), csrf: m[1]}
				if err := agCall(ctx, ep, "RetrieveUserQuotaSummary", out); err == nil {
					return ep, nil
				} else {
					lastErr = err
				}
			}
		}
	}
	return nil, lastErr
}

func agCall(ctx context.Context, ep *agEndpoint, method string, out any) error {
	url := ep.base + "/exa.language_server_pb.LanguageServerService/" + method
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(agMeta))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("X-Codeium-Csrf-Token", ep.csrf)
	resp, err := localClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func parseProcs(out []byte) []agProcess {
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return nil
	}
	var list []agProcess
	if out[0] == '[' {
		_ = json.Unmarshal(out, &list)
	} else {
		var one agProcess
		if json.Unmarshal(out, &one) == nil {
			list = append(list, one)
		}
	}
	return list
}
