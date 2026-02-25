// SPDX-License-Identifier: GPL-2.0-only
package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Work-Fort/sharkfin/pkg/db"
)

// wsURL converts an httptest.Server URL to a WebSocket URL with path.
func wsURL(server *httptest.Server) string {
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func TestPresenceTokenDelivery(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	sm := NewSessionManager(d, true)
	ph := NewPresenceHandler(sm)

	server := httptest.NewServer(ph)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	token := string(msg)
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if len(token) != 64 { // 32 bytes hex-encoded
		t.Errorf("token length = %d, want 64", len(token))
	}
}

func TestPresenceHoldsConnection(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	sm := NewSessionManager(d, true)
	ph := NewPresenceHandler(sm)

	server := httptest.NewServer(ph)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Read token
	_, _, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Verify presence is attached
	time.Sleep(50 * time.Millisecond)
	sm.mu.RLock()
	tokenCount := len(sm.tokens)
	sm.mu.RUnlock()
	if tokenCount == 0 {
		t.Error("expected token to exist in session manager")
	}
}

func TestPresenceDisconnectNotifiesSession(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	sm := NewSessionManager(d, true)
	ph := NewPresenceHandler(sm)

	server := httptest.NewServer(ph)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Read token
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	token := string(msg)

	// Register a user on this token
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

	// Disconnect by closing WebSocket
	conn.Close()
	time.Sleep(100 * time.Millisecond)

	if sm.IsUserOnline("alice") {
		t.Error("alice should be offline after disconnect")
	}
}

func TestPresenceRejectsNonWebSocket(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	sm := NewSessionManager(d, true)
	ph := NewPresenceHandler(sm)

	server := httptest.NewServer(ph)
	defer server.Close()

	// Plain HTTP POST — not a WebSocket upgrade
	resp, err := http.Post(server.URL, "application/json", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusSwitchingProtocols {
		t.Errorf("expected non-WebSocket request to be rejected, got %d", resp.StatusCode)
	}
}
