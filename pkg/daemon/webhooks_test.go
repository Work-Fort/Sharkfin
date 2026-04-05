// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Work-Fort/sharkfin/pkg/domain"
	"github.com/Work-Fort/sharkfin/pkg/infra/sqlite"
)

// testStore creates an in-memory SQLite store for tests.
func testStore(t *testing.T) domain.Store {
	t.Helper()
	store, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

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

func TestWebhookSubscriberSendsOnMention(t *testing.T) {
	var received []WebhookPayload
	var mu sync.Mutex
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p WebhookPayload
		json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		received = append(received, p)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	store := testStore(t)
	store.SetSetting("webhook_url", ts.URL)

	bus := domain.NewEventBus()
	sub := NewWebhookSubscriber(bus, store)
	defer sub.Close()

	bus.Publish(domain.Event{
		Type: domain.EventMessageNew,
		Payload: domain.MessageEvent{
			ChannelName: "general",
			ChannelType: "channel",
			From:        "alice",
			MessageID:   1,
			SentAt:      time.Now(),
			Mentions:    []string{"bob"},
		},
	})

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) == 1
	}, 2*time.Second, 50*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "general", received[0].Channel)
	assert.Equal(t, "bob", received[0].Recipient)
	assert.Equal(t, "alice", received[0].From)
}

func TestWebhookSubscriberNoWebhookURL(t *testing.T) {
	store := testStore(t)
	// No webhook_url set

	bus := domain.NewEventBus()
	sub := NewWebhookSubscriber(bus, store)
	defer sub.Close()

	bus.Publish(domain.Event{
		Type: domain.EventMessageNew,
		Payload: domain.MessageEvent{
			From:     "alice",
			Mentions: []string{"bob"},
		},
	})

	// Give it time to process — should not panic
	time.Sleep(100 * time.Millisecond)
}

func TestFirePerIdentityWebhooks_PostsToRegisteredURL(t *testing.T) {
	var mu sync.Mutex
	var received []map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]interface{}
		json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		received = append(received, p)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hook := domain.IdentityWebhook{
		ID:         "hook-1",
		IdentityID: "svc-id-1",
		URL:        srv.URL,
		Secret:     "",
		Active:     true,
	}

	firePerIdentityWebhook(hook, WebhookPayload{
		Event:       "message.new",
		ChannelID:   1,
		Channel:     "general",
		ChannelType: "channel",
		From:        "alice",
		FromType:    "user",
		MessageID:   42,
		Body:        "hello",
		SentAt:      "2026-04-05T00:00:00Z",
	})

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 POST, got %d", len(received))
	}
	if received[0]["event"] != "message.new" {
		t.Errorf("unexpected event: %v", received[0]["event"])
	}
}

func TestWebhookSubscriberExcludesSender(t *testing.T) {
	var received []WebhookPayload
	var mu sync.Mutex
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p WebhookPayload
		json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		received = append(received, p)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	store := testStore(t)
	store.SetSetting("webhook_url", ts.URL)

	bus := domain.NewEventBus()
	sub := NewWebhookSubscriber(bus, store)
	defer sub.Close()

	// Alice mentions herself — should not generate a webhook
	bus.Publish(domain.Event{
		Type: domain.EventMessageNew,
		Payload: domain.MessageEvent{
			ChannelName: "general",
			ChannelType: "channel",
			From:        "alice",
			MessageID:   1,
			SentAt:      time.Now(),
			Mentions:    []string{"alice"},
		},
	})

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, received, "sender should not receive webhook for self-mention")
}
