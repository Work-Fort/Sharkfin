// SPDX-License-Identifier: GPL-2.0-only
package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestFireWebhooks_SendsPayloadPerRecipient(t *testing.T) {
	var mu sync.Mutex
	var received []WebhookPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p WebhookPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Errorf("decode payload: %v", err)
			return
		}
		mu.Lock()
		received = append(received, p)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sentAt := time.Date(2026, 2, 27, 12, 0, 0, 0, time.UTC)
	fireWebhooks(srv.URL, WebhookEvent{
		ChannelName: "chat-ux",
		ChannelType: "channel",
		From:        "alice",
		MessageID:   42,
		SentAt:      sentAt,
		Recipients:  []string{"bob", "carol"},
	})

	// Wait for goroutines to complete.
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("expected 2 payloads, got %d", len(received))
	}

	byRecipient := make(map[string]WebhookPayload)
	for _, p := range received {
		byRecipient[p.Recipient] = p
	}

	for _, name := range []string{"bob", "carol"} {
		p, ok := byRecipient[name]
		if !ok {
			t.Errorf("missing payload for %s", name)
			continue
		}
		if p.Event != "message.new" {
			t.Errorf("%s: event = %q, want %q", name, p.Event, "message.new")
		}
		if p.Channel != "chat-ux" {
			t.Errorf("%s: channel = %q, want %q", name, p.Channel, "chat-ux")
		}
		if p.ChannelType != "channel" {
			t.Errorf("%s: channel_type = %q, want %q", name, p.ChannelType, "channel")
		}
		if p.From != "alice" {
			t.Errorf("%s: from = %q, want %q", name, p.From, "alice")
		}
		if p.MessageID != 42 {
			t.Errorf("%s: message_id = %d, want %d", name, p.MessageID, 42)
		}
		if p.SentAt != "2026-02-27T12:00:00Z" {
			t.Errorf("%s: sent_at = %q, want %q", name, p.SentAt, "2026-02-27T12:00:00Z")
		}
	}
}

func TestFireWebhooks_NoRecipientsNoRequests(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fireWebhooks(srv.URL, WebhookEvent{
		ChannelName: "chat-ux",
		ChannelType: "channel",
		From:        "alice",
		MessageID:   1,
		SentAt:      time.Now(),
		Recipients:  nil,
	})

	time.Sleep(200 * time.Millisecond)

	if called {
		t.Error("expected no HTTP calls for empty recipients")
	}
}

func TestFireWebhooks_ServerDown(t *testing.T) {
	// Use a closed server to simulate unreachable endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	// Should not panic.
	fireWebhooks(srv.URL, WebhookEvent{
		ChannelName: "chat-ux",
		ChannelType: "channel",
		From:        "alice",
		MessageID:   1,
		SentAt:      time.Now(),
		Recipients:  []string{"bob"},
	})

	time.Sleep(200 * time.Millisecond)
}
