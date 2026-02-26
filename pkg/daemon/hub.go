// SPDX-License-Identifier: GPL-2.0-only
package daemon

import (
	"encoding/json"
	"sync"

	"github.com/Work-Fort/sharkfin/pkg/db"
)

// Hub manages connected WebSocket clients and broadcasts events.
type Hub struct {
	mu      sync.RWMutex
	clients map[string]*WSClient // username → client
}

// WSClient represents a connected WebSocket client.
type WSClient struct {
	username string
	userID   int64
	send     chan []byte
	hub      *Hub
}

// NewHub creates a new hub.
func NewHub() *Hub {
	return &Hub{
		clients: make(map[string]*WSClient),
	}
}

// Register adds a client to the hub.
func (h *Hub) Register(client *WSClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[client.username] = client
}

// Unregister removes a client from the hub.
func (h *Hub) Unregister(client *WSClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.clients[client.username]; ok && c == client {
		delete(h.clients, client.username)
		close(client.send)
	}
}

// BroadcastMessage sends a message.new event to all connected members of a channel.
func (h *Hub) BroadcastMessage(channelID int64, channelName string, msg db.Message, database *db.DB) {
	event := wsEnvelope{
		Type: "message.new",
		D: map[string]interface{}{
			"id":      msg.ID,
			"channel": channelName,
			"from":    msg.Username,
			"body":    msg.Body,
			"sent_at": msg.CreatedAt.Format("2006-01-02T15:04:05Z"),
		},
	}
	data, _ := json.Marshal(event)

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		if client.username == msg.Username {
			continue // don't echo to sender
		}
		isMember, err := database.IsChannelMember(channelID, client.userID)
		if err != nil || !isMember {
			continue
		}
		select {
		case client.send <- data:
		default:
			// client send buffer full, skip
		}
	}
}

// BroadcastPresence sends a presence event to all connected clients.
func (h *Hub) BroadcastPresence(username string, online bool) {
	event := wsEnvelope{
		Type: "presence",
		D: map[string]interface{}{
			"username": username,
			"online":   online,
		},
	}
	data, _ := json.Marshal(event)

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		if client.username == username {
			continue // don't notify self
		}
		select {
		case client.send <- data:
		default:
		}
	}
}

// wsEnvelope is the JSON envelope for WebSocket messages.
type wsEnvelope struct {
	Type string      `json:"type"`
	D    interface{} `json:"d,omitempty"`
	Ref  string      `json:"ref,omitempty"`
	OK   *bool       `json:"ok,omitempty"`
}
