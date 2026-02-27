// SPDX-License-Identifier: GPL-2.0-only
package daemon

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/charmbracelet/log"

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

// BroadcastMessage sends a message.new event to all connected members of a channel.
func (h *Hub) BroadcastMessage(channelID int64, channelName string, channelType string, msg db.Message, mentions []string, threadID *int64, database *db.DB) {
	d := map[string]interface{}{
		"id":           msg.ID,
		"channel":      channelName,
		"channel_type": channelType,
		"from":         msg.Username,
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
		username string
		userID   int64
	}
	h.mu.RLock()
	targets := make([]target, 0, len(h.clients))
	for _, client := range h.clients {
		if client.username == msg.Username {
			continue // don't echo to sender
		}
		targets = append(targets, target{username: client.username, userID: client.userID})
	}
	h.mu.RUnlock()

	// Phase 2: check membership outside the lock so Register/Unregister
	// are never blocked by slow DB queries.
	t0 := time.Now()
	eligible := make(map[string]bool, len(targets))
	for _, t := range targets {
		isMember, err := database.IsChannelMember(channelID, t.userID)
		if err == nil && isMember {
			eligible[t.username] = true
		}
	}

	// Fire webhooks for mentions and DM recipients (non-blocking).
	if webhookURL, err := database.GetSetting("webhook_url"); err == nil && webhookURL != "" {
		seen := make(map[string]bool)
		var recipients []string

		// Add mentioned users (excluding sender).
		for _, m := range mentions {
			if m != msg.Username && !seen[m] {
				seen[m] = true
				recipients = append(recipients, m)
			}
		}

		// For DMs, look up channel members and add non-sender participants.
		if channelType == "dm" {
			if members, err := database.ChannelMemberUsernames(channelID); err == nil {
				for _, m := range members {
					if m != msg.Username && !seen[m] {
						seen[m] = true
						recipients = append(recipients, m)
					}
				}
			}
		}

		if len(recipients) > 0 {
			fireWebhooks(webhookURL, WebhookEvent{
				ChannelName: channelName,
				ChannelType: channelType,
				From:        msg.Username,
				MessageID:   msg.ID,
				SentAt:      msg.CreatedAt,
				Recipients:  recipients,
			})
		}
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
