//go:build windows

// Package hotkey is the global keyboard shortcut that opens the panel
// (RegisterHotKey on its own thread).
package hotkey

import (
	"errors"
	"runtime"
	"strings"
	"unsafe"

	"kota/internal/infrastructure/win32"
)

var (
	pRegisterHotKey     = win32.User32.NewProc("RegisterHotKey")
	pUnregisterHotKey   = win32.User32.NewProc("UnregisterHotKey")
	pPostThreadMessageW = win32.User32.NewProc("PostThreadMessageW")
	pGetCurrentThreadId = win32.Kernel32.NewProc("GetCurrentThreadId")
)

const (
	modAlt      = 0x0001
	modControl  = 0x0002
	modShift    = 0x0004
	modWin      = 0x0008
	modNoRepeat = 0x4000
	wmHotkey    = 0x0312
	msgChange   = win32.WmApp + 30
	hotkeyID    = 1
)

// Default is the shortcut used when none is chosen. (Win+Alt+K is taken by
// Windows: it mutes the microphone in calls.)
const Default = "Win+Shift+K"

// Off disables the shortcut.
const Off = "off"

// ErrInUse: another app already owns the combination.
var ErrInUse = errors.New("this shortcut is already used by another app")

// Parse turns "Win+Shift+K" into RegisterHotKey's modifiers and key.
func Parse(spec string) (mods, vk uint32, err error) {
	parts := strings.Split(spec, "+")
	if len(parts) < 2 {
		return 0, 0, errors.New("a shortcut needs a modifier and a key")
	}
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "win":
			mods |= modWin
		case "ctrl", "control":
			mods |= modControl
		case "alt":
			mods |= modAlt
		case "shift":
			mods |= modShift
		default:
			return 0, 0, errors.New("unknown modifier " + p)
		}
	}
	key := strings.ToUpper(strings.TrimSpace(parts[len(parts)-1]))
	switch {
	case len(key) == 1 && (key[0] >= 'A' && key[0] <= 'Z' || key[0] >= '0' && key[0] <= '9'):
		vk = uint32(key[0])
	case key == "SPACE":
		vk = 0x20
	default:
		return 0, 0, errors.New("unsupported key " + key)
	}
	return mods, vk, nil
}

// Hotkey owns the registration.
type Hotkey struct {
	onPress func()
	tid     uintptr
	req     chan string
	resp    chan error
	ready   chan struct{}
}

// Start begins listening; onPress runs on its own goroutine.
func Start(onPress func()) *Hotkey {
	h := &Hotkey{onPress: onPress, req: make(chan string), resp: make(chan error), ready: make(chan struct{})}
	go h.loop()
	<-h.ready
	return h
}

// Set registers spec ("Win+Shift+K", Off, or "" for Default).
func (h *Hotkey) Set(spec string) error {
	if h.tid == 0 {
		return errors.New("shortcut thread not running")
	}
	go func() { pPostThreadMessageW.Call(h.tid, msgChange, 0, 0) }()
	h.req <- spec
	return <-h.resp
}

func (h *Hotkey) loop() {
	runtime.LockOSThread()
	h.tid, _, _ = pGetCurrentThreadId.Call()
	// Make sure the thread has a message queue before anyone posts to it.
	var m win32.Msg
	win32.PeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, 0)
	close(h.ready)
	registered := false
	for {
		if r, _, _ := win32.GetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0); int32(r) <= 0 {
			return
		}
		switch m.Message {
		case wmHotkey:
			if h.onPress != nil {
				go h.onPress()
			}
		case msgChange:
			spec := <-h.req
			if registered {
				pUnregisterHotKey.Call(0, hotkeyID)
				registered = false
			}
			if spec == "" {
				spec = Default
			}
			if strings.EqualFold(spec, Off) {
				h.resp <- nil
				continue
			}
			mods, vk, err := Parse(spec)
			if err != nil {
				h.resp <- err
				continue
			}
			if ok, _, _ := pRegisterHotKey.Call(0, hotkeyID, uintptr(mods|modNoRepeat), uintptr(vk)); ok == 0 {
				h.resp <- ErrInUse
				continue
			}
			registered = true
			h.resp <- nil
		}
	}
}
