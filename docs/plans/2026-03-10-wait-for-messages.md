# wait_for_messages Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a domain-level event bus, refactor webhooks into a subscriber, add presence notifications, and implement `wait_for_messages` in the MCP bridge.

**Architecture:** In-process channel-based EventBus in `pkg/domain/`. Hub publishes `message.new` events. WebhookSubscriber and PresenceNotifier consume them independently. Bridge intercepts `wait_for_messages` tool calls client-side, blocking on presence WS notifications.

**Tech Stack:** Go channels, gorilla/websocket, existing MCP/bridge infrastructure

**Design:** [docs/2026-03-10-wait-for-messages-design.md](../2026-03-10-wait-for-messages-design.md)

---

### Task 1: Domain Event Bus Interface and Implementation

**Files:**
- Modify: `pkg/domain/ports.go`
- Modify: `pkg/domain/types.go`
- Create: `pkg/domain/eventbus.go`
- Create: `pkg/domain/eventbus_test.go`

**Step 1: Add event types to `pkg/domain/types.go`**

Append after the existing type definitions:

```go
// Event type constants.
const (
	EventMessageNew     = "message.new"
	EventPresenceUpdate = "presence.update"
)

// MessageEvent is the payload for EventMessageNew.
type MessageEvent struct {
	ChannelName string
	ChannelType string // "channel" or "dm"
	From        string
	MessageID   int64
	SentAt      time.Time
	Mentions    []string
	ThreadID    *int64
}
```

**Step 2: Add EventBus interfaces to `pkg/domain/ports.go`**

Append after the existing interface definitions:

```go
// Event is a typed message published to the event bus.
type Event struct {
	Type    string
	Payload any
}

// Subscription receives events from the bus.
type Subscription interface {
	Events() <-chan Event
	Close()
}

// EventBus is an in-process pub/sub system for domain events.
type EventBus interface {
	Publish(event Event)
	Subscribe(eventTypes ...string) Subscription
}
```

**Step 3: Write tests in `pkg/domain/eventbus_test.go`**

```go
package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sharkfin/pkg/domain"
)

func TestPublishSubscribe(t *testing.T) {
	bus := domain.NewEventBus()
	sub := bus.Subscribe(domain.EventMessageNew)
	defer sub.Close()

	bus.Publish(domain.Event{
		Type:    domain.EventMessageNew,
		Payload: domain.MessageEvent{From: "alice", ChannelName: "general"},
	})

	select {
	case evt := <-sub.Events():
		assert.Equal(t, domain.EventMessageNew, evt.Type)
		msg := evt.Payload.(domain.MessageEvent)
		assert.Equal(t, "alice", msg.From)
		assert.Equal(t, "general", msg.ChannelName)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestSubscribeFiltersByType(t *testing.T) {
	bus := domain.NewEventBus()
	sub := bus.Subscribe(domain.EventPresenceUpdate)
	defer sub.Close()

	bus.Publish(domain.Event{
		Type:    domain.EventMessageNew,
		Payload: domain.MessageEvent{From: "alice"},
	})

	select {
	case <-sub.Events():
		t.Fatal("should not receive non-matching event type")
	case <-time.After(50 * time.Millisecond):
		// expected: no event received
	}
}

func TestSubscribeAllTypes(t *testing.T) {
	bus := domain.NewEventBus()
	sub := bus.Subscribe() // no type args = all events
	defer sub.Close()

	bus.Publish(domain.Event{Type: domain.EventMessageNew, Payload: "msg"})
	bus.Publish(domain.Event{Type: domain.EventPresenceUpdate, Payload: "pres"})

	for i := 0; i < 2; i++ {
		select {
		case <-sub.Events():
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for event %d", i)
		}
	}
}

func TestPublishDropsWhenBufferFull(t *testing.T) {
	bus := domain.NewEventBus()
	sub := bus.Subscribe(domain.EventMessageNew)
	defer sub.Close()

	// Publish more events than buffer size (64) without reading
	for i := 0; i < 100; i++ {
		bus.Publish(domain.Event{
			Type:    domain.EventMessageNew,
			Payload: domain.MessageEvent{From: "alice"},
		})
	}
	// Should not panic or block — excess events are dropped
}

func TestSubscriptionClose(t *testing.T) {
	bus := domain.NewEventBus()
	sub := bus.Subscribe(domain.EventMessageNew)

	sub.Close()

	// Channel should be closed
	_, ok := <-sub.Events()
	assert.False(t, ok, "channel should be closed after Close()")

	// Publishing after close should not panic
	require.NotPanics(t, func() {
		bus.Publish(domain.Event{
			Type:    domain.EventMessageNew,
			Payload: domain.MessageEvent{From: "alice"},
		})
	})
}

func TestMultipleSubscribers(t *testing.T) {
	bus := domain.NewEventBus()
	sub1 := bus.Subscribe(domain.EventMessageNew)
	sub2 := bus.Subscribe(domain.EventMessageNew)
	defer sub1.Close()
	defer sub2.Close()

	bus.Publish(domain.Event{
		Type:    domain.EventMessageNew,
		Payload: domain.MessageEvent{From: "alice"},
	})

	for _, sub := range []domain.Subscription{sub1, sub2} {
		select {
		case evt := <-sub.Events():
			msg := evt.Payload.(domain.MessageEvent)
			assert.Equal(t, "alice", msg.From)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}
	}
}
```

**Step 4: Run tests to verify they fail**

Run: `go test ./pkg/domain/... -v -count=1`

Expected: FAIL — `NewEventBus` undefined.

**Step 5: Implement EventBus in `pkg/domain/eventbus.go`**

```go
package domain

import "sync"

type eventBus struct {
	mu   sync.RWMutex
	subs []*subscription
}

type subscription struct {
	bus    *eventBus
	ch     chan Event
	types  map[string]bool
	closed bool
}

// NewEventBus creates a new in-process event bus.
func NewEventBus() EventBus {
	return &eventBus{}
}

func (b *eventBus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.subs {
		if s.closed {
			continue
		}
		if len(s.types) > 0 && !s.types[event.Type] {
			continue
		}
		select {
		case s.ch <- event:
		default: // buffer full, drop
		}
	}
}

func (b *eventBus) Subscribe(eventTypes ...string) Subscription {
	s := &subscription{
		bus:   b,
		ch:    make(chan Event, 64),
		types: make(map[string]bool),
	}
	for _, t := range eventTypes {
		s.types[t] = true
	}
	b.mu.Lock()
	b.subs = append(b.subs, s)
	b.mu.Unlock()
	return s
}

func (s *subscription) Events() <-chan Event { return s.ch }

func (s *subscription) Close() {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
	// Remove from bus
	for i, sub := range s.bus.subs {
		if sub == s {
			s.bus.subs = append(s.bus.subs[:i], s.bus.subs[i+1:]...)
			break
		}
	}
}
```

**Step 6: Run tests to verify they pass**

Run: `go test ./pkg/domain/... -v -count=1`

Expected: PASS — all 6 tests pass.

**Step 7: Commit**

```
feat: add domain EventBus interface and implementation
```

---

### Task 2: WebhookSubscriber — Extract Webhook Logic from Hub

**Files:**
- Modify: `pkg/daemon/hub.go`
- Modify: `pkg/daemon/webhooks.go`
- Modify: `pkg/daemon/webhooks_test.go`
- Modify: `pkg/daemon/server.go`

**Step 1: Add WebhookSubscriber and `computeRecipients` to `pkg/daemon/webhooks.go`**

Add the subscriber struct and a shared `computeRecipients` helper (will also be used by PresenceNotifier in Task 3):

```go
// computeRecipients returns the list of users who should be notified:
// mentioned users + DM members, minus the sender.
func computeRecipients(msg domain.MessageEvent, store domain.Store) []string {
	seen := make(map[string]bool)
	var recipients []string
	for _, m := range msg.Mentions {
		if m != msg.From && !seen[m] {
			seen[m] = true
			recipients = append(recipients, m)
		}
	}
	if msg.ChannelType == "dm" {
		ch, err := store.GetChannelByName(msg.ChannelName)
		if err == nil {
			if members, err := store.ChannelMemberUsernames(ch.ID); err == nil {
				for _, m := range members {
					if m != msg.From && !seen[m] {
						seen[m] = true
						recipients = append(recipients, m)
					}
				}
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
	webhookURL, err := ws.store.GetSetting("webhook_url")
	if err != nil || webhookURL == "" {
		return
	}

	recipients := computeRecipients(msg, ws.store)
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

// Close stops the webhook subscriber.
func (ws *WebhookSubscriber) Close() {
	ws.sub.Close()
}
```

**Step 2: Update `pkg/daemon/webhooks_test.go`**

Replace the existing tests (which test `fireWebhooks` directly) with tests that exercise `WebhookSubscriber` via the event bus. Keep the `fireWebhooks` tests as-is since that function is unchanged, and add subscriber tests:

```go
func TestWebhookSubscriberSendsOnMention(t *testing.T) {
	// Set up test HTTP server to capture webhook calls
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

	// Set up store with webhook URL
	store := testStore(t) // uses existing test helper
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

	// Wait for async webhook delivery
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) == 1
	}, 2*time.Second, 50*time.Millisecond)

	assert.Equal(t, "general", received[0].Event.ChannelName)
	assert.Equal(t, []string{"bob"}, received[0].Event.Recipients)
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
```

**Step 3: Modify Hub to use EventBus — `pkg/daemon/hub.go`**

Add `bus` field to Hub struct and update constructor:

```go
type Hub struct {
	mu      sync.RWMutex
	clients map[string]*WSClient
	states  map[string]string
	bus     domain.EventBus
}

func NewHub(bus domain.EventBus) *Hub {
	return &Hub{
		clients: make(map[string]*WSClient),
		states:  make(map[string]string),
		bus:     bus,
	}
}
```

Remove the webhook logic from `BroadcastMessage()` (lines 130-165) and replace with a `bus.Publish()` call at the end of phase 2 (after the `msgs` map is populated):

```go
if h.bus != nil {
	h.bus.Publish(domain.Event{
		Type: domain.EventMessageNew,
		Payload: domain.MessageEvent{
			ChannelName: channelName,
			ChannelType: channelType,
			From:        msg.From,
			MessageID:   msg.ID,
			SentAt:      msg.CreatedAt,
			Mentions:    mentions,
			ThreadID:    threadID,
		},
	})
}
```

**Step 4: Wire EventBus in `pkg/daemon/server.go`**

Update `NewServer` signature to accept `domain.EventBus`, pass to `NewHub`, create `WebhookSubscriber`:

```go
func NewServer(addr string, store domain.Store, pongTimeout time.Duration, webhookURL string, bus domain.EventBus) *SharkfinMCP {
```

Inside `NewServer`:

```go
hub := NewHub(bus)
```

After the existing `webhookURL` → `store.SetSetting` logic, add:

```go
webhookSub := NewWebhookSubscriber(bus, store)
```

Store `webhookSub` on the server struct for cleanup on shutdown. Update `cmd/sharkfind/daemon.go` (or wherever `NewServer` is called) to pass a new `domain.NewEventBus()`.

**Step 5: Update any tests that call `NewHub()` to pass `nil`**

Any existing test that constructs `NewHub()` directly needs to pass `nil` for the bus:

```go
hub := NewHub(nil)
```

**Step 6: Run tests**

Run: `mise run test`

Expected: PASS — all unit tests pass.

Run: `mise run e2e`

Expected: PASS — existing webhook e2e tests still pass (webhook behavior is identical, just moved to a subscriber).

**Step 7: Commit**

```
refactor: extract webhook firing into EventBus subscriber
```

---

### Task 3: Presence Notifications — Send Events Over Presence WebSocket

**Files:**
- Modify: `pkg/daemon/session.go`
- Modify: `pkg/daemon/presence_handler.go`
- Create: `pkg/daemon/presence_notifier.go`
- Modify: `pkg/daemon/server.go`

**Step 1: Store presence connection in SessionManager — `pkg/daemon/session.go`**

Update `IdentityToken` struct to hold the WebSocket connection:

```go
type IdentityToken struct {
	// ... existing fields ...
	presenceConn *websocket.Conn
	presenceMu   sync.Mutex
}
```

Update `AttachPresence` to accept the connection:

```go
func (sm *SessionManager) AttachPresence(token string, conn *websocket.Conn) (<-chan struct{}, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	it, ok := sm.tokens[token]
	if !ok {
		return nil, fmt.Errorf("invalid token")
	}
	it.HasPresence = true
	it.presenceConn = conn
	return it.PresenceDone, nil
}
```

Add `SendNotification` method:

```go
func (sm *SessionManager) SendNotification(username string, data []byte) error {
	sm.mu.RLock()
	tokenStr, ok := sm.onlineUsers[username]
	if !ok {
		sm.mu.RUnlock()
		return fmt.Errorf("user not online")
	}
	it := sm.tokens[tokenStr]
	sm.mu.RUnlock()

	if it.presenceConn == nil {
		return fmt.Errorf("no presence connection")
	}

	it.presenceMu.Lock()
	defer it.presenceMu.Unlock()
	return it.presenceConn.WriteMessage(websocket.TextMessage, data)
}
```

**Step 2: Update presence handler — `pkg/daemon/presence_handler.go`**

Change the `AttachPresence` call to pass the connection:

```go
done, err := h.sessions.AttachPresence(token, conn)
```

**Step 3: Create `pkg/daemon/presence_notifier.go`**

```go
package daemon

import (
	"encoding/json"

	"sharkfin/pkg/domain"
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
```

**Step 4: Wire in `pkg/daemon/server.go`**

In `NewServer`, after creating the webhook subscriber:

```go
presenceNotifier := NewPresenceNotifier(bus, sessions, store)
```

Store for cleanup on shutdown.

**Step 5: Run tests**

Run: `mise run test`

Expected: PASS.

**Step 6: Commit**

```
feat: add presence notifications via EventBus subscriber
```

---

### Task 4: Bridge — Intercept `wait_for_messages`

**Files:**
- Modify: `cmd/mcpbridge/mcp_bridge.go`
- Modify: `pkg/daemon/mcp_tools.go`
- Modify: `pkg/daemon/mcp_server.go`

**Step 1: Add `wait_for_messages` tool definition in `pkg/daemon/mcp_tools.go`**

Follow the existing pattern (e.g. `newUnreadMessagesTool`):

```go
func newWaitForMessagesTool() mcp.Tool {
	return mcp.NewTool("wait_for_messages",
		mcp.WithDescription("Block until unread messages arrive or timeout. Returns unread messages."),
		mcp.WithNumber("timeout",
			mcp.Description("Max seconds to wait (default 30)"),
		),
	)
}
```

**Step 2: Register tool in `pkg/daemon/mcp_server.go`**

Add to the `AddTools` call in `NewSharkfinMCP`:

```go
server.ServerTool{Tool: newWaitForMessagesTool(), Handler: s.handleWaitForMessages},
```

Add stub handler (the bridge intercepts this, so it should never be reached):

```go
func (s *SharkfinMCP) handleWaitForMessages(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError("wait_for_messages is only available via mcp-bridge"), nil
}
```

**Step 3: Restructure bridge presence read loop in `cmd/mcpbridge/mcp_bridge.go`**

Add `notifications` channel to bridge struct:

```go
type bridge struct {
	client        *http.Client
	mcpURL        string
	wsURL         string
	sessionID     string
	token         string
	notifications chan json.RawMessage
}
```

Update `startPresence` to feed messages into the channel:

```go
b.notifications = make(chan json.RawMessage, 64)

go func() {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			close(b.notifications)
			return
		}
		if json.Valid(msg) {
			select {
			case b.notifications <- json.RawMessage(msg):
			default: // buffer full, drop
			}
		}
	}
}()
```

**Step 4: Implement `wait_for_messages` interceptor**

Follow the pattern of `interceptGetIdentityToken`:

```go
func (b *bridge) interceptWaitForMessages(line string) bool {
	// Parse JSON-RPC request
	var req struct {
		JSONRPC string `json:"jsonrpc"`
		ID      any    `json:"id"`
		Method  string `json:"method"`
		Params  struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		return false
	}
	if req.Method != "tools/call" || req.Params.Name != "wait_for_messages" {
		return false
	}

	// Extract timeout (default 30s)
	timeout := 30.0
	if t, ok := req.Params.Arguments["timeout"].(float64); ok && t > 0 {
		timeout = t
	}

	// First: check if there are already unread messages
	result, err := b.callUnreadMessages()
	if err == nil && result != "[]" && result != "" {
		b.respondToolResult(req.ID, result)
		return true
	}

	// Block waiting for notification or timeout
	select {
	case _, ok := <-b.notifications:
		if !ok {
			b.respondToolResult(req.ID, `{"status":"disconnected","messages":[]}`)
			return true
		}
		// Notification received — fetch unread messages
		result, err = b.callUnreadMessages()
		if err != nil {
			b.respondToolError(req.ID, err.Error())
			return true
		}
		b.respondToolResult(req.ID, result)
	case <-time.After(time.Duration(timeout) * time.Second):
		b.respondToolResult(req.ID, `{"status":"timeout","messages":[]}`)
	}
	return true
}
```

Add helper methods:

```go
func (b *bridge) callUnreadMessages() (string, error) {
	// Build a tools/call request for unread_messages and send via HTTP
	// Parse the response and return the text content
}

func (b *bridge) respondToolResult(id any, text string) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": text},
			},
		},
	}
	data, _ := json.Marshal(resp)
	fmt.Println(string(data))
}

func (b *bridge) respondToolError(id any, msg string) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"isError": true,
			"content": []map[string]any{
				{"type": "text", "text": msg},
			},
		},
	}
	data, _ := json.Marshal(resp)
	fmt.Println(string(data))
}
```

Add to `processStdin` after the `interceptGetIdentityToken` check:

```go
if b.interceptWaitForMessages(line) {
	continue
}
```

**Step 5: Run tests**

Run: `mise run ci`

Expected: PASS.

**Step 6: Commit**

```
feat: add wait_for_messages MCP tool with bridge interception
```

---

### Task 5: E2E Test

**Files:**
- Modify: `tests/e2e/sharkfin_test.go`

**Step 1: Write presence notification e2e test**

Test that a user with a presence WebSocket receives a notification when another user sends a message mentioning them. This exercises: EventBus → PresenceNotifier → SessionManager → WS write.

```go
func TestPresenceNotification(t *testing.T) {
	h := harness.Start(t)
	defer h.Stop()

	// Register two users via MCP
	alice := h.MCPClient("alice")
	bob := h.MCPClient("bob")

	// Bob connects a presence WebSocket
	bobPresenceConn := h.PresenceWS(bob.Token())

	// Alice sends a message mentioning bob
	alice.Call("send_message", map[string]any{
		"channel":  "general",
		"message":  "hey @bob",
		"mentions": []string{"bob"},
	})

	// Bob should receive a presence notification
	_, msg, err := bobPresenceConn.ReadMessage()
	require.NoError(t, err)

	var envelope struct {
		Type string `json:"type"`
		D    struct {
			Channel     string `json:"channel"`
			ChannelType string `json:"channel_type"`
			From        string `json:"from"`
			MessageID   int64  `json:"message_id"`
		} `json:"d"`
	}
	require.NoError(t, json.Unmarshal(msg, &envelope))
	assert.Equal(t, "message.new", envelope.Type)
	assert.Equal(t, "general", envelope.D.Channel)
	assert.Equal(t, "alice", envelope.D.From)
}
```

Note: The exact test structure depends on the harness capabilities. If the harness doesn't support presence WS connections, add a `PresenceWS` helper that dials `/presence`, sends the identity token, and returns the connection. Adapt as needed based on what helpers already exist.

**Step 2: Run full CI**

Run: `mise run ci`

Expected: PASS.

**Step 3: Commit**

```
test: add e2e tests for presence notifications and wait_for_messages
```

---

### Task 6: Full CI Pass

**Step 1: Run full CI**

Run: `mise run ci`

All lint, unit tests, and e2e tests must pass.

**Step 2: Commit any fixes**

---

## Verification

```bash
mise run ci
```
