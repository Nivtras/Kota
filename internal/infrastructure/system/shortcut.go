//go:build windows

package system

// .lnk files through the shell's own IShellLink object: the Start menu
// entry (install) and the Startup folder entry (start with Windows).

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"

	"kota/internal/infrastructure/win32"
)

var (
	clsidShellLink  = windows.GUID{Data1: 0x00021401, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIShellLinkW  = windows.GUID{Data1: 0x000214f9, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIPersistFile = windows.GUID{Data1: 0x0000010b, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
)

// IShellLinkW / IPersistFile vtable slots.
const (
	vtQueryInterface  = 0
	vtGetPath         = 3
	vtSetDescription  = 7
	vtSetWorkingDir   = 9
	vtSetArguments    = 11
	vtSetIconLocation = 17
	vtSetPath         = 20
	vtPersistFileLoad = 5
	vtPersistFileSave = 6
	coinitApartment   = 0x2
	rpcEChangedMode   = 0x80010106
)

// withShellLink gives f a fresh IShellLinkW and its IPersistFile.
func withShellLink(f func(link, pf unsafe.Pointer) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if hr, _, _ := win32.CoInitializeEx.Call(0, coinitApartment); int32(hr) >= 0 {
		defer win32.CoUninitialize.Call()
	} else if uint32(hr) != rpcEChangedMode {
		return fmt.Errorf("CoInitializeEx: 0x%x", uint32(hr))
	}
	var link unsafe.Pointer
	if hr, _, _ := win32.CoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, win32.ClsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIShellLinkW)), uintptr(unsafe.Pointer(&link))); int32(hr) < 0 || link == nil {
		return fmt.Errorf("ShellLink: 0x%x", uint32(hr))
	}
	defer win32.ComRelease(link)
	var pf unsafe.Pointer
	if hr := win32.ComCall(link, vtQueryInterface, uintptr(unsafe.Pointer(&iidIPersistFile)), uintptr(unsafe.Pointer(&pf))); int32(hr) < 0 || pf == nil {
		return fmt.Errorf("IPersistFile: 0x%x", uint32(hr))
	}
	defer win32.ComRelease(pf)
	return f(link, pf)
}

func wstr(v string) uintptr { return uintptr(unsafe.Pointer(win32.UTF16Ptr(v))) }

// createShortcut writes lnk pointing at target with the given arguments.
func createShortcut(lnk, target, args string) error {
	if err := os.MkdirAll(filepath.Dir(lnk), 0o755); err != nil {
		return err
	}
	err := withShellLink(func(link, pf unsafe.Pointer) error {
		for _, c := range []struct {
			slot int
			args []uintptr
		}{
			{vtSetPath, []uintptr{wstr(target)}},
			{vtSetArguments, []uintptr{wstr(args)}},
			{vtSetWorkingDir, []uintptr{wstr(filepath.Dir(target))}},
			{vtSetDescription, []uintptr{wstr("AI usage limits on the taskbar")}},
			{vtSetIconLocation, []uintptr{wstr(target), 0}},
		} {
			if hr := win32.ComCall(link, c.slot, c.args...); int32(hr) < 0 {
				return fmt.Errorf("IShellLink slot %d: 0x%x", c.slot, uint32(hr))
			}
		}
		if hr := win32.ComCall(pf, vtPersistFileSave, wstr(lnk), 1); int32(hr) < 0 {
			return fmt.Errorf("save shortcut: 0x%x", uint32(hr))
		}
		return nil
	})
	if err != nil {
		return err
	}
	if _, err := os.Stat(lnk); err != nil {
		return fmt.Errorf("shortcut not written: %v", err)
	}
	return nil
}

// shortcutTarget is the exe a .lnk points at ("" if it can't be read).
func shortcutTarget(lnk string) string {
	var out string
	_ = withShellLink(func(link, pf unsafe.Pointer) error {
		if hr := win32.ComCall(pf, vtPersistFileLoad, wstr(lnk), 0); int32(hr) < 0 {
			return fmt.Errorf("load: 0x%x", uint32(hr))
		}
		buf := make([]uint16, windows.MAX_PATH+1)
		var fd windows.Win32finddata
		const slgpRawPath = 0x4
		if hr := win32.ComCall(link, vtGetPath, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
			uintptr(unsafe.Pointer(&fd)), slgpRawPath); int32(hr) < 0 {
			return fmt.Errorf("GetPath: 0x%x", uint32(hr))
		}
		out = windows.UTF16ToString(buf)
		return nil
	})
	return out
}
