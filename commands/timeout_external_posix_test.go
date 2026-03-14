//go:build !windows

package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunExternalTimeoutProcessReturns124OnTimeout(t *testing.T) {
	script := filepath.Join(t.TempDir(), "hang.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 5 &\nwait\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", script, err)
	}

	startedAt := time.Now()
	exitCode, controlMessage, err := runExternalTimeoutProcess(
		context.Background(),
		20*time.Millisecond,
		script,
		nil,
		map[string]string{"PATH": os.Getenv("PATH")},
		t.TempDir(),
		nil,
		&bytes.Buffer{},
		&bytes.Buffer{},
	)
	if err != nil {
		t.Fatalf("runExternalTimeoutProcess() error = %v", err)
	}
	if exitCode != 124 {
		t.Fatalf("exitCode = %d, want 124", exitCode)
	}
	if !strings.Contains(controlMessage, "execution timed out") {
		t.Fatalf("controlMessage = %q, want timeout marker", controlMessage)
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("elapsed = %s, want fast timeout handling", elapsed)
	}
}

func TestRunExternalTimeoutProcessPreservesExitStatus(t *testing.T) {
	exitCode, controlMessage, err := runExternalTimeoutProcess(
		context.Background(),
		time.Second,
		"/bin/sh",
		[]string{"-c", "exit 7"},
		map[string]string{"PATH": os.Getenv("PATH")},
		t.TempDir(),
		nil,
		&bytes.Buffer{},
		&bytes.Buffer{},
	)
	if err != nil {
		t.Fatalf("runExternalTimeoutProcess() error = %v", err)
	}
	if exitCode != 7 {
		t.Fatalf("exitCode = %d, want 7", exitCode)
	}
	if controlMessage != "" {
		t.Fatalf("controlMessage = %q, want empty", controlMessage)
	}
}
