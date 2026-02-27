// SPDX-License-Identifier: GPL-2.0-only
package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/charmbracelet/log"
)

// WebhookPayload is the JSON body POSTed to the webhook URL.
type WebhookPayload struct {
	Event       string `json:"event"`
	Recipient   string `json:"recipient"`
	Channel     string `json:"channel"`
	ChannelType string `json:"channel_type"`
	From        string `json:"from"`
	MessageID   int64  `json:"message_id"`
	SentAt      string `json:"sent_at"`
}

// WebhookEvent contains the data needed to fire webhooks for a message.
type WebhookEvent struct {
	ChannelName string
	ChannelType string
	From        string
	MessageID   int64
	SentAt      time.Time
	Recipients  []string
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
