// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/charmbracelet/log"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

// Hub manages connected WebSocket clients and broadcasts events.
type Hub struct {
	mu      sync.RWMutex
	clients map[string]*WSClient // username → client
	states  map[string]string    // username → "active" or "idle"
	bus     domain.EventBus
}

// WSClient represents a connected WebSocket client.
type WSClient struct {
	username   string
	identityID string
	send       chan []byte
	hub        *Hub
}

// NewHub creates a new hub.
func NewHub(bus domain.EventBus) *Hub {
	return &Hub{
		clients: make(map[string]*WSClient),
		states:  make(map[string]string),
		bus:     bus,
	}
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
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

// SetState sets the active/idle state for a user.
func (h *Hub) SetState(username, state string) {
	h.mu.Lock()
	h.states[username] = state
	h.mu.Unlock()
}

// GetState returns the active/idle state for a user.
func (h *Hub) GetState(username string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.states[username]
}

// ClearState removes the state entry for a user.
func (h *Hub) ClearState(username string) {
	h.mu.Lock()
	delete(h.states, username)
	h.mu.Unlock()
}

// BroadcastMessage sends a message.new event to all connected members of a channel.
func (h *Hub) BroadcastMessage(channelID int64, channelName string, channelType string, msg domain.Message, mentions []string, threadID *int64, store domain.Store) {
	d := map[string]interface{}{
		"id":           msg.ID,
		"channel":      channelName,
		"channel_type": channelType,
		"from":         msg.From,
		"body":         msg.Body,
		"sent_at":      msg.CreatedAt.UTC().Format(time.RFC3339),
	}
	if threadID != nil {
		d["thread_id"] = *threadID
	}
	if len(mentions) > 0 {
		d["mentions"] = mentions
	}
	event := wsEnvelope{
		Type: "message.new",
		D:    d,
	}
	data, _ := json.Marshal(event)

	// Phase 1: snapshot client IDs under lock (fast, no DB calls).
	type target struct {
		username   string
		identityID string
	}
	h.mu.RLock()
	targets := make([]target, 0, len(h.clients))
	for _, client := range h.clients {
		targets = append(targets, target{username: client.username, identityID: client.identityID})
	}
	h.mu.RUnlock()

	// Phase 2: check membership outside the lock so Register/Unregister
	// are never blocked by slow DB queries.
	t0 := time.Now()
	eligible := make(map[string]bool, len(targets))
	for _, t := range targets {
		isMember, err := store.IsChannelMember(channelID, t.identityID)
		if err == nil && isMember {
			eligible[t.username] = true
		}
	}

	// Publish event for subscribers (webhooks, presence notifications, etc.).
	if h.bus != nil {
		h.bus.Publish(domain.Event{
			Type: domain.EventMessageNew,
			Payload: domain.MessageEvent{
				ChannelName: channelName,
				ChannelType: channelType,
				From:        msg.From,
				Body:        msg.Body,
				MessageID:   msg.ID,
				SentAt:      msg.CreatedAt,
				Mentions:    mentions,
				ThreadID:    threadID,
			},
		})
	}

	// Phase 3: send under RLock so we never race with Unregister closing
	// the send channel.
	sent := 0
	h.mu.RLock()
	for _, client := range h.clients {
		if !eligible[client.username] {
			continue
		}
		select {
		case client.send <- data:
			sent++
		default:
			// client send buffer full, skip
		}
	}
	h.mu.RUnlock()

	elapsed := time.Since(t0)
	if elapsed > 50*time.Millisecond {
		log.Warn("hub: slow broadcast", "channel", channelName, "targets", len(targets), "sent", sent, "elapsed", elapsed)
	} else {
		log.Debug("hub: broadcast", "channel", channelName, "targets", len(targets), "sent", sent, "elapsed", elapsed)
	}
}

// BroadcastToRole sends a pre-encoded event to all connected clients whose identity has the given role.
func (h *Hub) BroadcastToRole(role string, data []byte, store domain.Store) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, client := range h.clients {
		identity, err := store.GetIdentityByUsername(client.username)
		if err != nil || identity.Role != role {
			continue
		}
		select {
		case client.send <- data:
		default:
		}
	}
}

// BroadcastPresence sends a presence event to all connected clients.
func (h *Hub) BroadcastPresence(username string, online bool, state string) {
	d := map[string]interface{}{
		"username": username,
	}
	if online {
		d["status"] = "online"
		d["state"] = state
	} else {
		d["status"] = "offline"
	}
	event := wsEnvelope{Type: "presence", D: d}
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
