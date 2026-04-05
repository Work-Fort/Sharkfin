# Bot/Service Identity — Implementation Plan

**Goal:** Enable WorkFort services to register as bot identities, receive channel messages via per-identity webhooks, and attach structured metadata to messages.

**Architecture:** Four sequential changes: (1) a new `bot` role migration with auto-assignment for service identities, (2) a new `identity_webhooks` table with `register_webhook` / `unregister_webhook` MCP tools and a per-identity dispatch path in `WebhookSubscriber`, (3) an extension to `computeRecipients` so service members receive webhooks for all messages in joined channels, (4) an optional `metadata` TEXT column on `messages` threaded through store, domain, MCP, WS, and broadcast.

**Tech Stack:** Go, SQLite (`modernc.org/sqlite`), Postgres, Goose migrations, `charmbracelet/log`, `mcp-go`, `gorilla/websocket`

---

## Task 1: Bot role migration (SQLite)

**Files:**
- Create: `pkg/infra/sqlite/migrations/009_bot_role.sql`

### Step 1: Write migration

```sql
-- 009_bot_role.sql
-- Adds the built-in 'bot' role for service identities.

-- +goose Up
INSERT INTO roles (name, built_in) VALUES ('bot', 1);

INSERT INTO permissions (name) VALUES ('channel.join')   ON CONFLICT DO NOTHING;
INSERT INTO permissions (name) VALUES ('channel.read')   ON CONFLICT DO NOTHING;
INSERT INTO permissions (name) VALUES ('message.send')   ON CONFLICT DO NOTHING;
INSERT INTO permissions (name) VALUES ('message.read')   ON CONFLICT DO NOTHING;
INSERT INTO permissions (name) VALUES ('channel.list')   ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role, permission) VALUES ('bot', 'channel.join');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'channel.read');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'message.send');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'message.read');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'channel.list');

-- +goose Down
DELETE FROM role_permissions WHERE role = 'bot';
DELETE FROM roles WHERE name = 'bot';
```

> Note: The existing permission names use snake_case (e.g. `send_message`, `join_channel`). Check whether `bot` permissions should reuse those or introduce new names. If the RBAC check in `toolPermissions` uses `join_channel` / `send_message` etc., insert those names instead. Use whatever names the existing `toolPermissions` map references.

**Correction — use existing permission names** from `006_rbac.sql`:
```sql
-- +goose Up
INSERT INTO roles (name, built_in) VALUES ('bot', 1);

INSERT INTO role_permissions (role, permission) VALUES ('bot', 'send_message');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'join_channel');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'channel_list');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'history');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'unread_messages');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'unread_counts');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'mark_read');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'create_channel');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'dm_list');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'dm_open');
INSERT INTO role_permissions (role, permission) VALUES ('bot', 'user_list');

-- +goose Down
DELETE FROM role_permissions WHERE role = 'bot';
DELETE FROM roles WHERE name = 'bot';
```

### Step 2: Run tests

```
mise run test
```

Expected: PASS (Goose applies migration on DB open).

### Step 3: Commit

```
git add pkg/infra/sqlite/migrations/009_bot_role.sql
git commit -m "feat: add bot role migration (SQLite)"
```

---

## Task 2: Bot role migration (Postgres)

**Files:**
- Create: `pkg/infra/postgres/migrations/009_bot_role.sql`

Identical SQL to Task 1 (Postgres supports the same syntax here; `ON CONFLICT DO NOTHING` is valid in both).

### Step 1: Write migration

Copy the `-- +goose Up` / `-- +goose Down` block from Task 1 verbatim.

### Step 2: Run tests

```
mise run test
```

### Step 3: Commit

```
git add pkg/infra/postgres/migrations/009_bot_role.sql
git commit -m "feat: add bot role migration (Postgres)"
```

---

## Task 3: Auto-assign bot role for service identities

**Files:**
- Modify: `pkg/infra/sqlite/identities.go` — `UpsertIdentity`
- Modify: `pkg/infra/postgres/identities.go` — `UpsertIdentity`
- Modify: `pkg/infra/sqlite/store_test.go` — new test

### Step 1: Write failing test

Add to `pkg/infra/sqlite/store_test.go`:

```go
func TestUpsertIdentityServiceAutoBot(t *testing.T) {
    s := newTestStore(t)

    // First identity is auto-admin regardless — use a throwaway first.
    s.UpsertIdentity("uuid-admin", "admin-user", "Admin", "user", "user")

    ident, err := s.UpsertIdentity("uuid-flow", "flow-bot", "Flow Bot", "service", "")
    if err != nil {
        t.Fatalf("upsert service identity: %v", err)
    }
    if ident.Role != "bot" {
        t.Errorf("service identity role = %q, want bot", ident.Role)
    }
}
```

### Step 2: Run test to verify it fails

```
go test ./pkg/infra/sqlite/ -run TestUpsertIdentityServiceAutoBot -v -count=1
```

Expected: FAIL — role is `"user"`, not `"bot"`.

### Step 3: Implement

In `UpsertIdentity` (both SQLite and Postgres), replace the existing role default block:

```go
if role == "" {
    role = "user"
}
var count int
s.db.QueryRow("SELECT COUNT(*) FROM identities").Scan(&count)
if count == 0 {
    role = "admin"
} else if identityType == "service" && role == "user" {
    role = "bot"
}
```

The `else if` ensures the first-user admin promotion takes priority. A service identity in an empty DB becomes admin (acceptable for test environments), while any subsequent service identity gets `bot`.

Apply the identical change to `pkg/infra/postgres/identities.go` (same logic, `$1` placeholder already used for the COUNT query).

Apply the same change to `pkg/infra/postgres/identities.go`.

### Step 4: Run test

```
go test ./pkg/infra/sqlite/ -run TestUpsertIdentityServiceAutoBot -v -count=1
```

Expected: PASS

### Step 5: Run full suite

```
mise run test
```

### Step 6: Commit

```
git add pkg/infra/sqlite/identities.go pkg/infra/postgres/identities.go pkg/infra/sqlite/store_test.go
git commit -m "feat: auto-assign bot role for service identities"
```

---

## Task 4: identity_webhooks table (SQLite migration)

**Files:**
- Create: `pkg/infra/sqlite/migrations/010_identity_webhooks.sql`

### Step 1: Write migration

```sql
-- 010_identity_webhooks.sql
-- Per-identity webhook registrations.

-- +goose Up
CREATE TABLE identity_webhooks (
    id          TEXT PRIMARY KEY,
    identity_id TEXT NOT NULL REFERENCES identities(id),
    url         TEXT NOT NULL,
    secret      TEXT NOT NULL DEFAULT '',
    active      INTEGER NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_identity_webhooks_identity_id ON identity_webhooks(identity_id);

-- +goose Down
DROP INDEX IF EXISTS idx_identity_webhooks_identity_id;
DROP TABLE IF EXISTS identity_webhooks;
```

### Step 2: Run tests

```
mise run test
```

### Step 3: Commit

```
git add pkg/infra/sqlite/migrations/010_identity_webhooks.sql
git commit -m "feat: add identity_webhooks table (SQLite)"
```

---

## Task 5: identity_webhooks table (Postgres migration)

**Files:**
- Create: `pkg/infra/postgres/migrations/010_identity_webhooks.sql`

```sql
-- 010_identity_webhooks.sql

-- +goose Up
CREATE TABLE identity_webhooks (
    id          TEXT PRIMARY KEY,
    identity_id TEXT NOT NULL REFERENCES identities(id),
    url         TEXT NOT NULL,
    secret      TEXT NOT NULL DEFAULT '',
    active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_identity_webhooks_identity_id ON identity_webhooks(identity_id);

-- +goose Down
DROP INDEX IF EXISTS idx_identity_webhooks_identity_id;
DROP TABLE IF EXISTS identity_webhooks;
```

### Step 2: Run tests + commit

```
mise run test
git add pkg/infra/postgres/migrations/010_identity_webhooks.sql
git commit -m "feat: add identity_webhooks table (Postgres)"
```

---

## Task 6: WebhookStore interface + SQLite/Postgres implementations

**Files:**
- Modify: `pkg/domain/ports.go` — add `WebhookStore` interface
- Create: `pkg/infra/sqlite/webhooks.go`
- Create: `pkg/infra/postgres/webhooks.go`
- Create: `pkg/infra/sqlite/webhooks_test.go`

### Step 1: Write failing test

Create `pkg/infra/sqlite/webhooks_test.go`:

```go
package sqlite

import (
    "testing"

    "github.com/stretchr/testify/require"
)

func TestRegisterAndListWebhooks(t *testing.T) {
    s := newTestStore(t)

    // Need an identity first.
    s.UpsertIdentity("uuid-admin", "admin", "Admin", "user", "user")
    svcIdent, err := s.UpsertIdentity("uuid-svc", "flow-bot", "Flow", "service", "")
    require.NoError(t, err)

    err = s.RegisterWebhook(svcIdent.ID, "https://flow.internal/hook", "mysecret")
    require.NoError(t, err)

    hooks, err := s.GetActiveWebhooksForIdentity(svcIdent.ID)
    require.NoError(t, err)
    require.Len(t, hooks, 1)
    require.Equal(t, "https://flow.internal/hook", hooks[0].URL)
    require.Equal(t, "mysecret", hooks[0].Secret)
}

func TestUnregisterWebhook(t *testing.T) {
    s := newTestStore(t)

    s.UpsertIdentity("uuid-admin", "admin", "Admin", "user", "user")
    svcIdent, _ := s.UpsertIdentity("uuid-svc", "flow-bot", "Flow", "service", "")

    s.RegisterWebhook(svcIdent.ID, "https://flow.internal/hook", "")

    hooks, _ := s.GetActiveWebhooksForIdentity(svcIdent.ID)
    require.Len(t, hooks, 1)

    err := s.UnregisterWebhook(svcIdent.ID, hooks[0].ID)
    require.NoError(t, err)

    hooks, _ = s.GetActiveWebhooksForIdentity(svcIdent.ID)
    require.Len(t, hooks, 0)
}
```

### Step 2: Run to verify fails

```
go test ./pkg/infra/sqlite/ -run TestRegisterAndListWebhooks -v -count=1
```

Expected: FAIL — methods undefined.

### Step 3: Add domain types and interface

In `pkg/domain/types.go`, add alongside the other domain types:

```go
type IdentityWebhook struct {
    ID         string
    IdentityID string
    URL        string
    Secret     string
    Active     bool
}
```

In `pkg/domain/ports.go`, add after `SettingsStore`:

```go
type WebhookStore interface {
    RegisterWebhook(identityID, url, secret string) error
    UnregisterWebhook(identityID, webhookID string) error
    GetActiveWebhooksForIdentity(identityID string) ([]IdentityWebhook, error)
    // GetWebhooksForChannel returns active webhooks for all service members of a channel.
    GetWebhooksForChannel(channelID int64) ([]IdentityWebhook, error)
}
```

Also add `WebhookStore` to the composite `Store` interface:

```go
type Store interface {
    IdentityStore
    ChannelStore
    MessageStore
    RoleStore
    MentionGroupStore
    SettingsStore
    WebhookStore
    Close() error
}
```

### Step 4: SQLite implementation

Create `pkg/infra/sqlite/webhooks.go`:

```go
package sqlite

import (
    "crypto/rand"
    "encoding/hex"
    "fmt"

    "github.com/Work-Fort/sharkfin/pkg/domain"
)

func (s *Store) RegisterWebhook(identityID, url, secret string) error {
    buf := make([]byte, 16)
    if _, err := rand.Read(buf); err != nil {
        return fmt.Errorf("generate webhook id: %w", err)
    }
    id := hex.EncodeToString(buf)
    _, err := s.db.Exec(
        `INSERT INTO identity_webhooks (id, identity_id, url, secret) VALUES (?, ?, ?, ?)`,
        id, identityID, url, secret,
    )
    if err != nil {
        return fmt.Errorf("register webhook: %w", err)
    }
    return nil
}

func (s *Store) UnregisterWebhook(identityID, webhookID string) error {
    _, err := s.db.Exec(
        `DELETE FROM identity_webhooks WHERE id = ? AND identity_id = ?`,
        webhookID, identityID,
    )
    if err != nil {
        return fmt.Errorf("unregister webhook: %w", err)
    }
    return nil
}

func (s *Store) GetActiveWebhooksForIdentity(identityID string) ([]domain.IdentityWebhook, error) {
    rows, err := s.db.Query(
        `SELECT id, identity_id, url, secret FROM identity_webhooks WHERE identity_id = ? AND active = 1`,
        identityID,
    )
    if err != nil {
        return nil, fmt.Errorf("get webhooks: %w", err)
    }
    defer rows.Close()
    var hooks []domain.IdentityWebhook
    for rows.Next() {
        var h domain.IdentityWebhook
        if err := rows.Scan(&h.ID, &h.IdentityID, &h.URL, &h.Secret); err != nil {
            return nil, fmt.Errorf("scan webhook: %w", err)
        }
        h.Active = true
        hooks = append(hooks, h)
    }
    return hooks, rows.Err()
}

func (s *Store) GetWebhooksForChannel(channelID int64) ([]domain.IdentityWebhook, error) {
    // Returns active webhooks for all service-type identities who are members of channelID.
    rows, err := s.db.Query(`
        SELECT iw.id, iw.identity_id, iw.url, iw.secret
        FROM identity_webhooks iw
        JOIN identities i ON iw.identity_id = i.id
        JOIN channel_members cm ON cm.identity_id = i.id AND cm.channel_id = ?
        WHERE iw.active = 1
          AND i.type = 'service'
    `, channelID)
    if err != nil {
        return nil, fmt.Errorf("get channel webhooks: %w", err)
    }
    defer rows.Close()
    var hooks []domain.IdentityWebhook
    for rows.Next() {
        var h domain.IdentityWebhook
        if err := rows.Scan(&h.ID, &h.IdentityID, &h.URL, &h.Secret); err != nil {
            return nil, fmt.Errorf("scan webhook: %w", err)
        }
        h.Active = true
        hooks = append(hooks, h)
    }
    return hooks, rows.Err()
}
```

### Step 5: Postgres implementation

Create `pkg/infra/postgres/webhooks.go` — same logic, `$1/$2/$3/$4` placeholders, `active = true` in WHERE.

### Step 6: Run tests

```
go test ./pkg/infra/sqlite/ -run TestRegisterAndListWebhooks -v -count=1
go test ./pkg/infra/sqlite/ -run TestUnregisterWebhook -v -count=1
mise run test
```

Expected: All PASS.

### Step 7: Commit

```
git add pkg/domain/types.go pkg/domain/ports.go \
        pkg/infra/sqlite/webhooks.go pkg/infra/sqlite/webhooks_test.go \
        pkg/infra/postgres/webhooks.go
git commit -m "feat: add WebhookStore interface and SQLite/Postgres implementations"
```

---

## Task 7: MCP tools — register_webhook and unregister_webhook

**Files:**
- Modify: `pkg/daemon/mcp_server.go` — add tools + handlers

### Step 1: Write failing test

Add to `pkg/daemon/ws_handler_test.go` or a new `pkg/daemon/mcp_webhook_test.go`:

```go
// This is covered by integration test in Task 11.
// For unit coverage, verify the store calls are wired:
// use the existing wsTestEnv pattern but via MCP is awkward —
// test the store methods directly (Task 6 covers store),
// and rely on Task 11 integration test for the MCP layer.
```

Skip a dedicated unit test here; the store is tested in Task 6 and the wiring is verified in the integration test (Task 11).

### Step 2: Add tool definitions and handlers

In `pkg/daemon/mcp_server.go`, in `NewSharkfinMCP` add to `s.mcpServer.AddTools`:

```go
server.ServerTool{Tool: newRegisterWebhookTool(), Handler: s.handleRegisterWebhook},
server.ServerTool{Tool: newUnregisterWebhookTool(), Handler: s.handleUnregisterWebhook},
```

Add to `toolPermissions` (no special permission — authenticated service identities can register their own webhooks):

No entry in `toolPermissions` means the auth middleware still runs (identity is provisioned) but no additional permission gate is applied. This matches the design: any authenticated identity may register a webhook.

Add tool constructors and handlers:

```go
func newRegisterWebhookTool() mcp.Tool {
    return mcp.NewTool("register_webhook",
        mcp.WithDescription("Register a webhook callback URL for this identity"),
        mcp.WithString("url", mcp.Required(), mcp.Description("Callback URL to POST message.new events to")),
        mcp.WithString("secret", mcp.Description("Optional HMAC secret for signature verification")),
    )
}

func newUnregisterWebhookTool() mcp.Tool {
    return mcp.NewTool("unregister_webhook",
        mcp.WithDescription("Remove a webhook registration"),
        mcp.WithString("webhook_id", mcp.Required(), mcp.Description("ID of the webhook to remove")),
    )
}

func (s *SharkfinMCP) handleRegisterWebhook(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    url := req.GetString("url", "")
    if url == "" {
        return mcp.NewToolResultError("url is required"), nil
    }
    secret := req.GetString("secret", "")

    identity, err := s.store.GetIdentityByUsername(usernameFromCtx(ctx))
    if err != nil {
        return nil, fmt.Errorf("get identity: %w", err)
    }

    if err := s.store.RegisterWebhook(identity.ID, url, secret); err != nil {
        return nil, fmt.Errorf("register webhook: %w", err)
    }

    return mcp.NewToolResultText("webhook registered"), nil
}

func (s *SharkfinMCP) handleUnregisterWebhook(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    webhookID := req.GetString("webhook_id", "")
    if webhookID == "" {
        return mcp.NewToolResultError("webhook_id is required"), nil
    }

    identity, err := s.store.GetIdentityByUsername(usernameFromCtx(ctx))
    if err != nil {
        return nil, fmt.Errorf("get identity: %w", err)
    }

    if err := s.store.UnregisterWebhook(identity.ID, webhookID); err != nil {
        return nil, fmt.Errorf("unregister webhook: %w", err)
    }

    return mcp.NewToolResultText("webhook unregistered"), nil
}
```

### Step 3: Run tests

```
mise run test
```

Expected: PASS (no new unit test here; store + wiring covered by Task 6 and Task 11).

### Step 4: Commit

```
git add pkg/daemon/mcp_server.go
git commit -m "feat: add register_webhook and unregister_webhook MCP tools"
```

---

## Task 8: Per-identity webhook dispatch

**Files:**
- Modify: `pkg/daemon/webhooks.go` — `WebhookEvent`, `WebhookPayload`, `WebhookSubscriber.handleMessage`

### Step 1: Write failing test

Add to `pkg/daemon/webhooks_test.go` (file already exists):

```go
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
        Event:      "message.new",
        ChannelID:  1,
        Channel:    "general",
        ChannelType: "channel",
        From:       "alice",
        FromType:   "user",
        MessageID:  42,
        Body:       "hello",
        SentAt:     "2026-04-05T00:00:00Z",
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
```

### Step 2: Run to verify fails

```
go test ./pkg/daemon/ -run TestFirePerIdentityWebhooks -v -count=1
```

Expected: FAIL — `firePerIdentityWebhook` and extended `WebhookPayload` undefined.

### Step 3: Extend WebhookPayload and add firePerIdentityWebhook

In `pkg/daemon/webhooks.go`, extend `WebhookPayload`:

```go
type WebhookPayload struct {
    Event       string          `json:"event"`
    ChannelID   int64           `json:"channel_id"`
    ChannelName string          `json:"channel_name"`
    ChannelType string          `json:"channel_type"`
    From        string          `json:"from"`
    FromType    string          `json:"from_type"`
    MessageID   int64           `json:"message_id"`
    Body        string          `json:"body"`
    Metadata    *string         `json:"metadata"`
    SentAt      string          `json:"sent_at"`

    // Legacy field — kept for global webhook_url backwards compatibility.
    Recipient   string          `json:"recipient,omitempty"`
    Channel     string          `json:"channel,omitempty"`
}
```

Add `firePerIdentityWebhook`:

```go
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
```

Also extend `WebhookEvent` to carry `ChannelID`, `FromType`, `Body`, `Metadata`:

```go
type WebhookEvent struct {
    ChannelID   int64
    ChannelName string
    ChannelType string
    From        string
    FromType    string  // identity type of sender
    Body        string
    Metadata    *string
    MessageID   int64
    SentAt      time.Time
    Recipients  []string // for legacy global webhook
}
```

Update `WebhookSubscriber.handleMessage` to call per-identity webhooks:

```go
func (ws *WebhookSubscriber) handleMessage(msg domain.MessageEvent) {
    // 1. Legacy global webhook
    webhookURL, err := ws.store.GetSetting("webhook_url")
    if err == nil && webhookURL != "" {
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

    // 2. Per-identity webhooks for all service members of the channel.
    ch, err := ws.store.GetChannelByName(msg.ChannelName)
    if err != nil {
        return
    }
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
```

### Step 4: Update domain.MessageEvent to carry Metadata

In `pkg/domain/types.go`, add `Metadata *string` to `MessageEvent`:

```go
type MessageEvent struct {
    ChannelName string
    ChannelType string
    From        string
    Body        string
    MessageID   int64
    SentAt      time.Time
    Mentions    []string
    ThreadID    *int64
    Metadata    *string  // JSON string, opaque
}
```

### Step 5: Run tests

```
go test ./pkg/daemon/ -run TestFirePerIdentityWebhooks -v -count=1
mise run test
```

Expected: All PASS.

### Step 6: Commit

```
git add pkg/daemon/webhooks.go pkg/daemon/webhooks_test.go \
        pkg/domain/types.go
git commit -m "feat: per-identity webhook dispatch in WebhookSubscriber"
```

---

## Task 9: Webhook recipient scope (service members)

**Files:**
- Modify: `pkg/daemon/webhooks.go` — `computeRecipients`
- Modify: `pkg/daemon/webhooks_test.go` — new test

> Note: `computeRecipients` is used only for the legacy global `webhook_url` path. The per-identity path (Task 8) uses `GetWebhooksForChannel` which already scopes to service members. This task ensures the global webhook also fires for service members.

### Step 1: Write failing test

Add to `pkg/daemon/webhooks_test.go`:

```go
func TestComputeRecipients_IncludesServiceMembers(t *testing.T) {
    store, err := sqlite.Open(":memory:")
    if err != nil {
        t.Fatalf("open store: %v", err)
    }
    defer store.Close()

    // Create identities
    store.UpsertIdentity("uuid-admin", "admin", "Admin", "user", "admin")
    store.UpsertIdentity("uuid-alice", "alice", "Alice", "user", "user")
    store.UpsertIdentity("uuid-bot", "flow-bot", "Flow Bot", "service", "")

    // Create channel with alice and flow-bot as members
    chID, _ := store.CreateChannel("general", true, []string{}, "channel")

    aliceIdent, _ := store.GetIdentityByUsername("alice")
    botIdent, _ := store.GetIdentityByUsername("flow-bot")
    store.AddChannelMember(chID, aliceIdent.ID)
    store.AddChannelMember(chID, botIdent.ID)

    msg := domain.MessageEvent{
        ChannelName: "general",
        ChannelType: "channel",
        From:        "alice",
        Mentions:    []string{},
    }

    recipients := computeRecipients(msg, store)

    found := false
    for _, r := range recipients {
        if r == "flow-bot" {
            found = true
        }
    }
    if !found {
        t.Errorf("expected flow-bot in recipients, got: %v", recipients)
    }
    // alice (sender) should not be in recipients
    for _, r := range recipients {
        if r == "alice" {
            t.Errorf("sender alice should not be in recipients")
        }
    }
}
```

### Step 2: Run to verify fails

```
go test ./pkg/daemon/ -run TestComputeRecipients_IncludesServiceMembers -v -count=1
```

Expected: FAIL — flow-bot not in recipients.

### Step 3: Implement

In `pkg/daemon/webhooks.go`, extend `computeRecipients`:

```go
func computeRecipients(msg domain.MessageEvent, store domain.Store) []string {
    seen := make(map[string]bool)
    var recipients []string

    // Mentioned users
    for _, m := range msg.Mentions {
        if m != msg.From && !seen[m] {
            seen[m] = true
            recipients = append(recipients, m)
        }
    }

    ch, err := store.GetChannelByName(msg.ChannelName)
    if err != nil {
        return recipients
    }

    // DM participants
    if msg.ChannelType == "dm" {
        if members, err := store.ChannelMemberUsernames(ch.ID); err == nil {
            for _, m := range members {
                if m != msg.From && !seen[m] {
                    seen[m] = true
                    recipients = append(recipients, m)
                }
            }
        }
    }

    // Service members of any channel type
    members, err := store.ChannelMemberUsernames(ch.ID)
    if err == nil {
        for _, m := range members {
            if m == msg.From || seen[m] {
                continue
            }
            ident, err := store.GetIdentityByUsername(m)
            if err == nil && ident.Type == "service" {
                seen[m] = true
                recipients = append(recipients, m)
            }
        }
    }

    return recipients
}
```

### Step 4: Run test

```
go test ./pkg/daemon/ -run TestComputeRecipients_IncludesServiceMembers -v -count=1
mise run test
```

Expected: All PASS.

### Step 5: Commit

```
git add pkg/daemon/webhooks.go pkg/daemon/webhooks_test.go
git commit -m "feat: include service channel members in webhook recipients"
```

---

## Task 10: Message metadata column

**Files:**
- Create: `pkg/infra/sqlite/migrations/011_message_metadata.sql`
- Create: `pkg/infra/postgres/migrations/011_message_metadata.sql`
- Modify: `pkg/domain/types.go` — `Message` struct
- Modify: `pkg/domain/ports.go` — `MessageStore.SendMessage`
- Modify: `pkg/infra/sqlite/messages.go` — `SendMessage`, `GetMessages`, `GetUnreadMessages`, scan sites
- Modify: `pkg/infra/postgres/messages.go` — same
- Modify: `pkg/daemon/ws_handler.go` — `handleWSSendMessage`, `handleWSHistory`, `handleWSUnreadMessages`
- Modify: `pkg/daemon/mcp_server.go` — `handleSendMessage`, `handleUnreadMessages`, `handleHistory`
- Modify: `pkg/daemon/hub.go` — `BroadcastMessage` (carry metadata in broadcast envelope)

### Step 1: SQLite migration

```sql
-- 011_message_metadata.sql

-- +goose Up
ALTER TABLE messages ADD COLUMN metadata TEXT;

-- +goose Down
-- SQLite does not support DROP COLUMN in older versions.
-- Leave column in place; it is nullable and ignored by old code.
```

### Step 2: Postgres migration

```sql
-- 011_message_metadata.sql

-- +goose Up
ALTER TABLE messages ADD COLUMN metadata TEXT;

-- +goose Down
ALTER TABLE messages DROP COLUMN IF EXISTS metadata;
```

### Step 3: Write failing store test

In `pkg/infra/sqlite/store_test.go`, add:

```go
func TestSendMessageWithMetadata(t *testing.T) {
    s := newTestStore(t)

    aliceID := upsertIdentity(t, s, "uuid-alice", "alice")
    chID, _ := s.CreateChannel("general", true, []string{aliceID}, "channel")

    meta := `{"event_type":"task_transitioned","event_payload":{"id":"TK-1","to":"review"}}`
    msgID, err := s.SendMessage(chID, aliceID, "body", nil, nil, &meta)
    if err != nil {
        t.Fatalf("send message: %v", err)
    }

    messages, err := s.GetMessages(chID, nil, nil, 10, nil)
    if err != nil {
        t.Fatalf("get messages: %v", err)
    }
    if len(messages) != 1 {
        t.Fatalf("expected 1 message, got %d", len(messages))
    }
    if messages[0].ID != msgID {
        t.Errorf("wrong message id")
    }
    if messages[0].Metadata == nil || *messages[0].Metadata != meta {
        t.Errorf("metadata mismatch: got %v", messages[0].Metadata)
    }
}
```

### Step 4: Run to verify fails

```
go test ./pkg/infra/sqlite/ -run TestSendMessageWithMetadata -v -count=1
```

Expected: FAIL — `SendMessage` does not accept metadata parameter.

### Step 5: Update domain types

In `pkg/domain/types.go`, add `Metadata *string` to `Message`:

```go
type Message struct {
    ID         int64
    ChannelID  int64
    IdentityID string
    From       string
    Body       string
    ThreadID   *int64
    Mentions   []string
    Metadata   *string // JSON string, nullable
    CreatedAt  time.Time
}
```

In `pkg/domain/ports.go`, update `SendMessage` signature:

```go
type MessageStore interface {
    SendMessage(channelID int64, identityID string, body string, threadID *int64, mentionIdentityIDs []string, metadata *string) (int64, error)
    // ...rest unchanged
}
```

### Step 6: Update SQLite SendMessage

In `pkg/infra/sqlite/messages.go`, update `SendMessage`:

- Change signature to add `metadata *string`.
- Change INSERT: `INSERT INTO messages (channel_id, identity_id, body, thread_id, metadata) VALUES (?, ?, ?, ?, ?)`

```go
func (s *Store) SendMessage(channelID int64, identityID string, body string, threadID *int64, mentionIdentityIDs []string, metadata *string) (int64, error) {
    // ... (thread validation unchanged) ...

    res, err := tx.Exec(
        "INSERT INTO messages (channel_id, identity_id, body, thread_id, metadata) VALUES (?, ?, ?, ?, ?)",
        channelID, identityID, body, threadID, metadata,
    )
    // ... rest unchanged ...
}
```

Update the SELECT queries in `GetMessages` and `fetchUnreadForChannel` to include `m.metadata`:

Change:
```sql
SELECT m.id, m.channel_id, m.identity_id, m.body, m.created_at, i.username, m.thread_id
```
To:
```sql
SELECT m.id, m.channel_id, m.identity_id, m.body, m.created_at, i.username, m.thread_id, m.metadata
```

Update scan sites:

```go
if err := rows.Scan(&m.ID, &m.ChannelID, &m.IdentityID, &m.Body, &m.CreatedAt, &m.From, &m.ThreadID, &m.Metadata); err != nil {
```

### Step 7: Update Postgres SendMessage

Same changes in `pkg/infra/postgres/messages.go` — `$1...$5` placeholders, same SELECT extension.

### Step 8: Update callers

**`pkg/daemon/ws_handler.go` — `handleWSSendMessage`:**

Extend request struct:
```go
var d struct {
    Channel  string  `json:"channel"`
    Body     string  `json:"body"`
    ThreadID *int64  `json:"thread_id"`
    Metadata *string `json:"metadata"` // JSON string
}
```

Pass to store:
```go
msgID, err := h.store.SendMessage(ch.ID, identityID, d.Body, d.ThreadID, mentionIDs, d.Metadata)
```

Pass metadata to `BroadcastMessage` via `domain.Message`:
```go
msg := domain.Message{
    // ...
    Metadata: d.Metadata,
}
```

**`pkg/daemon/ws_handler.go` — `handleWSHistory`:**

`handleWSHistory` has its own local `msgInfo` struct. Add `Metadata`:
```go
type msgInfo struct {
    ID       int64    `json:"id"`
    From     string   `json:"from"`
    Body     string   `json:"body"`
    SentAt   string   `json:"sent_at"`
    ThreadID *int64   `json:"thread_id,omitempty"`
    Mentions []string `json:"mentions,omitempty"`
    Metadata *string  `json:"metadata,omitempty"`
}
```

Populate when building the list:
```go
list = append(list, msgInfo{
    ID:       m.ID,
    From:     m.From,
    Body:     m.Body,
    SentAt:   m.CreatedAt.UTC().Format(time.RFC3339),
    ThreadID: m.ThreadID,
    Mentions: m.Mentions,
    Metadata: m.Metadata,
})
```

**`pkg/daemon/ws_handler.go` — `handleWSUnreadMessages`:**

`handleWSUnreadMessages` has its own separate local `msgInfo` struct. Apply the identical change:
```go
type msgInfo struct {
    Channel  string   `json:"channel"`
    From     string   `json:"from"`
    Body     string   `json:"body"`
    SentAt   string   `json:"sent_at"`
    ThreadID *int64   `json:"thread_id,omitempty"`
    Mentions []string `json:"mentions,omitempty"`
    Metadata *string  `json:"metadata,omitempty"`
}
```

Populate `Metadata: m.Metadata` when appending to the list.

**`pkg/daemon/mcp_server.go` — `handleSendMessage`:**

```go
metadata := optionalString(req, "metadata")
msgID, err := s.store.SendMessage(ch.ID, sender.ID, message, threadID, mentionIdentityIDs, metadata)
```

Add `optionalString` helper (if not already present):
```go
func optionalString(req mcp.CallToolRequest, key string) *string {
    v := req.GetString(key, "")
    if v == "" {
        return nil
    }
    return &v
}
```

Update `newSendMessageTool()` to include optional `metadata` parameter:
```go
mcp.WithString("metadata", mcp.Description("Optional JSON metadata string (e.g. {\"event_type\":\"task_transitioned\",...})")),
```

**`pkg/daemon/mcp_server.go` — `handleUnreadMessages` and `handleHistory`:**

Both handlers have their own local `msgInfo` structs. Add `Metadata *string \`json:"metadata,omitempty"\`` to each and populate from `m.Metadata`.

Also update `hub.go` `BroadcastMessage` to carry metadata in WS broadcast:

```go
d := map[string]interface{}{
    // existing fields ...
}
if msg.Metadata != nil {
    d["metadata"] = *msg.Metadata
}
```

And update `MessageEvent` publish in `BroadcastMessage`:
```go
h.bus.Publish(domain.Event{
    Type: domain.EventMessageNew,
    Payload: domain.MessageEvent{
        // existing fields ...
        Metadata: msg.Metadata,
    },
})
```

### Step 9: Run tests

```
go test ./pkg/infra/sqlite/ -run TestSendMessageWithMetadata -v -count=1
mise run test
```

Expected: All PASS.

### Step 10: Commit

```
git add pkg/infra/sqlite/migrations/011_message_metadata.sql \
        pkg/infra/postgres/migrations/011_message_metadata.sql \
        pkg/domain/types.go pkg/domain/ports.go \
        pkg/infra/sqlite/messages.go pkg/infra/postgres/messages.go \
        pkg/daemon/ws_handler.go pkg/daemon/mcp_server.go pkg/daemon/hub.go
git commit -m "feat: add optional metadata column to messages"
```

---

## Task 11: Integration test — bot registration flow

**Files:**
- Modify: `tests/integration_test.go`

This verifies Requirement 5 (bot registration flow) end-to-end using the existing test harness.

### Step 1: Write integration test

Add to `tests/integration_test.go`. The test uses the existing helpers: `startTestServer`, `initUser`, `grantAdmin`, `toolCall`, `toolResultText`, and `jwks.signJWT`.

`initUser` provisions a `"user"`-type identity. For the bot we need `"service"` type, so we call `jwks.signJWT` directly and drive the MCP initialize + first tool call manually (same pattern as `initUser`, but with `userType: "service"`).

```go
func TestScenario_BotRegistrationFlow(t *testing.T) {
    // Capture webhook POSTs.
    var mu sync.Mutex
    var received []map[string]interface{}
    hookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var p map[string]interface{}
        json.NewDecoder(r.Body).Decode(&p)
        mu.Lock()
        received = append(received, p)
        mu.Unlock()
        w.WriteHeader(http.StatusOK)
    }))
    defer hookSrv.Close()

    env := startTestServer(t)

    // Step 1: Admin user (first identity → auto-promoted to admin).
    // Creates the "general" channel that the bot will join.
    alice := env.initUser("alice-uuid", "alice", "Alice")
    _, chResp := env.toolCall(alice.sessionID, alice.token, 10, "channel_create", map[string]interface{}{
        "name": "general", "public": true,
    })
    toolResultText(t, chResp) // panics on tool error

    // Step 2: Provision bot identity via a service-type JWT.
    botToken := env.jwks.signJWT("bot-uuid", "flow-bot", "Flow Bot", "service")
    httpResp, _ := env.mcpRequest("", botToken, "initialize", 1, map[string]interface{}{
        "protocolVersion": "2025-03-26",
        "capabilities":    map[string]interface{}{},
        "clientInfo":      map[string]string{"name": "flow-bot", "version": "0.1"},
    })
    botSessionID := httpResp.Header.Get("Mcp-Session-Id")
    if botSessionID == "" {
        t.Fatal("no session ID for bot")
    }
    // First tool call auto-provisions the identity with role=bot.
    _, ulResp := env.toolCall(botSessionID, botToken, 2, "user_list", map[string]interface{}{})
    toolResultText(t, ulResp)

    // Verify role=bot via store.
    botIdent, err := env.srv.Store().GetIdentityByUsername("flow-bot")
    if err != nil {
        t.Fatalf("get bot identity: %v", err)
    }
    if botIdent.Role != "bot" {
        t.Errorf("expected role=bot, got %q", botIdent.Role)
    }
    if botIdent.Type != "service" {
        t.Errorf("expected type=service, got %q", botIdent.Type)
    }

    // Step 3: Bot registers its webhook.
    _, regResp := env.toolCall(botSessionID, botToken, 3, "register_webhook", map[string]interface{}{
        "url": hookSrv.URL,
    })
    toolResultText(t, regResp)

    // Step 4: Bot joins "general".
    _, joinResp := env.toolCall(botSessionID, botToken, 4, "channel_join", map[string]interface{}{
        "channel": "general",
    })
    toolResultText(t, joinResp)

    // Step 5: Alice sends a message in "general".
    _, sendResp := env.toolCall(alice.sessionID, alice.token, 5, "send_message", map[string]interface{}{
        "channel": "general",
        "message": "hello from alice",
    })
    toolResultText(t, sendResp)

    // Step 6: Wait for async webhook delivery and assert payload.
    time.Sleep(300 * time.Millisecond)

    mu.Lock()
    defer mu.Unlock()
    if len(received) == 0 {
        t.Fatal("expected webhook POST, got none")
    }
    payload := received[0]
    if payload["event"] != "message.new" {
        t.Errorf("event = %v, want message.new", payload["event"])
    }
    if payload["channel_name"] != "general" {
        t.Errorf("channel_name = %v, want general", payload["channel_name"])
    }
    if payload["from"] != "alice" {
        t.Errorf("from = %v, want alice", payload["from"])
    }
    if payload["body"] != "hello from alice" {
        t.Errorf("body = %v, want 'hello from alice'", payload["body"])
    }
    msgID, ok := payload["message_id"].(string)
    if !ok || msgID == "" {
        t.Fatal("expected non-empty message_id in webhook payload")
    }

    // Step 7: Bot replies using message_id as thread_id (threading convention).
    mu.Unlock() // release before making MCP call to avoid deadlock
    _, replyResp := env.toolCall(botSessionID, botToken, 6, "send_message", map[string]interface{}{
        "channel":   "general",
        "message":   "hello back from bot",
        "thread_id": msgID,
    })
    toolResultText(t, replyResp)

    // Step 8: Verify the reply is threaded (thread_id matches the anchor message).
    msgs, err := env.srv.Store().GetMessages("general", 10, 0)
    if err != nil {
        t.Fatalf("GetMessages: %v", err)
    }
    var botReply *store.Message
    for i := range msgs {
        if msgs[i].From == "flow-bot" {
            botReply = &msgs[i]
            break
        }
    }
    if botReply == nil {
        t.Fatal("bot reply message not found in store")
    }
    if botReply.ThreadID == nil || *botReply.ThreadID != msgID {
        t.Errorf("bot reply thread_id = %v, want %q", botReply.ThreadID, msgID)
    }
    mu.Lock() // re-acquire so deferred Unlock is balanced
}
```

Required imports (add to the import block if not already present): `net/http/httptest`, `sync`.

### Step 2: Run integration test

```
mise run e2e
```

Expected: PASS.

### Step 3: Commit

```
git add tests/integration_test.go
git commit -m "test: integration test for bot registration flow"
```

---

## Task 12: WipeAll update

**Files:**
- Modify: `pkg/infra/sqlite/identities.go` — `WipeAll`
- Modify: `pkg/infra/postgres/identities.go` — `WipeAll`

The new `identity_webhooks` table must be cleared by `WipeAll`.

### Step 1: Add to wipe list

In both stores, add `"identity_webhooks"` to the tables slice (and to `validWipeTables` allowlist in the SQLite version):

```go
tables := []string{
    "mention_group_members",
    "mention_groups",
    "message_mentions",
    "read_cursors",
    "messages",
    "channel_members",
    "channels",
    "settings",
    "identity_webhooks",  // new
    "identities",
}
```

In `validWipeTables` (SQLite only):
```go
"identity_webhooks": true,
```

### Step 2: Run tests

```
mise run test
```

### Step 3: Commit

```
git add pkg/infra/sqlite/identities.go pkg/infra/postgres/identities.go
git commit -m "fix: include identity_webhooks in WipeAll"
```

---

## Verification Checklist

- [ ] `mise run test` passes with all new store tests
- [ ] `mise run build` succeeds
- [ ] `mise run lint` passes
- [ ] Migration 009 creates `bot` role with correct permissions (check in-memory SQLite via test)
- [ ] `UpsertIdentity` with `type="service"` returns `role="bot"` (TestUpsertIdentityServiceAutoBot)
- [ ] `RegisterWebhook` / `UnregisterWebhook` / `GetActiveWebhooksForIdentity` work (TestRegisterAndListWebhooks, TestUnregisterWebhook)
- [ ] `GetWebhooksForChannel` only returns webhooks for service-type channel members
- [ ] `computeRecipients` includes service channel members (TestComputeRecipients_IncludesServiceMembers)
- [ ] Per-identity webhook fires for service members on every channel message
- [ ] Legacy global `webhook_url` still fires (existing webhook tests pass unchanged)
- [ ] `SendMessage` accepts optional `metadata *string` parameter
- [ ] `GetMessages` / `GetUnreadMessages` return `metadata` field (TestSendMessageWithMetadata)
- [ ] WS `history` and `unread_messages` responses include `metadata` field
- [ ] MCP `send_message` accepts optional `metadata` argument
- [ ] WS broadcast envelope includes `metadata` when present
- [ ] `WipeAll` clears `identity_webhooks` table
- [ ] Integration test passes: bot registers, joins channel, receives webhook on message send
- [ ] Integration test verifies bot reply threading: `message_id` from webhook payload used as `thread_id`, stored reply has matching `thread_id`
