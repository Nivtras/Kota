package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		tag, cur string
		want     bool
	}{
		{"v1.2.0", "1.1.0", true}, {"1.1.1", "1.1.0", true}, {"v2.0", "1.9.9", true},
		{"v1.1.0", "1.1.0", false}, {"v1.0.9", "1.1.0", false}, {"v1.2.0-beta", "1.2.0", false}, {"", "1.1.0", false},
	} {
		if got := newerVersion(c.tag, c.cur); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v", c.tag, c.cur, got)
		}
	}
}

func TestLatestRelease(t *testing.T) {
	body := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	defer srv.Close()
	old := releasesAPI
	releasesAPI = srv.URL
	defer func() { releasesAPI = old }()

	body = `{"tag_name":"v9.0.0","html_url":"https://github.com/o/kota/releases/tag/v9.0.0"}`
	if u, err := latestRelease(context.Background()); err != nil || u == nil || u.Version != "9.0.0" {
		t.Fatalf("newer: %+v %v", u, err)
	}
	body = `{"tag_name":"v` + appVersion + `","html_url":"https://github.com/o/kota/releases/tag/x"}`
	if u, _ := latestRelease(context.Background()); u != nil {
		t.Fatalf("same version: %+v", u)
	}
	body = `{"tag_name":"v9.0.0","html_url":"https://evil.example/x"}`
	if u, _ := latestRelease(context.Background()); u != nil {
		t.Fatalf("non-GitHub URL accepted: %+v", u)
	}
	body = `{"tag_name":"v9.0.0","html_url":"https://github.com/o/kota/releases/tag/v9.0.0","prerelease":true}`
	if u, _ := latestRelease(context.Background()); u != nil {
		t.Fatalf("prerelease: %+v", u)
	}
}
