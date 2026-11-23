package application

import (
	"context"
	"sync"
	"time"

	"kota/internal/domain"
)

const (
	firstUpdateCheck = time.Minute
	updateEvery      = 24 * time.Hour
)

// UpdateService checks once a day whether a newer release exists.
type UpdateService struct {
	src      ReleaseSource
	settings *SettingsService

	mu       sync.Mutex
	current  *domain.Release
	onChange func(*domain.Release)
}

func NewUpdateService(src ReleaseSource, settings *SettingsService) *UpdateService {
	return &UpdateService{src: src, settings: settings}
}

// OnChange registers f for a newer release appearing (or being cleared).
func (s *UpdateService) OnChange(f func(*domain.Release)) { s.onChange = f }

// Current is the newer release, or nil.
func (s *UpdateService) Current() *domain.Release {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

func (s *UpdateService) set(r *domain.Release) {
	s.mu.Lock()
	s.current = r
	f := s.onChange
	s.mu.Unlock()
	if f != nil {
		f(r)
	}
}

// Clear forgets a found release (the check was turned off).
func (s *UpdateService) Clear() { s.set(nil) }

// Run checks shortly after start and then daily while enabled.
func (s *UpdateService) Run() {
	time.Sleep(firstUpdateCheck)
	for {
		if !s.settings.Prefs().UpdateCheckOff {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			r, err := s.src.Newer(ctx)
			cancel()
			if err == nil {
				s.set(r)
			}
		}
		time.Sleep(updateEvery)
	}
}
