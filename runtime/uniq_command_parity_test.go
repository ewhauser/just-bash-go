package runtime

import (
	"context"
	"testing"
)

func TestUniqSupportsIgnoreCaseIsolated(t *testing.T) {
	rt := newRuntime(t, &Config{})

	result, err := rt.Run(context.Background(), &ExecutionRequest{
		Script: "printf 'Apple\\napple\\nBanana\\n' > /tmp/in.txt\nuniq --ignore-case -c /tmp/in.txt\nuniq -i /tmp/in.txt\n",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0; stderr=%q", result.ExitCode, result.Stderr)
	}
	if got, want := result.Stdout, "   2 Apple\n   1 Banana\nApple\nBanana\n"; got != want {
		t.Fatalf("Stdout = %q, want %q", got, want)
	}
}
