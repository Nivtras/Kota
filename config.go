package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type config struct {
	CompactWidth int      `json:"compactWidth,omitempty"` // taskbar pill width, logical px (0 = default)
	PillAnchor   string   `json:"pillAnchor,omitempty"`   // "start" (default) | "tray"
	PillOffset   *int     `json:"pillOffset,omitempty"`   // logical px from the anchor to the pill's right edge
	Taskbar      []string `json:"taskbar,omitempty"`      // provider IDs on the taskbar pill
	Display      string   `json:"display,omitempty"`      // "used" (default) | "remaining"
	NotifyOff    bool     `json:"notifyOff,omitempty"`    // usage notifications disabled
	PortablePath string   `json:"portablePath,omitempty"` // exe the user chose to run without installing
	NotifyWarn   int      `json:"notifyWarn,omitempty"`   // warning threshold, % used (0 = 80)
	NotifyCrit   int      `json:"notifyCrit,omitempty"`   // critical threshold, % used (0 = 95)
	FollowActive bool     `json:"followActive,omitempty"` // show the AI tool in use first on the pill
	AllMonitors  bool     `json:"allMonitors,omitempty"`  // pill on every monitor's taskbar
}

func configPath() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "AIUsageWidget", "config.json")
}

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
