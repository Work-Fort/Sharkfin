# Webhook Notifications Implementation Plan

**Goal:** Fire async webhook notifications when users receive @mentions or DMs, enabling external systems (Nexus) to auto-invoke Claude CLI in response.

**Architecture:** A single global `webhook_url` stored in the existing `settings` table. On each `message.new` broadcast, if the setting is configured, sharkfin POSTs a lightweight notification payload (no message body) to that URL for each mentioned user and each DM recipient. Fire-and-forget via goroutine with a 5-second timeout. Never blocks broadcast.

**Tech Stack:** Go `net/http` client, existing `settings` table, existing `Hub.BroadcastMessage` injection point.

---

## Task 1: Add `fireWebhooks` function to hub

**Files:**
- Create: `pkg/daemon/webhooks.go`
- Test: `pkg/daemon/webhooks_test.go`

### Step 1: Write the failing test

```go
// webhooks_test.go
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
		json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		received = append(received, p)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fireWebhooks(srv.URL, WebhookEvent{
		ChannelName: "chat-ux",
		ChannelType: "channel",
		From:        "alice",
		MessageID:   42,
		SentAt:      time.Date(2026, 2, 27, 12, 0, 0, 0, time.UTC),
		Recipients:  []string{"bob", "carol"},
	})

	// Wait for goroutines to complete
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("expected 2 payloads, got %d", len(received))
	}
	if received[0].Recipient != "bob" || received[1].Recipient != "carol" {
		t.Errorf("unexpected recipients: %v, %v", received[0].Recipient, received[1].Recipient)
	}
	if received[0].Event != "message.new" {
		t.Errorf("unexpected event: %s", received[0].Event)
	}
}
```

### Step 2: Run test to verify it fails

Run: `go test ./pkg/daemon/ -run TestFireWebhooks_SendsPayloadPerRecipient -v -count=1`
Expected: FAIL — `WebhookPayload` and `fireWebhooks` undefined.

### Step 3: Write minimal implementation

```go
// webhooks.go
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
```

### Step 4: Run test to verify it passes

Run: `go test ./pkg/daemon/ -run TestFireWebhooks -v -count=1`
Expected: PASS

### Step 5: Write additional tests

Add tests for:
- `TestFireWebhooks_NoRecipientsNoRequests` — empty recipients slice makes zero HTTP calls.
- `TestFireWebhooks_ServerDown` — unreachable URL logs warning, does not panic.
- `TestFireWebhooks_EmptyURL` — empty webhookURL is a no-op (caller guards this).

### Step 6: Run all webhook tests

Run: `go test ./pkg/daemon/ -run TestFireWebhooks -v -count=1`
Expected: All PASS

### Step 7: Commit

```bash
git add pkg/daemon/webhooks.go pkg/daemon/webhooks_test.go
git commit -m "feat: add fireWebhooks for async webhook notifications"
```

---

## Task 2: Wire webhooks into Hub.BroadcastMessage

**Files:**
- Modify: `pkg/daemon/hub.go:60` (`BroadcastMessage` function)

### Step 1: Write the failing test

```go
// webhooks_test.go — add integration test
func TestBroadcastMessage_FiresWebhooks(t *testing.T) {
	// This test verifies the integration point.
	// Set up a test HTTP server, configure webhook_url setting,
	// call BroadcastMessage with mentions, verify webhook received.
	// (Detailed in implementation — requires test DB setup.)
}
```

> Note: This is an integration test that exercises the full path. It may be
> easier to verify via e2e tests. The unit test for `fireWebhooks` already
> covers the HTTP posting logic. The wiring is a small, reviewable change.

### Step 2: Modify BroadcastMessage

Add webhook firing after Phase 2 (DB queries complete, mentions resolved) and before Phase 3 (send to WS clients). The webhook call is non-blocking.

In `hub.go`, `BroadcastMessage`:
1. Add `database` parameter usage to read `webhook_url` setting.
2. Build the recipient list: mentioned users + DM participants (excluding sender).
3. Call `fireWebhooks` if `webhook_url` is configured.

```go
// After Phase 2 membership checks, before Phase 3 WS send:

// Fire webhooks for mentions and DM recipients.
if webhookURL, err := database.GetSetting("webhook_url"); err == nil && webhookURL != "" {
	var recipients []string

	// Add mentioned users (excluding sender)
	for _, m := range mentions {
		if m != msg.Username {
			recipients = append(recipients, m)
		}
	}

	// For DMs, add all channel members except sender
	if channelType == "dm" {
		for _, t := range targets {
			if t.username != msg.Username {
				// Deduplicate with mentions
				alreadyAdded := false
				for _, r := range recipients {
					if r == t.username {
						alreadyAdded = true
						break
					}
				}
				if !alreadyAdded {
					recipients = append(recipients, t.username)
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
```

### Step 3: Run unit tests

Run: `mise run test`
Expected: All PASS

### Step 4: Commit

```bash
git add pkg/daemon/hub.go
git commit -m "feat: wire webhook notifications into BroadcastMessage"
```

---

## Task 3: Add `--webhook-url` startup flag

**Files:**
- Modify: `cmd/sharkfind/main.go` (or wherever viper/cobra config is set up)

### Step 1: Identify config location

Check `cmd/sharkfind/main.go` for existing flag pattern.

### Step 2: Add `--webhook-url` flag

Add a string flag that, when set, writes the value to the `webhook_url` setting on startup. This provides a convenient way to configure the webhook without needing the WS `set_setting` command.

```go
webhookURL := flag.String("webhook-url", "", "URL to POST webhook notifications to on mentions and DMs")
// ... after DB init:
if *webhookURL != "" {
    database.SetSetting("webhook_url", *webhookURL)
}
```

### Step 3: Run tests

Run: `mise run test`
Expected: PASS

### Step 4: Commit

```bash
git add cmd/sharkfind/main.go
git commit -m "feat: add --webhook-url startup flag"
```

---

## Task 4: E2E test

**Files:**
- Modify: `tests/e2e/sharkfin_test.go`

### Step 1: Write e2e test

Start a daemon with `--webhook-url` pointing to a local httptest server. Register two users via MCP. Have user A send a message mentioning user B. Verify the test server receives a webhook POST with the correct payload.

```go
func TestWebhookOnMention(t *testing.T) {
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

	// Start daemon with --webhook-url=srv.URL
	// Register alice and bob via MCP
	// Alice sends: "Hey @bob check this"
	// Wait briefly for async webhook
	// Assert: received has 1 payload with recipient=bob, from=alice

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(received))
	}
	if received[0]["recipient"] != "bob" {
		t.Errorf("expected recipient bob, got %v", received[0]["recipient"])
	}
}
```

### Step 2: Write e2e test for DM webhooks

Similar pattern but with DM channel — open DM, send message, verify webhook fires for the DM recipient.

### Step 3: Run e2e tests

Run: `mise run e2e`
Expected: All PASS

### Step 4: Commit

```bash
git add tests/e2e/sharkfin_test.go
git commit -m "test: add e2e tests for webhook notifications"
```

---

## Summary

| Task | Description | Files |
|------|-------------|-------|
| 1 | `fireWebhooks` function + unit tests | `webhooks.go`, `webhooks_test.go` |
| 2 | Wire into `BroadcastMessage` | `hub.go` |
| 3 | `--webhook-url` startup flag | `main.go` |
| 4 | E2E tests | `sharkfin_test.go` |

**Webhook payload** (no message body — Claude reads messages via MCP):
```json
{
  "event": "message.new",
  "recipient": "nexus-team-lead",
  "channel": "chat-ux",
  "channel_type": "channel",
  "from": "tpm",
  "message_id": 123,
  "sent_at": "2026-02-27T21:15:16Z"
}
```

**Trigger conditions:** @mentions + DM messages (not every message in every channel).

**Fire-and-forget:** Goroutine per recipient, 5s HTTP timeout, log on failure, never block broadcast.

**Configuration:** Global `webhook_url` in `settings` table. Set via WS `set_setting` command or `--webhook-url` startup flag.
