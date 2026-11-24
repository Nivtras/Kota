package application

import (
	"sync"

	"kota/internal/domain"
)

// Settings is what the panel's settings view shows.
type Settings struct {
	Taskbar   []string `json:"taskbar"`  // provider IDs shown on the taskbar (max 2)
	Display   string   `json:"display"`  // "used" | "remaining"
	Width     int      `json:"width"`    // taskbar pill width, logical px
	MinWidth  int      `json:"minWidth"` // slider bounds (read-only)
	MaxWidth  int      `json:"maxWidth"`
	Autostart bool     `json:"autostart"`
	Notify    bool     `json:"notify"`
	Follow    bool     `json:"follow"` // show the tool in use first
	Multi     bool     `json:"multi"`  // pill on every monitor's taskbar
	Update    bool     `json:"update"` // daily release check
	Warn      int      `json:"warn"`   // notification thresholds, % used
	Critical  int      `json:"critical"`
	Hotkey    string   `json:"hotkey"`   // panel shortcut ("" = default, "off")
	Language  string   `json:"language"` // chosen: "" (follow Windows), "en", "tr"
	Lang      string   `json:"lang"`     // in use: "en" or "tr" (filled by the panel API)
}

// SettingsService owns the preferences and the "tool in use" (follow mode).
type SettingsService struct {
	mu        sync.Mutex
	prefs     domain.Preferences
	active    string // provider of the AI tool in use
	repo      PreferencesRepo
	autostart Autostart
	listeners []func()
}

func NewSettingsService(repo PreferencesRepo, autostart Autostart) *SettingsService {
	return &SettingsService{prefs: repo.Load(), repo: repo, autostart: autostart}
}

// OnChange registers f to run after any change (on the caller's goroutine).
func (s *SettingsService) OnChange(f func()) {
	s.mu.Lock()
	s.listeners = append(s.listeners, f)
	s.mu.Unlock()
}

func (s *SettingsService) changed() {
	s.mu.Lock()
	ls := append([]func(){}, s.listeners...)
	s.mu.Unlock()
	for _, f := range ls {
		guard("settings listener", f)
	}
}

// Prefs returns a copy of the preferences.
func (s *SettingsService) Prefs() domain.Preferences {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.prefs
	p.Taskbar = append([]string(nil), p.Taskbar...)
	return p
}

// Update changes the preferences with f; save=false keeps the change in
// memory only (e.g. while a slider is being dragged).
func (s *SettingsService) Update(save bool, f func(*domain.Preferences)) {
	s.mu.Lock()
	f(&s.prefs)
	if save {
		s.repo.Save(s.prefs)
	}
	s.mu.Unlock()
	s.changed()
}

// Active is the provider of the AI tool in use ("" if none).
func (s *SettingsService) Active() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// SetActive records the tool in use (from activity detection).
func (s *SettingsService) SetActive(id string) {
	s.mu.Lock()
	same := s.active == id
	s.active = id
	s.mu.Unlock()
	if !same {
		s.changed()
	}
}

// View is the settings for the panel.
func (s *SettingsService) View() Settings {
	p := s.Prefs()
	v := Settings{
		Taskbar: p.Taskbar, Display: domain.NormDisplay(p.Display),
		Width: domain.ClampWidth(p.CompactWidth), MinWidth: domain.MinWidth, MaxWidth: domain.MaxWidth,
		Autostart: s.autostart.Enabled(),
		Notify:    !p.NotifyOff,
		Follow:    p.FollowActive,
		Multi:     p.AllMonitors,
		Update:    !p.UpdateCheckOff,
		Hotkey:    p.Hotkey,
		Language:  p.Language,
	}
	v.Warn, v.Critical = domain.NormLevels(p.NotifyWarn, p.NotifyCrit)
	return v
}

// SaveDisplay stores the taskbar providers and the display mode.
func (s *SettingsService) SaveDisplay(taskbar []string, display string) {
	s.Update(true, func(p *domain.Preferences) {
		p.Taskbar, p.Display = domain.NormTaskbar(taskbar), domain.NormDisplay(display)
	})
}

// SetWidth sets the pill width; save persists it.
func (s *SettingsService) SetWidth(w int, save bool) int {
	w = domain.ClampWidth(w)
	s.Update(save, func(p *domain.Preferences) { p.CompactWidth = w })
	return w
}

// SetPosition stores where the pill was dragged to ("" / nil = default).
func (s *SettingsService) SetPosition(anchor string, offset *int) {
	s.Update(true, func(p *domain.Preferences) { p.PillAnchor, p.PillOffset = anchor, offset })
}

// SetNotifyLevels stores the thresholds and returns the values kept
// (50 ≤ warn < critical ≤ 100).
func (s *SettingsService) SetNotifyLevels(warn, critical int) (int, int) {
	warn, critical = domain.NormLevels(warn, critical)
	s.Update(true, func(p *domain.Preferences) { p.NotifyWarn, p.NotifyCrit = warn, critical })
	return warn, critical
}

func (s *SettingsService) SetNotifications(on bool) {
	s.Update(true, func(p *domain.Preferences) { p.NotifyOff = !on })
}

func (s *SettingsService) SetFollowActive(on bool) {
	s.Update(true, func(p *domain.Preferences) { p.FollowActive = on })
}

func (s *SettingsService) SetAllMonitors(on bool) {
	s.Update(true, func(p *domain.Preferences) { p.AllMonitors = on })
}

// SetLanguage stores the interface language ("" = follow Windows).
func (s *SettingsService) SetLanguage(lang string) {
	s.Update(true, func(p *domain.Preferences) { p.Language = lang })
}

// SetHotkey stores the panel shortcut ("" = default, "off" = none).
func (s *SettingsService) SetHotkey(spec string) {
	s.Update(true, func(p *domain.Preferences) { p.Hotkey = spec })
}

func (s *SettingsService) SetUpdateCheck(on bool) {
	s.Update(true, func(p *domain.Preferences) { p.UpdateCheckOff = !on })
}

// SetAutostart turns "Start with Windows" on or off and returns the state.
func (s *SettingsService) SetAutostart(on bool) (bool, error) {
	err := s.autostart.Set(on)
	return s.autostart.Enabled(), err
}
