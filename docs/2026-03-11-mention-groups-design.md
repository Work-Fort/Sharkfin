# Mention Groups Design

## Problem

Currently `@mentions` only target individual users. Teams working in sharkfin
need a way to notify multiple users at once — e.g. `@backend-team` or
`@reviewers` — without listing every username.

## Goal

Add mention groups: named sets of users that can be `@mentioned` in messages.
When a group is mentioned, every member of that group receives the mention
(unread counts, `mentions_only` filtering, presence notifications, webhooks).

## Design

### Data Model

Two new tables:

```sql
mention_groups (
    id         INTEGER PRIMARY KEY,
    slug       TEXT    NOT NULL UNIQUE,   -- the @-mentionable name
    created_by INTEGER NOT NULL REFERENCES users(id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

mention_group_members (
    group_id INTEGER NOT NULL REFERENCES mention_groups(id) ON DELETE CASCADE,
    user_id  INTEGER NOT NULL REFERENCES users(id),
    PRIMARY KEY (group_id, user_id)
);
```

Slugs follow the same character class as usernames: `[a-zA-Z0-9_-]+`. A slug
must not collide with an existing username (enforced at creation time).

### Prerequisite: Body-Only Mentions

[Design](2026-03-11-mentions-body-only-design.md) · [Plan](plans/2026-03-11-mentions-body-only.md)

The explicit `mentions` parameter has been removed from `send_message`. All
mentions are now extracted from the message body via `@username` regex. The
`resolveMentions` function has the simplified signature:

```go
func resolveMentions(store domain.UserStore, body string) ([]int64, []string)
```

Mention groups build on this by extending the resolution logic within
`resolveMentions`.

### Resolution Strategy: Expand at Write Time

When `resolveMentions` encounters a regex match `@foo` that doesn't match any
username, it checks if `foo` is a group slug. If so, it expands the group to
its member user IDs. These user IDs are inserted into `message_mentions`
alongside any directly-mentioned users (deduplicated).

This means **the entire read path requires zero changes**:

- `message_mentions` JOIN for `mentions_only` filtering — unchanged
- `GetUnreadCounts` `mention_count` — unchanged
- `loadMentions` after queries — unchanged
- `computeRecipients` for webhooks/presence — unchanged

The broadcast `mentions` field includes both individual usernames and group
slugs (prefixed with the group name for display). For example, mentioning
`@backend-team` (members: alice, bob) produces:

- `message_mentions` rows: alice's user_id, bob's user_id
- Broadcast `mentions`: `["backend-team", "alice", "bob"]`
- Presence notifications sent to: alice, bob

### Resolution Priority

When a candidate like `@foo` is encountered:

1. Check if `foo` is a username → resolve to that user
2. Check if `foo` is a group slug → expand to member user IDs
3. Neither → silently drop (existing behavior)

Usernames take priority over group slugs. Since slug creation rejects collisions
with existing usernames, this should not arise in practice, but the priority
order provides a safe fallback.

### Store Interface

Add to `domain.Store`:

```go
type MentionGroupStore interface {
    CreateMentionGroup(slug string, createdBy int64) (int64, error)
    DeleteMentionGroup(id int64) error
    GetMentionGroup(slug string) (*MentionGroup, error)
    ListMentionGroups() ([]MentionGroup, error)
    AddMentionGroupMember(groupID, userID int64) error
    RemoveMentionGroupMember(groupID, userID int64) error
    GetMentionGroupMembers(groupID int64) ([]string, error)
    ExpandMentionGroups(slugs []string) (map[string][]int64, error)
}
```

`ExpandMentionGroups` is the hot-path function called by `resolveMentions`. It
takes a batch of candidate slugs and returns a map of slug → member user IDs
for any that matched. This avoids N+1 queries during mention resolution.

### Domain Types

```go
type MentionGroup struct {
    ID        int64
    Slug      string
    CreatedBy string    // username of creator
    Members   []string  // member usernames
    CreatedAt time.Time
}
```

### Permissions

Any identified user can:
- Create a mention group (they become the creator)
- List and view all mention groups
- Mention any group in a message

Only the group creator can:
- Add/remove members
- Delete the group

### API Surface

**MCP tools** (added to `mcp_tools.go`):

| Tool | Parameters | Description |
|------|-----------|-------------|
| `mention_group_create` | `slug` | Create a new mention group |
| `mention_group_delete` | `slug` | Delete a group (creator only) |
| `mention_group_add_member` | `slug`, `username` | Add a user to a group |
| `mention_group_remove_member` | `slug`, `username` | Remove a user from a group |
| `mention_group_list` | — | List all groups with members |
| `mention_group_get` | `slug` | Get a group with its members |

**WS request types** (added to `ws_handler.go`):

| Type | Payload | Description |
|------|---------|-------------|
| `mention_group_create` | `{slug}` | Create a new mention group |
| `mention_group_delete` | `{slug}` | Delete a group (creator only) |
| `mention_group_add_member` | `{slug, username}` | Add a user to a group |
| `mention_group_remove_member` | `{slug, username}` | Remove a user from a group |
| `mention_group_list` | — | List all groups with members |
| `mention_group_get` | `{slug}` | Get a group with its members |

### Slug Validation

- Must match `^[a-zA-Z0-9_-]+$` (same as usernames)
- Must not collide with an existing username
- Must not collide with an existing group slug
- Case-sensitive (consistent with usernames)

### Migration

New migration `005_mention_groups.sql` (for both SQLite and Postgres) creates
the two tables and indexes.

## What This Design Does NOT Include

- **Built-in groups** (`@everyone`, `@here`) — can be added later as special
  slugs with dynamic membership.
- **Group mention provenance** — we don't store which group triggered a
  `message_mentions` row. If needed later, a `message_group_mentions` join
  table could record this without changing the read path.
- **Nested groups** — groups cannot contain other groups. Keeps resolution
  simple and avoids cycles.
- **Broadcast on group changes** — no real-time notification when members are
  added/removed. Clients can poll `mention_group_get` if needed.
