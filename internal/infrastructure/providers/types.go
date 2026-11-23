package providers

import (
	"context"
	"time"

	"kota/internal/domain"
)

func tokenWindow(label string, in, out int64) domain.TokenWindow {
	return domain.TokenWindow{Label: label, Input: human(in), Output: human(out), Total: human(in + out)}
}

// Provider fetches usage for one service.
type Provider interface {
	ID() string
	Name() string
	Fetch(ctx context.Context) domain.Usage
}

func newUsage(p Provider) domain.Usage {
	return domain.Usage{ID: p.ID(), Name: p.Name(), Groups: []domain.Group{}, UpdatedAt: time.Now().Format(time.RFC3339)}
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
