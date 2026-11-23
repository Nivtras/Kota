// Package github reads the latest Kota release from GitHub. Nothing is
// downloaded; the caller only gets the version and the release page.
package github

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"kota/internal/buildinfo"
	"kota/internal/domain"
)

// Release is a published release newer than the running version.
type Release struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

// Releases reads repo's "latest release".
type Releases struct {
	API     string // e.g. https://api.github.com/repos/owner/name/releases/latest
	Current string // running version
}

// NewReleases checks buildinfo.Repo against buildinfo.Version.
func NewReleases() *Releases {
	return &Releases{
		API:     "https://api.github.com/repos/" + buildinfo.Repo + "/releases/latest",
		Current: buildinfo.Version,
	}
}

// Newer returns the latest release if it is newer than Current, else nil.
// Drafts, pre-releases and non-GitHub links are ignored; a non-200 answer
// (no release yet, rate limited, …) is not an error.
func (r *Releases) Newer(ctx context.Context) (*Release, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, r.API, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", buildinfo.Name+"/"+buildinfo.Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	var v struct {
		Tag        string `json:"tag_name"`
		URL        string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil || v.Draft || v.Prerelease {
		return nil, err
	}
	if !domain.NewerVersion(v.Tag, r.Current) || !strings.HasPrefix(v.URL, "https://github.com/") {
		return nil, nil
	}
	return &Release{Version: strings.TrimPrefix(v.Tag, "v"), URL: v.URL}, nil
}
