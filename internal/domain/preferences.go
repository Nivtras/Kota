package domain

// Preferences are the user's settings, as stored in config.json.
type Preferences struct {
	CompactWidth   int      `json:"compactWidth,omitempty"`   // taskbar pill width, logical px (0 = default)
	PillAnchor     string   `json:"pillAnchor,omitempty"`     // "start" (default) | "tray"
	PillOffset     *int     `json:"pillOffset,omitempty"`     // logical px from the anchor to the pill's right edge
	Taskbar        []string `json:"taskbar,omitempty"`        // provider IDs on the taskbar pill
	Display        string   `json:"display,omitempty"`        // "used" (default) | "remaining"
	NotifyOff      bool     `json:"notifyOff,omitempty"`      // usage notifications disabled
	PortablePath   string   `json:"portablePath,omitempty"`   // exe the user chose to run without installing
	NotifyWarn     int      `json:"notifyWarn,omitempty"`     // warning threshold, % used (0 = 80)
	NotifyCrit     int      `json:"notifyCrit,omitempty"`     // critical threshold, % used (0 = 95)
	FollowActive   bool     `json:"followActive,omitempty"`   // show the AI tool in use first on the pill
	UpdateCheckOff bool     `json:"updateCheckOff,omitempty"` // daily GitHub release check disabled
	AllMonitors    bool     `json:"allMonitors,omitempty"`    // pill on every monitor's taskbar
	Hotkey         string   `json:"hotkey,omitempty"`         // panel shortcut, "" = default, "off" = none
	Language       string   `json:"language,omitempty"`       // "" = Windows' language, "en", "tr"
}

// Taskbar pill width bounds, logical px.
const (
	DefaultWidth = 200
	MinWidth     = 180
	MaxWidth     = 320
)

// ClampWidth keeps a pill width within bounds; 0 means the default.
func ClampWidth(w int) int {
	if w == 0 {
		return DefaultWidth
	}
	return max(MinWidth, min(MaxWidth, w))
}

// Display modes.
const (
	DisplayUsed      = "used"
	DisplayRemaining = "remaining"
)

// NormDisplay maps anything but "remaining" to "used".
func NormDisplay(d string) string {
	if d == DisplayRemaining {
		return d
	}
	return DisplayUsed
}

// NormTaskbar keeps at most MaxTaskbarProviders chosen providers.
func NormTaskbar(ids []string) []string {
	if len(ids) > MaxTaskbarProviders {
		return ids[:MaxTaskbarProviders]
	}
	return ids
}
