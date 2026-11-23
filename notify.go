package main

// notifier applies the domain's NotificationPolicy and persists which alerts
// were already sent, so a restart doesn't repeat them.

import (
	"sync"
	"time"

	"kota/internal/domain"
	"kota/internal/infrastructure/storage"
)

type notifier struct {
	mu     sync.Mutex
	policy domain.NotificationPolicy
	state  *domain.AlertState
	store  *storage.AlertStore
}

func newNotifier(store *storage.AlertStore) *notifier {
	st := domain.NewAlertState()
	st.Sent = store.Load()
	return &notifier{policy: domain.NewNotificationPolicy(0, 0), state: st, store: store}
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
	if changed {
		n.store.Save(n.state.Sent)
	}
	return alerts
}
