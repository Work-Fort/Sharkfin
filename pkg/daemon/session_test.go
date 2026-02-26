// SPDX-License-Identifier: GPL-2.0-only
package daemon

import (
	"testing"

	"github.com/Work-Fort/sharkfin/pkg/db"
)

func newTestSessionManager(t *testing.T) *SessionManager {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return NewSessionManager(d)
}

func TestCreateIdentityToken(t *testing.T) {
	sm := newTestSessionManager(t)
	token := sm.CreateIdentityToken()
	if token == "" {
		t.Fatal("token should not be empty")
	}
	// Token should be in pending state
	sm.mu.RLock()
	it, ok := sm.tokens[token]
	sm.mu.RUnlock()
	if !ok {
		t.Fatal("token not found in map")
	}
	if it.Identified {
		t.Error("token should not be identified yet")
	}
}

func TestAttachPresence(t *testing.T) {
	sm := newTestSessionManager(t)
	token := sm.CreateIdentityToken()

	done, err := sm.AttachPresence(token)
	if err != nil {
		t.Fatalf("attach presence: %v", err)
	}
	if done == nil {
		t.Fatal("done channel should not be nil")
	}
}

func TestAttachPresenceInvalidToken(t *testing.T) {
	sm := newTestSessionManager(t)
	_, err := sm.AttachPresence("bogus-token")
	if err == nil {
		t.Error("expected error for invalid token")
	}
}

func TestRegisterWithToken(t *testing.T) {
	sm := newTestSessionManager(t)
	token := sm.CreateIdentityToken()
	sm.AttachPresence(token)

	sessionID, err := sm.Register(token, "alice", "")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if sessionID == "" {
		t.Error("session ID should not be empty")
	}

	// Should be identified now
	sm.mu.RLock()
	it := sm.tokens[token]
	sm.mu.RUnlock()
	if !it.Identified {
		t.Error("token should be identified")
	}
	if it.Username != "alice" {
		t.Errorf("username = %q, want alice", it.Username)
	}
}

func TestIdentifyWithToken(t *testing.T) {
	sm := newTestSessionManager(t)

	// First register a user
	token1 := sm.CreateIdentityToken()
	sm.AttachPresence(token1)
	sm.Register(token1, "bob", "")
	sm.DisconnectPresence(token1)

	// Now identify as that user from a new session
	token2 := sm.CreateIdentityToken()
	sm.AttachPresence(token2)
	sessionID, err := sm.Identify(token2, "bob", "")
	if err != nil {
		t.Fatalf("identify: %v", err)
	}
	if sessionID == "" {
		t.Error("session ID should not be empty")
	}
}

func TestIdentifyAlreadyOnline(t *testing.T) {
	sm := newTestSessionManager(t)

	// Register user and stay online
	token1 := sm.CreateIdentityToken()
	sm.AttachPresence(token1)
	sm.Register(token1, "alice", "")

	// Try to identify as alice from another session
	token2 := sm.CreateIdentityToken()
	sm.AttachPresence(token2)
	_, err := sm.Identify(token2, "alice", "")
	if err == nil {
		t.Error("expected error: user already online")
	}
}

func TestRegisterAfterIdentified(t *testing.T) {
	sm := newTestSessionManager(t)
	token := sm.CreateIdentityToken()
	sm.AttachPresence(token)
	sm.Register(token, "alice", "")

	_, err := sm.Register(token, "bob", "")
	if err == nil {
		t.Error("expected error: already identified")
	}
}

func TestIdentifyAfterIdentified(t *testing.T) {
	sm := newTestSessionManager(t)
	token := sm.CreateIdentityToken()
	sm.AttachPresence(token)
	sm.Register(token, "alice", "")

	_, err := sm.Identify(token, "alice", "")
	if err == nil {
		t.Error("expected error: already identified")
	}
}

func TestGetSessionByMCPSessionID(t *testing.T) {
	sm := newTestSessionManager(t)
	token := sm.CreateIdentityToken()
	sm.AttachPresence(token)
	sessionID, _ := sm.Register(token, "alice", "")

	session, err := sm.GetSession(sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.Username != "alice" {
		t.Errorf("username = %q, want alice", session.Username)
	}
}

func TestDisconnectPresenceGoesOffline(t *testing.T) {
	sm := newTestSessionManager(t)
	token := sm.CreateIdentityToken()
	sm.AttachPresence(token)
	sm.Register(token, "alice", "")

	if !sm.IsUserOnline("alice") {
		t.Error("alice should be online")
	}

	sm.DisconnectPresence(token)

	if sm.IsUserOnline("alice") {
		t.Error("alice should be offline after disconnect")
	}
}
