//go:build windows

package system

// Per-user install without admin rights or an installer tool: the exe
// copies itself to %LOCALAPPDATA%\Programs\Kota, adds a Start menu
// shortcut and an entry in Settings → Apps (uninstall). Running a newer exe
// from a download updates the installed copy the same way.
//
//   Kota.exe               asks once whether to install (or update)
//   Kota.exe --uninstall   used by Settings → Apps
//   Kota.exe --portable    never asks; runs from where it is

import (
	"fmt"
	"io"
	"kota/internal/buildinfo"
	"kota/internal/i18n"
	"kota/internal/infrastructure/storage"
	"kota/internal/infrastructure/win32"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	appDisplayName = "Kota"
	uninstallKey   = `Software\Microsoft\Windows\CurrentVersion\Uninstall\Kota`
	exeFileName    = "Kota.exe"
	oldExeFileName = "AIUsageWidget.exe" // the app's name before it became Kota
)

var (
	pMessageBoxW = win32.User32.NewProc("MessageBoxW")
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
	r, _, _ := pMessageBoxW.Call(0, uintptr(unsafe.Pointer(win32.UTF16Ptr(text))), uintptr(unsafe.Pointer(win32.UTF16Ptr(appDisplayName))), flags)
	return int(r)
}

func installDir() string {
	d := os.Getenv("LOCALAPPDATA")
	if d == "" {
		h, _ := os.UserHomeDir()
		d = filepath.Join(h, "AppData", "Local")
	}
	return filepath.Join(d, "Programs", "Kota")
}

func installedExe() string { return filepath.Join(installDir(), exeFileName) }

func shortcutPath() string {
	return filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs`, appDisplayName+".lnk")
}

// samePath compares two paths, also when one uses 8.3 short names (MERTCA~1).
func samePath(a, b string) bool {
	return strings.EqualFold(longPath(a), longPath(b))
}

func longPath(p string) string {
	p, _ = filepath.Abs(p)
	p = filepath.Clean(p)
	src, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return p
	}
	buf := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetLongPathName(src, &buf[0], uint32(len(buf)))
		if err != nil || n == 0 {
			return p // doesn't exist (yet): compare as written
		}
		if int(n) <= len(buf) {
			return windows.UTF16ToString(buf[:n])
		}
		buf = make([]uint16, n)
	}
}

// handleInstall runs before the app starts. It returns true when this
// process should exit (it installed and relaunched, or uninstalled).
func HandleInstall() bool {
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
	prefs := storage.NewPreferencesStore(storage.Path("config.json"))
	cfg := prefs.Load()
	if samePath(cfg.PortablePath, self) {
		return false // the user chose to run this copy as is
	}

	installed := false
	if _, err := os.Stat(installedExe()); err == nil {
		installed = true
	}
	q := i18n.T("Install Kota for this user?\n\nIt is copied to %s and gets a Start menu entry (uninstall from Settings → Apps). No admin rights needed.\n\nYes: install    No: run it from here", installDir())
	if installed {
		q = i18n.T("Kota is already installed.\n\nUpdate the installed copy to version %s?\nA running copy will be restarted. Your settings are kept.\n\nYes: update    No: run this copy from here", buildinfo.Version)
	}
	switch messageBox(q, mbYesNoCancel|mbIconQ) {
	case idYes:
	case idNo:
		cfg.PortablePath = self
		prefs.Save(cfg)
		return false
	default:
		return true
	}
	if err := install(self); err != nil {
		messageBox(i18n.T("Install failed: %s", err.Error()), mbIconWarn)
		return false // run from here instead
	}
	cmd := exec.Command(installedExe(), fmt.Sprintf("--wait-pid=%d", os.Getpid()))
	if err := cmd.Start(); err != nil {
		messageBox(i18n.T("Installed, but it could not be started: %s", err.Error()), mbIconWarn)
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
	if err := createShortcut(shortcutPath(), installedExe(), ""); err != nil {
		storage.Log("install: shortcut: %v", err)
	}
	if err := writeUninstallEntry(dir); err != nil {
		storage.Log("install: uninstall entry: %v", err)
	}
	if AutostartEnabled() { // point "Start with Windows" at the installed copy
		_ = setAutostartPath(installedExe())
	}
	storage.Log("installed %s to %s", buildinfo.Version, dir)
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
		"DisplayVersion":  buildinfo.Version,
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
	if messageBox(i18n.T("Uninstall Kota?"), mbYesNo|mbIconQ) != idYes {
		return
	}
	self, _ := os.Executable()
	stopInstances(installedExe())
	_ = SetAutostart(false)
	_ = os.Remove(shortcutPath())
	_ = registry.DeleteKey(registry.CURRENT_USER, uninstallKey)
	data := storage.DataDir()
	removeData := messageBox(i18n.T("Also delete settings and history (%s)?", data), mbYesNo|mbIconQ) == idYes
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
	messageBox(i18n.T("Kota was uninstalled."), mbIconInfo)
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
		name := windows.UTF16ToString(e.ExeFile[:])
		if e.ProcessID == self || !(strings.EqualFold(name, exeFileName) || strings.EqualFold(name, oldExeFileName)) {
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

// migrateFromOldName carries settings, caches and history over from the
// app's old name (AI Usage Widget) and moves its autostart entry.
func MigrateOldName() {
	storage.MigrateOldFolder()
	k, err := registry.OpenKey(registry.CURRENT_USER, legacyRunKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	if _, _, err := k.GetStringValue("AIUsageWidget"); err == nil {
		_ = k.DeleteValue("AIUsageWidget")
		_ = SetAutostart(true) // now points at this exe, under the new name
	}
}
