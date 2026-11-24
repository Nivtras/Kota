package providers

import (
	"fmt"
	"kota/internal/domain"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexLocalTokens(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	dir := filepath.Join(home, "sessions", "2026", "11", "18")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	ts := func(d time.Duration) string { return time.Now().Add(-d).UTC().Format(time.RFC3339Nano) }
	ev := func(at string, total, lastIn, lastOut int64) string {
		return fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":0,"output_tokens":0,"total_tokens":%d},"last_token_usage":{"input_tokens":%d,"output_tokens":%d,"total_tokens":%d}}}}`,
			at, total, lastIn, lastOut, lastIn+lastOut)
	}
	lines := []string{
		`{"timestamp":"` + ts(30*time.Minute) + `","type":"event_msg","payload":{"type":"token_count","info":null}}`,
		ev(ts(2*24*time.Hour), 1100, 1000, 100), // 2 days ago: 7d only
		ev(ts(time.Hour), 3300, 2000, 200),      // 1h ago: 5h and 7d
		ev(ts(time.Hour), 3300, 2000, 200),      // duplicate emission: ignored
		`{"timestamp":"` + ts(time.Hour) + `","type":"response_item","payload":{"type":"message"}}`,
		ev(ts(10*24*time.Hour), 9999, 50000, 5000), // older than 7d: ignored
	}
	if err := os.WriteFile(filepath.Join(dir, "rollout-test.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tally, err := codexLocalTokens()
	if err != nil {
		t.Fatal(err)
	}
	tw := tally.windows()
	// per day: 2 days ago and today (local), 7 days, oldest first
	d := tally.daily()
	if len(d) != 7 || d[6].Day != time.Now().Format("2006-01-02") || d[6].Tokens != 2200 {
		t.Errorf("daily = %+v", d)
	}
	var sum int64
	for _, x := range d {
		sum += x.Tokens
	}
	if sum != 3300 {
		t.Errorf("daily sum = %d, want 3300", sum)
	}
	want := []domain.TokenWindow{
		{Label: "Last 5h", Input: "2.0K", Output: "200", Total: "2.2K"},
		{Label: "Last 7d", Input: "3.0K", Output: "300", Total: "3.3K"},
	}
	for i := range want {
		if tw[i] != want[i] {
			t.Errorf("window %d = %+v, want %+v", i, tw[i], want[i])
		}
	}
}
