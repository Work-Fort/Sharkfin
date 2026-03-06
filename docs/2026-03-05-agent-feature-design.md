# Agent Feature Design

## Overview

Add role-based access control (RBAC), an agent sidecar subcommand, presence broadcasts on `/ws`, and active/idle state tracking to sharkfin. This enables AI agents running on VMs to receive notifications and execute commands in response to chat activity.

## Data Model

### Roles

Roles are stored in the database and support arbitrary custom roles alongside three built-in defaults.

```sql
CREATE TABLE roles (
    name       TEXT PRIMARY KEY,
    built_in   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE permissions (
    name TEXT PRIMARY KEY
);

CREATE TABLE role_permissions (
    role       TEXT NOT NULL REFERENCES roles(name),
    permission TEXT NOT NULL REFERENCES permissions(name),
    PRIMARY KEY (role, permission)
);
```

The `users` table gets a `role` column:

```sql
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user' REFERENCES roles(name);
```

### Permissions

Permissions are protocol-agnostic. The same permission applies whether the action is performed over WS or MCP.

| Permission | Admin | Default (user/agent) |
|---|---|---|
| `send_message` | yes | yes |
| `create_channel` | yes | no |
| `join_channel` | yes | yes |
| `invite_channel` | yes | yes |
| `history` | yes | yes |
| `unread_messages` | yes | yes |
| `unread_counts` | yes | yes |
| `mark_read` | yes | yes |
| `user_list` | yes | yes |
| `channel_list` | yes | yes |
| `dm_open` | yes | yes |
| `dm_list` | yes | yes |
| `manage_roles` | yes | no |

Admins can create custom roles (e.g. `power-user`) and grant/revoke individual permissions to build any combination needed.

### User Types

Users have a `type` field (`user` or `agent`) that is informational only — it does not affect permissions. Default type is determined by registration path: WS registration sets `user`, MCP registration sets `agent`. Admins can change it.

## Role Management

### CLI (`sharkfin admin`)

Operates directly on the SQLite database. Does not require the daemon to be running.

```bash
sharkfin admin set-role alice admin
sharkfin admin create-role power-user
sharkfin admin grant power-user create_channel
sharkfin admin revoke power-user invite_channel
sharkfin admin delete-role power-user    # fails for built-in roles
sharkfin admin list-roles
```

Reads the DB path from the config file via Viper.

### Online Management (MCP + WS)

Admin-only tools/commands for managing roles while the daemon is running:

- `set_role` — assign a role to a user
- `create_role` — create a custom role
- `delete_role` — delete a custom role (not built-in)
- `grant_permission` — add a permission to a role
- `revoke_permission` — remove a permission from a role
- `list_roles` — list all roles with permissions

Gated by the `manage_roles` permission.

When permissions change, a `capabilities` broadcast is sent to all connected users with the affected role.

## Capabilities

### WS

**On-demand query:**
```json
// Request
{"type": "capabilities", "ref": "1"}

// Response
{"type": "capabilities", "d": {"permissions": ["send_message", "join_channel", ...]}, "ref": "1", "ok": true}
```

**Push broadcast** (sent when an admin changes the role's permissions):
```json
{"type": "capabilities", "d": {"permissions": ["send_message", "join_channel", ...]}}
```

### MCP

`capabilities` tool — returns the current user's permission list.

## Permission Enforcement

Both the WS handler and MCP auth middleware check the user's permissions before executing any action. Permission checks query the database (or an invalidating cache) so that changes take effect immediately without requiring reconnection.

## Presence Broadcasts on `/ws`

Currently, presence (online/offline) only exists on the `/presence` endpoint. This adds presence broadcasts to `/ws`.

**Broadcast format:**
```json
{"type": "presence", "d": {"username": "alice", "status": "online", "state": "idle"}}
{"type": "presence", "d": {"username": "alice", "status": "online", "state": "active"}}
{"type": "presence", "d": {"username": "alice", "status": "offline"}}
```

**When they fire:**
- User identifies/registers on any connection → `online` with `idle` state
- User's last connection disconnects → `offline` (state becomes irrelevant)

**Sent to:** All connected `/ws` clients except the user themselves.

**Multiple connections:** A user is only `offline` when all connections are gone. The session manager needs to support reference counting for multiple connections per user.

## Active/Idle State

All users (agents and regular users) have an `active` or `idle` state. The server stores and broadcasts it; the client decides when to set it.

**Client responsibilities:**
- Agent sidecar: `active` when command is running, `idle` when waiting
- TUI/browser: `active` when window focused, `idle` after inactivity timeout
- MCP bridge: up to the client

**Server invariants:**
- Offline users have no state (not stored or broadcast)
- Disconnect clears any active state
- `set_state` rejected from unidentified connections

**No other transitions are enforced server-side.** The server trusts clients to manage their own state.

**WS message:**
```json
{"type": "set_state", "d": {"state": "active"}, "ref": "1"}
```

**MCP tool:** `set_state` — sets the current user's active/idle state.

## WS Notification-Only Mode

Any client can connect to `/ws` in notification-only mode by passing a flag during identify/register:

```json
{"type": "identify", "d": {"username": "my-agent", "notifications_only": true}, "ref": "1"}
```

In this mode:
- Client receives all broadcasts (`message.new`, `presence`, `capabilities`, `unread_counts`)
- Client can send `set_state` and `capabilities` queries
- All other action messages are rejected

This is a connection-level choice, not a role restriction. The same user can have a full WS session and a notification-only session simultaneously.

## Agent Sidecar (`sharkfin agent`)

A subcommand that connects to a sharkfin daemon in notification-only mode, listens for notifications, and executes a command when triggered.

### Usage

```bash
sharkfin agent --exec "claude -p @prompt.md"
```

### Config

```yaml
daemon: 127.0.0.1:16000

agent:
  username: my-agent
  exec: "claude -p @prompt.md"
```

Config precedence (Viper): CLI flags > config file > defaults.

### Lifecycle

1. Connect to `/ws` in notification-only mode
2. Identify as the configured username
3. Set state to `idle`
4. Wait for any notification (`message.new`, DM, mention)
5. Set state to `active`
6. Execute the configured command via `sh -c`
7. Wait for command to finish
8. Check unreads via WS (`unread_counts`)
9. If unreads remain → execute again (stay `active`)
10. If no unreads → set state to `idle`, go back to waiting

### Process Execution

- Runs via `sh -c` so pipes and redirects work
- Stdout/stderr forwarded to the sidecar's stdout/stderr
- The invoked process handles all sharkfin interaction via MCP (reading messages, responding, clearing unreads)
- The sidecar does not pass message content to the command
