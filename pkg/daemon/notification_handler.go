// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gorilla/websocket"

	auth "github.com/Work-Fort/Passport/go/service-auth"
	"github.com/Work-Fort/sharkfin/pkg/domain"
)

// notificationPayload is the JSON envelope sent to notification subscribers.
type notificationPayload struct {
	Title   string `json:"title"`
	Body    string `json:"body,omitempty"`
	Urgency string `json:"urgency"` // "passive" or "active"
	Route   string `json:"route,omitempty"`
}

// handleNotificationSubscribe upgrades to WebSocket and streams pre-classified
// notifications for the authenticated user. Mentions and DMs are "active"
// urgency; regular channel messages are "passive". Messages in channels the
// user hasn't joined are skipped.
func (h *WSHandler) handleNotificationSubscribe(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Auto-provision identity (same as /ws).
	role := identity.Type
	if role == "" {
		role = "user"
	}
	localIdentity, err := h.store.UpsertIdentity(identity.ID, identity.Username, identity.DisplayName, identity.Type, role)
	if err != nil {
		http.Error(w, "identity provisioning failed", http.StatusInternalServerError)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	username := localIdentity.Username
	identityID := localIdentity.ID
	pingInterval := h.pongTimeout / 2

	// Subscribe to message events from the event bus.
	sub := h.hub.bus.Subscribe(domain.EventMessageNew)
	defer sub.Close()

	log.Info("notifications: connect", "user", username)
	defer func() {
		log.Info("notifications: disconnect", "user", username)
	}()

	// Set up keepalive.
	conn.SetReadDeadline(time.Now().Add(h.pongTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(h.pongTimeout))
		return nil
	})

	// Drain reads in a goroutine so we can detect client disconnect.
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// Ping ticker to keep the connection alive.
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	eventCh := sub.Events()

	for {
		select {
		case evt, ok := <-eventCh:
			if !ok {
				return
			}
			msg, _ := evt.Payload.(domain.MessageEvent)
			if msg.From == username {
				continue // don't notify sender
			}

			notification := h.classifyNotification(msg, username, identityID)
			if notification == nil {
				continue
			}

			data, err := json.Marshal(notification)
			if err != nil {
				continue
			}
			conn.SetWriteDeadline(time.Now().Add(pingInterval))
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}

		case <-ticker.C:
			conn.SetWriteDeadline(time.Now().Add(pingInterval))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-readDone:
			return
		}
	}
}

// classifyNotification determines the notification type for a message event
// relative to the subscribing user. Returns nil if the user should not be
// notified (e.g. not a member of the channel).
func (h *WSHandler) classifyNotification(msg domain.MessageEvent, username, identityID string) *notificationPayload {
	body := truncate(msg.Body, 100)

	// Check if this user is mentioned.
	mentioned := false
	for _, m := range msg.Mentions {
		if m == username {
			mentioned = true
			break
		}
	}

	if mentioned {
		return &notificationPayload{
			Title:   fmt.Sprintf("Mention in #%s", msg.ChannelName),
			Body:    body,
			Urgency: "active",
			Route:   "/chat",
		}
	}

	// DM — always active urgency for the other participant.
	if msg.ChannelType == "dm" {
		// Verify the user is a member of this DM channel.
		ch, err := h.store.GetChannelByName(msg.ChannelName)
		if err != nil {
			return nil
		}
		isMember, err := h.store.IsChannelMember(ch.ID, identityID)
		if err != nil || !isMember {
			return nil
		}
		return &notificationPayload{
			Title:   fmt.Sprintf("DM from %s", msg.From),
			Body:    body,
			Urgency: "active",
			Route:   "/chat",
		}
	}

	// Regular channel message — check membership.
	ch, err := h.store.GetChannelByName(msg.ChannelName)
	if err != nil {
		return nil
	}
	isMember, err := h.store.IsChannelMember(ch.ID, identityID)
	if err != nil || !isMember {
		return nil // skip channels the user hasn't joined
	}

	return &notificationPayload{
		Title:   fmt.Sprintf("New message in #%s", msg.ChannelName),
		Urgency: "passive",
		Route:   "/chat",
	}
}

// truncate returns s truncated to at most maxLen runes.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}
