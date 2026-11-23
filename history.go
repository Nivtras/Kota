package main

// history keeps the domain History in memory and in history.json.

import (
	"sync"
	"time"

	"kota/internal/domain"
	"kota/internal/infrastructure/storage"
)

type history struct {
	mu    sync.Mutex
	h     domain.History
	store *storage.HistoryStore
}

func newHistory(store *storage.HistoryStore) *history {
	return &history{h: store.Load(), store: store}
}

// record adds a sample for every limit in the data.
func (h *history) record(items []domain.Usage, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.h.Record(items, now) {
		h.store.Save(h.h)
	}
}

func (h *history) forProviders(items []domain.Usage, now time.Time) []domain.ProviderHistory {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.h.Charts(items, now)
}
