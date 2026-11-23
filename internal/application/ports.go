// Package application holds Kota's use cases: refresh usage, notify, keep
// history, check for updates, change settings. It talks to the outside world
// only through the ports below, which infrastructure and presentation
// implement; main.go wires them together.
package application

import (
	"context"
	"time"

	"kota/internal/domain"
)

// UsageSource reports one provider's usage (Claude, Codex, Antigravity).
type UsageSource interface {
	ID() string
	Name() string
	Fetch(ctx context.Context) domain.Usage
}

// PreferencesRepo stores the user's preferences.
type PreferencesRepo interface {
	Load() domain.Preferences
	Save(domain.Preferences)
}

// HistoryRepo stores the usage history.
type HistoryRepo interface {
	Load() domain.History
	Save(domain.History)
}

// AlertRepo stores which notifications were already sent.
type AlertRepo interface {
	Load() map[string]time.Time
	Save(map[string]time.Time)
}

// SecretStore keeps API keys the user enters (encrypted at rest).
type SecretStore interface {
	Get(name string) string
	Set(name, value string) error
}

// Notifier shows a notification to the user.
type Notifier interface {
	Notify(title, body string)
}

// ReleaseSource tells whether a newer release exists (nil if not).
type ReleaseSource interface {
	Newer(ctx context.Context) (*domain.Release, error)
}

// Autostart is the "Start with Windows" switch.
type Autostart interface {
	Enabled() bool
	Set(on bool) error
}

// Presence tells what the user is doing.
type Presence interface {
	Idle() time.Duration // since the last keyboard / mouse input
	Busy() bool          // fullscreen app, game or presentation
}
