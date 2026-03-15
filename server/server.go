package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ewhauser/gbash"
)

const (
	protocolVersion = "1"
	defaultTTL      = 30 * time.Minute
	defaultReplay   = 1 << 20
	minReaperTick   = 100 * time.Millisecond
	maxReaperTick   = time.Second
)

// Config configures the shared gbash server mode.
type Config struct {
	Runtime     *gbash.Runtime
	Name        string
	Version     string
	SessionTTL  time.Duration
	ReplayBytes int
}

// ListenAndServeUnix serves the gbash protocol on a Unix domain socket.
func ListenAndServeUnix(ctx context.Context, socketPath string, cfg Config) error {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return fmt.Errorf("server: socket path must not be empty")
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return fmt.Errorf("server: create socket directory: %w", err)
	}
	if err := removeStaleUnixSocket(socketPath); err != nil {
		return err
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("server: listen on unix socket: %w", err)
	}
	defer func() { _ = os.Remove(socketPath) }()
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = ln.Close()
		return fmt.Errorf("server: chmod socket: %w", err)
	}
	return Serve(ctx, ln, cfg)
}

// Serve serves the gbash protocol on an existing listener.
func Serve(ctx context.Context, ln net.Listener, cfg Config) error {
	if ln == nil {
		return fmt.Errorf("server: listener is nil")
	}
	cfg = normalizeConfig(cfg)
	if cfg.Runtime == nil {
		return fmt.Errorf("server: runtime is nil")
	}

	srv := &serverState{
		ctx:      ctx,
		cfg:      cfg,
		sessions: make(map[string]*serverSession),
		conns:    make(map[string]*clientConn),
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = ln.Close()
			srv.closeConnections()
		case <-done:
		}
	}()
	go srv.reapLoop(done)

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("server: accept: %w", err)
		}
		srv.addConn(conn)
	}
}

type serverState struct {
	ctx context.Context
	cfg Config

	nextID atomic.Uint64

	connsMu sync.RWMutex
	conns   map[string]*clientConn

	sessionsMu sync.Mutex
	sessions   map[string]*serverSession
}

type clientConn struct {
	server *serverState
	id     string
	conn   net.Conn
	enc    *json.Encoder
	dec    *json.Decoder

	send   chan *protocolMessage
	closed chan struct{}
	once   sync.Once
}

type protocolMessage struct {
	Version   string          `json:"version,omitempty"`
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Event     string          `json:"event,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	StreamID  string          `json:"stream_id,omitempty"`
	Channel   string          `json:"channel,omitempty"`
	Seq       uint64          `json:"seq,omitempty"`
	OK        *bool           `json:"ok,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Error     *protocolError  `json:"error,omitempty"`
}

type protocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type helloResponse struct {
	ServerName    string            `json:"server_name"`
	ServerVersion string            `json:"server_version"`
	Capabilities  helloCapabilities `json:"capabilities"`
}

type helloCapabilities struct {
	Binary           string `json:"binary"`
	Transport        string `json:"transport"`
	Reconnect        bool   `json:"reconnect"`
	FileSystemRPC    bool   `json:"filesystem_rpc"`
	PTY              bool   `json:"pty"`
	InteractiveShell bool   `json:"interactive_shell"`
}

type sessionListResponse struct {
	Sessions []sessionSummary `json:"sessions"`
}

type sessionDetailResponse struct {
	Session sessionSummary  `json:"session"`
	Streams []streamSummary `json:"streams,omitempty"`
}

type streamStartResponse struct {
	SessionID      string `json:"session_id"`
	StreamID       string `json:"stream_id"`
	Kind           string `json:"kind"`
	State          string `json:"state"`
	WriterAttached bool   `json:"writer_attached"`
}

type streamAttachResponse struct {
	SessionID      string `json:"session_id"`
	StreamID       string `json:"stream_id"`
	State          string `json:"state"`
	WriterAttached bool   `json:"writer_attached"`
}

type sessionCreateRequest struct{}

type execStartRequest struct {
	Name           string            `json:"name"`
	Script         string            `json:"script"`
	Args           []string          `json:"args"`
	StartupOptions []string          `json:"startup_options"`
	Env            map[string]string `json:"env"`
	WorkDir        string            `json:"work_dir"`
	ReplaceEnv     bool              `json:"replace_env"`
	TimeoutMs      int64             `json:"timeout_ms"`
	Stdin          string            `json:"stdin"`
}

type shellStartRequest struct {
	Name           string            `json:"name"`
	Args           []string          `json:"args"`
	StartupOptions []string          `json:"startup_options"`
	Env            map[string]string `json:"env"`
	WorkDir        string            `json:"work_dir"`
	ReplaceEnv     bool              `json:"replace_env"`
}

type streamAttachRequest struct {
	StreamID  string `json:"stream_id"`
	StdoutSeq uint64 `json:"stdout_seq"`
	StderrSeq uint64 `json:"stderr_seq"`
	Write     bool   `json:"write"`
}

type streamDataRequest struct {
	Data     string `json:"data"`
	Encoding string `json:"encoding"`
}

func normalizeConfig(cfg Config) Config {
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = defaultTTL
	}
	if cfg.ReplayBytes <= 0 {
		cfg.ReplayBytes = defaultReplay
	}
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		cfg.Name = "gbash"
	}
	cfg.Version = strings.TrimSpace(cfg.Version)
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	return cfg
}

func removeStaleUnixSocket(socketPath string) error {
	info, err := os.Lstat(socketPath)
	if err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("server: path exists and is not a socket: %s", socketPath)
		}
		if removeErr := os.Remove(socketPath); removeErr != nil {
			return fmt.Errorf("server: remove stale socket: %w", removeErr)
		}
		return nil
	}
	if os.IsNotExist(err) {
		return nil
	}
	return fmt.Errorf("server: stat socket path: %w", err)
}

func (s *serverState) reapLoop(done <-chan struct{}) {
	tick := s.cfg.SessionTTL / 2
	switch {
	case tick <= 0:
		tick = maxReaperTick
	case tick < minReaperTick:
		tick = minReaperTick
	case tick > maxReaperTick:
		tick = maxReaperTick
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.reapExpiredSessions(time.Now().UTC())
		case <-done:
			return
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *serverState) addConn(nc net.Conn) {
	conn := &clientConn{
		server: s,
		id:     s.nextIDValue("conn"),
		conn:   nc,
		enc:    json.NewEncoder(nc),
		dec:    json.NewDecoder(nc),
		send:   make(chan *protocolMessage, 128),
		closed: make(chan struct{}),
	}

	s.connsMu.Lock()
	s.conns[conn.id] = conn
	s.connsMu.Unlock()

	go conn.writeLoop()
	go conn.readLoop()
}

func (s *serverState) removeConn(id string) {
	s.connsMu.Lock()
	delete(s.conns, id)
	s.connsMu.Unlock()
}

func (s *serverState) closeConnections() {
	s.connsMu.RLock()
	conns := make([]*clientConn, 0, len(s.conns))
	for _, conn := range s.conns {
		conns = append(conns, conn)
	}
	s.connsMu.RUnlock()
	for _, conn := range conns {
		conn.close()
	}
}

func (s *serverState) sendToAll(msg *protocolMessage) {
	s.connsMu.RLock()
	conns := make([]*clientConn, 0, len(s.conns))
	for _, conn := range s.conns {
		conns = append(conns, conn)
	}
	s.connsMu.RUnlock()
	for _, conn := range conns {
		conn.sendMessage(msg)
	}
}

func (s *serverState) nextIDValue(prefix string) string {
	return fmt.Sprintf("%s-%06d", prefix, s.nextID.Add(1))
}

func (c *clientConn) writeLoop() {
	for {
		select {
		case <-c.closed:
			return
		case msg := <-c.send:
			if err := c.enc.Encode(msg); err != nil {
				c.close()
				return
			}
		}
	}
}

func (c *clientConn) readLoop() {
	defer c.close()
	for {
		var msg protocolMessage
		if err := c.dec.Decode(&msg); err != nil {
			return
		}
		if err := c.handleMessage(&msg); err != nil {
			c.respondError(msg.ID, msg.SessionID, msg.StreamID, "INVALID_REQUEST", err.Error())
		}
	}
}

func (c *clientConn) handleMessage(msg *protocolMessage) error {
	if msg == nil {
		return fmt.Errorf("request is nil")
	}
	if strings.TrimSpace(msg.Version) != "" && strings.TrimSpace(msg.Version) != protocolVersion {
		return fmt.Errorf("unsupported protocol version %q", msg.Version)
	}
	if strings.TrimSpace(msg.Type) != "request" {
		return fmt.Errorf("message type must be request")
	}
	if strings.TrimSpace(msg.ID) == "" {
		return fmt.Errorf("request id must not be empty")
	}

	switch msg.Method {
	case "hello":
		c.respondOK(msg.ID, "", "", helloResponse{
			ServerName:    c.server.cfg.Name,
			ServerVersion: c.server.cfg.Version,
			Capabilities: helloCapabilities{
				Binary:           c.server.cfg.Name,
				Transport:        "unix",
				Reconnect:        true,
				FileSystemRPC:    false,
				PTY:              false,
				InteractiveShell: true,
			},
		})
	case "ping":
		c.respondOK(msg.ID, "", "", map[string]any{"pong": true})
	case "session.create":
		return c.handleSessionCreate(msg)
	case "session.get":
		return c.handleSessionGet(msg)
	case "session.list":
		return c.handleSessionList(msg)
	case "session.destroy":
		return c.handleSessionDestroy(msg)
	case "exec.start":
		return c.handleExecStart(msg)
	case "shell.start":
		return c.handleShellStart(msg)
	case "stream.attach":
		return c.handleStreamAttach(msg)
	case "stream.detach":
		return c.handleStreamDetach(msg)
	case "stream.write":
		return c.handleStreamWrite(msg)
	case "stream.close":
		return c.handleStreamClose(msg)
	case "stream.cancel":
		return c.handleStreamCancel(msg)
	default:
		c.respondError(msg.ID, msg.SessionID, msg.StreamID, "METHOD_NOT_FOUND", fmt.Sprintf("unknown method %q", msg.Method))
	}
	return nil
}

func (c *clientConn) close() {
	c.once.Do(func() {
		close(c.closed)
		_ = c.conn.Close()
		c.server.removeConn(c.id)
		c.server.detachConnection(c.id)
	})
}

func (c *clientConn) sendMessage(msg *protocolMessage) bool {
	if msg == nil {
		return false
	}
	select {
	case <-c.closed:
		return false
	case c.send <- msg:
		return true
	}
}

func (c *clientConn) respondOK(id, sessionID, streamID string, payload any) {
	ok := true
	c.sendMessage(newProtocolMessage("response", id, sessionID, streamID, "", "", 0, &ok, payload, nil))
}

func (c *clientConn) respondError(id, sessionID, streamID, code, message string) {
	ok := false
	c.sendMessage(newProtocolMessage("response", id, sessionID, streamID, "", "", 0, &ok, nil, &protocolError{
		Code:    code,
		Message: message,
	}))
}

func newProtocolMessage(msgType, id, sessionID, streamID, event, channel string, seq uint64, ok *bool, payload any, err *protocolError) *protocolMessage {
	msg := &protocolMessage{
		Version:   protocolVersion,
		Type:      msgType,
		ID:        id,
		Event:     event,
		SessionID: sessionID,
		StreamID:  streamID,
		Channel:   channel,
		Seq:       seq,
		OK:        ok,
		Error:     err,
	}
	if payload != nil {
		data, marshalErr := json.Marshal(payload)
		if marshalErr == nil && string(data) != "null" {
			msg.Payload = data
		}
	}
	return msg
}

func decodePayload[T any](raw json.RawMessage) (T, error) {
	var out T
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *clientConn) handleSessionCreate(msg *protocolMessage) error {
	if _, err := decodePayload[sessionCreateRequest](msg.Payload); err != nil {
		return fmt.Errorf("decode session.create payload: %w", err)
	}
	session, err := c.server.createSession()
	if err != nil {
		c.respondError(msg.ID, "", "", "INTERNAL", err.Error())
		return nil
	}
	summary := session.summary()
	c.respondOK(msg.ID, session.id, "", sessionDetailResponse{Session: summary})
	c.server.sendToAll(session.eventMessage("session.created", summary))
	return nil
}

func (c *clientConn) handleSessionGet(msg *protocolMessage) error {
	session, err := c.server.lookupSession(msg.SessionID)
	if err != nil {
		c.respondError(msg.ID, msg.SessionID, "", "SESSION_NOT_FOUND", err.Error())
		return nil
	}
	summary, streams := session.detail()
	c.respondOK(msg.ID, session.id, "", sessionDetailResponse{Session: summary, Streams: streams})
	return nil
}

func (c *clientConn) handleSessionList(msg *protocolMessage) error {
	c.server.reapExpiredSessions(time.Now().UTC())
	c.respondOK(msg.ID, "", "", sessionListResponse{Sessions: c.server.listSessions()})
	return nil
}

func (c *clientConn) handleSessionDestroy(msg *protocolMessage) error {
	if strings.TrimSpace(msg.SessionID) == "" {
		return fmt.Errorf("session_id must not be empty")
	}
	session, err := c.server.destroySession(msg.SessionID)
	if err != nil {
		code := "INTERNAL"
		switch {
		case errors.Is(err, errSessionBusy):
			code = "SESSION_BUSY"
		case errors.Is(err, errSessionNotFound):
			code = "SESSION_NOT_FOUND"
		}
		c.respondError(msg.ID, msg.SessionID, "", code, err.Error())
		return nil
	}
	summary := session.summary()
	c.respondOK(msg.ID, session.id, "", sessionDetailResponse{Session: summary})
	c.server.sendToAll(newProtocolMessage("event", "", session.id, "", "session.updated", "", 0, nil, summary, nil))
	return nil
}

func (c *clientConn) handleExecStart(msg *protocolMessage) error {
	req, err := decodePayload[execStartRequest](msg.Payload)
	if err != nil {
		return fmt.Errorf("decode exec.start payload: %w", err)
	}
	session, err := c.server.lookupSession(msg.SessionID)
	if err != nil {
		c.respondError(msg.ID, msg.SessionID, "", "SESSION_NOT_FOUND", err.Error())
		return nil
	}
	stream, startErr := session.startExec(c, &req)
	if startErr != nil {
		c.respondError(msg.ID, msg.SessionID, "", protocolErrorCode(startErr), startErr.Error())
		return nil
	}
	c.respondOK(msg.ID, session.id, stream.id, streamStartResponse{
		SessionID:      session.id,
		StreamID:       stream.id,
		Kind:           stream.kind,
		State:          stream.state(),
		WriterAttached: stream.writerAttached(c.id),
	})
	c.server.sendToAll(newProtocolMessage("event", "", session.id, "", "session.updated", "", 0, nil, session.summary(), nil))
	stream.start()
	return nil
}

func (c *clientConn) handleShellStart(msg *protocolMessage) error {
	req, err := decodePayload[shellStartRequest](msg.Payload)
	if err != nil {
		return fmt.Errorf("decode shell.start payload: %w", err)
	}
	session, err := c.server.lookupSession(msg.SessionID)
	if err != nil {
		c.respondError(msg.ID, msg.SessionID, "", "SESSION_NOT_FOUND", err.Error())
		return nil
	}
	stream, startErr := session.startShell(c, &req)
	if startErr != nil {
		c.respondError(msg.ID, msg.SessionID, "", protocolErrorCode(startErr), startErr.Error())
		return nil
	}
	c.respondOK(msg.ID, session.id, stream.id, streamStartResponse{
		SessionID:      session.id,
		StreamID:       stream.id,
		Kind:           stream.kind,
		State:          stream.state(),
		WriterAttached: stream.writerAttached(c.id),
	})
	c.server.sendToAll(newProtocolMessage("event", "", session.id, "", "session.updated", "", 0, nil, session.summary(), nil))
	stream.start()
	return nil
}

func (c *clientConn) handleStreamAttach(msg *protocolMessage) error {
	req, err := decodePayload[streamAttachRequest](msg.Payload)
	if err != nil {
		return fmt.Errorf("decode stream.attach payload: %w", err)
	}
	session, stream, lookupErr := c.server.lookupStream(msg.SessionID, coalesce(req.StreamID, msg.StreamID))
	if lookupErr != nil {
		c.respondError(msg.ID, msg.SessionID, coalesce(req.StreamID, msg.StreamID), protocolErrorCode(lookupErr), lookupErr.Error())
		return nil
	}
	resp, messages, attachErr := stream.attach(c, req.StdoutSeq, req.StderrSeq, req.Write)
	if attachErr != nil {
		c.respondError(msg.ID, session.id, stream.id, protocolErrorCode(attachErr), attachErr.Error())
		return nil
	}
	c.respondOK(msg.ID, session.id, stream.id, resp)
	for i := range messages {
		c.sendMessage(messages[i])
	}
	return nil
}

func (c *clientConn) handleStreamDetach(msg *protocolMessage) error {
	req, err := decodePayload[streamAttachRequest](msg.Payload)
	if err != nil {
		return fmt.Errorf("decode stream.detach payload: %w", err)
	}
	session, stream, lookupErr := c.server.lookupStream(msg.SessionID, coalesce(req.StreamID, msg.StreamID))
	if lookupErr != nil {
		c.respondError(msg.ID, msg.SessionID, coalesce(req.StreamID, msg.StreamID), protocolErrorCode(lookupErr), lookupErr.Error())
		return nil
	}
	stream.detach(c.id)
	c.respondOK(msg.ID, session.id, stream.id, streamAttachResponse{
		SessionID:      session.id,
		StreamID:       stream.id,
		State:          stream.state(),
		WriterAttached: false,
	})
	return nil
}

func (c *clientConn) handleStreamWrite(msg *protocolMessage) error {
	req, err := decodePayload[streamDataRequest](msg.Payload)
	if err != nil {
		return fmt.Errorf("decode stream.write payload: %w", err)
	}
	session, stream, lookupErr := c.server.lookupStream(msg.SessionID, msg.StreamID)
	if lookupErr != nil {
		c.respondError(msg.ID, msg.SessionID, msg.StreamID, protocolErrorCode(lookupErr), lookupErr.Error())
		return nil
	}
	if err := stream.writeStdin(c.id, req.Encoding, req.Data); err != nil {
		c.respondError(msg.ID, session.id, stream.id, protocolErrorCode(err), err.Error())
		return nil
	}
	c.respondOK(msg.ID, session.id, stream.id, map[string]any{"written": true})
	return nil
}

func (c *clientConn) handleStreamClose(msg *protocolMessage) error {
	session, stream, lookupErr := c.server.lookupStream(msg.SessionID, msg.StreamID)
	if lookupErr != nil {
		c.respondError(msg.ID, msg.SessionID, msg.StreamID, protocolErrorCode(lookupErr), lookupErr.Error())
		return nil
	}
	if err := stream.closeStdin(c.id); err != nil {
		c.respondError(msg.ID, session.id, stream.id, protocolErrorCode(err), err.Error())
		return nil
	}
	c.respondOK(msg.ID, session.id, stream.id, map[string]any{"closed": true})
	return nil
}

func (c *clientConn) handleStreamCancel(msg *protocolMessage) error {
	session, stream, lookupErr := c.server.lookupStream(msg.SessionID, msg.StreamID)
	if lookupErr != nil {
		c.respondError(msg.ID, msg.SessionID, msg.StreamID, protocolErrorCode(lookupErr), lookupErr.Error())
		return nil
	}
	stream.cancelStream()
	c.respondOK(msg.ID, session.id, stream.id, map[string]any{"canceled": true})
	return nil
}

func coalesce(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
