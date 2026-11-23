package application

import (
	"fmt"
	"sync"
	"time"

	"kota/internal/domain"
)

const (
	maxHeld        = 4                // alerts kept while the user is busy
	busyCheckEvery = 10 * time.Second // how often a held queue looks again
	heldGap        = 6 * time.Second  // between held alerts: one balloon at a time
)

// NotificationService applies the notification policy to every new result
// and shows the alerts — or holds them while a fullscreen app, game or
// presentation runs, and shows them when it ends.
type NotificationService struct {
	settings *SettingsService
	repo     AlertRepo
	out      Notifier
	presence Presence

	mu    sync.Mutex
	state *domain.AlertState

	heldMu   sync.Mutex
	held     []domain.Alert
	watching bool
}

func NewNotificationService(settings *SettingsService, repo AlertRepo, out Notifier, presence Presence) *NotificationService {
	st := domain.NewAlertState()
	st.Sent = repo.Load()
	return &NotificationService{settings: settings, repo: repo, out: out, presence: presence, state: st}
}

// Check evaluates new data. Alerts are computed even while notifications
// are off, so the state stays current, but only shown when they're on.
func (n *NotificationService) Check(items []domain.Usage) {
	p := n.settings.Prefs()
	policy := domain.NewNotificationPolicy(p.NotifyWarn, p.NotifyCrit)
	n.mu.Lock()
	alerts, changed := policy.Check(n.state, items, time.Now())
	if changed {
		n.repo.Save(n.state.Sent)
	}
	n.mu.Unlock()
	if p.NotifyOff {
		return
	}
	for _, a := range alerts {
		n.show(a)
	}
}

// Test shows a sample notification (settings button).
func (n *NotificationService) Test() {
	p := n.settings.Prefs()
	w, c := domain.NormLevels(p.NotifyWarn, p.NotifyCrit)
	n.show(domain.Alert{
		Title: "Kota notifications are on",
		Body:  fmt.Sprintf("You'll be told at %d%% and %d%% of a limit, and when it's available again.", w, c),
	})
}

func (n *NotificationService) show(a domain.Alert) {
	if n.presence.Busy() {
		n.hold(a)
		return
	}
	n.out.Notify(a.Title, a.Body)
}

func (n *NotificationService) hold(a domain.Alert) {
	n.heldMu.Lock()
	defer n.heldMu.Unlock()
	for i, h := range n.held {
		if h.Title == a.Title { // a newer reading of the same thing
			n.held = append(n.held[:i], n.held[i+1:]...)
			break
		}
	}
	n.held = append(n.held, a)
	if len(n.held) > maxHeld {
		n.held = n.held[len(n.held)-maxHeld:]
	}
	if !n.watching {
		n.watching = true
		go n.flushWhenFree()
	}
}

func (n *NotificationService) flushWhenFree() {
	for range time.Tick(busyCheckEvery) {
		if n.presence.Busy() {
			continue
		}
		n.heldMu.Lock()
		items := n.held
		n.held, n.watching = nil, false
		n.heldMu.Unlock()
		for i, a := range items {
			if i > 0 {
				time.Sleep(heldGap)
			}
			n.out.Notify(a.Title, a.Body)
		}
		return
	}
}
