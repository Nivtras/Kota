package main

// history keeps the domain History in memory and in history.json.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"kota/internal/domain"
)

type history struct {
	mu   sync.Mutex
	h    domain.History
	path string // "" = don't persist (tests)
}

func newHistory(path string) *history {
	h := &history{h: domain.History{}, path: path}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, &h.h)
		}
	}
	return h
}

// record adds a sample for every limit in the data.
func (h *history) record(items []domain.Usage, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.h.Record(items, now) || h.path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(h.path), 0o755)
	if b, err := json.Marshal(h.h); err == nil {
		tmp := h.path + ".tmp"
		if os.WriteFile(tmp, b, 0o644) == nil {
			_ = os.Rename(tmp, h.path)
		}
	}
}

func (h *history) forProviders(items []domain.Usage, now time.Time) []domain.ProviderHistory {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.h.Charts(items, now)
}
