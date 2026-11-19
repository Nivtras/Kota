//go:build windows

package providers

import (
	"context"
	"os/exec"
	"syscall"
)

// runHidden runs a command without flashing a console window.
func runHidden(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd.Output()
}
