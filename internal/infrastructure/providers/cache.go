package providers

import (
	"encoding/json"
	"kota/internal/infrastructure/storage"
	"os"
	"path/filepath"
	"sync"
)

var cacheMu sync.Mutex

func cachePath(name string) string { return storage.Path(name) }

// loadJSON reads %APPDATA%\Kota\<name>; a missing file yields T's zero value.
func loadJSON[T any](name string) T {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	var v T
	if b, err := os.ReadFile(cachePath(name)); err == nil {
		_ = json.Unmarshal(b, &v)
	}
	return v
}

func saveJSON(name string, v any) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	p := cachePath(name)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if b, err := json.Marshal(v); err == nil {
		_ = os.WriteFile(p, b, 0o644)
	}
}
