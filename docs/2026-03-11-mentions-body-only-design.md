# Mentions: Body-Only Extraction Design

## Problem

The `send_message` API accepts mentions two ways:

1. Explicit `mentions` array parameter (list of usernames)
2. `@username` patterns extracted from the message body via regex

This creates an inconsistency: agents can silently mention users without the
mention appearing in the message text. It also means the same mention can be
specified twice (in the array and in the body), requiring deduplication logic.

## Goal

Remove the explicit `mentions` parameter. All mentions are extracted from the
message body via `@username` regex — the same behavior for agents and humans.

## Changes

### MCP (`mcp_tools.go`)

Remove the `mentions` parameter from the `send_message` tool definition:

```go
// Remove:
mcp.WithArray("mentions", mcp.Description("Usernames to @mention ..."), mcp.WithStringItems())
```

In `handleSendMessage`, stop reading the `mentions` field from arguments.

### WS (`ws_handler.go`)

Remove the `Mentions []string` field from the `handleWSSendMessage` payload
struct. Stop passing it to `resolveMentions`.

### Resolution (`mentions.go`)

Simplify `resolveMentions` signature:

```go
// Before:
func resolveMentions(store domain.UserStore, body string, explicit []string) ([]int64, []string)

// After:
func resolveMentions(store domain.UserStore, body string) ([]int64, []string)
```

Remove the explicit-mentions loop. The function now only extracts `@username`
patterns from the body.

### Broadcast

No change. The broadcast `mentions` field is populated from `resolveMentions`
return values, which now come exclusively from body extraction.

### Tests

Update any tests that pass explicit mentions to instead include `@username` in
the message body. This affects unit tests in `ws_handler_test.go`,
`mcp_server_test.go`, and e2e tests.

## Migration

This is a breaking API change for MCP clients that rely on the `mentions`
parameter. Clients must update to include `@username` in the message body
instead. Since sharkfin is pre-1.0 and all clients are internal, this is
acceptable.
