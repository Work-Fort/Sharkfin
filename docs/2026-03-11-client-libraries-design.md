# Sharkfin Client Libraries Design

## Goal

Provide idiomatic Go and TypeScript WebSocket client libraries for the Sharkfin messaging server, enabling applications in the WorkFort ecosystem to integrate with Sharkfin programmatically.

## Scope

- **Go WS client** — `client/` package in the sharkfin repo
- **TypeScript WS client** — `clients/ts/` package in the sharkfin repo, published as `@workfort/sharkfin-client`
- **REST client** — deferred until a REST API exists on the server

## Wire Protocol

All communication uses JSON envelopes over WebSocket.

**Request (client → server):**
```json
{"type": "operation_name", "d": {...}, "ref": "unique-id"}
```

**Reply (server → client):**
```json
{"type": "reply", "ref": "matching-ref", "ok": true, "d": {...}}
```

**Error reply (server → client):**
```json
{"type": "reply", "ref": "matching-ref", "ok": false, "d": {"message": "error description"}}
```

**Broadcast (server → client, no ref):**
```json
{"type": "message.new", "d": {...}}
```

### Connection Lifecycle

1. Client opens WebSocket to `/ws`
2. Server sends `hello` envelope:
   ```json
   {"type": "hello", "d": {"heartbeat_interval": 10, "version": "v0.6.0"}}
   ```
3. Client must `register` (new user) or `identify` (existing user) before any other operation
4. Client must send `ping` at the heartbeat interval to stay connected; server replies with `pong`

### `notifications_only` Mode

Both `register` and `identify` accept `"notifications_only": true` in their `d` payload. When enabled, only `ping`, `version`, `capabilities`, `set_state`, and `unread_counts` are permitted. All other operations return an error. This mode is for lightweight listeners (e.g., notification watchers). It cannot be changed mid-connection.

## Architecture

Both clients wrap the Sharkfin WebSocket protocol with language-idiomatic APIs. Each client is independently versioned and released. The APIs are not forced into the same shape — each is designed to be natural in its language.

### Go Client (`client/`)

**Module:** `github.com/Work-Fort/sharkfin/client` (own `go.mod` for independent versioning)

**Connection:**
```go
c, err := client.Dial(ctx, "ws://localhost:16000/ws", opts...)
defer c.Close()
```

Functional options: `WithDialer`, `WithReconnect`, `WithLogger`.

`Dial` connects, reads the `hello` envelope (exposing `c.HeartbeatInterval()` and `c.ServerVersion()`), and starts the read pump goroutine.

**Event delivery:** Channel-based.
```go
for ev := range c.Events() {
    switch ev.Type {
    case "message.new":
        msg, _ := ev.AsMessage()
    case "presence":
        p, _ := ev.AsPresence()
    }
}
```

The `Events()` channel is closed when the client disconnects and reconnection is not configured (or exhausted).

**Request methods:** One method per WS operation. Internally sends a ref-tagged envelope, blocks until matching reply arrives (discarding broadcasts to the Events channel).

```go
err := c.Register(ctx, "alice", nil)                   // notifications_only defaults to false
err := c.Register(ctx, "watcher", &RegisterOpts{NotificationsOnly: true})
err := c.Identify(ctx, "alice", nil)                   // identify takes username, not token
users, err := c.Users(ctx)
channels, err := c.Channels(ctx)
msgID, err := c.SendMessage(ctx, "general", "hello", nil)   // returns message ID, not full Message
history, err := c.History(ctx, "general", nil)               // HistoryOpts for before/after/limit/thread_id
```

Full operation list:
- Identity: `Register`, `Identify`, `Users`
- Channels: `Channels`, `CreateChannel`, `InviteToChannel`, `JoinChannel`
- Messages: `SendMessage`, `History`, `UnreadMessages`, `UnreadCounts`, `MarkRead`
- DMs: `DMOpen`, `DMList`
- Presence: `SetState`
- Mention groups: `CreateMentionGroup`, `DeleteMentionGroup`, `GetMentionGroup`, `ListMentionGroups`, `AddMentionGroupMember`, `RemoveMentionGroupMember`
- Info: `Ping`, `Version`, `Capabilities`
- Settings: `SetSetting`, `GetSettings`

Note: DMs use the same `SendMessage` and `History` operations — send to a DM channel name obtained from `DMOpen`. There are no separate `SendDM`/`DMHistory` operations. Role management is MCP-only and not exposed over WS.

**Reconnection:**

When `WithReconnect(backoff)` is set:
- On disconnect, the client attempts to reconnect using the provided backoff function
- After reconnect, the client re-reads `hello` but does NOT auto-re-identify — the consumer must call `Identify` or `Register` again
- In-flight requests fail with `ErrNotConnected`; the consumer retries as needed
- The `Events()` channel emits a `Disconnect` event and a `Reconnect` event

**Error handling:**
- `ServerError` for server-returned errors (ok: false)
- `ErrNotConnected`, `ErrTimeout` sentinel errors
- All methods accept `context.Context` for cancellation

**Dependencies:** gorilla/websocket (own `go.mod`, not shared with server's `go.mod`)

### TypeScript Client (`clients/ts/`)

**Package:** `@workfort/sharkfin-client`

**Platforms:** Node.js 18+ and modern browsers (Chrome, Firefox, Safari, Edge).

**Connection:**
```ts
const client = new SharkfinClient("ws://localhost:16000/ws", {
  reconnect: true,
  logger: console,
});
await client.connect();
// client.heartbeatInterval, client.serverVersion available
```

**Event delivery:** EventEmitter pattern with typed events.
```ts
client.on("message", (msg: BroadcastMessage) => { ... });
client.on("presence", (update: PresenceUpdate) => { ... });
client.on("disconnect", () => { ... });
client.on("reconnect", () => { ... });
```

Uses a tiny bundled isomorphic event emitter (~20 lines) instead of Node's `events` module.

**Request methods:** All async, throw `SharkfinError` on server errors.
```ts
await client.register("alice");
await client.identify("alice");
const channels = await client.channels();
const msgId = await client.sendMessage("general", "hello");
const history = await client.history("general", { limit: 50 });
```

Same operation coverage as the Go client, with camelCase naming.

**Reconnection:** Same contract as Go — auto-reconnect reads `hello` but does not re-identify. Consumer must re-identify in the `reconnect` event handler.

**Error handling:**
```ts
class SharkfinError extends Error {
  constructor(public readonly serverMessage: string) { ... }
}
```

**Dependencies:** Zero runtime dependencies. Uses native `WebSocket` (browser + Node 22+). For Node 18-21, consumers must install `ws` and pass it as `WebSocket` constructor option.

**Build:** ESM primary, CJS fallback via `"exports"` field. TypeScript declarations included. No Node-only APIs (`Buffer`, `fs`, `process`).

## Types

Types match the server's wire format exactly.

### Go

```go
type User struct {
    Username string `json:"username"`
    Online   bool   `json:"online"`
    Type     string `json:"type"`
    State    string `json:"state,omitempty"`
}

type Channel struct {
    Name   string `json:"name"`
    Public bool   `json:"public"`
    Member bool   `json:"member"`
}

type Message struct {
    ID       int64    `json:"id"`
    From     string   `json:"from"`
    Body     string   `json:"body"`
    SentAt   string   `json:"sent_at"`
    ThreadID *int64   `json:"thread_id,omitempty"`
    Mentions []string `json:"mentions,omitempty"`
    Channel  string   `json:"channel,omitempty"` // present in unread_messages, absent in history
}

// BroadcastMessage is the payload of a message.new broadcast.
type BroadcastMessage struct {
    ID          int64    `json:"id"`
    Channel     string   `json:"channel"`
    ChannelType string   `json:"channel_type"` // "channel" or "dm"
    From        string   `json:"from"`
    Body        string   `json:"body"`
    SentAt      string   `json:"sent_at"`
    ThreadID    *int64   `json:"thread_id,omitempty"`
    Mentions    []string `json:"mentions,omitempty"`
}

type PresenceUpdate struct {
    Username string `json:"username"`
    Status   string `json:"status"` // "online" or "offline"
    State    string `json:"state,omitempty"` // "active" or "idle", only when online
}

type UnreadCount struct {
    Channel      string `json:"channel"`
    Type         string `json:"type"`
    UnreadCount  int    `json:"unread_count"`
    MentionCount int    `json:"mention_count"`
}

type DM struct {
    Channel      string   `json:"channel"`
    Participants []string `json:"participants"`
}

type MentionGroup struct {
    ID        int64    `json:"id"`
    Slug      string   `json:"slug"`
    CreatedBy string   `json:"created_by,omitempty"`
    Members   []string `json:"members,omitempty"`
}
```

### TypeScript

```ts
interface User {
  username: string;
  online: boolean;
  type: string;
  state?: string;
}

interface Channel {
  name: string;
  public: boolean;
  member: boolean;
}

interface Message {
  id: number;
  from: string;
  body: string;
  sentAt: string;
  threadId?: number;
  mentions?: string[];
  channel?: string;
}

interface BroadcastMessage {
  id: number;
  channel: string;
  channelType: string;
  from: string;
  body: string;
  sentAt: string;
  threadId?: number;
  mentions?: string[];
}

interface PresenceUpdate {
  username: string;
  status: "online" | "offline";
  state?: "active" | "idle";
}

interface UnreadCount {
  channel: string;
  type: string;
  unreadCount: number;
  mentionCount: number;
}

interface DM {
  channel: string;
  participants: string[];
}

interface MentionGroup {
  id: number;
  slug: string;
  createdBy?: string;
  members?: string[];
}
```

## Request Method Signatures

### Go

```go
// Identity
func (c *Client) Register(ctx context.Context, username string, opts *RegisterOpts) error
func (c *Client) Identify(ctx context.Context, username string, opts *IdentifyOpts) error
func (c *Client) Users(ctx context.Context) ([]User, error)

// Channels
func (c *Client) Channels(ctx context.Context) ([]Channel, error)
func (c *Client) CreateChannel(ctx context.Context, name string, public bool) error
func (c *Client) InviteToChannel(ctx context.Context, channel, username string) error
func (c *Client) JoinChannel(ctx context.Context, channel string) error

// Messages
func (c *Client) SendMessage(ctx context.Context, channel, body string, opts *SendOpts) (int64, error)
func (c *Client) History(ctx context.Context, channel string, opts *HistoryOpts) ([]Message, error)
func (c *Client) UnreadMessages(ctx context.Context, channel string, opts *UnreadOpts) ([]Message, error)
func (c *Client) UnreadCounts(ctx context.Context) ([]UnreadCount, error)
func (c *Client) MarkRead(ctx context.Context, channel string, messageID int64) error

// DMs
func (c *Client) DMOpen(ctx context.Context, username string) (DMOpenResult, error)
func (c *Client) DMList(ctx context.Context) ([]DM, error)

// Presence
func (c *Client) SetState(ctx context.Context, state string) error // "active" or "idle"

// Mention groups
func (c *Client) CreateMentionGroup(ctx context.Context, slug string) (int64, error)
func (c *Client) DeleteMentionGroup(ctx context.Context, slug string) error
func (c *Client) GetMentionGroup(ctx context.Context, slug string) (*MentionGroup, error)
func (c *Client) ListMentionGroups(ctx context.Context) ([]MentionGroup, error)
func (c *Client) AddMentionGroupMember(ctx context.Context, slug, username string) error
func (c *Client) RemoveMentionGroupMember(ctx context.Context, slug, username string) error

// Info
func (c *Client) Ping(ctx context.Context) error
func (c *Client) Version(ctx context.Context) (string, error)
func (c *Client) Capabilities(ctx context.Context) ([]string, error)

// Settings
func (c *Client) SetSetting(ctx context.Context, key, value string) error
func (c *Client) GetSettings(ctx context.Context) (map[string]string, error)

// Options structs
type RegisterOpts struct { NotificationsOnly bool }
type IdentifyOpts struct { NotificationsOnly bool }
type SendOpts struct { ThreadID *int64 }
type HistoryOpts struct { Before *int64; After *int64; Limit *int; ThreadID *int64 }
type UnreadOpts struct { MentionsOnly bool; ThreadID *int64 }
type DMOpenResult struct { Channel string; Participant string; Created bool }
```

## Versioning

Each component is independently versioned using git tag prefixes:

| Component | Tag pattern | Example | Release trigger |
|---|---|---|---|
| Server | `v*` | `v0.6.0` | Auto-bump on push to master |
| Go client | `client/v*` | `client/v0.1.0` | Manual git tag |
| TS client | `clients/ts/v*` | `clients/ts/v0.1.0` | Manual git tag |

The Go client has its own `client/go.mod` so `go get github.com/Work-Fort/sharkfin/client@v0.1.0` works via Go's multi-module repo convention.

Each tag pattern triggers a dedicated release workflow:
- `release-client-go.yaml` — runs tests, creates GitHub release
- `release-client-ts.yaml` — runs tests, builds, publishes to npm

## Testing

### Go (`client/client_test.go`)

Unit tests with a mock WS server (httptest + gorilla upgrader). No dependency on a running sharkfin daemon.

Coverage: connection lifecycle (hello, heartbeat, close), request/response for each operation, event delivery and routing, error handling (server errors, disconnects), reconnection behavior, concurrency safety.

### TypeScript (`clients/ts/test/client.test.ts`)

Unit tests with vitest and a mock WS server.

Coverage: same areas as Go. Browser compatibility verified via type checking (no Node-only APIs in source).

### Integration

Existing e2e tests (`tests/e2e/`) exercise the WS protocol end-to-end. Client library tests focus on client-side logic against mocks.

## File Structure

```
client/
├── go.mod
├── go.sum
├── client.go         # Dial, Client, Options, Close, read pump, heartbeat
├── client_test.go    # unit tests with mock server
├── events.go         # Event type, channel delivery, typed helpers
├── types.go          # User, Channel, Message, MentionGroup, etc.
├── errors.go         # ServerError, ErrNotConnected, ErrTimeout
└── requests.go       # all request methods (Register, SendMessage, etc.)

clients/ts/
├── package.json
├── tsconfig.json
├── src/
│   ├── index.ts      # public exports
│   ├── client.ts     # SharkfinClient class
│   ├── types.ts      # Message, Channel, User interfaces
│   ├── emitter.ts    # isomorphic micro event emitter
│   └── errors.ts     # SharkfinError
└── test/
    └── client.test.ts

.github/workflows/
├── release-client-go.yaml
└── release-client-ts.yaml
```
