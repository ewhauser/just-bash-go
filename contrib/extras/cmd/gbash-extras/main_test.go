package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rootcli "github.com/ewhauser/gbash/cli"
)

func runCLI(ctx context.Context, argv0 string, args []string, stdin io.Reader, stdout, stderr io.Writer, stdinTTY bool) (int, error) {
	cfg := newCLIConfig()
	cfg.TTYDetector = func(io.Reader) bool { return stdinTTY }
	return rootcli.Run(ctx, cfg, argv0, args, stdin, stdout, stderr)
}

func TestCLIHelpAndVersionIdentifyBinary(t *testing.T) {
	prevVersion, prevCommit, prevDate, prevBuiltBy := version, commit, date, builtBy
	version, commit, date, builtBy = "v1.2.3", "abc123", "2026-03-10T20:00:00Z", "test"
	t.Cleanup(func() {
		version, commit, date, builtBy = prevVersion, prevCommit, prevDate, prevBuiltBy
	})

	var stdout strings.Builder
	var stderr strings.Builder

	exitCode, err := runCLI(context.Background(), "gbash-extras", []string{"--help"}, strings.NewReader(""), &stdout, &stderr, false)
	if err != nil {
		t.Fatalf("runCLI(--help) error = %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}
	if got := stdout.String(); !strings.Contains(got, "Usage: gbash-extras") {
		t.Fatalf("stdout = %q, want gbash-extras usage", got)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode, err = runCLI(context.Background(), "gbash-extras", []string{"--version"}, strings.NewReader(""), &stdout, &stderr, false)
	if err != nil {
		t.Fatalf("runCLI(--version) error = %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}
	want := "gbash-extras v1.2.3\ncommit: abc123\nbuilt: 2026-03-10T20:00:00Z\nbuilt-by: test\n"
	if got := stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}
}

func TestCLIRegistersStableExtras(t *testing.T) {
	var stdout strings.Builder
	var stderr strings.Builder

	exitCode, err := runCLI(context.Background(), "gbash-extras", []string{"-c", "printf 'a,b\\n' | awk -F, '{print $2}'\n" +
		"printf '<h1>docs</h1>' | html-to-markdown\n" +
		"printf '{\"name\":\"alice\"}\\n' | jq -r '.name'\n" +
		"printf 'name: alice\\n' | yq '.name'\n" +
		`sqlite3 :memory: "select 1;"`}, strings.NewReader(""), &stdout, &stderr, false)
	if err != nil {
		t.Fatalf("runCLI() error = %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if got, want := stdout.String(), "b\n# docs\nalice\nalice\n1\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}
}

func TestCLIDoesNotRegisterNodeJSByDefault(t *testing.T) {
	var stdout strings.Builder
	var stderr strings.Builder

	exitCode, err := runCLI(context.Background(), "gbash-extras", []string{"-c", "nodejs -e 'console.log(1)'"}, strings.NewReader(""), &stdout, &stderr, false)
	if err != nil {
		t.Fatalf("runCLI() error = %v", err)
	}
	if exitCode != 127 {
		t.Fatalf("exitCode = %d, want 127", exitCode)
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}
	if got := stderr.String(); !strings.Contains(got, "nodejs: command not found") {
		t.Fatalf("stderr = %q, want nodejs command-not-found", got)
	}
}

func TestCLIServerServesStableExtrasRegistry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	socket := filepath.Join(os.TempDir(), fmt.Sprintf("gbash-extras-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(socket) })
	var stdout strings.Builder
	var stderr strings.Builder
	errCh := make(chan error, 1)
	go func() {
		_, err := runCLI(ctx, "gbash-extras", []string{"--server", "--socket", socket}, strings.NewReader(""), &stdout, &stderr, false)
		errCh <- err
	}()

	var conn net.Conn
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			cancel()
			t.Fatalf("server exited before socket became ready: %v", err)
		default:
		}
		dialed, err := net.DialTimeout("unix", socket, 50*time.Millisecond)
		if err == nil {
			conn = dialed
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if conn == nil {
		t.Fatal("timed out waiting for gbash-extras server socket")
	}
	defer func() { _ = conn.Close() }()

	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)
	send := func(id, method, sessionID, streamID string, payload any) {
		t.Helper()
		if err := enc.Encode(map[string]any{
			"version":    "1",
			"type":       "request",
			"id":         id,
			"method":     method,
			"session_id": sessionID,
			"stream_id":  streamID,
			"payload":    payload,
		}); err != nil {
			t.Fatalf("Encode(%s) error = %v", method, err)
		}
	}
	readUntilResponse := func(id string) map[string]any {
		t.Helper()
		for {
			if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatalf("SetReadDeadline() error = %v", err)
			}
			var msg map[string]any
			if err := dec.Decode(&msg); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if got, _ := msg["type"].(string); got == "response" && msg["id"] == id {
				return msg
			}
		}
	}

	send("1", "session.create", "", "", nil)
	createResp := readUntilResponse("1")
	if ok, _ := createResp["ok"].(bool); !ok {
		t.Fatalf("session.create response = %#v, want ok", createResp)
	}
	sessionID, ok := createResp["session_id"].(string)
	if !ok || sessionID == "" {
		t.Fatalf("session.create response = %#v, want session_id", createResp)
	}

	send("2", "exec.start", sessionID, "", map[string]any{
		"script": "printf 'a,b\\n' | awk -F, '{print $2}'\n",
	})
	execResp := readUntilResponse("2")
	if ok, _ := execResp["ok"].(bool); !ok {
		t.Fatalf("exec.start response = %#v, want ok", execResp)
	}
	streamID, ok := execResp["stream_id"].(string)
	if !ok || streamID == "" {
		t.Fatalf("exec.start response = %#v, want stream_id", execResp)
	}

	var gotStdout strings.Builder
	for {
		if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatalf("SetReadDeadline() error = %v", err)
		}
		var msg struct {
			Type     string `json:"type"`
			Event    string `json:"event"`
			StreamID string `json:"stream_id"`
			Channel  string `json:"channel"`
			Payload  struct {
				Data string `json:"data"`
			} `json:"payload"`
		}
		if err := dec.Decode(&msg); err != nil {
			t.Fatalf("Decode(stream event) error = %v", err)
		}
		if msg.Type != "event" || msg.StreamID != streamID {
			continue
		}
		switch msg.Event {
		case "stream.data":
			if msg.Channel != "stdout" {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(msg.Payload.Data)
			if err != nil {
				t.Fatalf("DecodeString(stdout) error = %v", err)
			}
			gotStdout.Write(data)
		case "stream.exit":
			if got, want := gotStdout.String(), "b\n"; got != want {
				t.Fatalf("stdout = %q, want %q", got, want)
			}
			cancel()
			select {
			case err := <-errCh:
				if err != nil {
					t.Fatalf("server exited with error: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for gbash-extras server shutdown")
			}
			if got := stdout.String(); got != "" {
				t.Fatalf("server stdout = %q, want empty", got)
			}
			if got := stderr.String(); got != "" {
				t.Fatalf("server stderr = %q, want empty", got)
			}
			return
		}
	}
}
