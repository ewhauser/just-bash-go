package server_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ewhauser/gbash"
	gbserver "github.com/ewhauser/gbash/server"
)

const testChunkBytes = 16 << 10

type wireMessage struct {
	Version   string          `json:"version"`
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Method    string          `json:"method"`
	Event     string          `json:"event"`
	SessionID string          `json:"session_id"`
	StreamID  string          `json:"stream_id"`
	Channel   string          `json:"channel"`
	Seq       uint64          `json:"seq"`
	OK        *bool           `json:"ok"`
	Payload   json.RawMessage `json:"payload"`
	Error     *wireError      `json:"error"`
}

type wireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type helloPayload struct {
	ServerName    string `json:"server_name"`
	ServerVersion string `json:"server_version"`
	Capabilities  struct {
		Binary           string `json:"binary"`
		Transport        string `json:"transport"`
		Reconnect        bool   `json:"reconnect"`
		FileSystemRPC    bool   `json:"filesystem_rpc"`
		PTY              bool   `json:"pty"`
		InteractiveShell bool   `json:"interactive_shell"`
	} `json:"capabilities"`
}

type sessionSummaryPayload struct {
	Session struct {
		SessionID      string `json:"session_id"`
		State          string `json:"state"`
		ActiveStreamID string `json:"active_stream_id"`
	} `json:"session"`
	Streams []struct {
		StreamID string `json:"stream_id"`
		Kind     string `json:"kind"`
		State    string `json:"state"`
	} `json:"streams"`
}

type sessionListPayload struct {
	Sessions []struct {
		SessionID string `json:"session_id"`
		State     string `json:"state"`
	} `json:"sessions"`
}

type streamStartPayload struct {
	SessionID      string `json:"session_id"`
	StreamID       string `json:"stream_id"`
	Kind           string `json:"kind"`
	State          string `json:"state"`
	WriterAttached bool   `json:"writer_attached"`
}

type streamExitPayload struct {
	Kind          string            `json:"kind"`
	ExitCode      int               `json:"exit_code"`
	ShellExited   bool              `json:"shell_exited"`
	FinalEnv      map[string]string `json:"final_env"`
	ControlStderr string            `json:"control_stderr"`
}

type streamDataPayload struct {
	Data     string `json:"data"`
	Encoding string `json:"encoding"`
}

type serverHandle struct {
	socket string
	cancel context.CancelFunc
	errCh  chan error
}

type testClient struct {
	t      *testing.T
	conn   net.Conn
	enc    *json.Encoder
	dec    *json.Decoder
	queue  []wireMessage
	nextID atomic.Uint64
}

func TestServerSessionLifecycleAndExecStreaming(t *testing.T) {
	srv := startServer(t, gbserver.Config{
		Name:        "gbash",
		Version:     "test",
		SessionTTL:  time.Second,
		ReplayBytes: 1 << 20,
	})
	client := dialClient(t, srv.socket)
	defer client.Close()

	hello := client.call("hello", "", "", map[string]any{"client_name": "test"})
	if hello.OK == nil || !*hello.OK {
		t.Fatalf("hello response = %#v, want ok", hello)
	}
	var helloPayload helloPayload
	decodePayload(t, &hello, &helloPayload)
	if helloPayload.Capabilities.Transport != "unix" || !helloPayload.Capabilities.Reconnect || helloPayload.Capabilities.FileSystemRPC || helloPayload.Capabilities.PTY {
		t.Fatalf("hello capabilities = %+v, want unix reconnect without fs/pty", helloPayload.Capabilities)
	}
	if !helloPayload.Capabilities.InteractiveShell || helloPayload.Capabilities.Binary != "gbash" {
		t.Fatalf("hello capabilities = %+v, want interactive gbash", helloPayload.Capabilities)
	}

	create := client.call("session.create", "", "", nil)
	if create.OK == nil || !*create.OK {
		t.Fatalf("session.create response = %#v, want ok", create)
	}
	var created sessionSummaryPayload
	decodePayload(t, &create, &created)
	sessionID := created.Session.SessionID
	if sessionID == "" {
		t.Fatalf("session.create payload = %+v, want session id", created)
	}
	sessionCreated := client.waitFor(2*time.Second, func(msg wireMessage) bool {
		return msg.Type == "event" && msg.Event == "session.created" && msg.SessionID == sessionID
	})
	if sessionCreated.Event != "session.created" {
		t.Fatalf("session created event = %#v", sessionCreated)
	}

	getResp := client.call("session.get", sessionID, "", nil)
	if getResp.OK == nil || !*getResp.OK {
		t.Fatalf("session.get response = %#v, want ok", getResp)
	}

	listResp := client.call("session.list", "", "", nil)
	var listed sessionListPayload
	decodePayload(t, &listResp, &listed)
	if len(listed.Sessions) != 1 || listed.Sessions[0].SessionID != sessionID {
		t.Fatalf("session.list payload = %+v, want only %s", listed, sessionID)
	}

	largeOutput := strings.Repeat("x", testChunkBytes+4096)
	script := fmt.Sprintf("printf '%%s' '%s'\nprintf 'warn\\n' >&2\nexport MODE=debug\n", largeOutput)
	execStart := client.call("exec.start", sessionID, "", map[string]any{
		"script": script,
		"stdin":  "closed",
	})
	if execStart.OK == nil || !*execStart.OK {
		t.Fatalf("exec.start response = %#v, want ok", execStart)
	}
	var execPayload streamStartPayload
	decodePayload(t, &execStart, &execPayload)
	if execPayload.StreamID == "" || execPayload.Kind != "exec" {
		t.Fatalf("exec.start payload = %+v, want exec stream", execPayload)
	}

	var stdout strings.Builder
	var stderr strings.Builder
	stdoutEvents := 0
	for {
		msg := client.waitFor(5*time.Second, func(msg wireMessage) bool {
			if msg.StreamID != execPayload.StreamID {
				return false
			}
			return msg.Event == "stream.data" || msg.Event == "stream.exit"
		})
		switch msg.Event {
		case "stream.data":
			var payload streamDataPayload
			decodePayload(t, &msg, &payload)
			data, err := base64.StdEncoding.DecodeString(payload.Data)
			if err != nil {
				t.Fatalf("decode stream data: %v", err)
			}
			switch msg.Channel {
			case "stdout":
				stdoutEvents++
				stdout.Write(data)
			case "stderr":
				stderr.Write(data)
			}
		case "stream.exit":
			var exit streamExitPayload
			decodePayload(t, &msg, &exit)
			if exit.ExitCode != 0 || exit.FinalEnv["MODE"] != "debug" {
				t.Fatalf("stream.exit payload = %+v, want exit 0 and MODE=debug", exit)
			}
			if got, want := stdout.String(), largeOutput; got != want {
				t.Fatalf("stdout = %q, want %q", got, want)
			}
			if got, want := stderr.String(), "warn\n"; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
			if stdoutEvents < 2 {
				t.Fatalf("stdout events = %d, want stream split across multiple events", stdoutEvents)
			}
			goto destroy
		}
	}

destroy:
	destroyResp := client.call("session.destroy", sessionID, "", nil)
	if destroyResp.OK == nil || !*destroyResp.OK {
		t.Fatalf("session.destroy response = %#v, want ok", destroyResp)
	}
	listResp = client.call("session.list", "", "", nil)
	decodePayload(t, &listResp, &listed)
	if len(listed.Sessions) != 0 {
		t.Fatalf("session.list payload = %+v, want empty after destroy", listed)
	}
}

func TestServerShellConcurrentSessionsAndBusy(t *testing.T) {
	srv := startServer(t, gbserver.Config{
		Name:        "gbash",
		Version:     "test",
		SessionTTL:  time.Second,
		ReplayBytes: 1 << 20,
	})
	client := dialClient(t, srv.socket)
	defer client.Close()

	session1 := mustCreateSession(t, client)
	session2 := mustCreateSession(t, client)

	shell1 := client.call("shell.start", session1, "", nil)
	shell2 := client.call("shell.start", session2, "", nil)
	var shell1Payload streamStartPayload
	var shell2Payload streamStartPayload
	decodePayload(t, &shell1, &shell1Payload)
	decodePayload(t, &shell2, &shell2Payload)

	waitForStreamOutput(t, client, shell1Payload.StreamID, "~$ ")
	waitForStreamOutput(t, client, shell2Payload.StreamID, "~$ ")

	busy := client.call("exec.start", session1, "", map[string]any{"script": "echo no\n"})
	if busy.OK == nil || *busy.OK {
		t.Fatalf("busy response = %#v, want error", busy)
	}
	if busy.Error == nil || busy.Error.Code != "SESSION_BUSY" {
		t.Fatalf("busy error = %#v, want SESSION_BUSY", busy.Error)
	}

	writeResp := client.call("stream.write", session1, shell1Payload.StreamID, map[string]any{
		"encoding": "base64",
		"data":     base64.StdEncoding.EncodeToString([]byte("cd /tmp\nexport FOO=bar\npwd\necho $FOO\nexit\n")),
	})
	mustOK(t, &writeResp)
	writeResp = client.call("stream.write", session2, shell2Payload.StreamID, map[string]any{
		"encoding": "base64",
		"data":     base64.StdEncoding.EncodeToString([]byte("echo two\nexit\n")),
	})
	mustOK(t, &writeResp)

	output1, exit1 := collectStreamUntilExit(t, client, shell1Payload.StreamID)
	output2, exit2 := collectStreamUntilExit(t, client, shell2Payload.StreamID)
	if exit1.ExitCode != 0 || exit2.ExitCode != 0 {
		t.Fatalf("shell exits = %+v %+v, want 0", exit1, exit2)
	}
	if !strings.Contains(output1, "/tmp\n") || !strings.Contains(output1, "bar\n") {
		t.Fatalf("shell1 output = %q, want persisted cwd and env", output1)
	}
	if !strings.Contains(output2, "two\n") {
		t.Fatalf("shell2 output = %q, want echoed data", output2)
	}
}

func TestServerAttachReplayGapAndTTL(t *testing.T) {
	srv := startServer(t, gbserver.Config{
		Name:        "gbash",
		Version:     "test",
		SessionTTL:  250 * time.Millisecond,
		ReplayBytes: 4000,
	})
	client1 := dialClient(t, srv.socket)
	sessionID := mustCreateSession(t, client1)

	shellStart := client1.call("shell.start", sessionID, "", nil)
	var shellPayload streamStartPayload
	decodePayload(t, &shellStart, &shellPayload)
	waitForStreamOutput(t, client1, shellPayload.StreamID, "~$ ")
	client1.Close()

	client2 := dialClient(t, srv.socket)
	defer client2.Close()
	attach := client2.call("stream.attach", sessionID, "", map[string]any{
		"stream_id":  shellPayload.StreamID,
		"stdout_seq": 0,
		"stderr_seq": 0,
		"write":      true,
	})
	var attachPayload streamStartPayload
	decodePayload(t, &attach, &attachPayload)
	if !attachPayload.WriterAttached {
		t.Fatalf("attach payload = %+v, want writer ownership", attachPayload)
	}
	replayed := waitForStreamOutput(t, client2, shellPayload.StreamID, "~$ ")
	if !strings.Contains(replayed, "~$ ") {
		t.Fatalf("replayed output = %q, want prompt replay", replayed)
	}
	writeResp := client2.call("stream.write", sessionID, shellPayload.StreamID, map[string]any{
		"encoding": "base64",
		"data":     base64.StdEncoding.EncodeToString([]byte("cd /tmp\nexport FOO=bar\npwd\necho $FOO\nexit\n")),
	})
	mustOK(t, &writeResp)
	attachedOutput, attachedExit := collectStreamUntilExit(t, client2, shellPayload.StreamID)
	if attachedExit.ExitCode != 0 || !strings.Contains(attachedOutput, "/tmp\n") || !strings.Contains(attachedOutput, "bar\n") {
		t.Fatalf("attached output/exit = %q %+v, want replayed shell state", attachedOutput, attachedExit)
	}

	largeOutput := strings.Repeat("z", 34000)
	execStart := client2.call("exec.start", sessionID, "", map[string]any{
		"script": fmt.Sprintf("printf '%%s' '%s'\n", largeOutput),
	})
	var execPayload streamStartPayload
	decodePayload(t, &execStart, &execPayload)
	_, _ = collectStreamUntilExit(t, client2, execPayload.StreamID)

	gap := client2.call("stream.attach", sessionID, "", map[string]any{
		"stream_id":  execPayload.StreamID,
		"stdout_seq": 1,
	})
	if gap.OK == nil || *gap.OK {
		t.Fatalf("gap response = %#v, want error", gap)
	}
	if gap.Error == nil || gap.Error.Code != "REPLAY_GAP" {
		t.Fatalf("gap error = %#v, want REPLAY_GAP", gap.Error)
	}

	time.Sleep(500 * time.Millisecond)
	listResp := client2.call("session.list", "", "", nil)
	var listed sessionListPayload
	decodePayload(t, &listResp, &listed)
	if len(listed.Sessions) != 0 {
		t.Fatalf("session.list payload = %+v, want empty after ttl expiry", listed)
	}
	getResp := client2.call("session.get", sessionID, "", nil)
	if getResp.OK == nil || *getResp.OK {
		t.Fatalf("session.get after ttl = %#v, want error", getResp)
	}
	if getResp.Error == nil || getResp.Error.Code != "SESSION_NOT_FOUND" {
		t.Fatalf("session.get error = %#v, want SESSION_NOT_FOUND", getResp.Error)
	}
}

func startServer(t *testing.T, cfg gbserver.Config, opts ...gbash.Option) *serverHandle {
	t.Helper()

	rt, err := gbash.New(opts...)
	if err != nil {
		t.Fatalf("gbash.New() error = %v", err)
	}
	cfg.Runtime = rt

	ctx, cancel := context.WithCancel(context.Background())
	socket := filepath.Join(os.TempDir(), fmt.Sprintf("gbash-server-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(socket) })
	errCh := make(chan error, 1)
	go func() {
		errCh <- gbserver.ListenAndServeUnix(ctx, socket, cfg)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case err := <-errCh:
			cancel()
			t.Fatalf("server exited before socket became ready: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("timed out waiting for server socket")
		}
		conn, err := net.DialTimeout("unix", socket, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	handle := &serverHandle{socket: socket, cancel: cancel, errCh: errCh}
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("server exited with error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for server shutdown")
		}
	})
	return handle
}

func dialClient(t *testing.T, socket string) *testClient {
	t.Helper()

	conn, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		t.Fatalf("DialTimeout(%s) error = %v", socket, err)
	}
	return &testClient{
		t:    t,
		conn: conn,
		enc:  json.NewEncoder(conn),
		dec:  json.NewDecoder(conn),
	}
}

func (c *testClient) Close() {
	_ = c.conn.Close()
}

func (c *testClient) call(method, sessionID, streamID string, payload any) wireMessage {
	c.t.Helper()

	id := fmt.Sprintf("req-%d", c.nextID.Add(1))
	msg := map[string]any{
		"version":    "1",
		"type":       "request",
		"id":         id,
		"method":     method,
		"session_id": sessionID,
		"stream_id":  streamID,
		"payload":    payload,
	}
	if err := c.enc.Encode(msg); err != nil {
		c.t.Fatalf("Encode(%s) error = %v", method, err)
	}
	return c.waitFor(5*time.Second, func(msg wireMessage) bool {
		return msg.Type == "response" && msg.ID == id
	})
}

func (c *testClient) waitFor(timeout time.Duration, match func(wireMessage) bool) wireMessage {
	c.t.Helper()

	for i := range c.queue {
		if match(c.queue[i]) {
			msg := c.queue[i]
			c.queue = append(c.queue[:i], c.queue[i+1:]...)
			return msg
		}
	}

	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			c.t.Fatal("timed out waiting for protocol message")
		}
		if err := c.conn.SetReadDeadline(deadline); err != nil {
			c.t.Fatalf("SetReadDeadline() error = %v", err)
		}
		var msg wireMessage
		if err := c.dec.Decode(&msg); err != nil {
			c.t.Fatalf("Decode() error = %v", err)
		}
		if match(msg) {
			return msg
		}
		c.queue = append(c.queue, msg)
	}
}

func waitForStreamOutput(t *testing.T, client *testClient, streamID, want string) string {
	t.Helper()

	var out strings.Builder
	for {
		msg := client.waitFor(5*time.Second, func(msg wireMessage) bool {
			return msg.Type == "event" && msg.StreamID == streamID && msg.Event == "stream.data"
		})
		var payload streamDataPayload
		decodePayload(t, &msg, &payload)
		data, err := base64.StdEncoding.DecodeString(payload.Data)
		if err != nil {
			t.Fatalf("DecodeString() error = %v", err)
		}
		out.Write(data)
		if strings.Contains(out.String(), want) {
			return out.String()
		}
	}
}

func collectStreamUntilExit(t *testing.T, client *testClient, streamID string) (string, streamExitPayload) {
	t.Helper()

	var out strings.Builder
	for {
		msg := client.waitFor(5*time.Second, func(msg wireMessage) bool {
			if msg.StreamID != streamID || msg.Type != "event" {
				return false
			}
			return msg.Event == "stream.data" || msg.Event == "stream.exit"
		})
		switch msg.Event {
		case "stream.data":
			var payload streamDataPayload
			decodePayload(t, &msg, &payload)
			data, err := base64.StdEncoding.DecodeString(payload.Data)
			if err != nil {
				t.Fatalf("DecodeString() error = %v", err)
			}
			out.Write(data)
		case "stream.exit":
			var payload streamExitPayload
			decodePayload(t, &msg, &payload)
			return out.String(), payload
		}
	}
}

func mustCreateSession(t *testing.T, client *testClient) string {
	t.Helper()

	resp := client.call("session.create", "", "", nil)
	mustOK(t, &resp)
	var payload sessionSummaryPayload
	decodePayload(t, &resp, &payload)
	return payload.Session.SessionID
}

func mustOK(t *testing.T, msg *wireMessage) {
	t.Helper()
	if msg == nil || msg.OK == nil || !*msg.OK {
		t.Fatalf("response = %#v, want ok", msg)
	}
}

func decodePayload[T any](t *testing.T, msg *wireMessage, out *T) {
	t.Helper()
	if msg == nil || len(msg.Payload) == 0 {
		return
	}
	if err := json.Unmarshal(msg.Payload, out); err != nil {
		t.Fatalf("Unmarshal(payload) error = %v; raw=%s", err, string(msg.Payload))
	}
}
