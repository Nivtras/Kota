//go:build windows

package storage

import (
	"bytes"
	"os"
	"testing"
)

func TestSecretsRoundTripEncrypted(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir()) // os.UserConfigDir → a temp folder
	if Secret("openrouter") != "" {
		t.Fatal("empty store should have no secret")
	}
	const key = "sk-or-v1-test-not-a-real-key"
	if err := SetSecret("openrouter", key); err != nil {
		t.Fatal(err)
	}
	if got := Secret("openrouter"); got != key {
		t.Fatalf("got %q", got)
	}
	raw, err := os.ReadFile(Path(secretsFile))
	if err != nil || bytes.Contains(raw, []byte(key)) || bytes.Contains(raw, []byte("openrouter")) {
		t.Fatalf("secrets file must be encrypted (err=%v)", err)
	}
	if err := SetSecret("openrouter", ""); err != nil || Secret("openrouter") != "" {
		t.Fatal("removing failed")
	}
}
