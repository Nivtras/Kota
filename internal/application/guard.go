package application

import (
	"runtime/debug"
	"time"
)

// OnPanic receives panics the application recovers from, so one broken
// provider or listener can't take the whole app down. main.go points it at
// the crash log; nil ignores them.
var OnPanic func(where string, v any, stack []byte)

// guard runs f and turns a panic into a report (returning false).
func guard(where string, f func()) (ok bool) {
	defer func() {
		if v := recover(); v != nil {
			ok = false
			if OnPanic != nil {
				OnPanic(where, v, debug.Stack())
			}
		}
	}()
	f()
	return true
}

// Supervise runs a long-lived loop and restarts it (after a pause) if it
// panics, up to a few times, so a bug doesn't silently stop refreshing.
func Supervise(where string, loop func()) {
	for i := 0; i < 5; i++ {
		if guard(where, loop) {
			return // the loop ended normally
		}
		time.Sleep(time.Duration(i+1) * 5 * time.Second)
	}
}
