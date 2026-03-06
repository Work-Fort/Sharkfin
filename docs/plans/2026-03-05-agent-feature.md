# Agent Feature Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add RBAC, presence broadcasts on `/ws`, active/idle state, and an agent sidecar subcommand to sharkfin.

**Architecture:** A new migration adds `roles`, `permissions`, and `role_permissions` tables plus `role` and `type` columns on `users`. Permission checks are added to both the WS handler and MCP auth middleware. The Hub gains presence broadcasts with state tracking. A new `sharkfin agent` subcommand connects to `/ws` in notification-only mode and executes a configured command on trigger. A `sharkfin admin` subcommand manages roles directly via the DB.

**Tech Stack:** Go, SQLite (modernc.org/sqlite), Goose migrations, Cobra/Viper CLI, gorilla/websocket, mcp-go

---

### Task 1: RBAC Database Migration

**Files:**
- Create: `pkg/db/migrations/006_rbac.sql`

**Step 1: Write migration SQL**

```sql
-- +goose Up
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

ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user' REFERENCES roles(name);
ALTER TABLE users ADD COLUMN type TEXT NOT NULL DEFAULT 'user';

-- Seed built-in roles
INSERT INTO roles (name, built_in) VALUES ('admin', TRUE);
INSERT INTO roles (name, built_in) VALUES ('user', TRUE);
INSERT INTO roles (name, built_in) VALUES ('agent', TRUE);

-- Seed permissions
INSERT INTO permissions (name) VALUES ('send_message');
INSERT INTO permissions (name) VALUES ('create_channel');
INSERT INTO permissions (name) VALUES ('join_channel');
INSERT INTO permissions (name) VALUES ('invite_channel');
INSERT INTO permissions (name) VALUES ('history');
INSERT INTO permissions (name) VALUES ('unread_messages');
INSERT INTO permissions (name) VALUES ('unread_counts');
INSERT INTO permissions (name) VALUES ('mark_read');
INSERT INTO permissions (name) VALUES ('user_list');
INSERT INTO permissions (name) VALUES ('channel_list');
INSERT INTO permissions (name) VALUES ('dm_open');
INSERT INTO permissions (name) VALUES ('dm_list');
INSERT INTO permissions (name) VALUES ('manage_roles');

-- Admin: all permissions
INSERT INTO role_permissions (role, permission) SELECT 'admin', name FROM permissions;

-- User/Agent defaults: everything except create_channel and manage_roles
INSERT INTO role_permissions (role, permission)
    SELECT 'user', name FROM permissions WHERE name NOT IN ('create_channel', 'manage_roles');
INSERT INTO role_permissions (role, permission)
    SELECT 'agent', name FROM permissions WHERE name NOT IN ('create_channel', 'manage_roles');

-- +goose Down
ALTER TABLE users DROP COLUMN role;
ALTER TABLE users DROP COLUMN type;
DROP TABLE role_permissions;
DROP TABLE permissions;
DROP TABLE roles;
```

**Step 2: Run tests to verify migration applies**

Run: `mise run test`
Expected: PASS (goose auto-applies on DB open)

**Step 3: Commit**

```bash
git add pkg/db/migrations/006_rbac.sql
git commit -m "feat: add RBAC database migration with roles and permissions"
```

---

### Task 2: RBAC Database Functions

**Files:**
- Create: `pkg/db/roles.go`
- Modify: `pkg/db/users.go` (add role/type to User struct and queries)

**Step 1: Write the role/permission DB functions**

Create `pkg/db/roles.go`:

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package db

import "fmt"

// Role represents a role in the RBAC system.
type Role struct {
	Name    string
	BuiltIn bool
}

// CreateRole creates a new custom role.
func (d *DB) CreateRole(name string) error {
	_, err := d.db.Exec("INSERT INTO roles (name) VALUES (?)", name)
	if err != nil {
		return fmt.Errorf("create role: %w", err)
	}
	return nil
}

// DeleteRole deletes a custom role. Fails for built-in roles.
func (d *DB) DeleteRole(name string) error {
	res, err := d.db.Exec("DELETE FROM roles WHERE name = ? AND built_in = FALSE", name)
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("role not found or is built-in: %s", name)
	}
	return nil
}

// ListRoles returns all roles.
func (d *DB) ListRoles() ([]Role, error) {
	rows, err := d.db.Query("SELECT name, built_in FROM roles ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()
	var roles []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.Name, &r.BuiltIn); err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

// GrantPermission adds a permission to a role.
func (d *DB) GrantPermission(role, permission string) error {
	_, err := d.db.Exec(
		"INSERT OR IGNORE INTO role_permissions (role, permission) VALUES (?, ?)",
		role, permission,
	)
	if err != nil {
		return fmt.Errorf("grant permission: %w", err)
	}
	return nil
}

// RevokePermission removes a permission from a role.
func (d *DB) RevokePermission(role, permission string) error {
	_, err := d.db.Exec(
		"DELETE FROM role_permissions WHERE role = ? AND permission = ?",
		role, permission,
	)
	if err != nil {
		return fmt.Errorf("revoke permission: %w", err)
	}
	return nil
}

// GetRolePermissions returns all permissions for a role.
func (d *DB) GetRolePermissions(role string) ([]string, error) {
	rows, err := d.db.Query(
		"SELECT permission FROM role_permissions WHERE role = ? ORDER BY permission",
		role,
	)
	if err != nil {
		return nil, fmt.Errorf("get role permissions: %w", err)
	}
	defer rows.Close()
	var perms []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

// GetUserPermissions returns permissions for a user based on their role.
func (d *DB) GetUserPermissions(username string) ([]string, error) {
	rows, err := d.db.Query(`
		SELECT rp.permission FROM role_permissions rp
		JOIN users u ON u.role = rp.role
		WHERE u.username = ?
		ORDER BY rp.permission
	`, username)
	if err != nil {
		return nil, fmt.Errorf("get user permissions: %w", err)
	}
	defer rows.Close()
	var perms []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

// HasPermission checks if a user has a specific permission.
func (d *DB) HasPermission(username, permission string) (bool, error) {
	var count int
	err := d.db.QueryRow(`
		SELECT COUNT(*) FROM role_permissions rp
		JOIN users u ON u.role = rp.role
		WHERE u.username = ? AND rp.permission = ?
	`, username, permission).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check permission: %w", err)
	}
	return count > 0, nil
}

// SetUserRole assigns a role to a user.
func (d *DB) SetUserRole(username, role string) error {
	res, err := d.db.Exec("UPDATE users SET role = ? WHERE username = ?", role, username)
	if err != nil {
		return fmt.Errorf("set user role: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user not found: %s", username)
	}
	return nil
}

// SetUserType sets the user type (user or agent).
func (d *DB) SetUserType(username, userType string) error {
	res, err := d.db.Exec("UPDATE users SET type = ? WHERE username = ?", userType, username)
	if err != nil {
		return fmt.Errorf("set user type: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user not found: %s", username)
	}
	return nil
}
```

**Step 2: Update User struct and queries in `pkg/db/users.go`**

Add `Role` and `Type` fields to the `User` struct. Update all SELECT queries to include the new columns.

```go
type User struct {
	ID        int64
	Username  string
	Password  string
	Role      string
	Type      string
	CreatedAt time.Time
}
```

Update `GetUserByUsername`:
```go
err := d.db.QueryRow(
    "SELECT id, username, password, role, type, created_at FROM users WHERE username = ?",
    username,
).Scan(&u.ID, &u.Username, &u.Password, &u.Role, &u.Type, &u.CreatedAt)
```

Update `ListUsers`:
```go
rows, err := d.db.Query("SELECT id, username, password, role, type, created_at FROM users ORDER BY username")
// ...
if err := rows.Scan(&u.ID, &u.Username, &u.Password, &u.Role, &u.Type, &u.CreatedAt); err != nil {
```

**Step 3: Run tests**

Run: `mise run test`
Expected: PASS

**Step 4: Commit**

```bash
git add pkg/db/roles.go pkg/db/users.go
git commit -m "feat: add RBAC database functions and update User struct"
```

---

### Task 3: Permission Enforcement in WS Handler

**Files:**
- Modify: `pkg/daemon/ws_handler.go`

**Step 1: Add permission check helper and gate all handler dispatches**

Add a `checkPermission` method to `WSHandler`:

```go
func (h *WSHandler) checkPermission(sendCh chan<- []byte, ref, username, permission string) bool {
	ok, err := h.db.HasPermission(username, permission)
	if err != nil || !ok {
		sendError(sendCh, ref, fmt.Sprintf("permission denied: %s", permission))
		return false
	}
	return true
}
```

In the dispatch switch (after identification, lines 189-224), add permission checks before each handler call. Map each request type to its permission:

```go
case "channel_create":
    if !h.checkPermission(sendCh, req.Ref, username, "create_channel") {
        break
    }
    h.handleWSChannelCreate(sendCh, req.Ref, req.D, username, userID)
```

Permission mapping:
- `user_list` → `user_list`
- `channel_list` → `channel_list`
- `channel_create` → `create_channel`
- `channel_invite` → `invite_channel`
- `channel_join` → `join_channel`
- `send_message` → `send_message`
- `history` → `history`
- `unread_messages` → `unread_messages`
- `unread_counts` → `unread_counts`
- `dm_list` → `dm_list`
- `dm_open` → `dm_open`
- `mark_read` → `mark_read`
- `set_setting` / `get_settings` → `manage_roles` (admin-only)

Also add support for `notifications_only` flag in identify/register. Add a `notificationsOnly` bool to connection state. When set, reject all action messages except `set_state`, `capabilities`, and `ping`.

**Step 2: Add `capabilities` and `set_state` WS handlers**

Add new cases in the dispatch switch:

```go
case "capabilities":
    perms, err := h.db.GetUserPermissions(username)
    if err != nil {
        sendError(sendCh, req.Ref, err.Error())
        break
    }
    sendReply(sendCh, req.Ref, true, map[string]interface{}{"permissions": perms})
case "set_state":
    // handled in Task 5 (presence/state)
```

**Step 3: Run tests**

Run: `mise run test`
Expected: PASS

Run: `mise run e2e`
Expected: Some failures likely — existing tests don't account for permissions. Fix in next task.

**Step 4: Commit**

```bash
git add pkg/daemon/ws_handler.go
git commit -m "feat: add permission enforcement to WS handler"
```

---

### Task 4: Permission Enforcement in MCP Handler

**Files:**
- Modify: `pkg/daemon/mcp_server.go`
- Modify: `pkg/daemon/mcp_tools.go`

**Step 1: Add permission checks to MCP auth middleware**

Update `authMiddleware` in `mcp_server.go` to check permissions. Add a mapping from tool name to required permission:

```go
var toolPermissions = map[string]string{
	"user_list":        "user_list",
	"channel_list":     "channel_list",
	"channel_create":   "create_channel",
	"channel_invite":   "invite_channel",
	"channel_join":     "join_channel",
	"send_message":     "send_message",
	"unread_messages":  "unread_messages",
	"unread_counts":    "unread_counts",
	"mark_read":        "mark_read",
	"history":          "history",
	"dm_list":          "dm_list",
	"dm_open":          "dm_open",
	"set_role":         "manage_roles",
	"create_role":      "manage_roles",
	"delete_role":      "manage_roles",
	"grant_permission": "manage_roles",
	"revoke_permission":"manage_roles",
	"list_roles":       "manage_roles",
}
```

In the middleware, after getting username, check permission:

```go
if perm, ok := toolPermissions[req.Params.Name]; ok {
    hasPerm, err := s.db.HasPermission(username, perm)
    if err != nil || !hasPerm {
        return mcp.NewToolResultError(fmt.Sprintf("permission denied: %s", perm)), nil
    }
}
```

**Step 2: Add `capabilities` and `set_state` MCP tools**

Add tool definitions to `mcp_tools.go`:

```go
func newCapabilitiesTool() mcp.Tool {
	return mcp.NewTool("capabilities",
		mcp.WithDescription("Get your current permissions."),
	)
}

func newSetStateTool() mcp.Tool {
	return mcp.NewTool("set_state",
		mcp.WithDescription("Set your active/idle state."),
		mcp.WithString("state", mcp.Required(), mcp.Description("State: active or idle")),
	)
}
```

Add handlers to `mcp_server.go` and register them in `NewSharkfinMCP`.

**Step 3: Add role management MCP tools**

Add tool definitions for `set_role`, `create_role`, `delete_role`, `grant_permission`, `revoke_permission`, `list_roles` to `mcp_tools.go`. Add corresponding handlers to `mcp_server.go`. These call the DB functions from Task 2.

When `set_role`, `grant_permission`, or `revoke_permission` is called, broadcast a `capabilities` update to all connected WS clients with the affected role.

**Step 4: Set user type on register**

In `handleRegister`, after successful registration, set type to `agent`:

```go
s.db.SetUserType(username, "agent")
```

In the WS handler's register flow, user type is already `user` (the default).

**Step 5: Run tests**

Run: `mise run test`
Expected: PASS

**Step 6: Commit**

```bash
git add pkg/daemon/mcp_server.go pkg/daemon/mcp_tools.go
git commit -m "feat: add permission enforcement and role management to MCP handler"
```

---

### Task 5: Presence Broadcasts and Active/Idle State on `/ws`

**Files:**
- Modify: `pkg/daemon/hub.go` (add state tracking, update BroadcastPresence)
- Modify: `pkg/daemon/ws_handler.go` (add set_state handler, broadcast presence with state)
- Modify: `pkg/daemon/mcp_server.go` (add set_state handler)

**Step 1: Add state tracking to Hub**

Add a `states` map to `Hub`:

```go
type Hub struct {
	mu      sync.RWMutex
	clients map[string]*WSClient
	states  map[string]string // username → "active" or "idle"
}
```

Add methods:

```go
func (h *Hub) SetState(username, state string) {
	h.mu.Lock()
	h.states[username] = state
	h.mu.Unlock()
}

func (h *Hub) GetState(username string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.states[username]
}

func (h *Hub) ClearState(username string) {
	h.mu.Lock()
	delete(h.states, username)
	h.mu.Unlock()
}
```

**Step 2: Update BroadcastPresence to include state**

Update `BroadcastPresence` signature and payload:

```go
func (h *Hub) BroadcastPresence(username string, online bool, state string) {
	d := map[string]interface{}{
		"username": username,
		"status":   "offline",
	}
	if online {
		d["status"] = "online"
		d["state"] = state
	}
	// ... rest unchanged
}
```

Update all callsites to pass state:
- WS identify/register: `h.hub.BroadcastPresence(username, true, "idle")`
- WS disconnect: `h.hub.BroadcastPresence(username, false, "")`

**Step 3: Add set_state WS handler**

In `ws_handler.go`, handle `set_state`:

```go
case "set_state":
    var d struct {
        State string `json:"state"`
    }
    json.Unmarshal(req.D, &d)
    if d.State != "active" && d.State != "idle" {
        sendError(sendCh, req.Ref, "state must be 'active' or 'idle'")
        break
    }
    h.hub.SetState(username, d.State)
    h.hub.BroadcastPresence(username, true, d.State)
    sendReply(sendCh, req.Ref, true, nil)
```

Allow `set_state` even in `notifications_only` mode.

**Step 4: Add set_state MCP handler**

In `mcp_server.go`:

```go
func (s *SharkfinMCP) handleSetState(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	state := req.GetString("state", "")
	if state != "active" && state != "idle" {
		return mcp.NewToolResultError("state must be 'active' or 'idle'"), nil
	}
	username := usernameFromCtx(ctx)
	s.hub.SetState(username, state)
	s.hub.BroadcastPresence(username, true, state)
	return mcp.NewToolResultText(fmt.Sprintf("state set to %s", state)), nil
}
```

**Step 5: Clean up state on disconnect**

In `ws_handler.go` defer block, add `h.hub.ClearState(username)` before `BroadcastPresence`.

**Step 6: Update user_list to include state**

In both WS and MCP `user_list` handlers, add `state` and `type` fields to the response:

```go
type userInfo struct {
    Username string `json:"username"`
    Online   bool   `json:"online"`
    Type     string `json:"type"`
    State    string `json:"state,omitempty"`
}
```

**Step 7: Run tests**

Run: `mise run test`
Expected: PASS (may need to update unit tests for new BroadcastPresence signature)

**Step 8: Commit**

```bash
git add pkg/daemon/hub.go pkg/daemon/ws_handler.go pkg/daemon/mcp_server.go
git commit -m "feat: add presence broadcasts and active/idle state on /ws"
```

---

### Task 6: `sharkfin admin` CLI Subcommand

**Files:**
- Create: `cmd/admin/admin.go`
- Modify: `cmd/root.go` (register subcommand)

**Step 1: Create the admin subcommand**

Create `cmd/admin/admin.go` with subcommands:

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package admin

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Work-Fort/sharkfin/pkg/config"
	"github.com/Work-Fort/sharkfin/pkg/db"
)

func openDB() (*db.DB, error) {
	dbPath := filepath.Join(config.GlobalPaths.StateDir, "sharkfin.db")
	return db.Open(dbPath)
}

func NewAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Manage roles and permissions",
	}

	cmd.AddCommand(newSetRoleCmd())
	cmd.AddCommand(newCreateRoleCmd())
	cmd.AddCommand(newDeleteRoleCmd())
	cmd.AddCommand(newGrantCmd())
	cmd.AddCommand(newRevokeCmd())
	cmd.AddCommand(newListRolesCmd())

	return cmd
}

func newSetRoleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-role <username> <role>",
		Short: "Assign a role to a user",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, err := openDB()
			if err != nil {
				return err
			}
			defer database.Close()
			if err := database.SetUserRole(args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("set role of %s to %s\n", args[0], args[1])
			return nil
		},
	}
}

func newCreateRoleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create-role <name>",
		Short: "Create a custom role",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, err := openDB()
			if err != nil {
				return err
			}
			defer database.Close()
			if err := database.CreateRole(args[0]); err != nil {
				return err
			}
			fmt.Printf("created role %s\n", args[0])
			return nil
		},
	}
}

func newDeleteRoleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete-role <name>",
		Short: "Delete a custom role (not built-in)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, err := openDB()
			if err != nil {
				return err
			}
			defer database.Close()
			return database.DeleteRole(args[0])
		},
	}
}

func newGrantCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "grant <role> <permission>",
		Short: "Grant a permission to a role",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, err := openDB()
			if err != nil {
				return err
			}
			defer database.Close()
			if err := database.GrantPermission(args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("granted %s to %s\n", args[1], args[0])
			return nil
		},
	}
}

func newRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <role> <permission>",
		Short: "Revoke a permission from a role",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, err := openDB()
			if err != nil {
				return err
			}
			defer database.Close()
			if err := database.RevokePermission(args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("revoked %s from %s\n", args[1], args[0])
			return nil
		},
	}
}

func newListRolesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-roles",
		Short: "List all roles and their permissions",
		RunE: func(cmd *cobra.Command, args []string) error {
			database, err := openDB()
			if err != nil {
				return err
			}
			defer database.Close()
			roles, err := database.ListRoles()
			if err != nil {
				return err
			}
			for _, r := range roles {
				builtIn := ""
				if r.BuiltIn {
					builtIn = " (built-in)"
				}
				perms, _ := database.GetRolePermissions(r.Name)
				fmt.Printf("%s%s: %v\n", r.Name, builtIn, perms)
			}
			return nil
		},
	}
}
```

**Step 2: Register in root.go**

Add import and `rootCmd.AddCommand(admin.NewAdminCmd())` in `cmd/root.go`.

**Step 3: Run tests**

Run: `mise run build`
Expected: PASS (compiles)

**Step 4: Commit**

```bash
git add cmd/admin/admin.go cmd/root.go
git commit -m "feat: add sharkfin admin CLI for role management"
```

---

### Task 7: `sharkfin agent` Sidecar Subcommand

**Files:**
- Create: `cmd/agent/agent.go`
- Modify: `cmd/root.go` (register subcommand)
- Modify: `pkg/config/config.go` (add agent config defaults)

**Step 1: Add agent config defaults**

In `pkg/config/config.go` `InitViper()`, add:

```go
viper.SetDefault("agent.username", "")
viper.SetDefault("agent.exec", "")
```

**Step 2: Create the agent subcommand**

Create `cmd/agent/agent.go`:

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type wsEnvelope struct {
	Type string          `json:"type"`
	D    json.RawMessage `json:"d,omitempty"`
	Ref  string          `json:"ref,omitempty"`
	OK   *bool           `json:"ok,omitempty"`
}

func NewAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Run agent sidecar that executes a command on chat notifications",
		RunE:  runAgent,
	}
	cmd.Flags().String("username", "", "Username to identify as")
	cmd.Flags().String("exec", "", "Command to execute on notification")
	viper.BindPFlag("agent.username", cmd.Flags().Lookup("username"))
	viper.BindPFlag("agent.exec", cmd.Flags().Lookup("exec"))
	return cmd
}

func runAgent(cmd *cobra.Command, args []string) error {
	daemon := viper.GetString("daemon")
	username := viper.GetString("agent.username")
	execCmd := viper.GetString("agent.exec")

	if username == "" {
		return fmt.Errorf("username is required (--username or agent.username in config)")
	}
	if execCmd == "" {
		return fmt.Errorf("exec command is required (--exec or agent.exec in config)")
	}

	// Connect to /ws
	wsURL := fmt.Sprintf("ws://%s/ws", daemon)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer conn.Close()

	// Read hello
	_, _, err = conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read hello: %w", err)
	}

	// Identify with notifications_only
	identifyMsg, _ := json.Marshal(map[string]interface{}{
		"type": "identify",
		"d": map[string]interface{}{
			"username":           username,
			"notifications_only": true,
		},
		"ref": "identify",
	})
	if err := conn.WriteMessage(websocket.TextMessage, identifyMsg); err != nil {
		return fmt.Errorf("send identify: %w", err)
	}

	// Read identify response
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read identify response: %w", err)
	}
	var identResp wsEnvelope
	json.Unmarshal(raw, &identResp)
	if identResp.OK == nil || !*identResp.OK {
		return fmt.Errorf("identify failed: %s", string(identResp.D))
	}

	// Set state to idle
	setStateMsg, _ := json.Marshal(map[string]interface{}{
		"type": "set_state",
		"d":    map[string]string{"state": "idle"},
		"ref":  "state",
	})
	conn.WriteMessage(websocket.TextMessage, setStateMsg)

	log.Info("agent: ready", "username", username, "exec", execCmd)

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Notification loop
	msgCh := make(chan wsEnvelope, 64)
	go func() {
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				close(msgCh)
				return
			}
			var env wsEnvelope
			if json.Unmarshal(raw, &env) == nil {
				msgCh <- env
			}
		}
	}()

	for {
		// Wait for notification or signal
		select {
		case env, ok := <-msgCh:
			if !ok {
				return fmt.Errorf("websocket connection closed")
			}
			if env.Type != "message.new" {
				continue
			}
			log.Info("agent: triggered", "type", env.Type)

			// Set active
			activeMsg, _ := json.Marshal(map[string]interface{}{
				"type": "set_state",
				"d":    map[string]string{"state": "active"},
				"ref":  "state",
			})
			conn.WriteMessage(websocket.TextMessage, activeMsg)

			// Execute command loop
			for {
				if err := executeCommand(execCmd); err != nil {
					log.Error("agent: command failed", "error", err)
				}

				// Check unreads
				unreadReq, _ := json.Marshal(map[string]interface{}{
					"type": "unread_counts",
					"ref":  "unreads",
				})
				conn.WriteMessage(websocket.TextMessage, unreadReq)

				// Read response (drain non-matching)
				hasUnreads := false
				deadline := time.After(5 * time.Second)
			drainLoop:
				for {
					select {
					case resp, ok := <-msgCh:
						if !ok {
							return fmt.Errorf("websocket connection closed")
						}
						if resp.Ref == "unreads" {
							var counts struct {
								Counts []interface{} `json:"counts"`
							}
							json.Unmarshal(resp.D, &counts)
							hasUnreads = len(counts.Counts) > 0
							break drainLoop
						}
					case <-deadline:
						break drainLoop
					}
				}

				if !hasUnreads {
					break
				}
				log.Info("agent: unreads remain, re-executing")
			}

			// Set idle
			idleMsg, _ := json.Marshal(map[string]interface{}{
				"type": "set_state",
				"d":    map[string]string{"state": "idle"},
				"ref":  "state",
			})
			conn.WriteMessage(websocket.TextMessage, idleMsg)

		case <-sigCh:
			log.Info("agent: shutting down")
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return nil
		}
	}
}

func executeCommand(command string) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
```

**Step 3: Register in root.go**

Add import and `rootCmd.AddCommand(agent.NewAgentCmd())`.

**Step 4: Run build**

Run: `mise run build`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/agent/agent.go cmd/root.go pkg/config/config.go
git commit -m "feat: add sharkfin agent sidecar subcommand"
```

---

### Task 8: Fix Existing Tests

**Files:**
- Modify: `tests/integration_test.go`
- Modify: `tests/e2e/sharkfin_test.go`
- Modify: `pkg/daemon/ws_handler_test.go`

**Step 1: Update tests for new User struct**

Any test that creates users or reads User objects will need to account for the new `role` and `type` fields. Update test helpers and assertions.

**Step 2: Update tests for BroadcastPresence signature**

`BroadcastPresence` now takes a state parameter. Update all callsites in tests.

**Step 3: Update e2e tests for permission enforcement**

The `allow_channel_creation` setting check in WS and MCP handlers may conflict with the new RBAC checks. Ensure both systems work together — RBAC `create_channel` permission AND the setting must both allow it, or simplify by removing the setting in favor of RBAC.

**Step 4: Run full test suite**

Run: `mise run ci`
Expected: PASS (lint, test, e2e all green)

**Step 5: Commit**

```bash
git add tests/ pkg/daemon/ws_handler_test.go
git commit -m "test: update tests for RBAC and presence changes"
```

---

### Task 9: E2E Tests for New Features

**Files:**
- Modify: `tests/e2e/sharkfin_test.go`
- Modify: `tests/e2e/harness/harness.go`

**Step 1: Add harness helpers**

Add to `harness.go`:
- `WSClient.SetState(state string)` — sends `set_state` message
- `WSClient.GetCapabilities()` — sends `capabilities` request
- `Client.Capabilities()` — MCP tool call wrapper
- `Client.SetState(state string)` — MCP tool call wrapper

**Step 2: Write e2e tests**

Add tests to `sharkfin_test.go`:

- `TestRBACDefaultPermissions` — Register user, verify can send_message but not create_channel
- `TestRBACAdminCanManageRoles` — Use admin CLI to promote user, verify user can now create_channel
- `TestCapabilitiesQuery` — Query capabilities over WS and MCP, verify permission list
- `TestPresenceBroadcast` — Register two WS users, verify presence online/offline broadcasts
- `TestActiveIdleState` — Set state via WS, verify broadcast received by other clients
- `TestNotificationsOnlyMode` — Connect in notifications_only, verify action messages rejected but broadcasts received
- `TestAgentTypeOnMCPRegister` — Register via MCP, verify user type is `agent`
- `TestUserTypeOnWSRegister` — Register via WS, verify user type is `user`

**Step 3: Run tests**

Run: `mise run e2e`
Expected: PASS

**Step 4: Commit**

```bash
git add tests/e2e/
git commit -m "test: add e2e tests for RBAC, presence, and agent features"
```

---

### Task 10: Remove `allow_channel_creation` Setting

**Files:**
- Modify: `pkg/daemon/ws_handler.go` (remove setting check from handleWSChannelCreate)
- Modify: `pkg/daemon/mcp_server.go` (remove setting check from handleChannelCreate)
- Modify: `pkg/daemon/server.go` (remove setting seed and flag)
- Modify: `cmd/daemon/daemon.go` (remove --allow-channel-creation flag)
- Modify: `tests/e2e/harness/harness.go` (remove WithAllowChannelCreation option)
- Modify: tests that use `WithAllowChannelCreation`

**Step 1: Remove the setting checks**

Channel creation is now controlled by the `create_channel` permission in RBAC. Remove the `allow_channel_creation` setting check from both `handleWSChannelCreate` and `handleChannelCreate`. Remove the flag from the daemon command and the setting seed from `NewServer`.

**Step 2: Update e2e harness**

Remove `WithAllowChannelCreation` from `daemonConfig` and `StartDaemon`. Update any tests that used this option to instead use RBAC (grant `create_channel` permission to the test user's role).

**Step 3: Run full suite**

Run: `mise run ci`
Expected: PASS

**Step 4: Commit**

```bash
git add pkg/daemon/ cmd/daemon/ tests/
git commit -m "refactor: replace allow_channel_creation setting with RBAC permission"
```
