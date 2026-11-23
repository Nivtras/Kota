# Kota — architecture

Kota is layered in the spirit of Domain-Driven Design. The root holds only `main.go`, the
composition root; everything else lives under `internal/`. Dependencies point inwards only:

```
presentation ──► application ──► domain
       │               ▲
       └──► infrastructure (implements application's ports)
```

`domain` imports nothing of Kota's; `application` imports only `domain`; infrastructure never
imports application or presentation. Presentation uses a few infrastructure pieces directly:
`win32` for window handling, `storage.Log` for diagnostics and `system.WatchActiveTool` to feed
the "tool in use" into the settings service.

## Layout

```
main.go                       composition root: builds infrastructure → services → presentation, starts Wails
internal/
  buildinfo/                  name, version, release repo (shared by every layer)
  domain/                     pure rules — no Windows, files or network; fully unit tested
    usage.go                  Usage, Group, Bar, Stat, TokenWindow; expired windows count as 0 %
    selection.go              which limit the pill shows, taskbar providers, tool-in-use first, usage level
    pace.go                   elapsed share of a window, "at this pace … runs out in"
    notification.go           NotificationPolicy: thresholds, once per window, "available again"
    history.go                sampling rule (10 min / ≥ 1 point), 7-day series, charted limits
    schedule.go               adaptive refresh delay
    preferences.go            stored preferences + validation (width, display, max 2 providers)
    version.go                release version comparison, Release
  application/                use cases; the outside world only through ports.go
    ports.go                  UsageSource, PreferencesRepo, HistoryRepo, AlertRepo, Notifier,
                              ReleaseSource, Autostart, Presence
    settings.go               SettingsService (+ the Settings view the panel shows)
    usage.go                  UsageService: parallel fetch, single-flight, adaptive refresh loop
    notifications.go          NotificationService: applies the policy, holds alerts during fullscreen
    history.go                HistoryService
    updates.go                UpdateService: daily release check
    taskbar.go                PillRows: what the pill draws for the current data
  infrastructure/
    providers/                Claude, Codex, Antigravity (+ token parsing, caches)
    storage/                  %APPDATA%\Kota: config.json, history.json, notify-state.json, dock.log
    win32/                    Win32 + COM bindings, taskbar discovery
    system/                   autostart, installer, idle time, active tool, fullscreen, theme
    github/                   GitHub Releases client
  presentation/
    taskbar/                  native pill(s), tooltip, right-click menu, UI Automation, colours
    panel/                    Wails window handling + API (what the page calls: window.go.panel.API)
    toast/                    notification balloons from their own message-only window
cmd/probe/                    prints provider output as JSON (no UI)
frontend/dist/                index.html, styles.css, app.js, chart.js
```

## Decisions

- **Colours are presentation.** The domain classifies usage as ok / warn / bad (`domain.Level`);
  the pill and the page map levels to colours.
- **Ports are small interfaces in `application`**, implemented structurally: the providers satisfy
  `UsageSource` without importing `application`; `system.Autostart` / `system.Presence`,
  `storage.*Store`, `github.Releases` and `toast.Toaster` satisfy the others.
- **No global wiring.** Services are created in `main.go` and passed in; the notification balloon
  has its own hidden window, so it doesn't depend on the pill.
- **`win32` and `system` instead of one `windows` package**: the low-level bindings are shared by
  presentation and infrastructure, while `system` holds the OS services behind ports.
- **Tests sit with the code they test.** Domain rules and application services are tested with
  fakes; Windows-specific diagnostics are opt-in via environment variables
  (`ACTIVE_DIAG`, `KOTA_TOAST_DIAG`).
