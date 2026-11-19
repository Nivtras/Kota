package providers

import (
	"context"
	"time"
)

// Bar is a single limit line. Standard: show what is USED.
type Bar struct {
	Label    string  `json:"label"`
	UsedPct  float64 `json:"usedPct"` // 0..100
	ResetsAt string  `json:"resetsAt,omitempty"`
	Tooltip  string  `json:"tooltip,omitempty"`
}

// Group is a set of bars sharing one quota pool (e.g. "Gemini Models").
// Name may be empty when a provider has a single pool.
type Group struct {
	Name    string `json:"name,omitempty"`
	Tooltip string `json:"tooltip,omitempty"`
	Bars    []Bar  `json:"bars"`
}

// Stat is a key/value shown in the expandable details section.
type Stat struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// TokenWindow is one block in the token dropdown (e.g. "Last 5h").
// Values are pre-formatted (e.g. "45.5M").
type TokenWindow struct {
	Label  string `json:"label"`
	Input  string `json:"input"`
	Output string `json:"output"`
	Total  string `json:"total"`
}

func tokenWindow(label string, in, out int64) TokenWindow {
	return TokenWindow{Label: label, Input: human(in), Output: human(out), Total: human(in + out)}
}

// Usage is what one provider reports.
type Usage struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Plan      string        `json:"plan,omitempty"`
	Groups    []Group       `json:"groups"`
	Details   []Stat        `json:"details,omitempty"`
	Tokens    []TokenWindow `json:"tokens,omitempty"`
	Note      string        `json:"note,omitempty"`
	Error     string        `json:"error,omitempty"`
	UpdatedAt string        `json:"updatedAt"`
}

// Provider fetches usage for one service.
type Provider interface {
	ID() string
	Name() string
	Fetch(ctx context.Context) Usage
}

func newUsage(p Provider) Usage {
	return Usage{ID: p.ID(), Name: p.Name(), Groups: []Group{}, UpdatedAt: time.Now().Format(time.RFC3339)}
}

// installer is implemented by providers that should be hidden when the tool
// is not set up on this machine.
type installer interface{ Installed() bool }

// All returns the providers present on this machine, in display order.
func All() []Provider {
	var out []Provider
	for _, p := range []Provider{&Claude{}, &Codex{}, &Antigravity{}} {
		if i, ok := p.(installer); ok && !i.Installed() {
			continue
		}
		out = append(out, p)
	}
	return out
}
