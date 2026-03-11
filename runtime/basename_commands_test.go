package runtime

import (
	"context"
	"testing"
)

func TestBasenameSupportsSeparateLongSuffixArgument(t *testing.T) {
	rt := newRuntime(t, &Config{})

	result, err := rt.Run(context.Background(), &ExecutionRequest{
		Script: "basename --suffix .log /tmp/build.log\n",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0; stderr=%q", result.ExitCode, result.Stderr)
	}
	if got, want := result.Stdout, "build\n"; got != want {
		t.Fatalf("Stdout = %q, want %q", got, want)
	}
}

func TestBasenameGNUCompatibilityCases(t *testing.T) {
	rt := newRuntime(t, &Config{})

	result, err := rt.Run(context.Background(), &ExecutionRequest{
		Script: "basename fs fs\nbasename // /\nbasename -z a\nbasename --zero ba a\n",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0; stderr=%q", result.ExitCode, result.Stderr)
	}
	if got, want := result.Stdout, "fs\n/\na\x00b\x00"; got != want {
		t.Fatalf("Stdout = %q, want %q", got, want)
	}
}

func TestBasenameGNUOperandErrors(t *testing.T) {
	rt := newRuntime(t, &Config{})

	result, err := rt.Run(context.Background(), &ExecutionRequest{
		Script: "basename\nbasename a b c\n",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, want 1", result.ExitCode)
	}
	want := "basename: missing operand\nTry 'basename --help' for more information.\nbasename: extra operand 'c'\nTry 'basename --help' for more information.\n"
	if got := result.Stderr; got != want {
		t.Fatalf("Stderr = %q, want %q", got, want)
	}
}
