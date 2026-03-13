// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	auth "github.com/Work-Fort/Passport/go/service-auth"
)

type PresenceHandler struct {
	pongTimeout  time.Duration
	pingInterval time.Duration

	mu    sync.RWMutex
	conns map[string]*presenceConn // username → connection
}

type presenceConn struct {
	conn *websocket.Conn
	mu   sync.Mutex // serializes writes
}

func NewPresenceHandler(pongTimeout time.Duration) *PresenceHandler {
	return &PresenceHandler{
		pongTimeout:  pongTimeout,
		pingInterval: pongTimeout / 2,
		conns:        make(map[string]*presenceConn),
	}
}

func (h *PresenceHandler) SendNotification(username string, data []byte) error {
	h.mu.RLock()
	pc, ok := h.conns[username]
	h.mu.RUnlock()
	if !ok {
		return nil
	}
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return pc.conn.WriteMessage(websocket.TextMessage, data)
}

func (h *PresenceHandler) IsOnline(username string) bool {
	h.mu.RLock()
	_, ok := h.conns[username]
	h.mu.RUnlock()
	return ok
}

func (h *PresenceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	username := identity.Username
	pc := &presenceConn{conn: conn}

	h.mu.Lock()
	h.conns[username] = pc
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		if h.conns[username] == pc {
			delete(h.conns, username)
		}
		h.mu.Unlock()
	}()

	conn.SetReadDeadline(time.Now().Add(h.pongTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(h.pongTimeout))
		return nil
	})

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(h.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pc.mu.Lock()
			pc.conn.SetWriteDeadline(time.Now().Add(h.pingInterval))
			err := pc.conn.WriteMessage(websocket.PingMessage, nil)
			pc.mu.Unlock()
			if err != nil {
				return
			}
		case <-readDone:
			return
		}
	}
}
