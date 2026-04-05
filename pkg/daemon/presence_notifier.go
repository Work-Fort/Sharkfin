// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"encoding/json"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

type PresenceNotifier struct {
	presence *PresenceHandler
	store    domain.Store
	sub      domain.Subscription
}

func NewPresenceNotifier(bus domain.EventBus, presence *PresenceHandler, store domain.Store) *PresenceNotifier {
	pn := &PresenceNotifier{
		presence: presence,
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
	ch, err := pn.store.GetChannelByName(msg.ChannelName)
	if err != nil {
		return
	}
	recipients := computeRecipients(msg, ch.ID, pn.store)

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
		pn.presence.SendNotification(username, envelope)
	}
}

func (pn *PresenceNotifier) Close() {
	pn.sub.Close()
}
