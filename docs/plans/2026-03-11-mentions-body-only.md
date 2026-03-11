# Mentions Body-Only Extraction Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the explicit `mentions` parameter from `send_message` so all mentions are extracted from the message body via `@username` regex.

**Architecture:** Simplify `resolveMentions` to drop the `explicit` parameter, remove the `mentions` field from MCP and WS APIs, update all tests.

**Tech Stack:** Go, mcp-go, gorilla/websocket

---

### File Structure

All modifications — no new files:

| File | Responsibility | Change |
|------|---------------|--------|
| `pkg/daemon/mentions.go` | Mention extraction/resolution | Drop `explicit` param from `resolveMentions` |
| `pkg/daemon/mcp_tools.go` | MCP tool definitions | Remove `mentions` param from `send_message` |
| `pkg/daemon/mcp_server.go` | MCP request handlers | Stop reading `mentions` arg, update call |
| `pkg/daemon/ws_handler.go` | WS request handlers | Remove `Mentions` struct field, update call |
| `pkg/daemon/ws_handler_test.go` | WS unit tests | Remove `"mentions"` key from request maps |
| `tests/e2e/sharkfin_test.go` | E2e tests | Remove `"mentions"` key, ensure body has `@user` |

Note: The design doc mentions `mcp_server_test.go` — that file does not exist.
MCP handlers are tested via e2e tests only. No action needed.

---

### Task 1: Update unit tests to use body-only mentions

Tests are updated first while the code still compiles and the `mentions` field
is still accepted (but ignored by the test — body extraction already works).
This verifies that body-only extraction produces the same results.

**Files:**
- Modify: `pkg/daemon/ws_handler_test.go:528-563, 733-751`

- [ ] **Step 1: Update TestWSSendMessageWithMentions (lines 541-545)**

The body already contains `"hey @bob check this"`, so body extraction works.
Remove the redundant explicit `"mentions"` key:

```go
// Before (line 541-545):
	resp := wsReq(t, aliceConn, "send_message", map[string]interface{}{
		"channel":  "general",
		"body":     "hey @bob check this",
		"mentions": []string{"bob"},
	}, "m1")

// After:
	resp := wsReq(t, aliceConn, "send_message", map[string]interface{}{
		"channel": "general",
		"body":    "hey @bob check this",
	}, "m1")
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./pkg/daemon/ -run TestWSSendMessageWithMentions -v`
Expected: PASS — body extraction already resolves `@bob`

- [ ] **Step 3: Update TestWSSendMessageMentionInvalidUser (lines 743-747)**

Remove the explicit `"mentions"` key. Body already has `"hey @nobody"`:

```go
// Before (line 743-747):
	resp := wsReq(t, conn, "send_message", map[string]interface{}{
		"channel":  "general",
		"body":     "hey @nobody",
		"mentions": []string{"nobody"},
	}, "m1")

// After:
	resp := wsReq(t, conn, "send_message", map[string]interface{}{
		"channel": "general",
		"body":    "hey @nobody",
	}, "m1")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/daemon/ -run TestWSSendMessageMentionInvalidUser -v`
Expected: PASS

- [ ] **Step 5: Verify TestWSSendMessageAutoMention is unchanged**

Run: `go test ./pkg/daemon/ -run TestWSSendMessageAutoMention -v`
Expected: PASS — this test already uses body-only mentions

- [ ] **Step 6: Run all unit tests**

Run: `mise run test`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add pkg/daemon/ws_handler_test.go
git commit -m "test: remove explicit mentions from unit tests, rely on body extraction"
```

---

### Task 2: Update e2e tests to use body-only mentions

**Files:**
- Modify: `tests/e2e/sharkfin_test.go:2136-2140, 2217-2221, 3675-3677`

- [ ] **Step 1: Update TestWSMentions (lines 2136-2140)**

Body already has `"hey @bob look at this"`. Remove the `"mentions"` key:

```go
// Before (line 2136-2140):
	env, err := alice.Req("send_message", map[string]any{
		"channel":  "general",
		"body":     "hey @bob look at this",
		"mentions": []string{"bob"},
	}, "m1")

// After:
	env, err := alice.Req("send_message", map[string]any{
		"channel": "general",
		"body":    "hey @bob look at this",
	}, "m1")
```

- [ ] **Step 2: Update TestWSMentionInvalidUser (lines 2217-2221)**

Body already has `"hey @ghost"`. Remove the `"mentions"` key:

```go
// Before (line 2217-2221):
	env, err := ws.Req("send_message", map[string]any{
		"channel":  "general",
		"body":     "hey @ghost",
		"mentions": []string{"ghost"},
	}, "m1")

// After:
	env, err := ws.Req("send_message", map[string]any{
		"channel": "general",
		"body":    "hey @ghost",
	}, "m1")
```

- [ ] **Step 3: Update TestBackupExportImport (lines 3675-3677)**

The message body `"hello from alice"` does NOT contain `@bob`. Add it:

```go
// Before (line 3675-3677):
	r, err = alice.ToolCall("send_message", map[string]any{
		"channel": "general", "message": "hello from alice", "mentions": []string{"bob"},
	})

// After:
	r, err = alice.ToolCall("send_message", map[string]any{
		"channel": "general", "message": "hello from @bob via alice",
	})
```

- [ ] **Step 4: Run e2e tests**

Run: `mise run e2e`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add tests/e2e/sharkfin_test.go
git commit -m "test: remove explicit mentions from e2e tests, rely on body extraction"
```

---

### Task 3: Remove explicit mentions from API

All call sites (tests) have been updated. Now remove the parameter from the
code. This is one atomic change across 4 files.

**Files:**
- Modify: `pkg/daemon/mentions.go:12-47`
- Modify: `pkg/daemon/mcp_tools.go:71`
- Modify: `pkg/daemon/mcp_server.go:373, 394`
- Modify: `pkg/daemon/ws_handler.go:490, 514`

- [ ] **Step 1: Simplify resolveMentions**

Replace the function in `pkg/daemon/mentions.go`:

```go
// resolveMentions extracts @username patterns from the message body,
// deduplicates, and resolves each against the database. Invalid usernames
// are silently ignored.
func resolveMentions(store domain.UserStore, body string) ([]int64, []string) {
	seen := make(map[string]bool)
	var userIDs []int64
	var usernames []string

	for _, match := range mentionRe.FindAllStringSubmatch(body, -1) {
		u := match[1]
		if seen[u] {
			continue
		}
		seen[u] = true

		user, err := store.GetUserByUsername(u)
		if err != nil {
			continue
		}
		userIDs = append(userIDs, user.ID)
		usernames = append(usernames, user.Username)
	}

	return userIDs, usernames
}
```

- [ ] **Step 2: Remove mentions from MCP tool definition**

In `pkg/daemon/mcp_tools.go`, delete line 71:

```go
mcp.WithArray("mentions", mcp.Description("Usernames to @mention in this message"), mcp.WithStringItems()),
```

- [ ] **Step 3: Update MCP handler**

In `pkg/daemon/mcp_server.go`, delete line 373:

```go
mentionsList := req.GetStringSlice("mentions", nil)
```

Change line 394 from:

```go
mentionUserIDs, mentionUsernames := resolveMentions(s.store, message, mentionsList)
```

To:

```go
mentionUserIDs, mentionUsernames := resolveMentions(s.store, message)
```

- [ ] **Step 4: Update WS handler**

In `pkg/daemon/ws_handler.go`, delete the `Mentions` field (line 490):

```go
Mentions []string `json:"mentions"`
```

Change line 514 from:

```go
mentionUserIDs, mentionUsernames := resolveMentions(h.store, d.Body, d.Mentions)
```

To:

```go
mentionUserIDs, mentionUsernames := resolveMentions(h.store, d.Body)
```

- [ ] **Step 5: Verify it compiles**

Run: `mise run build`
Expected: PASS

- [ ] **Step 6: Run unit tests**

Run: `mise run test`
Expected: PASS

- [ ] **Step 7: Run e2e tests**

Run: `mise run e2e`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add pkg/daemon/mentions.go pkg/daemon/mcp_tools.go pkg/daemon/mcp_server.go pkg/daemon/ws_handler.go
git commit -m "feat: remove explicit mentions parameter, extract from body only"
```

---

### Task 4: Full CI pass

- [ ] **Step 1: Run CI**

Run: `mise run ci`
Expected: All lint, unit, and e2e tests pass.

- [ ] **Step 2: Verify no regressions**

Check that:
- `TestWSSendMessageWithMentions` passes (mentions extracted from body)
- `TestWSSendMessageAutoMention` passes (unchanged)
- `TestWSSendMessageMentionInvalidUser` passes (invalid mention in body ignored)
- `TestWSMentions` passes (e2e)
- `TestBackupExportImport` passes (mention now via `@bob` in body)
- All other tests pass
