# Design: `unread_counts` and `mark_read`

Approved by tpm on 2026-02-27 in chat-ux.

## Problem

The TUI needs lightweight unread badge counts per channel and the ability to
mark a channel as read when the user focuses it. The existing `unread_messages`
tool returns full message bodies (expensive for sidebar badges) and only
advances the cursor as a side-effect of fetching.

## New Endpoints

### `unread_counts`

Available on WS and MCP. No parameters.

Returns an array of channels with non-zero unreads:

```json
[
  {"channel": "general", "unread_count": 5, "mention_count": 1},
  {"channel": "dev", "unread_count": 12, "mention_count": 0}
]
```

- Only includes channels the user is a member of
- Only includes channels with >0 unreads (absence = 0)
- Excludes the user's own messages from counts
- Single SQL query using existing tables (no schema changes)

### `mark_read`

Available on WS and MCP.

Parameters:
- `channel` (string, required) — channel name
- `message_id` (int, optional) — specific message ID to mark as read

Behavior:
- Advances the per-user read cursor for the channel
- If no `message_id`, advances to the latest message in the channel
- Forward-only: cursor never moves backwards (MAX of current vs new)
- Requires channel membership
- Returns success/error

## DB Layer

### `GetUnreadCounts(userID int64) ([]UnreadCount, error)`

```sql
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
```

### `MarkRead(userID, channelID int64, messageID *int64) error`

- If `messageID` is nil: `SELECT MAX(id) FROM messages WHERE channel_id = ?`
- Forward-only upsert:
  ```sql
  INSERT INTO read_cursors (channel_id, user_id, last_read_message_id)
  VALUES (?, ?, ?)
  ON CONFLICT(channel_id, user_id)
  DO UPDATE SET last_read_message_id = MAX(excluded.last_read_message_id, last_read_message_id)
  ```

## No Schema Changes

Uses existing tables: `read_cursors`, `messages`, `message_mentions`, `channel_members`, `channels`.

## Testing

- E2E tests for both WS and MCP paths
- Forward-only cursor enforcement (mark_read with older ID is a no-op)
- Mention count accuracy
- Own messages excluded from unread counts
- Empty result when no unreads
