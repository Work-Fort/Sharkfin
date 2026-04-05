# Flow Adapter: REST API + Client Library — Implementation Plan

**Goal:** Expose Sharkfin's existing store operations as REST endpoints for service-to-service communication, update the Go client library to cover messaging/channel/webhook operations, and remove the unused `secret` field from `identity_webhooks`.

**Architecture:** Three sequential phases: (1) remove the `secret` field via migrations + code cleanup, simplifying all webhook store calls; (2) add a new `pkg/daemon/rest_handlers.go` file with `net/http` handlers for 8 REST endpoints, registered on the existing mux behind the `mw` middleware; (3) extend the existing `client/` package with REST-backed methods using a new `httpDo` helper that shares the same auth options as the WS client.

**Tech Stack:** Go, `net/http`, `modernc.org/sqlite`, Postgres, Goose migrations, `charmbracelet/log`, `github.com/Work-Fort/Passport/go/service-auth`

---

## Phase 1: Remove `secret` field from `identity_webhooks`

This is a pure cleanup pass. HMAC signing was removed from scope. The field is dead weight everywhere.

### Task 1.1: SQLite migration — drop `secret` column

**Files:**
- Create: `pkg/infra/sqlite/migrations/013_webhooks_drop_secret.sql`

```sql
-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Remove unused secret column from identity_webhooks.

-- +goose Up
ALTER TABLE identity_webhooks DROP COLUMN secret;

-- +goose Down
ALTER TABLE identity_webhooks ADD COLUMN secret TEXT NOT NULL DEFAULT '';
```

Note: `modernc.org/sqlite` supports `DROP COLUMN` (SQLite 3.35+). The existing migrations use plain `ALTER TABLE` for column additions, so this pattern is consistent.

Run: `go test ./pkg/infra/sqlite/...`

### Task 1.2: Postgres migration — drop `secret` column

**Files:**
- Create: `pkg/infra/postgres/migrations/013_webhooks_drop_secret.sql`

```sql
-- SPDX-License-Identifier: AGPL-3.0-or-later
-- Remove unused secret column from identity_webhooks.

-- +goose Up
ALTER TABLE identity_webhooks DROP COLUMN secret;

-- +goose Down
ALTER TABLE identity_webhooks ADD COLUMN secret TEXT NOT NULL DEFAULT '';
```

Run: `go test ./pkg/infra/postgres/...` (if postgres tests exist; otherwise build)

### Task 1.3: Remove `Secret` field from domain type

**File:** `pkg/domain/types.go`

```go
// before
type IdentityWebhook struct {
    ID         string
    IdentityID string
    URL        string
    Secret     string
    Active     bool
}

// after
type IdentityWebhook struct {
    ID         string
    IdentityID string
    URL        string
    Active     bool
}
```

### Task 1.4: Update `WebhookStore` interface — remove `secret` param, return ID from `RegisterWebhook`

**File:** `pkg/domain/ports.go`

```go
// before
RegisterWebhook(identityID, url, secret string) error

// after
RegisterWebhook(identityID, url string) (string, error)
```

Returning the generated (or existing) webhook ID eliminates the re-fetch in the REST handler.

### Task 1.5: Update SQLite store

**File:** `pkg/infra/sqlite/webhooks.go`

Use `RETURNING id` to get the ID back from INSERT/ON CONFLICT. SQLite supports `RETURNING` since 3.35+ (same version that supports `DROP COLUMN`).

```go
func (s *Store) RegisterWebhook(identityID, url string) (string, error) {
    buf := make([]byte, 16)
    if _, err := rand.Read(buf); err != nil {
        return "", fmt.Errorf("generate webhook id: %w", err)
    }
    id := hex.EncodeToString(buf)
    var returnedID string
    err := s.db.QueryRow(`
        INSERT INTO identity_webhooks (id, identity_id, url, active)
        VALUES (?, ?, ?, 1)
        ON CONFLICT(identity_id, url) DO UPDATE SET active = 1
        RETURNING id
    `, id, identityID, url).Scan(&returnedID)
    if err != nil {
        return "", fmt.Errorf("register webhook: %w", err)
    }
    return returnedID, nil
}

func (s *Store) GetActiveWebhooksForIdentity(identityID string) ([]domain.IdentityWebhook, error) {
    rows, err := s.db.Query(
        `SELECT id, identity_id, url FROM identity_webhooks WHERE identity_id = ? AND active = 1`,
        identityID,
    )
    if err != nil {
        return nil, fmt.Errorf("get webhooks: %w", err)
    }
    defer rows.Close()
    var hooks []domain.IdentityWebhook
    for rows.Next() {
        var h domain.IdentityWebhook
        if err := rows.Scan(&h.ID, &h.IdentityID, &h.URL); err != nil {
            return nil, fmt.Errorf("scan webhook: %w", err)
        }
        h.Active = true
        hooks = append(hooks, h)
    }
    return hooks, rows.Err()
}

func (s *Store) GetWebhooksForChannel(channelID int64) ([]domain.IdentityWebhook, error) {
    rows, err := s.db.Query(`
        SELECT iw.id, iw.identity_id, iw.url
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
        if err := rows.Scan(&h.ID, &h.IdentityID, &h.URL); err != nil {
            return nil, fmt.Errorf("scan webhook: %w", err)
        }
        h.Active = true
        hooks = append(hooks, h)
    }
    return hooks, rows.Err()
}
```

### Task 1.6: Update Postgres store

**File:** `pkg/infra/postgres/webhooks.go`

Same changes as Task 1.5: remove `secret` param, return ID, use `RETURNING id`, update SELECT to omit `secret`, update Scan calls.

```go
func (s *Store) RegisterWebhook(identityID, url string) (string, error) {
    buf := make([]byte, 16)
    if _, err := rand.Read(buf); err != nil {
        return "", fmt.Errorf("generate webhook id: %w", err)
    }
    id := hex.EncodeToString(buf)
    var returnedID string
    err := s.db.QueryRow(`
        INSERT INTO identity_webhooks (id, identity_id, url)
        VALUES ($1, $2, $3)
        ON CONFLICT (identity_id, url) DO UPDATE SET active = true
        RETURNING id
    `, id, identityID, url).Scan(&returnedID)
    if err != nil {
        return "", fmt.Errorf("register webhook: %w", err)
    }
    return returnedID, nil
}
```

(Update GetActiveWebhooksForIdentity and GetWebhooksForChannel selects/scans in same file.)

### Task 1.7: Update MCP handler — remove secret param

**Files:**
- `pkg/daemon/mcp_server.go` — `handleRegisterWebhook`
- `pkg/daemon/mcp_tools.go` — `newRegisterWebhookTool` schema

In `handleRegisterWebhook` (`mcp_server.go`), remove the `secret` extraction and update the store call:

```go
func (s *SharkfinMCP) handleRegisterWebhook(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    url := req.GetString("url", "")
    if url == "" {
        return mcp.NewToolResultError("url is required"), nil
    }

    identity, err := s.store.GetIdentityByUsername(usernameFromCtx(ctx))
    if err != nil {
        return nil, fmt.Errorf("get identity: %w", err)
    }

    if _, err := s.store.RegisterWebhook(identity.ID, url); err != nil {
        return nil, fmt.Errorf("register webhook: %w", err)
    }

    return mcp.NewToolResultText("webhook registered"), nil
}
```

In `newRegisterWebhookTool()` (`mcp_tools.go`), remove the `secret` parameter from the tool's input schema.

### Task 1.8: Build + test

```
mise run build
mise run test
```

Expected: PASS. No other callers of `RegisterWebhook` with the old signature should exist after steps 1.4–1.7.

### Commit 1

```
git add pkg/infra/sqlite/migrations/013_webhooks_drop_secret.sql
git add pkg/infra/postgres/migrations/013_webhooks_drop_secret.sql
git add pkg/domain/types.go pkg/domain/ports.go
git add pkg/infra/sqlite/webhooks.go pkg/infra/postgres/webhooks.go
git add pkg/daemon/mcp_server.go pkg/daemon/mcp_tools.go
git commit -m "refactor: remove secret field from identity_webhooks"
```

---

## Phase 2: REST API Endpoints

New file `pkg/daemon/rest_handlers.go`. All handlers registered in `server.go` under `mux`. Auth via the same `mw` middleware already used for WS/MCP. Identity auto-provisioned on first call (same pattern as WS handler's `ServeHTTP`).

### Task 2.1: Write tests first

**File:** `tests/integration_test.go` (add to existing file)

Add a helper `restRequest` that sends an authenticated HTTP request to the test server:

```go
func (e *testEnv) restRequest(method, path, token string, body interface{}) (*http.Response, []byte) {
    e.t.Helper()
    var bodyReader io.Reader
    if body != nil {
        b, _ := json.Marshal(body)
        bodyReader = bytes.NewReader(b)
    }
    req, err := http.NewRequest(method, fmt.Sprintf("http://%s%s", e.addr, path), bodyReader)
    if err != nil {
        e.t.Fatalf("create request: %v", err)
    }
    if body != nil {
        req.Header.Set("Content-Type", "application/json")
    }
    if token != "" {
        req.Header.Set("Authorization", "Bearer "+token)
    }
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        e.t.Fatalf("do request: %v", err)
    }
    defer resp.Body.Close()
    b, _ := io.ReadAll(resp.Body)
    return resp, b
}
```

Write `TestRESTMessages`, `TestRESTChannels`, `TestRESTWebhooks`, `TestRESTIdentityRegister` covering:
- POST /api/v1/channels/{channel}/messages → 201 with message id
- GET /api/v1/channels/{channel}/messages → 200 with messages array
- POST /api/v1/channels → 201 with channel object
- GET /api/v1/channels → 200 with channels array
- POST /api/v1/channels/{channel}/join → 200
- POST /api/v1/webhooks → 201 with webhook object (id, url, active)
- GET /api/v1/webhooks → 200 with webhooks array
- DELETE /api/v1/webhooks/{id} → 204
- POST /api/v1/auth/register → 201 with identity object

Run: `go test ./tests/ -run TestREST` — expect compile failures initially (handlers don't exist yet).

### Task 2.2: Create `rest_handlers.go`

**File:** `pkg/daemon/rest_handlers.go`

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
    "encoding/json"
    "net/http"
    "strconv"
    "time"

    "github.com/charmbracelet/log"

    auth "github.com/Work-Fort/Passport/go/service-auth"
    "github.com/Work-Fort/sharkfin/pkg/domain"
)

// restIdentity auto-provisions and returns the calling identity.
// Returns nil and writes 401/500 on failure.
func restIdentity(w http.ResponseWriter, r *http.Request, store domain.Store) *domain.Identity {
    passportIdent, ok := auth.IdentityFromContext(r.Context())
    if !ok {
        http.Error(w, "unauthorized", http.StatusUnauthorized)
        return nil
    }
    role := passportIdent.Type
    if role == "" {
        role = "user"
    }
    localIdentity, err := store.UpsertIdentity(passportIdent.ID, passportIdent.Username, passportIdent.DisplayName, passportIdent.Type, role)
    if err != nil {
        log.Error("rest: identity provisioning", "err", err)
        http.Error(w, "identity provisioning failed", http.StatusInternalServerError)
        return nil
    }
    return localIdentity
}

func writeJSON(w http.ResponseWriter, status int, v any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(v)
}

// RESTHandler holds all REST endpoint handlers.
type RESTHandler struct {
    store domain.Store
    hub   *Hub
    bus   domain.EventBus
}

func NewRESTHandler(store domain.Store, hub *Hub, bus domain.EventBus) *RESTHandler {
    return &RESTHandler{store: store, hub: hub, bus: bus}
}

// --- POST /api/v1/auth/register ---

func (h *RESTHandler) handleRegisterIdentity(w http.ResponseWriter, r *http.Request) {
    identity := restIdentity(w, r, h.store)
    if identity == nil {
        return
    }
    writeJSON(w, http.StatusCreated, map[string]any{
        "id":       identity.ID,
        "username": identity.Username,
        "type":     identity.Type,
        "role":     identity.Role,
    })
}

// --- GET /api/v1/channels ---

func (h *RESTHandler) handleListChannels(w http.ResponseWriter, r *http.Request) {
    identity := restIdentity(w, r, h.store)
    if identity == nil {
        return
    }
    channels, err := h.store.ListAllChannelsWithMembership(identity.ID)
    if err != nil {
        log.Error("rest: list channels", "err", err)
        http.Error(w, "list channels failed", http.StatusInternalServerError)
        return
    }
    type channelResp struct {
        ID     int64  `json:"id"`
        Name   string `json:"name"`
        Public bool   `json:"public"`
        Member bool   `json:"member"`
    }
    var out []channelResp
    for _, ch := range channels {
        out = append(out, channelResp{ID: ch.ID, Name: ch.Name, Public: ch.Public, Member: ch.Member})
    }
    if out == nil {
        out = []channelResp{}
    }
    writeJSON(w, http.StatusOK, out)
}

// --- POST /api/v1/channels ---

func (h *RESTHandler) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
    identity := restIdentity(w, r, h.store)
    if identity == nil {
        return
    }
    var req struct {
        Name   string `json:"name"`
        Public bool   `json:"public"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
        http.Error(w, "name is required", http.StatusBadRequest)
        return
    }
    channelID, err := h.store.CreateChannel(req.Name, req.Public, []string{identity.ID}, "channel")
    if err != nil {
        log.Error("rest: create channel", "err", err)
        http.Error(w, "create channel failed", http.StatusInternalServerError)
        return
    }
    writeJSON(w, http.StatusCreated, map[string]any{
        "id":     channelID,
        "name":   req.Name,
        "public": req.Public,
    })
}

// --- POST /api/v1/channels/{channel}/join ---

func (h *RESTHandler) handleJoinChannel(w http.ResponseWriter, r *http.Request) {
    identity := restIdentity(w, r, h.store)
    if identity == nil {
        return
    }
    channelName := r.PathValue("channel")
    ch, err := h.store.GetChannelByName(channelName)
    if err != nil {
        http.Error(w, "channel not found", http.StatusNotFound)
        return
    }
    if err := h.store.AddChannelMember(ch.ID, identity.ID); err != nil {
        log.Error("rest: join channel", "err", err)
        http.Error(w, "join channel failed", http.StatusInternalServerError)
        return
    }
    w.WriteHeader(http.StatusOK)
}

// --- POST /api/v1/channels/{channel}/messages ---

func (h *RESTHandler) handleSendMessage(w http.ResponseWriter, r *http.Request) {
    identity := restIdentity(w, r, h.store)
    if identity == nil {
        return
    }
    channelName := r.PathValue("channel")
    ch, err := h.store.GetChannelByName(channelName)
    if err != nil {
        http.Error(w, "channel not found", http.StatusNotFound)
        return
    }

    var req struct {
        Body     string         `json:"body"`
        Metadata map[string]any `json:"metadata"`
        ThreadID *int64         `json:"thread_id"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" {
        http.Error(w, "body is required", http.StatusBadRequest)
        return
    }

    var metadataStr *string
    if req.Metadata != nil {
        b, _ := json.Marshal(req.Metadata)
        s := string(b)
        metadataStr = &s
    }

    sentAt := time.Now()
    msgID, err := h.store.SendMessage(ch.ID, identity.ID, req.Body, req.ThreadID, nil, metadataStr)
    if err != nil {
        log.Error("rest: send message", "err", err)
        http.Error(w, "send message failed", http.StatusInternalServerError)
        return
    }

    // Publish to event bus so WebhookSubscriber and hub broadcast pick it up,
    // exactly as the WS handler does.
    if h.bus != nil {
        h.bus.Publish(domain.Event{
            Type: domain.EventMessageNew,
            Payload: domain.MessageEvent{
                ChannelName: channelName,
                ChannelType: ch.Type,
                From:        identity.Username,
                Body:        req.Body,
                MessageID:   msgID,
                SentAt:      sentAt,
                ThreadID:    req.ThreadID,
                Metadata:    metadataStr,
            },
        })
    }

    writeJSON(w, http.StatusCreated, map[string]any{
        "id":       msgID,
        "body":     req.Body,
        "metadata": req.Metadata,
        "sent_at":  sentAt.UTC().Format(time.RFC3339),
    })
}

// --- GET /api/v1/channels/{channel}/messages ---

func (h *RESTHandler) handleListMessages(w http.ResponseWriter, r *http.Request) {
    identity := restIdentity(w, r, h.store)
    if identity == nil {
        return
    }
    channelName := r.PathValue("channel")
    ch, err := h.store.GetChannelByName(channelName)
    if err != nil {
        http.Error(w, "channel not found", http.StatusNotFound)
        return
    }

    q := r.URL.Query()
    var before, after *int64
    var limit int = 50
    if v := q.Get("before"); v != "" {
        if n, err := strconv.ParseInt(v, 10, 64); err == nil {
            before = &n
        }
    }
    if v := q.Get("after"); v != "" {
        if n, err := strconv.ParseInt(v, 10, 64); err == nil {
            after = &n
        }
    }
    if v := q.Get("limit"); v != "" {
        if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
            limit = n
        }
    }

    msgs, err := h.store.GetMessages(ch.ID, before, after, limit, nil)
    if err != nil {
        log.Error("rest: list messages", "err", err)
        http.Error(w, "list messages failed", http.StatusInternalServerError)
        return
    }

    type msgResp struct {
        ID       int64   `json:"id"`
        From     string  `json:"from"`
        Body     string  `json:"body"`
        Metadata *string `json:"metadata,omitempty"`
        ThreadID *int64  `json:"thread_id,omitempty"`
        SentAt   string  `json:"sent_at"`
    }
    out := make([]msgResp, 0, len(msgs))
    for _, m := range msgs {
        out = append(out, msgResp{
            ID:       m.ID,
            From:     m.From,
            Body:     m.Body,
            Metadata: m.Metadata,
            ThreadID: m.ThreadID,
            SentAt:   m.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
        })
    }
    writeJSON(w, http.StatusOK, out)
}

// --- POST /api/v1/webhooks ---

func (h *RESTHandler) handleRegisterWebhook(w http.ResponseWriter, r *http.Request) {
    identity := restIdentity(w, r, h.store)
    if identity == nil {
        return
    }
    var req struct {
        URL string `json:"url"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
        http.Error(w, "url is required", http.StatusBadRequest)
        return
    }
    webhookID, err := h.store.RegisterWebhook(identity.ID, req.URL)
    if err != nil {
        log.Error("rest: register webhook", "err", err)
        http.Error(w, "register webhook failed", http.StatusInternalServerError)
        return
    }
    writeJSON(w, http.StatusCreated, map[string]any{
        "id":     webhookID,
        "url":    req.URL,
        "active": true,
    })
}

// --- GET /api/v1/webhooks ---

func (h *RESTHandler) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
    identity := restIdentity(w, r, h.store)
    if identity == nil {
        return
    }
    hooks, err := h.store.GetActiveWebhooksForIdentity(identity.ID)
    if err != nil {
        log.Error("rest: list webhooks", "err", err)
        http.Error(w, "list webhooks failed", http.StatusInternalServerError)
        return
    }
    type hookResp struct {
        ID     string `json:"id"`
        URL    string `json:"url"`
        Active bool   `json:"active"`
    }
    out := make([]hookResp, 0, len(hooks))
    for _, h := range hooks {
        out = append(out, hookResp{ID: h.ID, URL: h.URL, Active: h.Active})
    }
    writeJSON(w, http.StatusOK, out)
}

// --- DELETE /api/v1/webhooks/{id} ---

func (h *RESTHandler) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
    identity := restIdentity(w, r, h.store)
    if identity == nil {
        return
    }
    webhookID := r.PathValue("id")
    if webhookID == "" {
        http.Error(w, "id is required", http.StatusBadRequest)
        return
    }
    if err := h.store.UnregisterWebhook(identity.ID, webhookID); err != nil {
        log.Error("rest: delete webhook", "err", err)
        http.Error(w, "delete webhook failed", http.StatusInternalServerError)
        return
    }
    w.WriteHeader(http.StatusNoContent)
}
```

### Task 2.3: Register routes in `server.go`

**File:** `pkg/daemon/server.go`

After creating `wsHandler` and before `return`, add:

```go
rest := NewRESTHandler(store, hub, bus)

mux.Handle("POST /api/v1/auth/register", mw(http.HandlerFunc(rest.handleRegisterIdentity)))
mux.Handle("GET /api/v1/channels", mw(http.HandlerFunc(rest.handleListChannels)))
mux.Handle("POST /api/v1/channels", mw(http.HandlerFunc(rest.handleCreateChannel)))
mux.Handle("POST /api/v1/channels/{channel}/join", mw(http.HandlerFunc(rest.handleJoinChannel)))
mux.Handle("POST /api/v1/channels/{channel}/messages", mw(http.HandlerFunc(rest.handleSendMessage)))
mux.Handle("GET /api/v1/channels/{channel}/messages", mw(http.HandlerFunc(rest.handleListMessages)))
mux.Handle("POST /api/v1/webhooks", mw(http.HandlerFunc(rest.handleRegisterWebhook)))
mux.Handle("GET /api/v1/webhooks", mw(http.HandlerFunc(rest.handleListWebhooks)))
mux.Handle("DELETE /api/v1/webhooks/{id}", mw(http.HandlerFunc(rest.handleDeleteWebhook)))
```

These use Go 1.22 method+path pattern syntax (`"POST /api/v1/..."`) consistent with existing routes like `"GET /presence"` in server.go.

### Task 2.4: Build + run tests

```
mise run build
go test ./tests/ -run TestREST
mise run test
```

### Commit 2



```
git add pkg/daemon/rest_handlers.go pkg/daemon/server.go
git add tests/integration_test.go
git commit -m "feat: REST API endpoints for service-to-service communication"
```

---

## Phase 3: Go Client Library Updates

The existing `client/` package is WebSocket-only. The new REST operations will be added as REST-backed methods. Rather than refactoring the WS client, add a parallel HTTP transport path: a `baseURL` field (derived from the WS URL) and an `httpDo` helper.

The existing `Dial` function takes a WS URL (e.g. `ws://localhost:16000/ws`). The REST base URL is derived from that by stripping `/ws` and changing the scheme.

### Task 3.1: Add REST transport to Client struct

**File:** `client/client.go`

Add `baseURL string` and `httpClient *http.Client` fields to `Client`. Populate them in `Dial`:

```go
type Client struct {
    conn    *websocket.Conn
    url     string
    baseURL string      // e.g. "http://localhost:16000"
    httpClient *http.Client

    // ... existing fields unchanged
}
```

In `Dial`, derive `baseURL`:

```go
// Derive HTTP base URL from WS URL.
baseURL := strings.TrimSuffix(url, "/ws")
baseURL = strings.TrimSuffix(baseURL, "/")
if strings.HasPrefix(baseURL, "ws://") {
    baseURL = "http://" + baseURL[5:]
} else if strings.HasPrefix(baseURL, "wss://") {
    baseURL = "https://" + baseURL[6:]
}

c := &Client{
    conn:       conn,
    url:        url,
    baseURL:    baseURL,
    httpClient: &http.Client{Timeout: 30 * time.Second},
    // ... rest unchanged
}
```

Add `httpDo` helper:

```go
// httpDo performs an authenticated HTTP request and decodes the JSON response into out.
// Pass out=nil to discard the body (e.g. for 204 responses).
func (c *Client) httpDo(ctx context.Context, method, path string, reqBody, out any) (int, error) {
    var bodyReader io.Reader
    if reqBody != nil {
        b, err := json.Marshal(reqBody)
        if err != nil {
            return 0, fmt.Errorf("client: marshal: %w", err)
        }
        bodyReader = bytes.NewReader(b)
    }

    req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
    if err != nil {
        return 0, fmt.Errorf("client: new request: %w", err)
    }
    if reqBody != nil {
        req.Header.Set("Content-Type", "application/json")
    }
    if c.opts.token != "" {
        req.Header.Set("Authorization", "Bearer "+c.opts.token)
    } else if c.opts.apiKey != "" {
        req.Header.Set("Authorization", "Bearer "+c.opts.apiKey)
    }

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return 0, fmt.Errorf("client: http: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 400 {
        b, _ := io.ReadAll(resp.Body)
        return resp.StatusCode, &ServerError{Message: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))}
    }

    if out != nil && resp.StatusCode != http.StatusNoContent {
        if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
            return resp.StatusCode, fmt.Errorf("client: decode response: %w", err)
        }
    }
    return resp.StatusCode, nil
}
```

Required imports to add: `"bytes"`, `"io"`, `"strings"`, `"net/http"`.

### Task 3.2: Add `Webhook` type to `types.go`

**File:** `client/types.go`

```go
// Webhook represents a registered per-identity webhook.
type Webhook struct {
    ID     string `json:"id"`
    URL    string `json:"url"`
    Active bool   `json:"active"`
}
```

Also update `SendOpts` to add `Metadata`:

```go
type SendOpts struct {
    ThreadID *int64
    Metadata *string // JSON string
}
```

### Task 3.3: Add REST-backed methods to `requests.go`

**File:** `client/requests.go`

Add the following methods. The existing WS-backed methods are not changed.

```go
// --- REST: Webhooks ---

// RegisterWebhook registers a webhook URL for the calling identity.
// Returns the webhook ID.
func (c *Client) RegisterWebhook(ctx context.Context, url string) (string, error) {
    var out struct {
        ID string `json:"id"`
    }
    if _, err := c.httpDo(ctx, http.MethodPost, "/api/v1/webhooks", map[string]string{"url": url}, &out); err != nil {
        return "", err
    }
    return out.ID, nil
}

// UnregisterWebhook removes a registered webhook by ID.
func (c *Client) UnregisterWebhook(ctx context.Context, id string) error {
    _, err := c.httpDo(ctx, http.MethodDelete, "/api/v1/webhooks/"+id, nil, nil)
    return err
}

// ListWebhooks returns all active webhooks for the calling identity.
func (c *Client) ListWebhooks(ctx context.Context) ([]Webhook, error) {
    var out []Webhook
    if _, err := c.httpDo(ctx, http.MethodGet, "/api/v1/webhooks", nil, &out); err != nil {
        return nil, err
    }
    return out, nil
}

// --- REST: Identity ---

// Register registers the calling identity as a service bot.
func (c *Client) Register(ctx context.Context) error {
    _, err := c.httpDo(ctx, http.MethodPost, "/api/v1/auth/register", nil, nil)
    return err
}
```

**Update existing `SendMessage`** to support metadata via the REST endpoint (or add metadata support to the WS path). The requirement says `SendOpts` gains a `Metadata` field. Update the WS-backed `SendMessage` to pass `metadata` if set:

```go
func (c *Client) SendMessage(ctx context.Context, channel, body string, opts *SendOpts) (int64, error) {
    d := map[string]any{
        "channel": channel,
        "body":    body,
    }
    if opts != nil && opts.ThreadID != nil {
        d["thread_id"] = *opts.ThreadID
    }
    if opts != nil && opts.Metadata != nil {
        d["metadata"] = *opts.Metadata
    }
    raw, err := c.request(ctx, "send_message", d)
    if err != nil {
        return 0, err
    }
    var resp struct {
        ID int64 `json:"id"`
    }
    if err := json.Unmarshal(raw, &resp); err != nil {
        return 0, err
    }
    return resp.ID, nil
}
```

**Note:** `CreateChannel`, `JoinChannel`, and `ListChannels` (`Channels`) already exist as WS-backed methods in `requests.go`. No changes needed unless they should fall back to REST. The requirements say "the client can either use REST directly or continue using WebSocket internally — that's an implementation detail." Keep them WS-backed; only add REST for the operations that have no WS equivalent (webhooks, identity registration).

### Task 3.4: Update `client_test.go`

**File:** `client/client_test.go`

Add tests for `RegisterWebhook`, `UnregisterWebhook`, `ListWebhooks`, and `Register`. These require a test HTTP server (use `httptest.NewServer`) rather than a WS server, since they call the REST endpoints. Or: run against the integration test server if tests are in `tests/` package.

The cleanest approach is to add a `TestClientREST` in `tests/integration_test.go` that spins up the full test server (already available via `startTestServer`) and calls the client methods.

### Task 3.5: Build + test

```
mise run build
mise run test
```

### Commit 3

```
git add client/client.go client/types.go client/requests.go client/client_test.go
git add tests/integration_test.go
git commit -m "feat: add REST-backed webhook and identity methods to Go client"
```

---

## Verification Checklist

Before calling this work done, confirm each item by running the relevant command or reading the output:

### Secret cleanup
- [ ] `grep -r 'secret' pkg/domain/types.go` — no match
- [ ] `grep -r '\.Secret' pkg/` — no match
- [ ] `grep -r 'secret' pkg/infra/sqlite/webhooks.go pkg/infra/postgres/webhooks.go` — no match
- [ ] `grep -r 'RegisterWebhook.*secret\|secret.*RegisterWebhook' pkg/` — no match
- [ ] Migration 013 exists for both sqlite and postgres
- [ ] `mise run build` — no compile errors

### REST endpoints
- [ ] `go test ./tests/ -run TestREST` — all pass
- [ ] `curl -X POST http://localhost:16000/api/v1/auth/register -H "Authorization: Bearer <token>"` — 201
- [ ] `curl http://localhost:16000/api/v1/channels -H "Authorization: Bearer <token>"` — 200 with JSON array
- [ ] `curl -X POST http://localhost:16000/api/v1/channels -H "Authorization: Bearer <token>" -d '{"name":"test","public":true}'` — 201
- [ ] `curl -X POST http://localhost:16000/api/v1/channels/test/messages -H "Authorization: Bearer <token>" -d '{"body":"hello"}'` — 201
- [ ] `curl http://localhost:16000/api/v1/channels/test/messages -H "Authorization: Bearer <token>"` — 200 with messages
- [ ] `curl -X POST http://localhost:16000/api/v1/webhooks -H "Authorization: Bearer <token>" -d '{"url":"http://flow:17200/v1/webhooks/sharkfin"}'` — 201 with id
- [ ] `curl http://localhost:16000/api/v1/webhooks -H "Authorization: Bearer <token>"` — 200 with webhooks
- [ ] `curl -X DELETE http://localhost:16000/api/v1/webhooks/<id> -H "Authorization: Bearer <token>"` — 204
- [ ] Unauthenticated request to any endpoint returns 401
- [ ] `mise run test` — full test suite passes

### Client library
- [ ] `grep -r 'RegisterWebhook\|UnregisterWebhook\|ListWebhooks\|Register' client/requests.go` — all present
- [ ] `SendOpts.Metadata` field exists in `client/types.go`
- [ ] `Webhook` type exists in `client/types.go`
- [ ] `httpDo` helper exists in `client/client.go`
- [ ] `go test ./client/...` — passes
- [ ] `mise run test` — full suite passes

---

## Notes

**`r.PathValue`:** Available in `net/http` from Go 1.22. The codebase already uses `"GET /presence"` etc. on the mux, confirming Go 1.22+. `r.PathValue("channel")` works with `{channel}` patterns.

**`RegisterWebhook` — secret param in requirements doc:** The requirements doc's webhook endpoint body shows a `"secret"` field, but HMAC signing was explicitly removed from scope per the task brief. The REST handler accepts and ignores any `secret` field in the request body (it decodes into an anonymous struct that does not include `secret`). No secret storage or HMAC computation needed.
