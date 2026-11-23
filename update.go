package main

// Update check: once a day, ask GitHub whether a newer release exists.

import (
	"context"
	"time"

	"kota/internal/infrastructure/github"
)

// UpdateInfo is a newer release, if any.
type UpdateInfo = github.Release

var releases = github.NewReleases()

// updateLoop checks shortly after start and then daily while enabled.
func (a *App) updateLoop() {
	time.Sleep(time.Minute)
	for {
		a.cfgMu.Lock()
		off := a.cfg.UpdateCheckOff
		a.cfgMu.Unlock()
		if !off {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			info, err := releases.Newer(ctx)
			cancel()
			if err == nil {
				a.dataMu.Lock()
				a.update = info
				a.dataMu.Unlock()
				a.emitUpdate()
			}
		}
		time.Sleep(24 * time.Hour)
	}
}
