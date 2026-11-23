//go:build windows

package system

// Follow mode: find which AI tool the user is working in. The foreground
// app itself (Claude, Codex, Antigravity), or — for terminals and editors —
// a CLI running inside it (claude.exe, codex.exe, agy.exe).

import (
	"kota/internal/infrastructure/win32"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var pQueryFullProcessImageNameW = win32.Kernel32.NewProc("QueryFullProcessImageNameW")

// exe name (lower case) → provider ID
var toolExes = map[string]string{
	"claude.exe":      "claude",
	"codex.exe":       "codex",
	"antigravity.exe": "antigravity",
	"agy.exe":         "antigravity",
}

// Hosts whose child processes are searched for a CLI.
var cliHosts = map[string]bool{
	"windowsterminal.exe": true, "openconsole.exe": true, "conhost.exe": true,
	"cmd.exe": true, "powershell.exe": true, "pwsh.exe": true, "wsl.exe": true,
	"code.exe": true, "cursor.exe": true, "windsurf.exe": true, "wezterm-gui.exe": true,
	"alacritty.exe": true, "tabby.exe": true, "hyper.exe": true, "mintty.exe": true,
}

func exeName(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH)
	n := uint32(len(buf))
	if ok, _, _ := pQueryFullProcessImageNameW.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); ok == 0 {
		return ""
	}
	return strings.ToLower(filepath.Base(windows.UTF16ToString(buf[:n])))
}

// cliUnder looks for a known CLI among the descendants of root.
func cliUnder(root uint32, prefer string) string {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(snap)
	children := map[uint32][]uint32{}
	names := map[uint32]string{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		children[e.ParentProcessID] = append(children[e.ParentProcessID], e.ProcessID)
		names[e.ProcessID] = strings.ToLower(windows.UTF16ToString(e.ExeFile[:]))
	}
	found := map[string]bool{}
	var walk func(pid uint32, depth int)
	walk = func(pid uint32, depth int) {
		if depth > 6 {
			return
		}
		for _, c := range children[pid] {
			if id, ok := toolExes[names[c]]; ok {
				found[id] = true
			}
			walk(c, depth+1)
		}
	}
	walk(root, 0)
	if found[prefer] {
		return prefer // several CLIs open: keep the current one
	}
	for _, id := range []string{"claude", "codex", "antigravity"} {
		if found[id] {
			return id
		}
	}
	return ""
}

// watchActiveTool reports the provider of the app in use whenever it
// changes. Unrelated apps (a browser, …) keep the last detected tool.
func WatchActiveTool(enabled func() bool, onChange func(id string)) {
	var lastPid uint32
	current := ""
	lastScan := time.Time{}
	for range time.Tick(time.Second) {
		if !enabled() {
			continue
		}
		fg, _, _ := win32.GetForegroundWindow.Call()
		var pid uint32
		win32.GetWindowThreadProcessId.Call(fg, uintptr(unsafe.Pointer(&pid)))
		if pid == 0 {
			continue
		}
		name := exeName(pid)
		id, isTool := toolExes[name]
		switch {
		case isTool:
		case cliHosts[name]:
			// A CLI may start after the terminal got focus: rescan every 3 s.
			if pid == lastPid && time.Since(lastScan) < 3*time.Second {
				continue
			}
			lastScan = time.Now()
			id = cliUnder(pid, current)
		default:
			id = ""
		}
		lastPid = pid
		if id != "" && id != current {
			current = id
			onChange(id)
		}
	}
}
