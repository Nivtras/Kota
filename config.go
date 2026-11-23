package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"kota/internal/domain"
	"kota/providers"
)

// config is the stored preferences (config.json).
type config = domain.Preferences

func configPath() string { return filepath.Join(providers.DataDir(), "config.json") }

func loadConfig() config {
	var c config
	if b, err := os.ReadFile(configPath()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func saveConfig(c config) {
	p := configPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if b, err := json.MarshalIndent(c, "", "  "); err == nil {
		_ = os.WriteFile(p, b, 0o644)
	}
}
