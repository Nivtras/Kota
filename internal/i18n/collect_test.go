package i18n

// Every English text the interface translates must have a Turkish entry in
// tr.json. This test collects the keys from the sources (Go i18n.T calls,
// the page's t() calls and data-i18n texts, and the provider messages that
// are translated where they are shown) and fails on a missing one.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var (
	goCall     = regexp.MustCompile(`i18n\.T\(\s*("(?:[^"\\]|\\.)*")`)
	jsCall     = regexp.MustCompile(`\bt\(\s*'((?:[^'\\]|\\.)*)'`)
	htmlStatic = regexp.MustCompile(`data-i18n(?:-title)?[^>]*>([^<]+)<|title="([^"]+)"[^>]*data-i18n-title`)
	// Provider texts shown through t()/i18n.T at display time: errors, notes,
	// labels, plan names, key hints. Collected from the provider sources.
	providerMsg = regexp.MustCompile(`(?:errNotConfigured\{|errors\.New\(|Note = |Error = |authHint:\s*|LastErr = |Label: |label: |Plan = |Plan: |Name: |Hint:\s*|\{"[a-z_]+", )("(?:[^"\\]|\\.)*")`)
)

func collect(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..")
	keys := map[string]string{} // key → where
	add := func(k, where string) {
		k = strings.TrimSpace(k)
		if k == "" || !strings.ContainsAny(k, "abcdefghijklmnopqrstuvwxyz") {
			return
		}
		keys[k] = where
	}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == "build" || d.Name() == ".git") {
			return filepath.SkipDir
		}
		b, _ := os.ReadFile(p)
		src := string(b)
		switch {
		case strings.HasSuffix(p, "_test.go"):
		case strings.HasSuffix(p, ".go"):
			for _, m := range goCall.FindAllStringSubmatch(src, -1) {
				if s, err := strconv.Unquote(m[1]); err == nil {
					add(s, p)
				}
			}
			if strings.Contains(p, "providers") || strings.HasSuffix(p, "main.go") {
				for _, m := range providerMsg.FindAllStringSubmatch(src, -1) {
					if s, err := strconv.Unquote(m[1]); err == nil && strings.Contains(s, " ") || err == nil && len(s) > 3 {
						add(s, p)
					}
				}
			}
		case strings.HasSuffix(p, ".js") && strings.Contains(p, "dist"):
			for _, m := range jsCall.FindAllStringSubmatch(src, -1) {
				add(strings.ReplaceAll(m[1], `\'`, `'`), p)
			}
		case strings.HasSuffix(p, ".html") && strings.Contains(p, "dist"):
			for _, m := range htmlStatic.FindAllStringSubmatch(src, -1) {
				add(m[1]+m[2], p)
			}
		}
		return nil
	})
	return keys
}

// skip: things that are names, not words (provider and product names,
// technical identifiers picked up by the broad provider pattern).
var skip = map[string]bool{
	"Claude": true, "Codex": true, "Antigravity": true, "Gemini CLI": true, "OpenRouter": true,
	"GitHub Copilot": true, "Cursor": true, "Kota": true, "Total": true, "HTTP %d": true,
	// identifiers the broad provider pattern also matches
	"archived_sessions": true, "codex": true, "https": true, "bad protobuf": true,
	"bad refresh response": true, "could not list processes:": true,
}

func TestEveryKeyIsTranslated(t *testing.T) {
	var missing []string
	for k, where := range collect(t) {
		if skip[k] || strings.HasPrefix(k, "kota/") {
			continue
		}
		if v, ok := tr[k]; !ok || v == "" {
			missing = append(missing, strconv.Quote(k)+"  ("+filepath.ToSlash(where)+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d texts have no Turkish translation in tr.json:\n%s", len(missing), strings.Join(missing, "\n"))
	}
}

// Translations must keep the same fmt verbs (in any order for indexed ones).
func TestVerbsMatch(t *testing.T) {
	verbs := regexp.MustCompile(`%(\[\d+\])?[-+#0]*\d*(\.\d+)?[a-zA-Z%]`)
	norm := func(s string) []string {
		v := verbs.FindAllString(s, -1)
		for i, x := range v {
			v[i] = regexp.MustCompile(`\[\d+\]`).ReplaceAllString(x, "")
		}
		sort.Strings(v)
		return v
	}
	for k, v := range tr {
		if strings.Join(norm(k), ",") != strings.Join(norm(v), ",") {
			t.Errorf("verbs differ:\n  %q\n  %q", k, v)
		}
	}
}
