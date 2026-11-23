package application

import (
	"sync"
	"time"

	"kota/internal/domain"
)

// HistoryService samples every new result into the 7-day history.
type HistoryService struct {
	mu   sync.Mutex
	h    domain.History
	repo HistoryRepo
}

func NewHistoryService(repo HistoryRepo) *HistoryService {
	return &HistoryService{h: repo.Load(), repo: repo}
}

// Record adds the samples due for this data.
func (s *HistoryService) Record(items []domain.Usage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h.Record(items, time.Now()) {
		s.repo.Save(s.h)
	}
}

// Charts returns what the panel charts per provider.
func (s *HistoryService) Charts(items []domain.Usage) []domain.ProviderHistory {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.h.Charts(items, time.Now())
}
