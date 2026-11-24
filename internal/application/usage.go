package application

import (
	"context"
	"sync"
	"time"

	"kota/internal/domain"
)

const (
	fetchDeadline = 30 * time.Second
	repaintEvery  = 30 * time.Second // keep reset countdowns fresh
)

// UsageService fetches all providers and keeps the latest data. Listeners
// (taskbar, panel, history, notifications) get every new result.
type UsageService struct {
	sources  func() []UsageSource // asked on every fetch: providers come and go
	presence Presence

	mu        sync.Mutex
	last      []domain.Usage
	fetching  chan struct{} // non-nil while a fetch runs; closed when done
	fetchedAt time.Time
	kick      chan struct{} // asks the loop to fetch now
	onUsage   []func([]domain.Usage)
	onTick    []func()
}

func NewUsageService(sources func() []UsageSource, presence Presence) *UsageService {
	return &UsageService{sources: sources, presence: presence, kick: make(chan struct{}, 1)}
}

// OnUsage registers f for every new result (called on the fetching goroutine).
func (s *UsageService) OnUsage(f func([]domain.Usage)) { s.onUsage = append(s.onUsage, f) }

// OnTick registers f to run every 30 s between fetches (countdowns).
func (s *UsageService) OnTick(f func()) { s.onTick = append(s.onTick, f) }

// Snapshot is the latest data (nil before the first fetch).
func (s *UsageService) Snapshot() []domain.Usage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// Latest returns the latest data, fetching once if there is none yet.
func (s *UsageService) Latest() []domain.Usage {
	if d := s.Snapshot(); d != nil {
		return d
	}
	return s.Fetch()
}

// Fetch queries all providers in parallel. Concurrent callers share one run.
func (s *UsageService) Fetch() []domain.Usage {
	s.mu.Lock()
	if ch := s.fetching; ch != nil {
		s.mu.Unlock()
		<-ch
		return s.Snapshot()
	}
	ch := make(chan struct{})
	s.fetching = ch
	s.mu.Unlock()

	srcs := s.sources()
	out := make([]domain.Usage, len(srcs))
	var wg sync.WaitGroup
	for i, src := range srcs {
		wg.Add(1)
		go func(i int, src UsageSource) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), fetchDeadline)
			defer cancel()
			if !guard("fetch "+src.ID(), func() { out[i] = src.Fetch(ctx) }) {
				out[i] = domain.Usage{ID: src.ID(), Name: src.Name(), Groups: []domain.Group{},
					Error: "Internal error while reading usage (details in crash.log)."}
			}
		}(i, src)
	}
	wg.Wait()

	s.mu.Lock()
	s.last, s.fetching, s.fetchedAt = out, nil, time.Now()
	s.mu.Unlock()
	close(ch)
	for _, f := range s.onUsage {
		guard("usage listener", func() { f(out) })
	}
	return out
}

// RefreshIfStale asks the loop to fetch now when the data is older than d
// (e.g. the panel was just opened).
func (s *UsageService) RefreshIfStale(d time.Duration) {
	s.mu.Lock()
	stale := time.Since(s.fetchedAt) > d
	s.mu.Unlock()
	if stale {
		select {
		case s.kick <- struct{}{}:
		default:
		}
	}
}

// Run is the refresh loop. The delay adapts (domain.RefreshDelay): every
// 2 min while the user is active, every 15 min while idle or locked, right
// after a limit resets, and at once when the user comes back.
func (s *UsageService) Run() {
	repaint := time.NewTicker(repaintEvery)
	defer repaint.Stop()
	var nextAt time.Time
	fetchNow := func() {
		s.Fetch()
		nextAt = time.Now().Add(domain.RefreshDelay(s.presence.Idle(), s.Snapshot(), time.Now()))
	}
	fetchNow()
	wasIdle := false
	for {
		timer := time.NewTimer(max(time.Until(nextAt), 0))
		select {
		case <-timer.C:
			fetchNow()
		case <-s.kick:
			timer.Stop()
			fetchNow()
		case <-repaint.C:
			timer.Stop()
			idle := s.presence.Idle() >= domain.IdleAfter
			if wasIdle && !idle {
				fetchNow() // back at the keyboard: show fresh numbers
			} else {
				for _, f := range s.onTick {
					guard("tick listener", f)
				}
			}
			wasIdle = idle
		}
	}
}
