package main

// Update check: once a day, ask GitHub for the latest release. Nothing is
// downloaded; the panel shows a link to the release page.

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// updateRepo is the GitHub "owner/name" releases are published to.
const updateRepo = "OWNER/kota"

var releasesAPI = "https://api.github.com/repos/" + updateRepo + "/releases/latest"

// UpdateInfo is a newer release, if any.
type UpdateInfo struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

func latestRelease(ctx context.Context) (*UpdateInfo, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, releasesAPI, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Kota/"+appVersion)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil // no release yet, rate limited, …: try again tomorrow
	}
	var r struct {
		Tag        string `json:"tag_name"`
		URL        string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil || r.Draft || r.Prerelease {
		return nil, err
	}
	if !newerVersion(r.Tag, appVersion) || !strings.HasPrefix(r.URL, "https://github.com/") {
		return nil, nil
	}
	return &UpdateInfo{Version: strings.TrimPrefix(r.Tag, "v"), URL: r.URL}, nil
}

// newerVersion reports whether tag (e.g. "v1.2.0") is newer than cur ("1.1.0").
func newerVersion(tag, cur string) bool {
	parse := func(s string) [3]int {
		var v [3]int
		s = strings.TrimPrefix(strings.TrimSpace(s), "v")
		s, _, _ = strings.Cut(s, "-")
		for i, part := range strings.SplitN(s, ".", 3) {
			v[i], _ = strconv.Atoi(part)
		}
		return v
	}
	a, b := parse(tag), parse(cur)
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// updateLoop checks shortly after start and then daily while enabled.
func (a *App) updateLoop() {
	time.Sleep(time.Minute)
	for {
		a.cfgMu.Lock()
		off := a.cfg.UpdateCheckOff
		a.cfgMu.Unlock()
		if !off {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			info, err := latestRelease(ctx)
			cancel()
			if err == nil {
				a.dataMu.Lock()
				a.update = info
				a.dataMu.Unlock()
				a.emitUpdate()
			}
		}
		time.Sleep(24 * time.Hour)
	}
}
