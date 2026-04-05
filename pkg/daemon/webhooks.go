// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/charmbracelet/log"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

// WebhookPayload is the JSON body POSTed to the webhook URL.
type WebhookPayload struct {
	Event       string  `json:"event"`
	ChannelID   int64   `json:"channel_id"`
	ChannelName string  `json:"channel_name"`
	ChannelType string  `json:"channel_type"`
	From        string  `json:"from"`
	FromType    string  `json:"from_type"`
	MessageID   int64   `json:"message_id"`
	Body        string  `json:"body"`
	Metadata    *string `json:"metadata"`
	SentAt      string  `json:"sent_at"`

	// Legacy fields — kept for global webhook_url backwards compatibility.
	Recipient string `json:"recipient,omitempty"`
	Channel   string `json:"channel,omitempty"`
}

// WebhookEvent contains the data needed to fire webhooks for a message.
type WebhookEvent struct {
	ChannelID   int64
	ChannelName string
	ChannelType string
	From        string
	FromType    string // identity type of sender
	Body        string
	Metadata    *string
	MessageID   int64
	SentAt      time.Time
	Recipients  []string // for legacy global webhook
}

var webhookClient = &http.Client{Timeout: 5 * time.Second}

// fireWebhooks POSTs a notification to webhookURL for each recipient.
// Each POST runs in its own goroutine. Failures are logged and ignored.
func fireWebhooks(webhookURL string, evt WebhookEvent) {
	for _, recipient := range evt.Recipients {
		payload := WebhookPayload{
			Event:       "message.new",
			Recipient:   recipient,
			Channel:     evt.ChannelName,
			ChannelType: evt.ChannelType,
			From:        evt.From,
			MessageID:   evt.MessageID,
			SentAt:      evt.SentAt.UTC().Format(time.RFC3339),
		}
		go func() {
			body, err := json.Marshal(payload)
			if err != nil {
				log.Error("webhook: marshal", "err", err)
				return
			}
			resp, err := webhookClient.Post(webhookURL, "application/json", bytes.NewReader(body))
			if err != nil {
				log.Warn("webhook: post failed", "recipient", payload.Recipient, "err", err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode >= 400 {
				log.Warn("webhook: bad status", "recipient", payload.Recipient, "status", resp.StatusCode)
			}
		}()
	}
}

// firePerIdentityWebhook POSTs a per-identity webhook payload.
// Runs in its own goroutine. Failures are logged and ignored.
func firePerIdentityWebhook(hook domain.IdentityWebhook, payload WebhookPayload) {
	go func() {
		body, err := json.Marshal(payload)
		if err != nil {
			log.Error("webhook: marshal per-identity payload", "err", err)
			return
		}
		resp, err := webhookClient.Post(hook.URL, "application/json", bytes.NewReader(body))
		if err != nil {
			log.Warn("webhook: per-identity post failed", "identity_id", hook.IdentityID, "url", hook.URL, "err", err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			log.Warn("webhook: per-identity bad status", "identity_id", hook.IdentityID, "status", resp.StatusCode)
		}
	}()
}

// computeRecipients returns the list of users who should be notified:
// mentioned users + DM members + service channel members, minus the sender.
// channelID must already be resolved by the caller to avoid a redundant lookup.
func computeRecipients(msg domain.MessageEvent, channelID int64, store domain.Store) []string {
	seen := make(map[string]bool)
	var recipients []string

	// Mentioned users
	for _, m := range msg.Mentions {
		if m != msg.From && !seen[m] {
			seen[m] = true
			recipients = append(recipients, m)
		}
	}

	// DM participants — fetch member list once and reuse for both DM and service scan.
	if msg.ChannelType == "dm" {
		if members, err := store.ChannelMemberUsernames(channelID); err == nil {
			for _, m := range members {
				if m != msg.From && !seen[m] {
					seen[m] = true
					recipients = append(recipients, m)
				}
			}
		}
	}

	// Service members of any channel type — single SQL JOIN, no N+1.
	if serviceMembers, err := store.GetServiceMemberUsernames(channelID); err == nil {
		for _, m := range serviceMembers {
			if m != msg.From && !seen[m] {
				seen[m] = true
				recipients = append(recipients, m)
			}
		}
	}

	return recipients
}

// WebhookSubscriber listens for message events and fires webhooks.
type WebhookSubscriber struct {
	store domain.Store
	sub   domain.Subscription
}

// NewWebhookSubscriber creates a subscriber that fires webhooks on new messages.
func NewWebhookSubscriber(bus domain.EventBus, store domain.Store) *WebhookSubscriber {
	ws := &WebhookSubscriber{
		store: store,
		sub:   bus.Subscribe(domain.EventMessageNew),
	}
	go ws.run()
	return ws
}

func (ws *WebhookSubscriber) run() {
	for evt := range ws.sub.Events() {
		msg := evt.Payload.(domain.MessageEvent)
		ws.handleMessage(msg)
	}
}

func (ws *WebhookSubscriber) handleMessage(msg domain.MessageEvent) {
	// Resolve channel once; used by both the legacy and per-identity paths.
	ch, err := ws.store.GetChannelByName(msg.ChannelName)
	if err != nil {
		return
	}

	// 1. Legacy global webhook
	webhookURL, err := ws.store.GetSetting("webhook_url")
	if err == nil && webhookURL != "" {
		recipients := computeRecipients(msg, ch.ID, ws.store)
		if len(recipients) > 0 {
			fireWebhooks(webhookURL, WebhookEvent{
				ChannelName: msg.ChannelName,
				ChannelType: msg.ChannelType,
				From:        msg.From,
				MessageID:   msg.MessageID,
				SentAt:      msg.SentAt,
				Recipients:  recipients,
			})
		}
	}

	// 2. Per-identity webhooks for all service members of the channel.
	hooks, err := ws.store.GetWebhooksForChannel(ch.ID)
	if err != nil {
		log.Warn("webhook: get channel hooks", "channel", msg.ChannelName, "err", err)
		return
	}

	// Lookup sender identity type.
	senderIdent, err := ws.store.GetIdentityByUsername(msg.From)
	fromType := "user"
	if err == nil {
		fromType = senderIdent.Type
	}

	payload := WebhookPayload{
		Event:       "message.new",
		ChannelID:   ch.ID,
		ChannelName: msg.ChannelName,
		ChannelType: msg.ChannelType,
		From:        msg.From,
		FromType:    fromType,
		MessageID:   msg.MessageID,
		Body:        msg.Body,
		Metadata:    msg.Metadata,
		SentAt:      msg.SentAt.UTC().Format(time.RFC3339),
	}

	for _, hook := range hooks {
		firePerIdentityWebhook(hook, payload)
	}
}

// Close stops the webhook subscriber.
func (ws *WebhookSubscriber) Close() {
	ws.sub.Close()
}
