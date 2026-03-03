# Migrate MCP Implementation to mcp-go

**Date:** 2026-03-03
**Status:** Proposed
**Context:** tpm directed both Sharkfin and Nexus to standardize on
[mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) as the MCP surface
for both projects will grow.

## Current State

Sharkfin hand-rolls its MCP server:

| Component | File | Purpose |
|-----------|------|---------|
| JSON-RPC types | `pkg/protocol/jsonrpc.go` | Request, Response, RequestID, error codes |
| MCP handler | `pkg/daemon/mcp_handler.go` | 16 tools, HTTP handler, tool dispatch |
| Session manager | `pkg/daemon/session.go` | Identity tokens, register/identify, presence |
| MCP bridge | `cmd/mcpbridge/mcp_bridge.go` | stdio ↔ HTTP proxy, token interception |
| Server wiring | `pkg/daemon/server.go` | Routes `POST /mcp` to MCPHandler |
| Tests | `pkg/daemon/mcp_handler_test.go` | 17 unit tests |
| Tests | `pkg/protocol/jsonrpc_test.go` | 8 unit tests |

## Target State

Replace the hand-rolled JSON-RPC routing and tool dispatch with mcp-go's
`MCPServer` + `StreamableHTTPServer`. Keep all business logic (DB queries,
session management, broadcast, webhooks) intact.

### What changes

- **Tool definitions** — move from inline `map[string]interface{}` to
  `mcp.NewTool()` builder API with typed schemas.
- **Tool handlers** — convert from
  `handleFoo(w, req, args, session)` to
  `func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)`.
- **Transport** — replace our `http.Handler` with mcp-go's
  `StreamableHTTPServer.ServeHTTP` mounted at `/mcp`.
- **Protocol types** — delete `pkg/protocol/`; mcp-go provides JSON-RPC
  types, error codes, and marshaling.
- **Session auth** — use mcp-go's `ToolHandlerMiddleware` to enforce
  identification on protected tools, mapping mcp-go session IDs to our
  `SessionManager` usernames.

### What stays the same

- `SessionManager` and identity token lifecycle (custom auth flow).
- `Hub` (WebSocket broadcast), `PresenceHandler`, `WSHandler`.
- `mentions.go`, `webhooks.go`, all DB layer code.
- MCP bridge — minimal changes (it's a 178-line proxy; the protocol is
  the same on the wire).

---

## Tasks

### Task 1 — Add mcp-go dependency and create tool registry

Add the dependency and define all 16 tools using mcp-go's builder API in a
new file `pkg/daemon/mcp_tools.go`.

```go
// pkg/daemon/mcp_tools.go

func newSendMessageTool() mcp.Tool {
    return mcp.NewTool("send_message",
        mcp.WithDescription("Send a text message to a channel."),
        mcp.WithString("channel", mcp.Required(), mcp.Description("Channel name")),
        mcp.WithString("message", mcp.Required(), mcp.Description("Message text (UTF-8)")),
        mcp.WithArray("mentions", mcp.Description("Usernames to @mention")),
        mcp.WithNumber("thread_id", mcp.Description("Parent message ID for threading")),
    )
}
// ... similar for all 16 tools
```

**Verify:** `mise run build` compiles.

### Task 2 — Migrate tool handlers to mcp-go signature

Create `pkg/daemon/mcp_server.go` with:

1. A `SharkfinMCP` struct holding `*db.DB`, `*SessionManager`, `*Hub`.
2. A constructor that creates an `*server.MCPServer`, registers all tools,
   and installs auth middleware.
3. Handler methods matching
   `func(ctx, mcp.CallToolRequest) (*mcp.CallToolResult, error)`.

**Auth middleware pattern:**

```go
func (s *SharkfinMCP) authMiddleware(next server.ToolHandlerFunc) server.ToolHandlerFunc {
    return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        // Public tools pass through
        if req.Params.Name == "register" || req.Params.Name == "identify" ||
           req.Params.Name == "get_identity_token" {
            return next(ctx, req)
        }
        // Protected tools require an identified session
        sess := server.ClientSessionFromContext(ctx)
        username, ok := s.sessions.GetUsernameByMCPSession(sess.SessionID())
        if !ok {
            return mcp.NewToolResultError("not identified: call register or identify first"), nil
        }
        ctx = context.WithValue(ctx, usernameKey, username)
        return next(ctx, req)
    }
}
```

**Handler conversion example (send_message):**

```go
func (s *SharkfinMCP) handleSendMessage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    username := ctx.Value(usernameKey).(string)
    channel := req.GetString("channel", "")
    message := req.GetString("message", "")
    // ... same DB logic, broadcast, return mcp.NewToolResultText(...)
}
```

**Verify:** `mise run build` compiles. Unit tests in next task.

### Task 3 — Wire StreamableHTTPServer into daemon

In `pkg/daemon/server.go`:

1. Create `SharkfinMCP` and get its `*server.MCPServer`.
2. Create `server.NewStreamableHTTPServer(mcpServer, ...)` with stateful
   sessions and endpoint path `/mcp`.
3. Mount `streamableHTTP.ServeHTTP` on the mux instead of the old
   `MCPHandler`.
4. `PresenceHandler` and `WSHandler` stay on their existing routes.

```go
// In NewServer():
sharkfinMCP := NewSharkfinMCP(sm, database, hub)
httpTransport := server.NewStreamableHTTPServer(sharkfinMCP.Server(),
    server.WithStateful(true),
    server.WithEndpointPath("/mcp"),
)
mux.Handle("/mcp", httpTransport)
```

**Session mapping update:**

`SessionManager` needs a new mapping: mcp-go session ID → username.
Add `GetUsernameByMCPSession(sessionID string) (string, bool)` and
update `Register`/`Identify` to store this mapping using the mcp-go
session ID obtained from context.

**Verify:** `mise run build` compiles. Manual smoke test with bridge.

### Task 4 — Delete deprecated code and pkg/protocol

1. Delete `pkg/protocol/jsonrpc.go` and `pkg/protocol/jsonrpc_test.go`.
2. Delete the old `MCPHandler` struct and all `handle*` methods from
   `pkg/daemon/mcp_handler.go` (file can be removed entirely).
3. Remove helper functions (`writeJSONRPCResult`, `writeJSONRPCError`,
   `writeToolResult`) that are now handled by mcp-go.
4. Update any remaining imports.

**Verify:** `mise run build` compiles with no references to `pkg/protocol`.

### Task 5 — Migrate tests

Rewrite `pkg/daemon/mcp_handler_test.go` to work with the new mcp-go
server. Two approaches:

- **Option A:** Use mcp-go's `server.NewTestStreamableHTTPServer()` and
  send tool calls via mcp-go's client.
- **Option B:** Use `httptest.NewServer` wrapping
  `streamableHTTP.ServeHTTP` and send raw JSON-RPC (keeps tests closer to
  current style).

All 17 existing test cases must pass with equivalent assertions.
Update `tests/integration_test.go` for the new `NewServer` signature if it
changed.

**Verify:** `mise run test` passes. `mise run e2e` passes.

### Task 6 — Update bridge (if needed)

The bridge speaks JSON-RPC 2.0 over HTTP — the wire protocol is unchanged.
Likely no changes needed. Verify:

- `get_identity_token` interception still works (bridge-side, never hits
  server).
- `Mcp-Session-Id` header is set by mcp-go's StreamableHTTPServer.
- `register`/`identify` flow works end-to-end.

If mcp-go uses a different session header name or protocol behavior,
update the bridge accordingly.

**Verify:** `mise run e2e` passes. Manual test with live daemon + bridge.

---

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| mcp-go session ID format/header differs from ours | Check mcp-go docs; bridge may need header name update |
| `get_identity_token` tool must appear in tools/list but is never called server-side | Register it with a no-op handler; bridge intercepts before it reaches server |
| mcp-go adds overhead or changes response format | E2E tests catch regressions; bridge is format-agnostic (passes bytes through) |
| Session lifecycle mismatch (mcp-go sessions vs our presence sessions) | Use `OnRegisterSession`/`OnUnregisterSession` hooks to sync state |

## Dependencies

- `github.com/mark3labs/mcp-go` (MIT license, latest release 2026-02-27)
