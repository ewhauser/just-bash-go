package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ewhauser/gbash"
)

func TestDestroyedSessionRejectsExecFromStalePointer(t *testing.T) {
	rt, err := gbash.New()
	if err != nil {
		t.Fatalf("gbash.New() error = %v", err)
	}

	state := &serverState{
		ctx: context.Background(),
		cfg: normalizeConfig(Config{
			Runtime:    rt,
			Name:       "gbash",
			Version:    "test",
			SessionTTL: time.Second,
		}),
		conns:    make(map[string]*clientConn),
		sessions: make(map[string]*serverSession),
	}

	session, err := state.createSession()
	if err != nil {
		t.Fatalf("createSession() error = %v", err)
	}

	stale, err := state.lookupSession(session.id)
	if err != nil {
		t.Fatalf("lookupSession(%q) error = %v", session.id, err)
	}

	if _, err := state.destroySession(session.id); err != nil {
		t.Fatalf("destroySession(%q) error = %v", session.id, err)
	}

	_, err = stale.exec(context.Background(), &sessionExecParams{Script: "printf 'should not run\\n'"})
	if !errors.Is(err, errSessionNotFound) {
		t.Fatalf("stale.exec() error = %v, want session not found", err)
	}
}
