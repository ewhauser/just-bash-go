package runtime

import (
	"strings"
	"testing"
)

func TestDefaultRuntimeDoesNotIncludeSQLite3(t *testing.T) {
	session := newSession(t, nil)

	result := mustExecSession(t, session, `sqlite3 :memory: "select 1;"`+"\n")
	if result.ExitCode != 127 {
		t.Fatalf("ExitCode = %d, want 127; stderr=%q", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stderr, "sqlite3: command not found") {
		t.Fatalf("Stderr = %q, want command-not-found error", result.Stderr)
	}
}
