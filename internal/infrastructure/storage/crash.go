package storage

// Crash reports: %APPDATA%\Kota\crash.log gets the Go runtime's traceback
// when Kota crashes, and the stack of any panic Kota recovers from (a
// provider or a listener that failed while the app kept running).

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
)

const crashFile = "crash.log"

var crashMu sync.Mutex

func openCrashLog() (*os.File, error) {
	p := Path(crashFile)
	if fi, err := os.Stat(p); err == nil && fi.Size() > 256<<10 {
		_ = os.Rename(p, p+".old") // keep one older file
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	return os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

// StartCrashLog makes the runtime write fatal errors (unrecovered panics,
// concurrent map writes, …) to crash.log. Call once at start.
func StartCrashLog(version string) {
	f, err := openCrashLog()
	if err != nil {
		return
	}
	if debug.SetCrashOutput(f, debug.CrashOptions{}) != nil {
		f.Close()
		return
	}
	// Mark the session so a traceback can be matched to a run and version.
	fmt.Fprintf(f, "=== Kota %s started %s (pid %d)\n", version, time.Now().Format(time.RFC3339), os.Getpid())
	f.Close() // SetCrashOutput keeps its own duplicate of the file
}

// LogPanic records a recovered panic with its stack.
func LogPanic(where string, v any, stack []byte) {
	crashMu.Lock()
	defer crashMu.Unlock()
	f, err := openCrashLog()
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "--- recovered panic in %s at %s: %v\n%s\n", where, time.Now().Format(time.RFC3339), v, stack)
}
