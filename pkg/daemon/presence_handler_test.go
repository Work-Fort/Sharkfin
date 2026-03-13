// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	auth "github.com/Work-Fort/Passport/go/service-auth"
)

// wsURL converts an httptest.Server URL to a WebSocket URL with path.
func wsURL(server *httptest.Server) string {
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

// wrapWithIdentity wraps a handler to inject a Passport identity into the request context.
func wrapWithIdentity(h http.Handler, username string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := auth.Identity{ID: "uuid-" + username, Username: username, DisplayName: username, Type: "user"}
		ctx := auth.ContextWithIdentity(r.Context(), identity)
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func TestPresenceHoldsConnection(t *testing.T) {
	ph := NewPresenceHandler(20 * time.Second)

	server := httptest.NewServer(wrapWithIdentity(ph, "alice"))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Verify presence is attached
	time.Sleep(50 * time.Millisecond)
	if !ph.IsOnline("alice") {
		t.Error("alice should be online")
	}
}

func TestPresenceDisconnectGoesOffline(t *testing.T) {
	ph := NewPresenceHandler(20 * time.Second)

	server := httptest.NewServer(wrapWithIdentity(ph, "alice"))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Wait for connection to be established
	time.Sleep(50 * time.Millisecond)
	if !ph.IsOnline("alice") {
		t.Error("alice should be online")
	}

	// Disconnect by closing WebSocket
	conn.Close()
	time.Sleep(100 * time.Millisecond)

	if ph.IsOnline("alice") {
		t.Error("alice should be offline after disconnect")
	}
}

func TestPresenceRejectsNonWebSocket(t *testing.T) {
	ph := NewPresenceHandler(20 * time.Second)

	server := httptest.NewServer(wrapWithIdentity(ph, "alice"))
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

func TestPresenceRejectsUnauthenticated(t *testing.T) {
	ph := NewPresenceHandler(20 * time.Second)

	// No identity wrapper — should get 401
	server := httptest.NewServer(ph)
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}
