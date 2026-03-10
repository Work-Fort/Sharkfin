# wait_for_messages: Event Bus and MCP Blocking Tool

## Goal

Add a `wait_for_messages` MCP tool that blocks until unread messages arrive, and introduce a domain-level event bus that decouples message notification delivery from the hub's broadcast logic.

## Problem

Agents currently poll `unread_messages` in a loop, wasting API calls and adding latency. The hub directly calls `fireWebhooks()` for notification delivery, coupling transport concerns into broadcast logic. Adding a new notification channel (presence WebSocket notifications for the bridge) would further entangle these responsibilities.

## Design

### 1. Domain Event Bus

A pub/sub event bus lives in the domain layer (`pkg/domain/`). Any component can publish events; any component can subscribe. The bus is the single point of coordination for cross-cutting notifications.

**Interface** (`pkg/domain/ports.go`):

```go
type Event struct {
    Type    string // namespaced: "message.new", "presence.update", etc.
    Payload any
}

type Subscription interface {
    Events() <-chan Event
    Close()
}

type EventBus interface {
    Publish(event Event)
    Subscribe(eventTypes ...string) Subscription
}
```

**Implementation** (`pkg/domain/eventbus.go`):

In-process, channel-based. Each `Subscribe()` creates a buffered channel. `Publish()` fans out to all matching subscribers. Non-blocking send — if a subscriber's buffer is full, the event is dropped (same semantics as the current WS `client.send` channel).

Subscribers filter by event type at subscription time (not at delivery time). `Subscribe()` with no args subscribes to all events.

**Event types defined as constants:**

```go
const (
    EventMessageNew    = "message.new"
    EventPresenceUpdate = "presence.update"
)
```

**Payload types** (`pkg/domain/types.go`):

```go
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

### 2. Hub Refactor

The hub currently has three responsibilities in `BroadcastMessage()`:

1. Build WS envelope and deliver to connected WS clients (phases 1–3)
2. Compute webhook recipients and fire HTTP webhooks (lines 130–165)
3. _(New)_ Notify presence connections

After the refactor, the hub does only #1, then publishes a single `message.new` event to the bus. Everything else is handled by subscribers.

**Before:**
```
Hub.BroadcastMessage()
  ├── Phase 1-3: WS client delivery
  └── fireWebhooks() (inline)
```

**After:**
```
Hub.BroadcastMessage()
  ├── Phase 1-3: WS client delivery
  └── bus.Publish(Event{Type: "message.new", Payload: MessageEvent{...}})

WebhookSubscriber (subscribes to "message.new")
  └── Computes recipients (mentions + DM members - sender)
  └── fireWebhooks()

PresenceNotifier (subscribes to "message.new")
  └── Sends notification to recipient presence WebSocket connections
```

The webhook recipient computation (mention dedup, DM participant lookup, sender exclusion) moves from the hub into the `WebhookSubscriber`. This requires the subscriber to have access to the store for `ChannelMemberUsernames()` lookups on DM channels.

### 3. Presence Notifications

The presence WebSocket, currently ping/pong only, gains the ability to send JSON text messages to the client.

**Daemon side:** A `PresenceNotifier` subscribes to the event bus. On `message.new`, it determines which online users should be notified (same logic as webhooks: mentions + DM participants - sender) and writes a JSON envelope to their presence WebSocket connection.

The `SessionManager` gains a `SendNotification(username string, data []byte) error` method. The `IdentityToken` struct stores the presence `*websocket.Conn` (set during `AttachPresence`). Writes are serialized per-connection (one writer at a time; gorilla requires this). If the write fails, the notification is silently dropped.

**Envelope format** (matches WS handler convention):

```json
{"type": "message.new", "d": {"channel": "general", "channel_type": "channel", "from": "alice", "message_id": 42}}
```

The `d` field carries summary data — enough for the bridge to decide whether to wake a blocking call, but not the full message body.

### 4. Bridge: `wait_for_messages` Tool

The bridge intercepts `wait_for_messages` tool calls the same way it intercepts `get_identity_token` — entirely client-side, no round-trip to the daemon for the tool itself.

**Flow:**

1. Bridge receives `wait_for_messages` tool call with optional `timeout` parameter (seconds, default 30).
2. Bridge calls the daemon's `unread_messages` endpoint via HTTP.
3. If there are unread messages, return them immediately.
4. If empty, block reading the presence WebSocket until:
   - A `message.new` notification arrives → call `unread_messages` again → return result.
   - Timeout expires → return `{"status": "timeout", "messages": []}`.
   - Context cancelled → return error.

**Bridge changes:**

The bridge's presence read loop (currently a fire-and-forget goroutine that discards all messages) must be restructured. Instead of discarding, it feeds messages into a Go channel that the `wait_for_messages` interceptor can select on.

```go
type bridge struct {
    client       *http.Client
    mcpURL       string
    wsURL        string
    sessionID    string
    token        string
    notifications chan json.RawMessage // fed by presence read loop
}
```

The read loop parses incoming text messages as JSON envelopes and sends them to the `notifications` channel. Ping/pong frames continue to be handled automatically by gorilla.

**Tool definition** (registered in the daemon's tool list so it appears in `tools/list`, but the bridge intercepts it before it reaches the daemon):

```json
{
  "name": "wait_for_messages",
  "description": "Block until unread messages arrive or timeout. Returns unread messages.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "timeout": {
        "type": "integer",
        "description": "Max seconds to wait (default 30)"
      }
    }
  }
}
```

### 5. Subscriber Lifecycle

Subscribers are created during server startup and closed during shutdown. The event bus itself is created first, passed to the server, which then wires subscribers.

```go
bus := domain.NewEventBus()
server := daemon.NewServer(addr, store, pongTimeout, webhookURL, bus)
// NewServer internally creates:
//   - WebhookSubscriber(bus, store, webhookURL)
//   - PresenceNotifier(bus, sessions)
```

On shutdown, subscribers are closed (which closes their subscription channels), then the bus is closed.

### 6. What Doesn't Change

- **WS client broadcast** stays in the hub. It's real-time delivery to connected interactive clients, not a notification concern. The hub still does phases 1–3.
- **MCP `unread_messages` tool** on the daemon stays unchanged. The bridge calls it via HTTP.
- **Webhook payload format** stays identical. Only the call site moves.
- **`tools/list` response** from the daemon includes `wait_for_messages` so MCP clients discover it, even though the bridge intercepts it.

## File Changes

| File | Change |
|------|--------|
| `pkg/domain/ports.go` | Add `Event`, `Subscription`, `EventBus` interfaces |
| `pkg/domain/types.go` | Add `MessageEvent` payload type |
| `pkg/domain/eventbus.go` | New: in-process channel-based `EventBus` implementation |
| `pkg/daemon/hub.go` | Remove webhook logic from `BroadcastMessage`, add `bus.Publish()` |
| `pkg/daemon/webhooks.go` | Extract into `WebhookSubscriber` that subscribes to bus |
| `pkg/daemon/presence_notifier.go` | New: subscribes to bus, writes to presence WebSocket connections |
| `pkg/daemon/session.go` | Store presence `*websocket.Conn`, add `SendNotification()` |
| `pkg/daemon/presence_handler.go` | Pass `conn` to `AttachPresence` |
| `pkg/daemon/server.go` | Wire bus, create subscribers at startup |
| `pkg/daemon/mcp_tools.go` | Add `wait_for_messages` tool definition |
| `cmd/mcpbridge/mcp_bridge.go` | Restructure presence read loop, intercept `wait_for_messages` |

## Testing

- **EventBus unit tests**: publish/subscribe, filtering, buffer-full drop, close semantics.
- **WebhookSubscriber unit tests**: verify recipient computation matches current behavior (extract from existing webhook tests).
- **PresenceNotifier unit tests**: mock SessionManager, verify correct users get notifications.
- **Bridge `wait_for_messages` tests**: mock presence WS, verify immediate-return and blocking-then-wake paths.
- **E2E**: existing webhook e2e tests validate the refactored path still works. New e2e test for `wait_for_messages` through the bridge.
