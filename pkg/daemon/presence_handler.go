// SPDX-License-Identifier: GPL-2.0-only
package daemon

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// PresenceHandler handles WebSocket presence connections.
// On connect: creates an identity token and sends it as the first message.
// Keeps the connection alive with ping/pong. When the client disconnects
// (or pong times out), the user is marked offline.
type PresenceHandler struct {
	sessions     *SessionManager
	hub          *Hub
	pongTimeout  time.Duration
	pingInterval time.Duration
}

// NewPresenceHandler creates a new presence handler.
func NewPresenceHandler(sessions *SessionManager, hub *Hub, pongTimeout time.Duration) *PresenceHandler {
	return &PresenceHandler{
		sessions:     sessions,
		hub:          hub,
		pongTimeout:  pongTimeout,
		pingInterval: pongTimeout / 2,
	}
}

func (h *PresenceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	token := h.sessions.CreateIdentityToken()

	done, err := h.sessions.AttachPresence(token)
	if err != nil {
		return
	}
	defer h.sessions.DisconnectPresence(token)

	if err := conn.WriteMessage(websocket.TextMessage, []byte(token)); err != nil {
		return
	}

	// Pong handler resets read deadline (keepalive timeout)
	conn.SetReadDeadline(time.Now().Add(h.pongTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(h.pongTimeout))
		return nil
	})

	// Read loop in goroutine — processes pong frames and detects disconnect.
	// gorilla/websocket: one concurrent reader + one concurrent writer is safe.
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// Write loop: send pings periodically
	ticker := time.NewTicker(h.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			conn.SetWriteDeadline(time.Now().Add(h.pingInterval))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-readDone:
			return
		case <-done:
			return
		}
	}
}
