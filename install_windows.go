//go:build windows

package main

// Per-user install without admin rights or an installer tool: the exe
// copies itself to %LOCALAPPDATA%\Programs\AIUsageWidget, adds a Start menu
// shortcut and an entry in Settings → Apps (uninstall). Running a newer exe
// from a download updates the installed copy the same way.
//
//   AIUsageWidget.exe               asks once whether to install (or update)
//   AIUsageWidget.exe --uninstall   used by Settings → Apps
//   AIUsageWidget.exe --portable    never asks; runs from where it is

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	appVersion     = "1.1.0"
	appDisplayName = "AI Usage Widget"
	uninstallKey   = `Software\Microsoft\Windows\CurrentVersion\Uninstall\AIUsageWidget`
	exeFileName    = "AIUsageWidget.exe"
)

var (
	pMessageBoxW    = user32.NewProc("MessageBoxW")
	pCoUninitialize = ole32.NewProc("CoUninitialize")
)

const (
	mbYesNo       = 0x04
	mbYesNoCancel = 0x03
	mbIconQ       = 0x20
	mbIconInfo    = 0x40
	mbIconWarn    = 0x30
	idYes         = 6
	idNo          = 7
)

func messageBox(text string, flags uintptr) int {
	r, _, _ := pMessageBoxW.Call(0, uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16(appDisplayName))), flags)
	return int(r)
}

func installDir() string {
	d := os.Getenv("LOCALAPPDATA")
	if d == "" {
		h, _ := os.UserHomeDir()
		d = filepath.Join(h, "AppData", "Local")
	}
	return filepath.Join(d, "Programs", "AIUsageWidget")
}

func installedExe() string { return filepath.Join(installDir(), exeFileName) }

func shortcutPath() string {
	return filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`, appDisplayName+".lnk")
}

func samePath(a, b string) bool {
	ca, _ := filepath.Abs(a)
	cb, _ := filepath.Abs(b)
	return strings.EqualFold(filepath.Clean(ca), filepath.Clean(cb))
}

// handleInstall runs before the app starts. It returns true when this
// process should exit (it installed and relaunched, or uninstalled).
func handleInstall() bool {
	args := os.Args[1:]
	for _, a := range args {
		if a == "--uninstall" {
			uninstall()
			return true
		}
		if v, ok := strings.CutPrefix(a, "--wait-pid="); ok {
			if pid, err := strconv.Atoi(v); err == nil {
				waitForExit(uint32(pid), 15*time.Second)
			}
		}
	}
	self, err := os.Executable()
	if err != nil || samePath(self, installedExe()) {
		return false
	}
	for _, a := range args {
		if a == "--portable" {
			return false
		}
	}
	cfg := loadConfig()
	if samePath(cfg.PortablePath, self) {
		return false // the user chose to run this copy as is
	}

	installed := false
	if _, err := os.Stat(installedExe()); err == nil {
		installed = true
	}
	q := "Install AI Usage Widget for this user?\n\n" +
		"It is copied to " + installDir() + " and gets a Start menu entry " +
		"(uninstall from Settings → Apps). No admin rights needed.\n\n" +
		"Yes: install    No: run it from here"
	if installed {
		q = "AI Usage Widget is already installed.\n\nUpdate the installed copy to version " + appVersion + "?\n" +
			"A running copy will be restarted. Your settings are kept.\n\n" +
			"Yes: update    No: run this copy from here"
	}
	switch messageBox(q, mbYesNoCancel|mbIconQ) {
	case idYes:
	case idNo:
		cfg.PortablePath = self
		saveConfig(cfg)
		return false
	default:
		return true
	}
	if err := install(self); err != nil {
		messageBox("Install failed: "+err.Error(), mbIconWarn)
		return false // run from here instead
	}
	cmd := exec.Command(installedExe(), fmt.Sprintf("--wait-pid=%d", os.Getpid()))
	if err := cmd.Start(); err != nil {
		messageBox("Installed, but it could not be started: "+err.Error(), mbIconWarn)
	}
	return true
}

func install(self string) error {
	dir := installDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Any running copy holds the single-instance lock the new one needs.
	stopInstances("")
	if err := copyFile(self, installedExe()); err != nil {
		return err
	}
	if err := createShortcut(shortcutPath(), installedExe()); err != nil {
		dockLog("install: shortcut: %v", err)
	}
	if err := writeUninstallEntry(dir); err != nil {
		dockLog("install: uninstall entry: %v", err)
	}
	if autostartEnabled() { // point "Start with Windows" at the installed copy
		_ = setAutostartPath(installedExe())
	}
	dockLog("installed %s to %s", appVersion, dir)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// The old exe may still be locked for a moment after its process ended.
	for i := 0; ; i++ {
		err = os.Rename(tmp, dst)
		if err == nil || i == 20 {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

var (
	clsidShellLink  = windows.GUID{Data1: 0x00021401, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIShellLinkW  = windows.GUID{Data1: 0x000214f9, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIPersistFile = windows.GUID{Data1: 0x0000010b, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
)

// IShellLinkW / IPersistFile vtable slots.
const (
	vtQueryInterface  = 0
	vtSetDescription  = 7
	vtSetWorkingDir   = 9
	vtSetIconLocation = 17
	vtSetPath         = 20
	vtPersistFileSave = 6
	coinitApartment   = 0x2
	rpcEChangedMode   = 0x80010106
)

// createShortcut writes a .lnk with the shell's own IShellLink object.
func createShortcut(lnk, target string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if hr, _, _ := pCoInitializeEx.Call(0, coinitApartment); int32(hr) >= 0 {
		defer pCoUninitialize.Call()
	} else if uint32(hr) != rpcEChangedMode {
		return fmt.Errorf("CoInitializeEx: 0x%x", uint32(hr))
	}
	var link unsafe.Pointer
	if hr, _, _ := pCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIShellLinkW)), uintptr(unsafe.Pointer(&link))); int32(hr) < 0 || link == nil {
		return fmt.Errorf("ShellLink: 0x%x", uint32(hr))
	}
	defer comRelease(link)
	str := func(v string) uintptr { return uintptr(unsafe.Pointer(utf16(v))) }
	for _, c := range []struct {
		slot int
		args []uintptr
	}{
		{vtSetPath, []uintptr{str(target)}},
		{vtSetWorkingDir, []uintptr{str(filepath.Dir(target))}},
		{vtSetDescription, []uintptr{str("AI usage limits on the taskbar")}},
		{vtSetIconLocation, []uintptr{str(target), 0}},
	} {
		if hr := comCall(link, c.slot, c.args...); int32(hr) < 0 {
			return fmt.Errorf("IShellLink slot %d: 0x%x", c.slot, uint32(hr))
		}
	}
	var pf unsafe.Pointer
	if hr := comCall(link, vtQueryInterface, uintptr(unsafe.Pointer(&iidIPersistFile)), uintptr(unsafe.Pointer(&pf))); int32(hr) < 0 || pf == nil {
		return fmt.Errorf("IPersistFile: 0x%x", uint32(hr))
	}
	defer comRelease(pf)
	if hr := comCall(pf, vtPersistFileSave, str(lnk), 1); int32(hr) < 0 {
		return fmt.Errorf("save shortcut: 0x%x", uint32(hr))
	}
	if _, err := os.Stat(lnk); err != nil {
		return fmt.Errorf("shortcut not written: %v", err)
	}
	return nil
}

func writeUninstallEntry(dir string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	exe := filepath.Join(dir, exeFileName)
	size := uint32(0)
	if fi, err := os.Stat(exe); err == nil {
		size = uint32(fi.Size() / 1024)
	}
	for name, v := range map[string]string{
		"DisplayName":     appDisplayName,
		"DisplayVersion":  appVersion,
		"Publisher":       appDisplayName,
		"DisplayIcon":     exe,
		"InstallLocation": dir,
		"UninstallString": `"` + exe + `" --uninstall`,
	} {
		if err := k.SetStringValue(name, v); err != nil {
			return err
		}
	}
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
	_ = k.SetDWordValue("EstimatedSize", size)
	return nil
}

func uninstall() {
	if messageBox("Uninstall AI Usage Widget?", mbYesNo|mbIconQ) != idYes {
		return
	}
	self, _ := os.Executable()
	stopInstances(installedExe())
	_ = setAutostart(false)
	_ = os.Remove(shortcutPath())
	_ = registry.DeleteKey(registry.CURRENT_USER, uninstallKey)
	data := filepath.Dir(configPath())
	removeData := messageBox("Also delete settings and history ("+data+")?", mbYesNo|mbIconQ) == idYes
	// This exe can't delete itself while running: a hidden cmd removes the
	// folder a moment after we exit.
	dir := installDir()
	if samePath(filepath.Dir(self), dir) {
		cmdline := `/c ping 127.0.0.1 -n 3 >nul & rmdir /s /q "` + dir + `"`
		if removeData {
			cmdline += ` & rmdir /s /q "` + data + `"`
		}
		cmd := exec.Command("cmd.exe")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CmdLine: "cmd.exe " + cmdline, CreationFlags: 0x08000000}
		_ = cmd.Start()
	} else {
		_ = os.RemoveAll(dir)
		if removeData {
			_ = os.RemoveAll(data)
		}
	}
	messageBox("AI Usage Widget was uninstalled.", mbIconInfo)
}

// stopInstances ends running copies of exe ("" = any copy of the app),
// other than this process.
func stopInstances(exe string) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(snap)
	self := uint32(os.Getpid())
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if e.ProcessID == self || !strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), exeFileName) {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, e.ProcessID)
		if err != nil {
			continue
		}
		buf := make([]uint16, windows.MAX_PATH)
		n := uint32(len(buf))
		if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) == nil && (exe == "" || samePath(windows.UTF16ToString(buf[:n]), exe)) {
			_ = windows.TerminateProcess(h, 0)
			_, _ = windows.WaitForSingleObject(h, 5000)
		}
		windows.CloseHandle(h)
	}
}

func waitForExit(pid uint32, limit time.Duration) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return // already gone
	}
	defer windows.CloseHandle(h)
	_, _ = windows.WaitForSingleObject(h, uint32(limit/time.Millisecond))
}
