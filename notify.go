package main

// notifier applies the domain's NotificationPolicy and persists which alerts
// were already sent (notify-state.json), so a restart doesn't repeat them.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"kota/internal/domain"
)

type notifier struct {
	mu     sync.Mutex
	policy domain.NotificationPolicy
	state  *domain.AlertState
	path   string // "" = don't persist (tests)
}

func newNotifier(path string) *notifier {
	n := &notifier{policy: domain.NewNotificationPolicy(0, 0), state: domain.NewAlertState(), path: path}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, &n.state.Sent)
		}
	}
	return n
}

func (n *notifier) setLevels(warn, critical int) {
	n.mu.Lock()
	n.policy = domain.NewNotificationPolicy(warn, critical)
	n.mu.Unlock()
}

// check returns the notifications due for this data.
func (n *notifier) check(items []domain.Usage, now time.Time) []domain.Alert {
	n.mu.Lock()
	defer n.mu.Unlock()
	alerts, changed := n.policy.Check(n.state, items, now)
	if changed && n.path != "" {
		_ = os.MkdirAll(filepath.Dir(n.path), 0o755)
		if b, err := json.Marshal(n.state.Sent); err == nil {
			_ = os.WriteFile(n.path, b, 0o644)
		}
	}
	return alerts
}
