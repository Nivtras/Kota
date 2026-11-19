//go:build !windows

package providers

import (
	"context"
	"os/exec"
)

func runHidden(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}
