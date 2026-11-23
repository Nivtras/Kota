package main

import (
	"kota/internal/domain"
	"kota/internal/infrastructure/storage"
)

// config is the stored preferences (config.json).
type config = domain.Preferences

var prefsStore = storage.NewPreferencesStore(storage.Path("config.json"))

func loadConfig() config  { return prefsStore.Load() }
func saveConfig(c config) { prefsStore.Save(c) }
