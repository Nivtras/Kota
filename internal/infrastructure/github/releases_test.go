package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewer(t *testing.T) {
	body := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	defer srv.Close()
	r := &Releases{API: srv.URL, Current: "1.1.0"}

	body = `{"tag_name":"v9.0.0","html_url":"https://github.com/o/kota/releases/tag/v9.0.0"}`
	if u, err := r.Newer(context.Background()); err != nil || u == nil || u.Version != "9.0.0" {
		t.Fatalf("newer: %+v %v", u, err)
	}
	body = `{"tag_name":"v1.1.0","html_url":"https://github.com/o/kota/releases/tag/x"}`
	if u, _ := r.Newer(context.Background()); u != nil {
		t.Fatalf("same version: %+v", u)
	}
	body = `{"tag_name":"v9.0.0","html_url":"https://evil.example/x"}`
	if u, _ := r.Newer(context.Background()); u != nil {
		t.Fatalf("non-GitHub URL accepted: %+v", u)
	}
	body = `{"tag_name":"v9.0.0","html_url":"https://github.com/o/kota/releases/tag/v9.0.0","prerelease":true}`
	if u, _ := r.Newer(context.Background()); u != nil {
		t.Fatalf("prerelease: %+v", u)
	}
}
