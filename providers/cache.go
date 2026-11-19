package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

var cacheMu sync.Mutex

func cachePath(name string) string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "AIUsageWidget", name)
}

// loadJSON reads %APPDATA%\AIUsageWidget\<name>; a missing file yields T's zero value.
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
