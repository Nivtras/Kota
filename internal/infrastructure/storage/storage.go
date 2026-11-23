// Package storage keeps Kota's files in %APPDATA%\Kota: preferences,
// history, notification state and the diagnostic log.
package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"kota/internal/domain"
)

// DataDir is where Kota keeps settings, caches and history (%APPDATA%\Kota).
func DataDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "Kota")
}

// Path is a file in DataDir.
func Path(name string) string { return filepath.Join(DataDir(), name) }

// MigrateOldFolder moves the data of the app's old name (AI Usage Widget)
// to DataDir, once.
func MigrateOldFolder() {
	newDir := DataDir()
	oldDir := filepath.Join(filepath.Dir(newDir), "AIUsageWidget")
	if _, err := os.Stat(newDir); os.IsNotExist(err) {
		if _, err := os.Stat(oldDir); err == nil {
			_ = os.Rename(oldDir, newDir)
		}
	}
}

// file is one JSON file; reads and writes are serialised.
type file struct {
	mu   sync.Mutex
	path string // "" = memory only (tests)
}

func (f *file) load(v any) {
	if f.path == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, err := os.ReadFile(f.path); err == nil {
		_ = json.Unmarshal(b, v)
	}
}

// save writes atomically (temp file + rename), so a crash can't leave a
// half-written file behind.
func (f *file) save(v any, indent bool) {
	if f.path == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var b []byte
	var err error
	if indent {
		b, err = json.MarshalIndent(v, "", "  ")
	} else {
		b, err = json.Marshal(v)
	}
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(f.path), 0o755)
	tmp := f.path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, f.path)
	}
}

// PreferencesStore is config.json.
type PreferencesStore struct{ f file }

// NewPreferencesStore uses path ("" = memory only).
func NewPreferencesStore(path string) *PreferencesStore { return &PreferencesStore{file{path: path}} }

func (s *PreferencesStore) Load() domain.Preferences {
	var p domain.Preferences
	s.f.load(&p)
	return p
}

func (s *PreferencesStore) Save(p domain.Preferences) { s.f.save(p, true) }

// HistoryStore is history.json.
type HistoryStore struct{ f file }

func NewHistoryStore(path string) *HistoryStore { return &HistoryStore{file{path: path}} }

func (s *HistoryStore) Load() domain.History {
	h := domain.History{}
	s.f.load(&h)
	return h
}

func (s *HistoryStore) Save(h domain.History) { s.f.save(h, false) }

// AlertStore is notify-state.json: which alerts were already sent.
type AlertStore struct{ f file }

func NewAlertStore(path string) *AlertStore { return &AlertStore{file{path: path}} }

func (s *AlertStore) Load() map[string]time.Time {
	m := map[string]time.Time{}
	s.f.load(&m)
	return m
}

func (s *AlertStore) Save(sent map[string]time.Time) { s.f.save(sent, false) }

var logMu sync.Mutex

// Log appends a line to dock.log (kept under 64 KB) for diagnosing taskbar
// placement and installs.
func Log(format string, args ...any) {
	logMu.Lock()
	defer logMu.Unlock()
	p := Path("dock.log")
	if fi, err := os.Stat(p); err == nil && fi.Size() > 64<<10 {
		_ = os.Remove(p)
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
}
