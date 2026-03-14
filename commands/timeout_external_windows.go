//go:build windows

package commands

import (
	"context"
	"errors"
	"time"
)

func runExternalCompatTimeout(_ context.Context, _ *Invocation, _ time.Duration, _ []string) (int, string, error) {
	return 0, "", errors.New("timeout: external compat timeout is unsupported on windows")
}
