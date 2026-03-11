package runtime

import (
	"context"
	"testing"
)

func TestBase64SupportsSeparateLongWrapArgument(t *testing.T) {
	rt := newRuntime(t, &Config{})

	result, err := rt.Run(context.Background(), &ExecutionRequest{
		Script: "printf 'hello world' | base64 --wrap 0\n",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0; stderr=%q", result.ExitCode, result.Stderr)
	}
	if got, want := result.Stdout, "aGVsbG8gd29ybGQ=\n"; got != want {
		t.Fatalf("Stdout = %q, want %q", got, want)
	}
}
