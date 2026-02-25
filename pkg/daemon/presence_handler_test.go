// SPDX-License-Identifier: GPL-2.0-only
package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Work-Fort/sharkfin/pkg/db"
)

func TestPresenceValidToken(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	sm := NewSessionManager(d, true)
	ph := NewPresenceHandler(sm)

	token := sm.CreateIdentityToken()

	// Run presence in a goroutine since it blocks
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan int)
	go func() {
		body, _ := json.Marshal(map[string]string{"token": token})
		req := httptest.NewRequest("POST", "/presence", bytes.NewReader(body)).WithContext(ctx)
		w := httptest.NewRecorder()
		ph.ServeHTTP(w, req)
		done <- w.Code
	}()

	// Give it a moment to connect
	time.Sleep(50 * time.Millisecond)

	// Verify presence is attached
	sm.mu.RLock()
	it := sm.tokens[token]
	sm.mu.RUnlock()
	if !it.HasPresence {
		t.Error("expected presence to be attached")
	}

	// Cancel to disconnect
	cancel()
	code := <-done
	if code != http.StatusOK {
		t.Errorf("status = %d, want 200", code)
	}
}

func TestPresenceInvalidToken(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	sm := NewSessionManager(d, true)
	ph := NewPresenceHandler(sm)

	body, _ := json.Marshal(map[string]string{"token": "bogus"})
	req := httptest.NewRequest("POST", "/presence", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ph.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestPresenceDisconnectNotifiesSession(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	sm := NewSessionManager(d, true)
	ph := NewPresenceHandler(sm)

	token := sm.CreateIdentityToken()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		body, _ := json.Marshal(map[string]string{"token": token})
		req := httptest.NewRequest("POST", "/presence", bytes.NewReader(body)).WithContext(ctx)
		w := httptest.NewRecorder()
		ph.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	// Register while presence is connected
	d.CreateUser("alice", "")
	sm.mu.Lock()
	it := sm.tokens[token]
	it.Identified = true
	it.Username = "alice"
	sm.onlineUsers["alice"] = token
	sm.mu.Unlock()

	if !sm.IsUserOnline("alice") {
		t.Error("alice should be online")
	}

	// Disconnect
	cancel()
	<-done

	if sm.IsUserOnline("alice") {
		t.Error("alice should be offline after disconnect")
	}
}
