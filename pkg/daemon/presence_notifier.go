// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"encoding/json"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

// PresenceNotifier subscribes to message events and pushes notifications
// to users' presence WebSocket connections.
type PresenceNotifier struct {
	sessions *SessionManager
	store    domain.Store
	sub      domain.Subscription
}

// NewPresenceNotifier creates a notifier that sends message notifications
// to connected presence WebSocket clients.
func NewPresenceNotifier(bus domain.EventBus, sessions *SessionManager, store domain.Store) *PresenceNotifier {
	pn := &PresenceNotifier{
		sessions: sessions,
		store:    store,
		sub:      bus.Subscribe(domain.EventMessageNew),
	}
	go pn.run()
	return pn
}

func (pn *PresenceNotifier) run() {
	for evt := range pn.sub.Events() {
		msg := evt.Payload.(domain.MessageEvent)
		pn.handleMessage(msg)
	}
}

func (pn *PresenceNotifier) handleMessage(msg domain.MessageEvent) {
	recipients := computeRecipients(msg, pn.store)

	envelope, _ := json.Marshal(map[string]any{
		"type": "message.new",
		"d": map[string]any{
			"channel":      msg.ChannelName,
			"channel_type": msg.ChannelType,
			"from":         msg.From,
			"message_id":   msg.MessageID,
		},
	})

	for _, username := range recipients {
		pn.sessions.SendNotification(username, envelope)
	}
}

// Close stops the presence notifier.
func (pn *PresenceNotifier) Close() {
	pn.sub.Close()
}
