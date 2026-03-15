package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ewhauser/gbash"
)

const streamChunkBytes = 16 << 10

var (
	errInvalidArgument    = errors.New("invalid argument")
	errSessionNotFound    = errors.New("session not found")
	errSessionBusy        = errors.New("session is busy")
	errStreamNotFound     = errors.New("stream not found")
	errStreamClosed       = errors.New("stream is closed")
	errStreamWriterBusy   = errors.New("stream stdin writer is busy")
	errStreamWriterNeeded = errors.New("stream stdin owner required")
	errReplayGap          = errors.New("requested replay is no longer available")
)

type sessionSummary struct {
	SessionID      string `json:"session_id"`
	State          string `json:"state"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	ActiveStreamID string `json:"active_stream_id,omitempty"`
	StreamCount    int    `json:"stream_count"`
}

type streamSummary struct {
	StreamID   string `json:"stream_id"`
	Kind       string `json:"kind"`
	State      string `json:"state"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
}

type streamExitPayload struct {
	Kind          string            `json:"kind"`
	ExitCode      int               `json:"exit_code"`
	ShellExited   bool              `json:"shell_exited,omitempty"`
	FinalEnv      map[string]string `json:"final_env,omitempty"`
	ControlStderr string            `json:"control_stderr,omitempty"`
}

type streamErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type streamDataPayload struct {
	Data     string `json:"data"`
	Encoding string `json:"encoding"`
}

type streamAttachment struct {
	conn  *clientConn
	write bool
}

type serverSession struct {
	server *serverState
	id     string
	shell  *gbash.Session

	createdAt time.Time
	updatedAt time.Time
	idleSince time.Time

	mu             sync.Mutex
	streams        map[string]*serverStream
	activeStreamID string
}

type serverStream struct {
	session *serverSession
	id      string
	kind    string

	createdAt  time.Time
	startedAt  time.Time
	updatedAt  time.Time
	finishedAt time.Time

	mu          sync.Mutex
	running     bool
	stdin       io.WriteCloser
	stdinClosed bool
	cancel      context.CancelFunc

	attachments map[string]*streamAttachment
	writerOwner string

	stdout replayBuffer
	stderr replayBuffer

	exitPayload *streamExitPayload

	startOnce sync.Once
	startFn   func()
}

type replayChunk struct {
	seq  uint64
	data []byte
}

type replayBuffer struct {
	limit  int
	size   int
	next   uint64
	chunks []replayChunk
}

func protocolErrorCode(err error) string {
	switch {
	case errors.Is(err, errInvalidArgument):
		return "INVALID_ARGUMENT"
	case errors.Is(err, errSessionNotFound):
		return "SESSION_NOT_FOUND"
	case errors.Is(err, errSessionBusy):
		return "SESSION_BUSY"
	case errors.Is(err, errStreamNotFound):
		return "STREAM_NOT_FOUND"
	case errors.Is(err, errStreamClosed):
		return "STREAM_CLOSED"
	case errors.Is(err, errStreamWriterBusy):
		return "STREAM_STDIN_BUSY"
	case errors.Is(err, errStreamWriterNeeded):
		return "STREAM_STDIN_OWNER_REQUIRED"
	case errors.Is(err, errReplayGap):
		return "REPLAY_GAP"
	default:
		return "INTERNAL"
	}
}

func (s *serverState) createSession() (*serverSession, error) {
	s.reapExpiredSessions(time.Now().UTC())

	sessionShell, err := s.cfg.Runtime.NewSession(s.ctx)
	if err != nil {
		return nil, fmt.Errorf("create gbash session: %w", err)
	}
	now := time.Now().UTC()
	session := &serverSession{
		server:    s,
		id:        s.nextIDValue("sess"),
		shell:     sessionShell,
		createdAt: now,
		updatedAt: now,
		idleSince: now,
		streams:   make(map[string]*serverStream),
	}

	s.sessionsMu.Lock()
	s.sessions[session.id] = session
	s.sessionsMu.Unlock()
	return session, nil
}

func (s *serverState) lookupSession(id string) (*serverSession, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: session_id must not be empty", errInvalidArgument)
	}
	s.reapExpiredSessions(time.Now().UTC())
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errSessionNotFound, id)
	}
	return session, nil
}

func (s *serverState) listSessions() []sessionSummary {
	s.sessionsMu.Lock()
	sessions := make([]*serverSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.sessionsMu.Unlock()

	out := make([]sessionSummary, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, session.summary())
	}
	slices.SortFunc(out, func(a, b sessionSummary) int {
		return strings.Compare(a.SessionID, b.SessionID)
	})
	return out
}

func (s *serverState) destroySession(id string) (*serverSession, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: session_id must not be empty", errInvalidArgument)
	}

	s.sessionsMu.Lock()
	session, ok := s.sessions[id]
	if !ok {
		s.sessionsMu.Unlock()
		return nil, fmt.Errorf("%w: %s", errSessionNotFound, id)
	}
	if session.busy() {
		s.sessionsMu.Unlock()
		return nil, fmt.Errorf("%w: %s", errSessionBusy, id)
	}
	delete(s.sessions, id)
	s.sessionsMu.Unlock()

	session.destroy()
	return session, nil
}

func (s *serverState) lookupStream(sessionID, streamID string) (*serverSession, *serverStream, error) {
	session, err := s.lookupSession(sessionID)
	if err != nil {
		return nil, nil, err
	}
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return nil, nil, fmt.Errorf("%w: stream_id must not be empty", errInvalidArgument)
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	stream, ok := session.streams[streamID]
	if !ok {
		return nil, nil, fmt.Errorf("%w: %s", errStreamNotFound, streamID)
	}
	return session, stream, nil
}

func (s *serverState) reapExpiredSessions(now time.Time) {
	s.sessionsMu.Lock()
	expired := make([]*serverSession, 0)
	for id, session := range s.sessions {
		if session.expired(now) {
			delete(s.sessions, id)
			expired = append(expired, session)
		}
	}
	s.sessionsMu.Unlock()

	for _, session := range expired {
		session.destroy()
	}
}

func (s *serverState) detachConnection(connID string) {
	s.sessionsMu.Lock()
	sessions := make([]*serverSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.sessionsMu.Unlock()

	for _, session := range sessions {
		session.detachConnection(connID)
	}
}

func (s *serverSession) summary() sessionSummary {
	s.mu.Lock()
	defer s.mu.Unlock()

	summary := sessionSummary{
		SessionID:      s.id,
		State:          "idle",
		CreatedAt:      formatTime(s.createdAt),
		UpdatedAt:      formatTime(s.updatedAt),
		ActiveStreamID: s.activeStreamID,
		StreamCount:    len(s.streams),
	}
	if s.activeStreamID != "" {
		summary.State = "running"
	}
	if s.idleSince.IsZero() {
		return summary
	}
	summary.ExpiresAt = formatTime(s.idleSince.Add(s.server.cfg.SessionTTL))
	return summary
}

func (s *serverSession) detail() (sessionSummary, []streamSummary) {
	s.mu.Lock()
	defer s.mu.Unlock()

	streams := make([]streamSummary, 0, len(s.streams))
	for _, stream := range s.streams {
		streams = append(streams, stream.summary())
	}
	slices.SortFunc(streams, func(a, b streamSummary) int {
		return strings.Compare(a.StreamID, b.StreamID)
	})
	return s.summaryLocked(), streams
}

func (s *serverSession) summaryLocked() sessionSummary {
	summary := sessionSummary{
		SessionID:      s.id,
		State:          "idle",
		CreatedAt:      formatTime(s.createdAt),
		UpdatedAt:      formatTime(s.updatedAt),
		ActiveStreamID: s.activeStreamID,
		StreamCount:    len(s.streams),
	}
	if s.activeStreamID != "" {
		summary.State = "running"
	}
	if !s.idleSince.IsZero() {
		summary.ExpiresAt = formatTime(s.idleSince.Add(s.server.cfg.SessionTTL))
	}
	return summary
}

func (s *serverSession) eventMessage(event string, payload any) *protocolMessage {
	return newProtocolMessage("event", "", s.id, "", event, "", 0, nil, payload, nil)
}

func (s *serverSession) startExec(conn *clientConn, req *execStartRequest) (*serverStream, error) {
	if req == nil {
		req = &execStartRequest{}
	}
	stdinMode := strings.TrimSpace(req.Stdin)
	if stdinMode == "" {
		stdinMode = "closed"
	}
	if stdinMode != "closed" && stdinMode != "stream" {
		return nil, fmt.Errorf("%w: unsupported exec stdin mode %q", errInvalidArgument, req.Stdin)
	}

	stream := s.newStream("exec")
	if stream == nil {
		return nil, fmt.Errorf("%w: %s", errSessionBusy, s.id)
	}

	ctx, cancel := context.WithCancel(s.server.ctx)
	stream.cancel = cancel
	if stdinMode == "stream" {
		reader, writer := io.Pipe()
		stream.stdin = writer
		stream.attachments[conn.id] = &streamAttachment{conn: conn, write: true}
		stream.writerOwner = conn.id
		stream.startFn = func() {
			go stream.exec(ctx, req, reader)
		}
		return stream, nil
	}

	stream.attachments[conn.id] = &streamAttachment{conn: conn}
	stream.startFn = func() {
		go stream.exec(ctx, req, nil)
	}
	return stream, nil
}

func (s *serverSession) startShell(conn *clientConn, req *shellStartRequest) (*serverStream, error) {
	if req == nil {
		req = &shellStartRequest{}
	}
	stream := s.newStream("shell")
	if stream == nil {
		return nil, fmt.Errorf("%w: %s", errSessionBusy, s.id)
	}

	ctx, cancel := context.WithCancel(s.server.ctx)
	stream.cancel = cancel
	reader, writer := io.Pipe()
	stream.stdin = writer
	stream.attachments[conn.id] = &streamAttachment{conn: conn, write: true}
	stream.writerOwner = conn.id
	stream.startFn = func() {
		go stream.shellInteract(ctx, req, reader)
	}
	return stream, nil
}

func (s *serverSession) newStream(kind string) *serverStream {
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.activeStreamID != "" {
		if active, ok := s.streams[s.activeStreamID]; ok && active.running {
			return nil
		}
	}

	stream := &serverStream{
		session:     s,
		id:          s.server.nextIDValue("stream"),
		kind:        kind,
		createdAt:   now,
		startedAt:   now,
		updatedAt:   now,
		running:     true,
		attachments: make(map[string]*streamAttachment),
		stdout: replayBuffer{
			limit: s.server.cfg.ReplayBytes,
			next:  1,
		},
		stderr: replayBuffer{
			limit: s.server.cfg.ReplayBytes,
			next:  1,
		},
	}
	s.streams[stream.id] = stream
	s.activeStreamID = stream.id
	s.updatedAt = now
	s.idleSince = time.Time{}
	return stream
}

func (s *serverSession) busy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeStreamID == "" {
		return false
	}
	stream, ok := s.streams[s.activeStreamID]
	return ok && stream.running
}

func (s *serverSession) expired(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeStreamID != "" {
		if stream, ok := s.streams[s.activeStreamID]; ok && stream.running {
			return false
		}
	}
	if s.idleSince.IsZero() || s.server.cfg.SessionTTL <= 0 {
		return false
	}
	return !now.Before(s.idleSince.Add(s.server.cfg.SessionTTL))
}

func (s *serverSession) destroy() {
	s.mu.Lock()
	streams := make([]*serverStream, 0, len(s.streams))
	for _, stream := range s.streams {
		streams = append(streams, stream)
	}
	s.streams = nil
	s.activeStreamID = ""
	s.updatedAt = time.Now().UTC()
	s.idleSince = s.updatedAt
	s.mu.Unlock()

	for _, stream := range streams {
		stream.shutdown()
	}
}

func (s *serverSession) detachConnection(connID string) {
	s.mu.Lock()
	streams := make([]*serverStream, 0, len(s.streams))
	for _, stream := range s.streams {
		streams = append(streams, stream)
	}
	s.mu.Unlock()

	for _, stream := range streams {
		stream.detach(connID)
	}
}

func (s *serverSession) markStreamFinished(streamID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.activeStreamID == streamID {
		s.activeStreamID = ""
	}
	now := time.Now().UTC()
	s.updatedAt = now
	s.idleSince = now
}

func (s *serverStream) state() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked()
}

func (s *serverStream) stateLocked() string {
	if s.running {
		return "running"
	}
	return "exited"
}

func (s *serverStream) writerAttached(connID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writerOwner == connID
}

func (s *serverStream) summary() streamSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return streamSummary{
		StreamID:   s.id,
		Kind:       s.kind,
		State:      s.stateLocked(),
		CreatedAt:  formatTime(s.createdAt),
		UpdatedAt:  formatTime(s.updatedAt),
		StartedAt:  formatTime(s.startedAt),
		FinishedAt: formatTime(s.finishedAt),
	}
}

func (s *serverStream) attach(conn *clientConn, stdoutSeq, stderrSeq uint64, write bool) (streamAttachResponse, []*protocolMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stdoutReplay, err := s.stdout.replay(stdoutSeq)
	if err != nil {
		return streamAttachResponse{}, nil, fmt.Errorf("%w: stdout replay for %s", errReplayGap, s.id)
	}
	stderrReplay, err := s.stderr.replay(stderrSeq)
	if err != nil {
		return streamAttachResponse{}, nil, fmt.Errorf("%w: stderr replay for %s", errReplayGap, s.id)
	}

	if write {
		switch {
		case !s.running || s.stdin == nil || s.stdinClosed:
			return streamAttachResponse{}, nil, fmt.Errorf("%w: %s", errStreamClosed, s.id)
		case s.writerOwner != "" && s.writerOwner != conn.id:
			return streamAttachResponse{}, nil, fmt.Errorf("%w: %s", errStreamWriterBusy, s.id)
		default:
			s.writerOwner = conn.id
		}
	}
	if s.running {
		attachment := &streamAttachment{conn: conn, write: write}
		if existing, ok := s.attachments[conn.id]; ok && existing.write {
			attachment.write = true
		}
		s.attachments[conn.id] = attachment
	}

	messages := make([]*protocolMessage, 0, len(stdoutReplay)+len(stderrReplay)+1)
	messages = append(messages, replayMessages(s.session.id, s.id, "stdout", stdoutReplay)...)
	messages = append(messages, replayMessages(s.session.id, s.id, "stderr", stderrReplay)...)
	if !s.running && s.exitPayload != nil {
		messages = append(messages, newProtocolMessage("event", "", s.session.id, s.id, "stream.exit", "", 0, nil, s.exitPayload, nil))
	}

	return streamAttachResponse{
		SessionID:      s.session.id,
		StreamID:       s.id,
		State:          s.stateLocked(),
		WriterAttached: s.writerOwner == conn.id,
	}, messages, nil
}

func (s *serverStream) detach(connID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attachments, connID)
	if s.writerOwner == connID {
		s.writerOwner = ""
	}
}

func (s *serverStream) writeStdin(connID, encoding, data string) error {
	s.mu.Lock()
	writer := s.stdin
	closed := s.stdinClosed
	isOwner := s.writerOwner == connID
	s.mu.Unlock()

	if !isOwner {
		return fmt.Errorf("%w: %s", errStreamWriterNeeded, s.id)
	}
	if writer == nil || closed {
		return fmt.Errorf("%w: %s", errStreamClosed, s.id)
	}

	encoding = strings.TrimSpace(encoding)
	if encoding == "" {
		encoding = "base64"
	}
	if encoding != "base64" {
		return fmt.Errorf("%w: unsupported encoding %q", errInvalidArgument, encoding)
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("%w: decode base64 stdin: %v", errInvalidArgument, err)
	}
	if len(decoded) == 0 {
		return nil
	}
	_, err = writer.Write(decoded)
	return err
}

func (s *serverStream) closeStdin(connID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.writerOwner != connID {
		return fmt.Errorf("%w: %s", errStreamWriterNeeded, s.id)
	}
	if s.stdin == nil || s.stdinClosed {
		return fmt.Errorf("%w: %s", errStreamClosed, s.id)
	}
	s.stdinClosed = true
	s.writerOwner = ""
	return s.stdin.Close()
}

func (s *serverStream) cancelStream() {
	s.mu.Lock()
	cancel := s.cancel
	stdin := s.stdin
	alreadyClosed := s.stdinClosed
	s.stdinClosed = true
	s.writerOwner = ""
	s.mu.Unlock()

	if pipeWriter, ok := stdin.(*io.PipeWriter); ok && !alreadyClosed {
		_ = pipeWriter.CloseWithError(context.Canceled)
	} else if stdin != nil && !alreadyClosed {
		_ = stdin.Close()
	}
	if cancel != nil {
		cancel()
	}
}

func (s *serverStream) shutdown() {
	s.cancelStream()
	s.mu.Lock()
	s.attachments = nil
	s.mu.Unlock()
}

func (s *serverStream) start() {
	s.startOnce.Do(func() {
		if s.startFn != nil {
			s.startFn()
		}
	})
}

func (s *serverStream) exec(ctx context.Context, req *execStartRequest, stdin io.Reader) {
	if req == nil {
		req = &execStartRequest{}
	}
	stdout := &streamChunkWriter{stream: s, channel: "stdout"}
	stderr := &streamChunkWriter{stream: s, channel: "stderr"}
	result, err := s.session.shell.Exec(ctx, &gbash.ExecutionRequest{
		Name:           req.Name,
		Script:         req.Script,
		Args:           append([]string(nil), req.Args...),
		StartupOptions: append([]string(nil), req.StartupOptions...),
		Env:            copyStringMap(req.Env),
		WorkDir:        req.WorkDir,
		ReplaceEnv:     req.ReplaceEnv,
		Timeout:        durationFromMillis(req.TimeoutMs),
		Stdin:          stdin,
		Stdout:         stdout,
		Stderr:         stderr,
	})
	s.finishExec(result, err)
}

func (s *serverStream) shellInteract(ctx context.Context, req *shellStartRequest, stdin io.Reader) {
	if req == nil {
		req = &shellStartRequest{}
	}
	stdout := &streamChunkWriter{stream: s, channel: "stdout"}
	stderr := &streamChunkWriter{stream: s, channel: "stderr"}
	result, err := s.session.shell.Interact(ctx, &gbash.InteractiveRequest{
		Name:           req.Name,
		Args:           append([]string(nil), req.Args...),
		StartupOptions: append([]string(nil), req.StartupOptions...),
		Env:            copyStringMap(req.Env),
		WorkDir:        req.WorkDir,
		ReplaceEnv:     req.ReplaceEnv,
		Stdin:          stdin,
		Stdout:         stdout,
		Stderr:         stderr,
	})
	s.finishShell(result, err)
}

func (s *serverStream) finishExec(result *gbash.ExecutionResult, err error) {
	if err != nil && result == nil {
		s.emitError(streamErrorPayload{Code: "INTERNAL", Message: err.Error()})
		s.finish(streamExitPayload{Kind: s.kind, ExitCode: 1}, false)
		return
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		s.emitError(streamErrorPayload{Code: "INTERNAL", Message: err.Error()})
	}
	payload := streamExitPayload{Kind: s.kind}
	if result != nil {
		payload.ExitCode = result.ExitCode
		payload.ShellExited = result.ShellExited
		payload.FinalEnv = copyStringMap(result.FinalEnv)
		payload.ControlStderr = strings.TrimSpace(result.ControlStderr)
	}
	if errors.Is(err, context.Canceled) {
		payload.ExitCode = 130
		payload.ControlStderr = "execution canceled"
	}
	s.finish(payload, true)
}

func (s *serverStream) finishShell(result *gbash.InteractiveResult, err error) {
	if err != nil && !errors.Is(err, context.Canceled) {
		s.emitError(streamErrorPayload{Code: "INTERNAL", Message: err.Error()})
	}
	payload := streamExitPayload{Kind: s.kind}
	if result != nil {
		payload.ExitCode = result.ExitCode
	}
	if errors.Is(err, context.Canceled) {
		payload.ExitCode = 130
	}
	s.finish(payload, true)
}

func (s *serverStream) finish(payload streamExitPayload, broadcastSession bool) {
	s.mu.Lock()
	if !s.finishedAt.IsZero() {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.updatedAt = time.Now().UTC()
	s.finishedAt = s.updatedAt
	s.exitPayload = &payload
	s.writerOwner = ""
	stdin := s.stdin
	s.stdinClosed = true
	attachments := make([]*clientConn, 0, len(s.attachments))
	for _, attachment := range s.attachments {
		attachments = append(attachments, attachment.conn)
	}
	s.mu.Unlock()

	if stdin != nil {
		_ = stdin.Close()
	}
	s.session.markStreamFinished(s.id)
	event := newProtocolMessage("event", "", s.session.id, s.id, "stream.exit", "", 0, nil, payload, nil)
	for _, conn := range attachments {
		conn.sendMessage(event)
	}
	if broadcastSession {
		s.session.server.sendToAll(newProtocolMessage("event", "", s.session.id, "", "session.updated", "", 0, nil, s.session.summary(), nil))
	}
}

func (s *serverStream) emitData(channel string, p []byte) {
	if len(p) == 0 {
		return
	}

	s.mu.Lock()
	if channel == "stdout" {
		_ = s.stdout.append(p)
	} else {
		_ = s.stderr.append(p)
	}
	var seq uint64
	if channel == "stdout" {
		seq = s.stdout.next - 1
	} else {
		seq = s.stderr.next - 1
	}
	attachments := make([]*clientConn, 0, len(s.attachments))
	for _, attachment := range s.attachments {
		attachments = append(attachments, attachment.conn)
	}
	s.updatedAt = time.Now().UTC()
	s.mu.Unlock()

	msg := newProtocolMessage("event", "", s.session.id, s.id, "stream.data", channel, seq, nil, streamDataPayload{
		Data:     base64.StdEncoding.EncodeToString(append([]byte(nil), p...)),
		Encoding: "base64",
	}, nil)
	for _, conn := range attachments {
		conn.sendMessage(msg)
	}
}

func (s *serverStream) emitError(payload streamErrorPayload) {
	s.mu.Lock()
	attachments := make([]*clientConn, 0, len(s.attachments))
	for _, attachment := range s.attachments {
		attachments = append(attachments, attachment.conn)
	}
	s.mu.Unlock()

	msg := newProtocolMessage("event", "", s.session.id, s.id, "stream.error", "", 0, nil, payload, nil)
	for _, conn := range attachments {
		conn.sendMessage(msg)
	}
}

func replayMessages(sessionID, streamID, channel string, chunks []replayChunk) []*protocolMessage {
	messages := make([]*protocolMessage, 0, len(chunks))
	for _, chunk := range chunks {
		messages = append(messages, newProtocolMessage("event", "", sessionID, streamID, "stream.data", channel, chunk.seq, nil, streamDataPayload{
			Data:     base64.StdEncoding.EncodeToString(chunk.data),
			Encoding: "base64",
		}, nil))
	}
	return messages
}

func (b *replayBuffer) append(p []byte) uint64 {
	copied := append([]byte(nil), p...)
	seq := b.next
	b.next++
	b.chunks = append(b.chunks, replayChunk{seq: seq, data: copied})
	b.size += len(copied)
	for b.limit > 0 && len(b.chunks) > 0 && b.size > b.limit {
		b.size -= len(b.chunks[0].data)
		b.chunks = b.chunks[1:]
	}
	return seq
}

func (b *replayBuffer) replay(lastSeen uint64) ([]replayChunk, error) {
	if len(b.chunks) == 0 {
		return nil, nil
	}
	first := b.chunks[0].seq
	if lastSeen != 0 && lastSeen < first-1 {
		return nil, errReplayGap
	}
	out := make([]replayChunk, 0, len(b.chunks))
	for _, chunk := range b.chunks {
		if chunk.seq > lastSeen {
			out = append(out, replayChunk{
				seq:  chunk.seq,
				data: append([]byte(nil), chunk.data...),
			})
		}
	}
	return out, nil
}

type streamChunkWriter struct {
	stream  *serverStream
	channel string
}

func (w *streamChunkWriter) Write(p []byte) (int, error) {
	written := len(p)
	for len(p) > 0 {
		chunk := p
		if len(chunk) > streamChunkBytes {
			chunk = p[:streamChunkBytes]
		}
		w.stream.emitData(w.channel, chunk)
		p = p[len(chunk):]
	}
	return written, nil
}

func durationFromMillis(ms int64) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func formatTime(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339Nano)
}

func copyStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	maps.Copy(out, src)
	return out
}
