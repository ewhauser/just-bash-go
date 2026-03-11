package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCLIPrintsVersion(t *testing.T) {
	prevVersion, prevCommit, prevDate, prevBuiltBy := version, commit, date, builtBy
	version, commit, date, builtBy = "v1.2.3", "abc123", "2026-03-10T20:00:00Z", "test"
	t.Cleanup(func() {
		version, commit, date, builtBy = prevVersion, prevCommit, prevDate, prevBuiltBy
	})

	var stdout strings.Builder
	var stderr strings.Builder

	exitCode, err := runCLI(context.Background(), "jbgo", []string{"--version"}, strings.NewReader("echo ignored"), &stdout, &stderr, false)
	if err != nil {
		t.Fatalf("runCLI() error = %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}

	want := "jbgo v1.2.3\ncommit: abc123\nbuilt: 2026-03-10T20:00:00Z\nbuilt-by: test\n"
	if got := stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRunCLICompatExecPassesStdin(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	var stdout strings.Builder
	var stderr strings.Builder

	exitCode, err := runCLI(context.Background(), "jbgo", []string{"compat", "exec", "cat"}, strings.NewReader("stdin-data"), &stdout, &stderr, false)
	if err != nil {
		t.Fatalf("runCLI() error = %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0; stderr=%q", exitCode, stderr.String())
	}
	if got := stdout.String(); got != "stdin-data" {
		t.Fatalf("stdout = %q, want %q", got, "stdin-data")
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}
}

func TestRunCLICompatExecUnknownCommandReturns127(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	var stdout strings.Builder
	var stderr strings.Builder

	exitCode, err := runCLI(context.Background(), "jbgo", []string{"compat", "exec", "missing-command"}, strings.NewReader(""), &stdout, &stderr, false)
	if err != nil {
		t.Fatalf("runCLI() error = %v", err)
	}
	if exitCode != 127 {
		t.Fatalf("exitCode = %d, want 127", exitCode)
	}
	if !strings.Contains(stderr.String(), "missing-command: command not found") {
		t.Fatalf("stderr = %q, want command-not-found message", stderr.String())
	}
}

func TestRunCLIMulticallUsesArgv0CommandAndBypassesTTYRepl(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	commandDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(commandDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	commandPath := filepath.Join(commandDir, "pwd")
	if err := os.WriteFile(commandPath, []byte("# compat shim\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", commandPath, err)
	}

	var stdout strings.Builder
	var stderr strings.Builder

	exitCode, err := runCLI(context.Background(), commandPath, nil, strings.NewReader("ignored"), &stdout, &stderr, true)
	if err != nil {
		t.Fatalf("runCLI() error = %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0; stderr=%q", exitCode, stderr.String())
	}
	want := filepath.ToSlash(tmp) + "\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if strings.Contains(stdout.String(), "~$") {
		t.Fatalf("stdout = %q, did not expect interactive prompt", stdout.String())
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}
}
