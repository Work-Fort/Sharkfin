# Unread Counts & Mark Read Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `unread_counts` and `mark_read` endpoints to both WS and MCP interfaces.

**Architecture:** Two new DB methods (`GetUnreadCounts`, `MarkRead`) wired into both handler layers. No schema changes — builds on existing `read_cursors`, `messages`, `message_mentions`, `channel_members` tables.

**Tech Stack:** Go, SQLite, gorilla/websocket, MCP JSON-RPC

---

### Task 1: Add `GetUnreadCounts` DB method

**Files:**
- Modify: `pkg/db/messages.go`

**Step 1: Add the UnreadCount type and method**

Add after the `Message` struct (line ~21):

```go
// UnreadCount holds per-channel unread and mention counts.
type UnreadCount struct {
	ChannelName  string
	UnreadCount  int
	MentionCount int
}
```

Add the DB method at end of file:

```go
// GetUnreadCounts returns per-channel unread message and mention counts for a user.
// Only returns channels with >0 unreads. Excludes the user's own messages.
func (d *DB) GetUnreadCounts(userID int64) ([]UnreadCount, error) {
	rows, err := d.db.Query(`
		SELECT c.name,
		       COUNT(m.id) AS unread_count,
		       COUNT(mm.message_id) AS mention_count
		FROM channel_members cm
		JOIN channels c ON cm.channel_id = c.id
		JOIN messages m ON m.channel_id = c.id
		  AND m.user_id != ?
		  AND m.id > COALESCE(
			(SELECT last_read_message_id FROM read_cursors
			 WHERE channel_id = c.id AND user_id = ?), 0)
		LEFT JOIN message_mentions mm ON mm.message_id = m.id AND mm.user_id = ?
		WHERE cm.user_id = ?
		GROUP BY c.id
		HAVING unread_count > 0
	`, userID, userID, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("get unread counts: %w", err)
	}
	defer rows.Close()

	var counts []UnreadCount
	for rows.Next() {
		var c UnreadCount
		if err := rows.Scan(&c.ChannelName, &c.UnreadCount, &c.MentionCount); err != nil {
			return nil, fmt.Errorf("scan unread count: %w", err)
		}
		counts = append(counts, c)
	}
	return counts, rows.Err()
}
```

**Step 2: Build**

Run: `mise run build`
Expected: Clean build

**Step 3: Commit**

```bash
git add pkg/db/messages.go
git commit -m "feat: add GetUnreadCounts DB method"
```

---

### Task 2: Add `MarkRead` DB method

**Files:**
- Modify: `pkg/db/messages.go`

**Step 1: Add the MarkRead method**

Add at end of file:

```go
// MarkRead advances the read cursor for a user in a channel.
// If messageID is nil, advances to the latest message.
// Forward-only: never moves the cursor backwards.
func (d *DB) MarkRead(userID, channelID int64, messageID *int64) error {
	var targetID int64
	if messageID != nil {
		targetID = *messageID
	} else {
		err := d.db.QueryRow(
			"SELECT COALESCE(MAX(id), 0) FROM messages WHERE channel_id = ?",
			channelID,
		).Scan(&targetID)
		if err != nil {
			return fmt.Errorf("get max message id: %w", err)
		}
	}

	if targetID == 0 {
		return nil // no messages in channel
	}

	_, err := d.db.Exec(`
		INSERT INTO read_cursors (channel_id, user_id, last_read_message_id)
		VALUES (?, ?, ?)
		ON CONFLICT(channel_id, user_id)
		DO UPDATE SET last_read_message_id = MAX(excluded.last_read_message_id, last_read_message_id)
	`, channelID, userID, targetID)
	if err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	return nil
}
```

**Step 2: Build**

Run: `mise run build`
Expected: Clean build

**Step 3: Commit**

```bash
git add pkg/db/messages.go
git commit -m "feat: add MarkRead DB method"
```

---

### Task 3: Wire `unread_counts` into WS handler

**Files:**
- Modify: `pkg/daemon/ws_handler.go`

**Step 1: Add dispatch case**

In the `ServeHTTP` switch after `"unread_messages"` (line ~207), add:

```go
		case "unread_counts":
			h.handleWSUnreadCounts(sendCh, req.Ref, userID)
```

**Step 2: Add handler method**

Add after `handleWSUnreadMessages`:

```go
func (h *WSHandler) handleWSUnreadCounts(sendCh chan<- []byte, ref string, userID int64) {
	counts, err := h.db.GetUnreadCounts(userID)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	type countInfo struct {
		Channel      string `json:"channel"`
		UnreadCount  int    `json:"unread_count"`
		MentionCount int    `json:"mention_count"`
	}
	var list []countInfo
	for _, c := range counts {
		list = append(list, countInfo{
			Channel:      c.ChannelName,
			UnreadCount:  c.UnreadCount,
			MentionCount: c.MentionCount,
		})
	}
	sendReply(sendCh, ref, true, map[string]interface{}{"counts": list})
}
```

**Step 3: Build**

Run: `mise run build`
Expected: Clean build

**Step 4: Commit**

```bash
git add pkg/daemon/ws_handler.go
git commit -m "feat: wire unread_counts into WS handler"
```

---

### Task 4: Wire `mark_read` into WS handler

**Files:**
- Modify: `pkg/daemon/ws_handler.go`

**Step 1: Add dispatch case**

In the `ServeHTTP` switch after the `unread_counts` case, add:

```go
		case "mark_read":
			h.handleWSMarkRead(sendCh, req.Ref, req.D, userID)
```

**Step 2: Add handler method**

Add after `handleWSUnreadCounts`:

```go
func (h *WSHandler) handleWSMarkRead(sendCh chan<- []byte, ref string, rawD json.RawMessage, userID int64) {
	var d struct {
		Channel   string `json:"channel"`
		MessageID *int64 `json:"message_id"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}
	if d.Channel == "" {
		sendError(sendCh, ref, "channel is required")
		return
	}

	ch, err := h.db.GetChannelByName(d.Channel)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}

	isMember, err := h.db.IsChannelMember(ch.ID, userID)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	if !isMember {
		sendError(sendCh, ref, "you are not a participant of this channel")
		return
	}

	if err := h.db.MarkRead(userID, ch.ID, d.MessageID); err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, nil)
}
```

**Step 3: Build**

Run: `mise run build`
Expected: Clean build

**Step 4: Commit**

```bash
git add pkg/daemon/ws_handler.go
git commit -m "feat: wire mark_read into WS handler"
```

---

### Task 5: Wire `unread_counts` into MCP handler

**Files:**
- Modify: `pkg/daemon/mcp_handler.go`

**Step 1: Add tool definition to `handleToolsList`**

Add to the tools array (after the `history` tool definition, before the closing `}`):

```go
			{
				"name":        "unread_counts",
				"description": "Get unread message and mention counts per channel. Returns only channels with unreads.",
				"inputSchema": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
```

**Step 2: Add dispatch case**

In `handleToolsCall` switch after `"history"` (line ~248):

```go
	case "unread_counts":
		h.handleUnreadCounts(w, req, session)
```

**Step 3: Add handler method**

Add after `handleHistory`:

```go
func (h *MCPHandler) handleUnreadCounts(w http.ResponseWriter, req *protocol.Request, session *MCPSession) {
	user, err := h.db.GetUserByUsername(session.Username)
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
		return
	}

	counts, err := h.db.GetUnreadCounts(user.ID)
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
		return
	}

	type countInfo struct {
		Channel      string `json:"channel"`
		UnreadCount  int    `json:"unread_count"`
		MentionCount int    `json:"mention_count"`
	}
	var list []countInfo
	for _, c := range counts {
		list = append(list, countInfo{
			Channel:      c.ChannelName,
			UnreadCount:  c.UnreadCount,
			MentionCount: c.MentionCount,
		})
	}

	data, _ := json.Marshal(list)
	writeToolResult(w, req.ID, string(data))
}
```

**Step 4: Build**

Run: `mise run build`
Expected: Clean build

**Step 5: Commit**

```bash
git add pkg/daemon/mcp_handler.go
git commit -m "feat: wire unread_counts into MCP handler"
```

---

### Task 6: Wire `mark_read` into MCP handler

**Files:**
- Modify: `pkg/daemon/mcp_handler.go`

**Step 1: Add tool definition to `handleToolsList`**

Add to the tools array (after `unread_counts`):

```go
			{
				"name":        "mark_read",
				"description": "Mark a channel as read up to a specific message, or the latest message if not specified. Forward-only: cannot move cursor backwards.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"channel":    map[string]string{"type": "string", "description": "Channel name"},
						"message_id": map[string]interface{}{"type": "integer", "description": "Message ID to mark as read up to (default: latest)"},
					},
					"required": []string{"channel"},
				},
			},
```

**Step 2: Add dispatch case**

In `handleToolsCall` switch after `"unread_counts"`:

```go
	case "mark_read":
		h.handleMarkRead(w, req, params.Arguments, session)
```

**Step 3: Add handler method**

Add after `handleUnreadCounts`:

```go
func (h *MCPHandler) handleMarkRead(w http.ResponseWriter, req *protocol.Request, args json.RawMessage, session *MCPSession) {
	var a struct {
		Channel   string `json:"channel"`
		MessageID *int64 `json:"message_id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		writeJSONRPCError(w, req.ID, protocol.InvalidParams, "invalid arguments")
		return
	}

	user, err := h.db.GetUserByUsername(session.Username)
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
		return
	}

	ch, err := h.getChannelByName(a.Channel)
	if err != nil {
		writeJSONRPCError(w, req.ID, -32001, err.Error())
		return
	}

	isMember, err := h.db.IsChannelMember(ch.ID, user.ID)
	if err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
		return
	}
	if !isMember {
		writeJSONRPCError(w, req.ID, -32001, "you are not a participant of this channel")
		return
	}

	if err := h.db.MarkRead(user.ID, ch.ID, a.MessageID); err != nil {
		writeJSONRPCError(w, req.ID, protocol.InternalError, err.Error())
		return
	}

	writeToolResult(w, req.ID, fmt.Sprintf("marked %s as read", a.Channel))
}
```

**Step 4: Build**

Run: `mise run build`
Expected: Clean build

**Step 5: Commit**

```bash
git add pkg/daemon/mcp_handler.go
git commit -m "feat: wire mark_read into MCP handler"
```

---

### Task 7: E2E tests

**Files:**
- Modify: `tests/e2e/sharkfin_test.go`

**Step 1: Add `TestUnreadCounts` e2e test**

Uses the MCP harness (Client). Tests:
- Two users in a channel, Alice sends 3 messages (1 mentioning Bob)
- Bob calls `unread_counts` → expects `{channel, unread_count: 3, mention_count: 1}`
- Bob calls `mark_read(channel)` → success
- Bob calls `unread_counts` → expects empty array

```go
func TestUnreadCounts(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	bob := harness.NewClient(addr)
	if err := bob.RegisterFlow("bob"); err != nil {
		t.Fatal(err)
	}
	defer bob.DisconnectPresence()

	// Create channel with both users
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name": "counts-test", "public": false, "members": []string{"bob"},
	})
	if err != nil || r.Error != nil {
		t.Fatalf("create channel: err=%v rpc=%+v", err, r.Error)
	}

	// Alice sends 3 messages, one mentioning bob
	for i, body := range []string{"hello", "world", "hey @bob check this"} {
		r, err := alice.ToolCall("send_message", map[string]any{
			"channel": "counts-test", "message": body,
		})
		if err != nil || r.Error != nil {
			t.Fatalf("send message %d: err=%v rpc=%+v", i, err, r.Error)
		}
	}

	// Bob checks unread counts
	r, err = bob.ToolCall("unread_counts", map[string]any{})
	if err != nil || r.Error != nil {
		t.Fatalf("unread_counts: err=%v rpc=%+v", err, r.Error)
	}

	var counts []struct {
		Channel      string `json:"channel"`
		UnreadCount  int    `json:"unread_count"`
		MentionCount int    `json:"mention_count"`
	}
	if err := json.Unmarshal([]byte(r.Text), &counts); err != nil {
		t.Fatalf("unmarshal counts: %v (text: %s)", err, r.Text)
	}

	found := false
	for _, c := range counts {
		if c.Channel == "counts-test" {
			found = true
			if c.UnreadCount != 3 {
				t.Errorf("unread_count = %d, want 3", c.UnreadCount)
			}
			if c.MentionCount != 1 {
				t.Errorf("mention_count = %d, want 1", c.MentionCount)
			}
		}
	}
	if !found {
		t.Errorf("counts-test not in unread_counts response: %s", r.Text)
	}

	// Bob marks channel as read
	r, err = bob.ToolCall("mark_read", map[string]any{"channel": "counts-test"})
	if err != nil || r.Error != nil {
		t.Fatalf("mark_read: err=%v rpc=%+v", err, r.Error)
	}

	// Unread counts should now be empty for this channel
	r, err = bob.ToolCall("unread_counts", map[string]any{})
	if err != nil || r.Error != nil {
		t.Fatalf("unread_counts after mark_read: err=%v rpc=%+v", err, r.Error)
	}

	var countsAfter []struct {
		Channel string `json:"channel"`
	}
	if r.Text != "null" && r.Text != "[]" {
		if err := json.Unmarshal([]byte(r.Text), &countsAfter); err != nil {
			t.Fatalf("unmarshal counts after: %v", err)
		}
		for _, c := range countsAfter {
			if c.Channel == "counts-test" {
				t.Error("counts-test still in unread_counts after mark_read")
			}
		}
	}
}
```

**Step 2: Add `TestMarkReadForwardOnly` e2e test**

Tests that marking with an older message ID doesn't regress the cursor.

```go
func TestMarkReadForwardOnly(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	bob := harness.NewClient(addr)
	if err := bob.RegisterFlow("bob"); err != nil {
		t.Fatal(err)
	}
	defer bob.DisconnectPresence()

	// Create channel
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name": "fwd-test", "public": false, "members": []string{"bob"},
	})
	if err != nil || r.Error != nil {
		t.Fatalf("create channel: err=%v rpc=%+v", err, r.Error)
	}

	// Alice sends 3 messages
	var msgIDs []int64
	for i := 0; i < 3; i++ {
		r, err := alice.ToolCall("send_message", map[string]any{
			"channel": "fwd-test", "message": fmt.Sprintf("msg %d", i),
		})
		if err != nil || r.Error != nil {
			t.Fatalf("send message %d: err=%v rpc=%+v", i, err, r.Error)
		}
		// Extract message ID from "message sent (id: N)"
		var id int64
		fmt.Sscanf(r.Text, "message sent (id: %d)", &id)
		msgIDs = append(msgIDs, id)
	}

	// Bob marks read up to message 3 (latest)
	r, err = bob.ToolCall("mark_read", map[string]any{
		"channel": "fwd-test", "message_id": msgIDs[2],
	})
	if err != nil || r.Error != nil {
		t.Fatalf("mark_read latest: err=%v rpc=%+v", err, r.Error)
	}

	// Bob tries to mark read at message 1 (older) — should be a no-op
	r, err = bob.ToolCall("mark_read", map[string]any{
		"channel": "fwd-test", "message_id": msgIDs[0],
	})
	if err != nil || r.Error != nil {
		t.Fatalf("mark_read older: err=%v rpc=%+v", err, r.Error)
	}

	// Alice sends a 4th message
	r, err = alice.ToolCall("send_message", map[string]any{
		"channel": "fwd-test", "message": "msg 3",
	})
	if err != nil || r.Error != nil {
		t.Fatalf("send msg 3: err=%v rpc=%+v", err, r.Error)
	}

	// Bob should have exactly 1 unread (the 4th message)
	r, err = bob.ToolCall("unread_counts", map[string]any{})
	if err != nil || r.Error != nil {
		t.Fatalf("unread_counts: err=%v rpc=%+v", err, r.Error)
	}

	var counts []struct {
		Channel     string `json:"channel"`
		UnreadCount int    `json:"unread_count"`
	}
	if err := json.Unmarshal([]byte(r.Text), &counts); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	found := false
	for _, c := range counts {
		if c.Channel == "fwd-test" {
			found = true
			if c.UnreadCount != 1 {
				t.Errorf("unread_count = %d, want 1 (forward-only cursor should not have regressed)", c.UnreadCount)
			}
		}
	}
	if !found {
		t.Error("fwd-test not in unread_counts (expected 1 unread)")
	}
}
```

**Step 3: Add `TestWSUnreadCountsAndMarkRead` e2e test**

Tests the same flow via WS client instead of MCP.

```go
func TestWSUnreadCountsAndMarkRead(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Register alice via MCP (needed for channel creation)
	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	// Bob connects via WS
	ws, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := ws.WSRegister("bob"); err != nil {
		t.Fatal(err)
	}

	// Create channel with both users
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name": "ws-counts", "public": false, "members": []string{"bob"},
	})
	if err != nil || r.Error != nil {
		t.Fatalf("create channel: err=%v rpc=%+v", err, r.Error)
	}

	// Alice sends 2 messages
	for _, body := range []string{"hello", "world"} {
		alice.ToolCall("send_message", map[string]any{
			"channel": "ws-counts", "message": body,
		})
	}

	// Bob checks unread_counts via WS
	env, err := ws.Req("unread_counts", nil, "uc1")
	if err != nil {
		t.Fatalf("ws unread_counts: %v", err)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("ws unread_counts failed: %s", string(env.D))
	}

	// Bob marks read via WS
	env, err = ws.Req("mark_read", map[string]string{"channel": "ws-counts"}, "mr1")
	if err != nil {
		t.Fatalf("ws mark_read: %v", err)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("ws mark_read failed: %s", string(env.D))
	}

	// Verify cleared
	env, err = ws.Req("unread_counts", nil, "uc2")
	if err != nil {
		t.Fatalf("ws unread_counts after mark: %v", err)
	}

	var resp struct {
		Counts []struct {
			Channel string `json:"channel"`
		} `json:"counts"`
	}
	json.Unmarshal(env.D, &resp)
	for _, c := range resp.Counts {
		if c.Channel == "ws-counts" {
			t.Error("ws-counts still in unread_counts after mark_read")
		}
	}
}
```

**Step 4: Run all e2e tests**

Run: `mise run e2e`
Expected: All tests pass, no data races

**Step 5: Commit**

```bash
git add tests/e2e/sharkfin_test.go
git commit -m "test: add e2e tests for unread_counts and mark_read"
```

---

### Task 8: Deploy and notify

**Step 1: Run full test suite**

Run: `mise run test`
Expected: All unit tests pass

Run: `mise run e2e`
Expected: All e2e tests pass

**Step 2: Deploy**

Run: `mise run deploy`

**Step 3: Notify in chat-ux**

Send message to chat-ux:
> @workfort-cli-team-lead `unread_counts` and `mark_read` are deployed on both WS and MCP. Ready for TUI integration.
