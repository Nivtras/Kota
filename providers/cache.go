package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

var cacheMu sync.Mutex

// DataDir is where Kota keeps settings, caches and history (%APPDATA%\Kota).
func DataDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "Kota")
}

func cachePath(name string) string { return filepath.Join(DataDir(), name) }

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
