# Passport Authentication Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Sharkfin's trust-based register/identify auth with Passport JWT + API key authentication.

**Architecture:** Passport SDK middleware wraps all HTTP endpoints. JWT validated at WS upgrade. API key for bridge. Identity auto-provisioned from JWT claims on first auth. All `user_id INTEGER` FKs become `identity_id TEXT` (Passport UUID).

**Tech Stack:** `github.com/Work-Fort/Passport/go/service-auth` (JWT validator via `lestrrat-go/jwx/v2`, API key validator with cache), `gorilla/websocket`, `mcp-go`

**Spec:** `docs/2026-03-13-passport-auth-design.md`

---

## Chunk 1: Domain & Store Layer

All subsequent tasks depend on this foundation. Change the domain types, store interfaces, SQL schema, and store implementations from int64 user IDs to string identity IDs.

### Task 1: Domain Types — User → Identity

**Files:**
- Modify: `pkg/domain/types.go`

- [ ] **Step 1: Replace User struct with Identity**

```go
// pkg/domain/types.go — replace lines 6-13

type Identity struct {
	ID          string // Passport UUID
	Username    string
	DisplayName string
	Type        string // "user", "agent", "service"
	Role        string
	CreatedAt   time.Time
}
```

- [ ] **Step 2: Update Message.UserID to IdentityID**

```go
// pkg/domain/types.go — update Message struct

type Message struct {
	ID         int64
	ChannelID  int64
	IdentityID string // was UserID int64
	From       string
	Body       string
	ThreadID   *int64
	Mentions   []string
	CreatedAt  time.Time
}
```

- [ ] **Step 3: Update DMInfo and AllDMInfo**

```go
type DMInfo struct {
	ChannelID     int64
	ChannelName   string
	OtherUsername string // removed OtherUserID — not needed
}

type AllDMInfo struct {
	ChannelID     int64
	ChannelName   string
	User1Username string // removed User1ID, User2ID — not needed
	User2Username string
}
```

- [ ] **Step 4: Run build to see all compile errors**

Run: `go build ./...`
Expected: FAIL — many compile errors referencing User and int64 userID. This is expected; subsequent tasks fix them.

- [ ] **Step 5: Commit**

```bash
git add pkg/domain/types.go
git commit -m "refactor(domain): replace User with Identity, int64 IDs to string"
```

### Task 2: Domain Ports — Store Interfaces

**Files:**
- Modify: `pkg/domain/ports.go`

- [ ] **Step 1: Replace UserStore with IdentityStore**

```go
// pkg/domain/ports.go — replace lines 4-8

type IdentityStore interface {
	UpsertIdentity(id, username, displayName, identityType, role string) error
	GetIdentityByID(id string) (*Identity, error)
	GetIdentityByUsername(username string) (*Identity, error)
	ListIdentities() ([]Identity, error)
}
```

- [ ] **Step 2: Update ChannelStore — int64 userIDs → string identityIDs**

```go
type ChannelStore interface {
	CreateChannel(name string, public bool, memberIDs []string, channelType string) (int64, error)
	GetChannelByID(id int64) (*Channel, error)
	GetChannelByName(name string) (*Channel, error)
	ListChannelsForUser(identityID string) ([]ChannelWithMembership, error)
	ListAllChannelsWithMembership(identityID string) ([]ChannelWithMembership, error)
	AddChannelMember(channelID int64, identityID string) error
	ChannelMemberUsernames(channelID int64) ([]string, error)
	IsChannelMember(channelID int64, identityID string) (bool, error)
	ListDMsForUser(identityID string) ([]DMInfo, error)
	ListAllDMs() ([]AllDMInfo, error)
	OpenDM(identityID string, otherIdentityID string, otherUsername string) (string, bool, error)
}
```

- [ ] **Step 3: Update MessageStore**

```go
type MessageStore interface {
	SendMessage(channelID int64, identityID string, body string, threadID *int64, mentionIdentityIDs []string) (int64, error)
	GetMessages(channelID int64, before *int64, after *int64, limit int, threadID *int64) ([]Message, error)
	GetUnreadMessages(identityID string, channelID *int64, mentionsOnly bool, threadID *int64) ([]Message, error)
	GetUnreadCounts(identityID string) ([]UnreadCount, error)
	MarkRead(identityID string, channelID int64, messageID *int64) error
}
```

- [ ] **Step 4: Update MentionGroupStore**

```go
type MentionGroupStore interface {
	CreateMentionGroup(slug string, createdByID string) (int64, error)
	DeleteMentionGroup(id int64) error
	GetMentionGroup(slug string) (*MentionGroup, error)
	ListMentionGroups() ([]MentionGroup, error)
	AddMentionGroupMember(groupID int64, identityID string) error
	RemoveMentionGroupMember(groupID int64, identityID string) error
	GetMentionGroupMembers(groupID int64) ([]string, error)
	ExpandMentionGroups(slugs []string) (map[string][]string, error)
}
```

- [ ] **Step 5: Update composite Store interface**

```go
type Store interface {
	IdentityStore
	ChannelStore
	MessageStore
	RoleStore
	MentionGroupStore
	SettingsStore
	Close() error
}
```

- [ ] **Step 6: Commit**

```bash
git add pkg/domain/ports.go
git commit -m "refactor(domain): update Store interfaces for string identity IDs"
```

### Task 3: SQLite Migrations — Rewrite Schema

**Files:**
- Modify: `pkg/infra/sqlite/migrations/001_init.sql`
- Modify: `pkg/infra/sqlite/migrations/003_mentions_threads.sql`
- Modify: `pkg/infra/sqlite/migrations/004_unique_channel_name.sql`
- Modify: `pkg/infra/sqlite/migrations/006_rbac.sql`
- Modify: `pkg/infra/sqlite/migrations/007_mention_groups.sql`

Since fresh installs create the schema directly and existing DB migration is a separate manual task, rewrite migrations to use `identities` from the start.

- [ ] **Step 1: Rewrite 001_init.sql**

```sql
-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
CREATE TABLE IF NOT EXISTS identities (
    id           TEXT PRIMARY KEY,
    username     TEXT UNIQUE NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    type         TEXT NOT NULL DEFAULT 'user',
    created_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS channels (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    public     BOOLEAN DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS channel_members (
    channel_id  INTEGER NOT NULL REFERENCES channels(id),
    identity_id TEXT    NOT NULL REFERENCES identities(id),
    joined_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (channel_id, identity_id)
);

CREATE TABLE IF NOT EXISTS messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id  INTEGER NOT NULL REFERENCES channels(id),
    identity_id TEXT    NOT NULL REFERENCES identities(id),
    body        TEXT NOT NULL,
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS read_cursors (
    channel_id           INTEGER NOT NULL REFERENCES channels(id),
    identity_id          TEXT    NOT NULL REFERENCES identities(id),
    last_read_message_id INTEGER NOT NULL REFERENCES messages(id),
    PRIMARY KEY (channel_id, identity_id)
);

-- +goose Down
DROP TABLE IF EXISTS read_cursors;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS channel_members;
DROP TABLE IF EXISTS channels;
DROP TABLE IF EXISTS identities;
```

- [ ] **Step 2: Rewrite 003_mentions_threads.sql**

```sql
-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
CREATE TABLE IF NOT EXISTS message_mentions (
    message_id  INTEGER NOT NULL REFERENCES messages(id),
    identity_id TEXT    NOT NULL REFERENCES identities(id),
    PRIMARY KEY (message_id, identity_id)
);

ALTER TABLE messages ADD COLUMN thread_id INTEGER REFERENCES messages(id);

CREATE INDEX idx_messages_thread_id ON messages(thread_id);
CREATE INDEX idx_message_mentions_identity_id ON message_mentions(identity_id);

-- +goose Down
DROP INDEX IF EXISTS idx_message_mentions_identity_id;
DROP INDEX IF EXISTS idx_messages_thread_id;
DROP TABLE IF EXISTS message_mentions;
ALTER TABLE messages DROP COLUMN thread_id;
```

- [ ] **Step 3: Rewrite 004_unique_channel_name.sql**

Change all `user_id` references to `identity_id`:

```sql
-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up

-- Remove duplicate channels, keeping the oldest (lowest id) for each name.
-- First, move members from duplicate channels to the kept channel.
INSERT OR IGNORE INTO channel_members (channel_id, identity_id, joined_at)
SELECT keeper.id, cm.identity_id, cm.joined_at
FROM channel_members cm
JOIN channels dup ON cm.channel_id = dup.id
JOIN (SELECT MIN(id) AS id, name FROM channels GROUP BY name) keeper ON dup.name = keeper.name
WHERE dup.id != keeper.id;

-- Re-point messages from duplicate channels to the kept channel.
UPDATE messages SET channel_id = (
    SELECT MIN(c2.id) FROM channels c2 WHERE c2.name = (SELECT c3.name FROM channels c3 WHERE c3.id = messages.channel_id)
)
WHERE channel_id NOT IN (SELECT MIN(id) FROM channels GROUP BY name);

-- Re-point read_cursors from duplicate channels to the kept channel.
INSERT OR REPLACE INTO read_cursors (channel_id, identity_id, last_read_message_id)
SELECT (SELECT MIN(c2.id) FROM channels c2 WHERE c2.name = (SELECT c3.name FROM channels c3 WHERE c3.id = rc.channel_id)),
       rc.identity_id, rc.last_read_message_id
FROM read_cursors rc
WHERE rc.channel_id NOT IN (SELECT MIN(id) FROM channels GROUP BY name);

DELETE FROM read_cursors WHERE channel_id NOT IN (SELECT MIN(id) FROM channels GROUP BY name);

-- Delete members of duplicate channels.
DELETE FROM channel_members WHERE channel_id NOT IN (SELECT MIN(id) FROM channels GROUP BY name);

-- Delete duplicate channels.
DELETE FROM channels WHERE id NOT IN (SELECT MIN(id) FROM channels GROUP BY name);

-- Now safe to add the unique index.
CREATE UNIQUE INDEX idx_channels_name ON channels(name);

-- +goose Down
DROP INDEX IF EXISTS idx_channels_name;
```

Note: `005_channel_type.sql` has no `user_id` references — no changes needed.

- [ ] **Step 4: Rewrite 006_rbac.sql** (was Step 3)

```sql
-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up

-- RBAC tables
CREATE TABLE IF NOT EXISTS roles (
    name       TEXT PRIMARY KEY,
    built_in   BOOLEAN NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS permissions (
    name TEXT PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role       TEXT NOT NULL REFERENCES roles(name),
    permission TEXT NOT NULL REFERENCES permissions(name),
    PRIMARY KEY (role, permission)
);

-- Add role column to identities.
ALTER TABLE identities ADD COLUMN role TEXT NOT NULL DEFAULT 'user';

-- Seed built-in roles.
INSERT INTO roles (name, built_in) VALUES ('admin', 1);
INSERT INTO roles (name, built_in) VALUES ('user',  1);
INSERT INTO roles (name, built_in) VALUES ('agent', 1);

-- Seed permissions.
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

-- Admin gets all permissions.
INSERT INTO role_permissions (role, permission)
SELECT 'admin', name FROM permissions;

-- User and agent get everything except create_channel and manage_roles.
INSERT INTO role_permissions (role, permission)
SELECT 'user', name FROM permissions
WHERE name NOT IN ('create_channel', 'manage_roles');

INSERT INTO role_permissions (role, permission)
SELECT 'agent', name FROM permissions
WHERE name NOT IN ('create_channel', 'manage_roles');

-- +goose Down
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS roles;
```

- [ ] **Step 5: Rewrite 007_mention_groups.sql**

```sql
-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
CREATE TABLE IF NOT EXISTS mention_groups (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    slug       TEXT    NOT NULL UNIQUE,
    created_by TEXT    NOT NULL REFERENCES identities(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mention_group_members (
    group_id    INTEGER NOT NULL REFERENCES mention_groups(id) ON DELETE CASCADE,
    identity_id TEXT    NOT NULL REFERENCES identities(id),
    PRIMARY KEY (group_id, identity_id)
);

CREATE INDEX idx_mention_group_members_identity ON mention_group_members(identity_id);

-- +goose Down
DROP TABLE IF EXISTS mention_group_members;
DROP TABLE IF EXISTS mention_groups;
```

- [ ] **Step 6: Commit**

```bash
git add pkg/infra/sqlite/migrations/
git commit -m "refactor(sqlite): rewrite migrations for identity-based schema"
```

### Task 4: Postgres Migrations — Mirror SQLite Changes

**Files:**
- Modify: `pkg/infra/postgres/migrations/001_init.sql`
- Modify: `pkg/infra/postgres/migrations/003_mentions_threads.sql`
- Modify: `pkg/infra/postgres/migrations/004_unique_channel_name.sql`
- Modify: `pkg/infra/postgres/migrations/006_rbac.sql`
- Modify: `pkg/infra/postgres/migrations/007_mention_groups.sql`

- [ ] **Step 1: Apply identical schema changes as SQLite to all Postgres migration files**

Same table structures — Postgres uses `TEXT PRIMARY KEY` for identities.id, `TEXT NOT NULL REFERENCES identities(id)` for all FKs. The only dialect difference: Postgres uses `SERIAL` for auto-increment integers (channels.id, messages.id, etc.) instead of `INTEGER PRIMARY KEY AUTOINCREMENT`.

For `004_unique_channel_name.sql` specifically: change `user_id` → `identity_id` in all `INSERT INTO channel_members` and `INSERT INTO read_cursors` statements. Postgres uses `ON CONFLICT DO NOTHING` and `ON CONFLICT (channel_id, identity_id) DO UPDATE SET ... GREATEST(...)` instead of SQLite's `INSERT OR IGNORE`/`INSERT OR REPLACE`.

- [ ] **Step 2: Commit**

```bash
git add pkg/infra/postgres/migrations/
git commit -m "refactor(postgres): rewrite migrations for identity-based schema"
```

### Task 5: SQLite Store — Rewrite users.go → identities.go

**Files:**
- Delete: `pkg/infra/sqlite/users.go`
- Create: `pkg/infra/sqlite/identities.go`

- [ ] **Step 1: Create identities.go with UpsertIdentity, GetIdentityByID, GetIdentityByUsername, ListIdentities**

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package sqlite

import (
	"database/sql"
	"fmt"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

// UpsertIdentity creates or updates a local identity from Passport claims.
// Uses INSERT OR IGNORE + UPDATE to handle concurrent first-connections safely.
func (s *Store) UpsertIdentity(id, username, displayName, identityType, role string) error {
	if role == "" {
		role = "user"
	}
	_, err := s.db.Exec(`
		INSERT INTO identities (id, username, display_name, type, role)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			username = excluded.username,
			display_name = excluded.display_name,
			type = excluded.type
	`, id, username, displayName, identityType, role)
	if err != nil {
		return fmt.Errorf("upsert identity: %w", err)
	}
	return nil
}

// GetIdentityByID returns an identity by Passport UUID.
func (s *Store) GetIdentityByID(id string) (*domain.Identity, error) {
	var i domain.Identity
	err := s.db.QueryRow(
		"SELECT id, username, display_name, type, role, created_at FROM identities WHERE id = ?", id,
	).Scan(&i.ID, &i.Username, &i.DisplayName, &i.Type, &i.Role, &i.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("identity not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
	}
	return &i, nil
}

// GetIdentityByUsername returns an identity by username.
func (s *Store) GetIdentityByUsername(username string) (*domain.Identity, error) {
	var i domain.Identity
	err := s.db.QueryRow(
		"SELECT id, username, display_name, type, role, created_at FROM identities WHERE username = ?", username,
	).Scan(&i.ID, &i.Username, &i.DisplayName, &i.Type, &i.Role, &i.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("identity not found: %s", username)
	}
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
	}
	return &i, nil
}

// ListIdentities returns all identities.
func (s *Store) ListIdentities() ([]domain.Identity, error) {
	rows, err := s.db.Query(
		"SELECT id, username, display_name, type, role, created_at FROM identities ORDER BY username",
	)
	if err != nil {
		return nil, fmt.Errorf("list identities: %w", err)
	}
	defer rows.Close()

	var identities []domain.Identity
	for rows.Next() {
		var i domain.Identity
		if err := rows.Scan(&i.ID, &i.Username, &i.DisplayName, &i.Type, &i.Role, &i.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan identity: %w", err)
		}
		identities = append(identities, i)
	}
	return identities, rows.Err()
}
```

- [ ] **Step 2: Update IsEmpty and WipeAll in identities.go**

Move `IsEmpty` and `WipeAll` from users.go, changing `users` → `identities`:

```go
func (s *Store) IsEmpty() (bool, error) {
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM identities").Scan(&count); err != nil {
		return false, fmt.Errorf("is empty: %w", err)
	}
	return count == 0, nil
}

func (s *Store) WipeAll() error {
	tables := []string{
		"mention_group_members",
		"mention_groups",
		"message_mentions",
		"read_cursors",
		"messages",
		"channel_members",
		"channels",
		"settings",
		"identities",
	}
	for _, t := range tables {
		if _, err := s.db.Exec("DELETE FROM " + t); err != nil {
			return fmt.Errorf("wipe %s: %w", t, err)
		}
	}
	if _, err := s.db.Exec("DELETE FROM role_permissions WHERE role NOT IN (SELECT name FROM roles WHERE built_in = 1)"); err != nil {
		return fmt.Errorf("wipe custom role_permissions: %w", err)
	}
	if _, err := s.db.Exec("DELETE FROM roles WHERE built_in = 0"); err != nil {
		return fmt.Errorf("wipe custom roles: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: Delete users.go and commit**

```bash
git rm pkg/infra/sqlite/users.go
git add pkg/infra/sqlite/identities.go
git commit -m "refactor(sqlite): replace users.go with identities.go"
```

### Task 6: SQLite Store — Update channels.go for string IDs

**Files:**
- Modify: `pkg/infra/sqlite/channels.go`

- [ ] **Step 1: Update all method signatures and SQL**

Change every `int64` userID parameter to `string` identityID. Change SQL column refs from `user_id` to `identity_id`. Change `JOIN users u ON` to `JOIN identities i ON`. Full file rewrite — every method affected.

Key changes:
- `CreateChannel(name string, public bool, memberIDs []string, ...)` — memberIDs now `[]string`
- `ListChannelsForUser(identityID string)` — SQL: `cm.identity_id = ?`
- `AddChannelMember(channelID int64, identityID string)` — SQL: `INSERT INTO channel_members (channel_id, identity_id)`
- `IsChannelMember(channelID int64, identityID string)` — SQL: `WHERE channel_id = ? AND identity_id = ?`
- `ChannelMemberUsernames(channelID int64)` — SQL: `JOIN identities i ON cm.identity_id = i.id`
- `ListDMsForUser(identityID string)` — SQL: `JOIN identities i ON cm2.identity_id = i.id`, return `DMInfo` without OtherUserID
- `ListAllDMs()` — SQL: `JOIN identities i ON cm.identity_id = i.id`, return `AllDMInfo` without User1ID/User2ID
- `OpenDM(identityID, otherIdentityID string, otherUsername string)` — SQL: get caller username from identities

- [ ] **Step 2: Commit**

```bash
git add pkg/infra/sqlite/channels.go
git commit -m "refactor(sqlite): update channels.go for string identity IDs"
```

### Task 7: SQLite Store — Update messages.go for string IDs

**Files:**
- Modify: `pkg/infra/sqlite/messages.go`

- [ ] **Step 1: Update all method signatures and SQL**

Key changes:
- `SendMessage(channelID int64, identityID string, body string, threadID *int64, mentionIdentityIDs []string)` — SQL: `INSERT INTO messages (channel_id, identity_id, body, thread_id)`
- `ImportMessage` — same pattern
- `GetMessages` — SQL: `JOIN identities i ON m.identity_id = i.id`
- `GetUnreadMessages(identityID string, ...)` — SQL: all `user_id` → `identity_id`
- `GetUnreadCounts(identityID string)` — SQL: all `user_id` → `identity_id`
- `MarkRead(identityID string, channelID int64, ...)` — SQL: `identity_id`
- `loadMentions` — SQL: `JOIN identities i ON mm.identity_id = i.id`
- `fetchUnreadForChannel` — SQL: `cm.identity_id`, `mm.identity_id`, `read_cursors WHERE identity_id`
- `advanceCursor` — `identity_id` parameters

- [ ] **Step 2: Commit**

```bash
git add pkg/infra/sqlite/messages.go
git commit -m "refactor(sqlite): update messages.go for string identity IDs"
```

### Task 8: SQLite Store — Update mention_groups.go and roles.go

**Files:**
- Modify: `pkg/infra/sqlite/mention_groups.go`
- Modify: `pkg/infra/sqlite/roles.go`

- [ ] **Step 1: Update mention_groups.go**

Key changes:
- `CreateMentionGroup(slug string, createdByID string)` — SQL: `INSERT INTO mention_groups (slug, created_by)`
- `GetMentionGroup` — SQL: `JOIN identities i ON mg.created_by = i.id`
- `ListMentionGroups` — SQL: `JOIN identities i ON mg.created_by = i.id`
- `AddMentionGroupMember(groupID int64, identityID string)` — SQL: `identity_id`
- `RemoveMentionGroupMember(groupID int64, identityID string)` — SQL: `identity_id`
- `GetMentionGroupMembers` — SQL: `JOIN identities i ON mgm.identity_id = i.id`
- `ExpandMentionGroups` — returns `map[string][]string` (identity IDs, not int64s)

- [ ] **Step 2: Update roles.go**

`SetUserRole` and `SetUserType` — SQL: `UPDATE identities SET role = ? WHERE username = ?` and `UPDATE identities SET type = ? WHERE username = ?`. `GetUserPermissions` and `HasPermission` — SQL: `JOIN identities i ON i.username = ?` to get role.

- [ ] **Step 3: Commit**

```bash
git add pkg/infra/sqlite/mention_groups.go pkg/infra/sqlite/roles.go
git commit -m "refactor(sqlite): update mention_groups and roles for string identity IDs"
```

### Task 9: Postgres Store — Mirror All SQLite Changes

**Files:**
- Delete: `pkg/infra/postgres/users.go`
- Create: `pkg/infra/postgres/identities.go`
- Modify: `pkg/infra/postgres/channels.go`
- Modify: `pkg/infra/postgres/messages.go`
- Modify: `pkg/infra/postgres/mention_groups.go`
- Modify: `pkg/infra/postgres/roles.go`

- [ ] **Step 1: Apply identical changes as SQLite tasks 5-8**

Same logic, but with `$1`, `$2` placeholders instead of `?`, and Postgres-specific SQL where needed (e.g. `ON CONFLICT ... DO UPDATE SET` syntax is the same).

- [ ] **Step 2: Verify build compiles**

Run: `go build ./pkg/infra/...`
Expected: May still have errors from daemon package — that's OK.

- [ ] **Step 3: Commit**

```bash
git add pkg/infra/postgres/
git commit -m "refactor(postgres): mirror identity-based store changes from SQLite"
```

## Chunk 2: Auth Infrastructure & Server Core

### Task 10: Add Passport SDK Dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add the Passport auth SDK**

Run: `go get github.com/Work-Fort/Passport/go/service-auth@latest`

- [ ] **Step 2: Tidy**

Run: `go mod tidy`

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add Passport service-auth SDK"
```

### Task 11: Delete session.go

**Files:**
- Delete: `pkg/daemon/session.go`

- [ ] **Step 1: Delete and commit**

```bash
git rm pkg/daemon/session.go
git commit -m "refactor: remove SessionManager (replaced by Passport auth)"
```

### Task 12: Rewrite presence_handler.go

**Files:**
- Modify: `pkg/daemon/presence_handler.go`

The presence handler no longer creates tokens or uses SessionManager. Identity comes from Passport middleware on the HTTP upgrade request. It still maintains a presence WebSocket for notifications.

- [ ] **Step 1: Rewrite PresenceHandler**

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	auth "github.com/Work-Fort/Passport/go/service-auth"
)

// PresenceHandler handles WebSocket presence connections.
// Identity is extracted from the HTTP upgrade request context (set by Passport middleware).
// Keeps the connection alive with ping/pong.
type PresenceHandler struct {
	pongTimeout  time.Duration
	pingInterval time.Duration

	mu    sync.RWMutex
	conns map[string]*presenceConn // username → connection
}

type presenceConn struct {
	conn *websocket.Conn
	mu   sync.Mutex // serializes writes
}

func NewPresenceHandler(pongTimeout time.Duration) *PresenceHandler {
	return &PresenceHandler{
		pongTimeout:  pongTimeout,
		pingInterval: pongTimeout / 2,
		conns:        make(map[string]*presenceConn),
	}
}

// SendNotification sends a JSON notification to a user's presence connection.
func (h *PresenceHandler) SendNotification(username string, data []byte) error {
	h.mu.RLock()
	pc, ok := h.conns[username]
	h.mu.RUnlock()
	if !ok {
		return nil // not connected
	}
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return pc.conn.WriteMessage(websocket.TextMessage, data)
}

// IsOnline returns true if the user has a presence connection.
func (h *PresenceHandler) IsOnline(username string) bool {
	h.mu.RLock()
	_, ok := h.conns[username]
	h.mu.RUnlock()
	return ok
}

func (h *PresenceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	username := identity.Username
	pc := &presenceConn{conn: conn}

	h.mu.Lock()
	h.conns[username] = pc
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		if h.conns[username] == pc {
			delete(h.conns, username)
		}
		h.mu.Unlock()
	}()

	conn.SetReadDeadline(time.Now().Add(h.pongTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(h.pongTimeout))
		return nil
	})

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(h.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pc.mu.Lock()
			pc.conn.SetWriteDeadline(time.Now().Add(h.pingInterval))
			err := pc.conn.WriteMessage(websocket.PingMessage, nil)
			pc.mu.Unlock()
			if err != nil {
				return
			}
		case <-readDone:
			return
		}
	}
}
```

- [ ] **Step 2: Commit**

```bash
git add pkg/daemon/presence_handler.go
git commit -m "refactor: rewrite PresenceHandler for Passport auth"
```

### Task 13: Rewrite presence_notifier.go

**Files:**
- Modify: `pkg/daemon/presence_notifier.go`

- [ ] **Step 1: Update to use PresenceHandler instead of SessionManager**

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"encoding/json"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

type PresenceNotifier struct {
	presence *PresenceHandler
	store    domain.Store
	sub      domain.Subscription
}

func NewPresenceNotifier(bus domain.EventBus, presence *PresenceHandler, store domain.Store) *PresenceNotifier {
	pn := &PresenceNotifier{
		presence: presence,
		store:    store,
		sub:      bus.Subscribe(domain.EventMessageNew),
	}
	go pn.run()
	return pn
}

func (pn *PresenceNotifier) run() {
	for evt := range pn.sub.Events() {
		msg := evt.Payload.(domain.MessageEvent)
		pn.handleMessage(msg)
	}
}

func (pn *PresenceNotifier) handleMessage(msg domain.MessageEvent) {
	recipients := computeRecipients(msg, pn.store)

	envelope, _ := json.Marshal(map[string]any{
		"type": "message.new",
		"d": map[string]any{
			"channel":      msg.ChannelName,
			"channel_type": msg.ChannelType,
			"from":         msg.From,
			"message_id":   msg.MessageID,
		},
	})

	for _, username := range recipients {
		pn.presence.SendNotification(username, envelope)
	}
}

func (pn *PresenceNotifier) Close() {
	pn.sub.Close()
}
```

- [ ] **Step 2: Commit**

```bash
git add pkg/daemon/presence_notifier.go
git commit -m "refactor: update PresenceNotifier to use PresenceHandler"
```

### Task 14: Rewrite server.go — Passport Middleware & Wiring

**Files:**
- Modify: `pkg/daemon/server.go`
- Modify: `cmd/daemon/daemon.go`

- [ ] **Step 1: Update server.go**

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/charmbracelet/log"
	mcpserver "github.com/mark3labs/mcp-go/server"

	auth "github.com/Work-Fort/Passport/go/service-auth"
	authapikey "github.com/Work-Fort/Passport/go/service-auth/apikey"
	authjwt "github.com/Work-Fort/Passport/go/service-auth/jwt"
	"github.com/Work-Fort/sharkfin/pkg/domain"
)

type Server struct {
	addr       string
	store      domain.Store
	httpServer *http.Server
	closers    []interface{ Close() }
}

func NewServer(ctx context.Context, addr string, store domain.Store, pongTimeout time.Duration, webhookURL string, bus domain.EventBus, version string, passportURL string) (*Server, error) {
	if webhookURL != "" {
		store.SetSetting("webhook_url", webhookURL)
	}

	// Initialize Passport auth middleware.
	opts := auth.DefaultOptions(passportURL)
	jwtV, err := authjwt.New(ctx, opts.JWKSURL, opts.JWKSRefreshInterval)
	if err != nil {
		return nil, fmt.Errorf("init JWT validator: %w", err)
	}
	akV := authapikey.New(opts.VerifyAPIKeyURL, opts.APIKeyCacheTTL)
	mw := auth.NewFromValidators(jwtV, akV)

	hub := NewHub(bus)
	presenceHandler := NewPresenceHandler(pongTimeout)

	var closers []interface{ Close() }
	closers = append(closers, jwtV)
	if bus != nil {
		closers = append(closers, NewWebhookSubscriber(bus, store))
		closers = append(closers, NewPresenceNotifier(bus, presenceHandler, store))
	}

	wsHandler := NewWSHandler(store, hub, presenceHandler, pongTimeout, version)

	sharkfinMCP := NewSharkfinMCP(store, hub, presenceHandler, version)
	mcpTransport := mcpserver.NewStreamableHTTPServer(sharkfinMCP.Server(),
		mcpserver.WithStateful(true),
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", mw(mcpTransport))
	mux.Handle("GET /presence", mw(presenceHandler))
	mux.Handle("GET /ws", mw(wsHandler))

	return &Server{
		addr:    addr,
		store:   store,
		closers: closers,
		httpServer: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
	}, nil
}

func (s *Server) Store() domain.Store { return s.store }

func (s *Server) Start() error {
	ln, err := net.Listen("tcp4", s.addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	fmt.Fprintf(os.Stderr, "sharkfind listening on %s\n", ln.Addr())
	return s.httpServer.Serve(ln)
}

func (s *Server) Shutdown(ctx context.Context) error {
	log.Info("shutting down sharkfind")
	for _, c := range s.closers {
		c.Close()
	}
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return err
	}
	return s.store.Close()
}
```

- [ ] **Step 2: Update daemon.go — add --passport-url flag**

```go
// In NewDaemonCmd RunE, after bus creation:
passportURL := viper.GetString("passport-url")
if passportURL == "" {
	return fmt.Errorf("--passport-url is required")
}

srv, err := pkgdaemon.NewServer(cmd.Context(), addr, store, pongTimeout, webhookURL, bus, version, passportURL)
```

Add flag registration:
```go
cmd.Flags().String("passport-url", "", "Passport identity provider URL (required)")
_ = viper.BindPFlag("passport-url", cmd.Flags().Lookup("passport-url"))
```

- [ ] **Step 3: Commit**

```bash
git add pkg/daemon/server.go cmd/daemon/daemon.go
git commit -m "feat: wire Passport auth middleware into server"
```

**Note:** The build will NOT compile at the end of Chunk 2. `server.go` calls `NewWSHandler` and `NewSharkfinMCP` with new signatures, but those files still have the old `*SessionManager` parameters. Chunk 3 fixes them.

## Chunk 3: WS & MCP Handlers

### Task 15: Rewrite hub.go — String Identity IDs

**Files:**
- Modify: `pkg/daemon/hub.go`

- [ ] **Step 1: Change WSClient.userID to identityID string**

```go
type WSClient struct {
	username   string
	identityID string
	send       chan []byte
	hub        *Hub
}
```

- [ ] **Step 2: Update BroadcastMessage**

In phase 1 snapshot and phase 2 membership check, change `target.userID` to `target.identityID`, `store.IsChannelMember(channelID, t.identityID)`.

- [ ] **Step 3: Update BroadcastToRole**

Change `store.GetUserByUsername` to `store.GetIdentityByUsername`, `user.Role` to `identity.Role`.

- [ ] **Step 4: Commit**

```bash
git add pkg/daemon/hub.go
git commit -m "refactor: update Hub for string identity IDs"
```

### Task 16: Rewrite ws_handler.go

**Files:**
- Modify: `pkg/daemon/ws_handler.go`

This is the biggest single-file change. Remove: hello envelope, register/identify handlers, SessionManager dependency. Add: identity from context at ServeHTTP entry, auto-provisioning, `notifications_only` from query param.

- [ ] **Step 1: Update WSHandler struct — remove sessions, add presenceHandler**

```go
type WSHandler struct {
	store       domain.Store
	hub         *Hub
	presence    *PresenceHandler
	pongTimeout time.Duration
	version     string
}

func NewWSHandler(store domain.Store, hub *Hub, presence *PresenceHandler, pongTimeout time.Duration, version string) *WSHandler {
	return &WSHandler{store: store, hub: hub, presence: presence, pongTimeout: pongTimeout, version: version}
}
```

- [ ] **Step 2: Rewrite ServeHTTP — remove hello and register/identify**

```go
func (h *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Auto-provision identity.
	role := identity.Type
	if role == "" {
		role = "user"
	}
	if err := h.store.UpsertIdentity(identity.ID, identity.Username, identity.DisplayName, identity.Type, role); err != nil {
		http.Error(w, "identity provisioning failed", http.StatusInternalServerError)
		return
	}

	localIdentity, err := h.store.GetIdentityByID(identity.ID)
	if err != nil {
		http.Error(w, "identity lookup failed", http.StatusInternalServerError)
		return
	}

	notificationsOnly := r.URL.Query().Get("notifications_only") == "true"

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	username := localIdentity.Username
	identityID := localIdentity.ID
	pingInterval := h.pongTimeout / 2

	client := &WSClient{username: username, identityID: identityID, send: make(chan []byte, 256), hub: h.hub}
	h.hub.Register(client)
	h.hub.SetState(username, "idle")
	h.hub.BroadcastPresence(username, true, "idle")
	log.Info("ws: connect", "user", username, "notifications_only", notificationsOnly, "clients", h.hub.ClientCount())

	defer func() {
		h.hub.Unregister(client)
		h.hub.ClearState(username)
		h.hub.BroadcastPresence(username, false, "")
		log.Info("ws: disconnect", "user", username, "clients", h.hub.ClientCount())
	}()

	// ... rest of keepalive, write pump, read pump (unchanged except
	// remove the !identified branch and remove hello/register/identify cases)
```

- [ ] **Step 3: Remove the pre-identification branch**

The entire `if !identified { ... }` block (lines 124-197) is removed. The read pump starts directly with the post-identification dispatch.

Remove `case "identify"`, `case "register"` from the post-identification switch too (the "already identified" error response).

- [ ] **Step 4: Update handler methods — int64 userID → string identityID**

Every `handleWS*` method that takes `userID int64` now takes `identityID string`. Example:
- `handleWSChannelList(sendCh, ref, identityID)` — `h.store.ListAllChannelsWithMembership(identityID)`
- `handleWSSendMessage(sendCh, ref, rawD, username, identityID)` — `h.store.IsChannelMember(ch.ID, identityID)`, `h.store.SendMessage(ch.ID, identityID, ...)`
- `handleWSChannelCreate(...)` — `memberIDs := []string{identityID}`, `store.GetIdentityByUsername(m).ID`
- `handleWSChannelInvite(...)` — `store.GetIdentityByUsername(d.Username).ID`
- `handleWSMentionGroupCreate(...)` — `store.CreateMentionGroup(d.Slug, identityID)`
- `handleWSMentionGroupAddMember/RemoveMember(...)` — `store.GetIdentityByUsername(d.Username).ID`
- `handleWSUserList(...)` — `store.ListIdentities()`, `h.presence.IsOnline(i.Username)`

- [ ] **Step 5: Commit**

```bash
git add pkg/daemon/ws_handler.go
git commit -m "refactor: rewrite WSHandler for Passport auth"
```

### Task 17: Rewrite mcp_server.go and mcp_tools.go

**Files:**
- Modify: `pkg/daemon/mcp_server.go`
- Modify: `pkg/daemon/mcp_tools.go`

- [ ] **Step 1: Remove get_identity_token, register, identify tools from mcp_tools.go**

Delete `newGetIdentityTokenTool()`, `newRegisterTool()`, `newIdentifyTool()`.

- [ ] **Step 2: Rewrite SharkfinMCP struct — remove sessions, mcpGoUsernames**

```go
type SharkfinMCP struct {
	mcpServer *server.MCPServer
	store     domain.Store
	hub       *Hub
	presence  *PresenceHandler
}
```

- [ ] **Step 3: Rewrite NewSharkfinMCP — remove 3 tool registrations**

Remove the `handleGetIdentityToken`, `handleRegister`, `handleIdentify` server tools. Remove the `setUsername`/`getUsername` helpers. Remove the `OnUnregisterSession` hook.

- [ ] **Step 4: Rewrite authMiddleware — use Passport context**

```go
func (s *SharkfinMCP) authMiddleware(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		identity, ok := auth.IdentityFromContext(ctx)
		if !ok {
			return mcp.NewToolResultError("unauthorized: no valid token"), nil
		}

		// Auto-provision
		role := identity.Type
		if role == "" {
			role = "user"
		}
		_ = s.store.UpsertIdentity(identity.ID, identity.Username, identity.DisplayName, identity.Type, role)

		username := identity.Username

		if perm, ok := toolPermissions[req.Params.Name]; ok {
			hasPerm, err := s.store.HasPermission(username, perm)
			if err != nil || !hasPerm {
				return mcp.NewToolResultError(fmt.Sprintf("permission denied: %s", perm)), nil
			}
		}

		t0 := time.Now()
		ctx = context.WithValue(ctx, usernameKey, username)
		result, err := next(ctx, req)
		if elapsed := time.Since(t0); elapsed > 50*time.Millisecond {
			log.Warn("mcp: slow tool", "tool", req.Params.Name, "user", username, "elapsed", elapsed)
		}
		return result, err
	}
}
```

Note: `auth.IdentityFromContext` works here because the Passport middleware ran on the HTTP request, and `mcp-go`'s `StreamableHTTPServer` propagates the request context to tool handlers.

- [ ] **Step 5: Update all tool handlers — GetUserByUsername → GetIdentityByUsername**

Every handler that does `s.store.GetUserByUsername(usernameFromCtx(ctx))` changes to `s.store.GetIdentityByUsername(usernameFromCtx(ctx))`. Every `.ID` that was `int64` is now `string`. Same pattern as the WS handler changes.

- [ ] **Step 6: Delete handleGetIdentityToken, handleRegister, handleIdentify**

Remove lines 177-228 entirely.

- [ ] **Step 7: Update handleUserList — ListUsers → ListIdentities, sessions.IsUserOnline → presence.IsOnline**

- [ ] **Step 8: Commit**

```bash
git add pkg/daemon/mcp_server.go pkg/daemon/mcp_tools.go
git commit -m "refactor: rewrite MCP server for Passport auth"
```

### Task 18: Update mentions.go — String IDs

**Files:**
- Modify: `pkg/daemon/mentions.go`

- [ ] **Step 1: Update resolveMentions to return []string**

```go
func resolveMentions(store domain.Store, body string) ([]string, []string) {
	seen := make(map[string]bool)
	seenIDs := make(map[string]bool)
	var identityIDs []string
	var usernames []string
	var unresolved []string

	for _, match := range mentionRe.FindAllStringSubmatch(body, -1) {
		u := match[1]
		if seen[u] {
			continue
		}
		seen[u] = true

		identity, err := store.GetIdentityByUsername(u)
		if err != nil {
			unresolved = append(unresolved, u)
			continue
		}
		seenIDs[identity.ID] = true
		identityIDs = append(identityIDs, identity.ID)
		usernames = append(usernames, identity.Username)
	}

	if len(unresolved) > 0 {
		expanded, err := store.ExpandMentionGroups(unresolved)
		if err == nil {
			for slug, memberIDs := range expanded {
				usernames = append(usernames, slug)
				for _, id := range memberIDs {
					if !seenIDs[id] {
						seenIDs[id] = true
						identityIDs = append(identityIDs, id)
					}
				}
			}
		}
	}

	return identityIDs, usernames
}
```

- [ ] **Step 2: Commit**

```bash
git add pkg/daemon/mentions.go
git commit -m "refactor: update resolveMentions for string identity IDs"
```

### Task 19: Build & Fix Compile Errors

- [ ] **Step 1: Build the entire project**

Run: `mise run build`
Expected: Compile errors from any remaining references to old types/methods.

- [ ] **Step 2: Fix all compile errors**

Address each error — typically changing `int64` → `string`, `User` → `Identity`, `GetUserByUsername` → `GetIdentityByUsername`, removing SessionManager references.

- [ ] **Step 3: Run unit tests**

Run: `mise run test`
Expected: Some test failures from tests still using old patterns (e.g., ws_handler_test.go).

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "fix: resolve remaining compile errors from auth refactor"
```

## Chunk 4: Bridge

### Task 20: Rewrite MCP Bridge for API Key Auth

**Files:**
- Modify: `cmd/mcpbridge/mcp_bridge.go`

- [ ] **Step 1: Update bridge struct for API key auth**

Note: The design doc says `X-API-Key: <key>`, but the Passport SDK middleware extracts tokens from `Authorization: Bearer` and dispatches to validators. API keys go through the same Bearer flow — the APIKeyValidator POSTs the extracted token to `/v1/verify-api-key`. So we use `Authorization: Bearer <api-key>`.

```go
type bridge struct {
	client        *http.Client
	mcpURL        string
	presenceURL   string // ws://<addr>/presence — still needed for wait_for_messages
	sessionID     string // MCP StreamableHTTP session tracking (not auth)
	apiKey        string
	notifications chan json.RawMessage
}
```

- [ ] **Step 2: Update NewMCPBridgeCmd**

Add `--api-key` flag. The bridge authenticates with `Authorization: Bearer <api-key>` on every MCP request and on the presence WebSocket upgrade.

```go
cmd.Flags().String("api-key", "", "API key for bridge authentication")
_ = viper.BindPFlag("api-key", cmd.Flags().Lookup("api-key"))
```

In RunE:
```go
apiKey := viper.GetString("api-key")
if apiKey == "" {
	return fmt.Errorf("--api-key is required")
}

b := &bridge{
	client:      &http.Client{},
	mcpURL:      fmt.Sprintf("http://%s/mcp", addr),
	presenceURL: fmt.Sprintf("ws://%s/presence", addr),
	apiKey:      apiKey,
}
```

- [ ] **Step 3: Update startPresence — pass API key in WS upgrade header**

```go
func (b *bridge) startPresence(ctx context.Context) error {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+b.apiKey)
	conn, _, err := websocket.DefaultDialer.Dial(b.presenceURL, header)
	if err != nil {
		return fmt.Errorf("dial presence: %w", err)
	}
	// No token to read — connection is authenticated and ready
	// ... rest of read loop unchanged (notifications channel)
```

- [ ] **Step 4: Update processStdin — add auth header, remove token intercept**

Remove `interceptGetIdentityToken` entirely. Add auth header to every MCP request:

```go
req.Header.Set("Authorization", "Bearer "+b.apiKey)
```

- [ ] **Step 5: Update callUnreadMessages — add auth header**

```go
httpReq.Header.Set("Authorization", "Bearer "+b.apiKey)
```

- [ ] **Step 6: Commit**

```bash
git add cmd/mcpbridge/mcp_bridge.go
git commit -m "refactor: rewrite bridge for API key auth"
```

## Chunk 5: Client Libraries

### Task 21: Go Client — Remove Register/Identify, Add Auth Options

**Files:**
- Modify: `client/client.go`
- Modify: `client/requests.go`
- Modify: `client/types.go`

- [ ] **Step 1: Add WithToken and WithAPIKey options**

In `client/client.go`, update `clientOpts`:

```go
type clientOpts struct {
	dialer    *websocket.Dialer
	reconnect BackoffFunc
	logger    *slog.Logger
	token     string
	apiKey    string
}

func WithToken(t string) Option {
	return func(o *clientOpts) { o.token = t }
}

func WithAPIKey(k string) Option {
	return func(o *clientOpts) { o.apiKey = k }
}
```

- [ ] **Step 2: Update Dial — pass auth header, remove hello handshake**

In the Dial function, attach auth header to the WebSocket upgrade request:

```go
header := http.Header{}
if opts.token != "" {
	header.Set("Authorization", "Bearer "+opts.token)
} else if opts.apiKey != "" {
	header.Set("Authorization", "Bearer "+opts.apiKey)
}

conn, _, err := dialer.DialContext(ctx, url, header)
```

Remove the hello handshake code (reading `hello` envelope, parsing `heartbeat_interval` and `version`). Connection is ready immediately after upgrade.

- [ ] **Step 3: Remove Register and Identify from requests.go**

Delete `Register()`, `Identify()` methods and their option types.

- [ ] **Step 4: Remove RegisterOpts, IdentifyOpts from types.go**

- [ ] **Step 5: Update reconnectLoop — no re-auth needed**

The reconnect just re-dials with the same auth headers. No hello to read after reconnect.

- [ ] **Step 6: Update client tests**

Update the mock server in `client/client_test.go` to not send a hello envelope. Update all tests that call `Register()`/`Identify()` — they should just connect and start making requests directly.

- [ ] **Step 7: Commit**

```bash
git add client/
git commit -m "refactor(client-go): remove register/identify, add token/apiKey auth"
```

### Task 22: TypeScript Client — Remove register/identify, Add Auth Options

**Files:**
- Modify: `clients/ts/src/client.ts`
- Modify: `clients/ts/src/types.ts`
- Modify: `clients/ts/test/client.test.ts`

- [ ] **Step 1: Add token/apiKey to ClientOptions**

```typescript
// types.ts
export interface ClientOptions {
  token?: string;
  apiKey?: string;
  WebSocket?: any;
  reconnect?: BackoffFunction | boolean;
}
```

- [ ] **Step 2: Update connect() — pass auth header, remove hello handshake**

In `client.ts`, pass auth as protocol or header:

```typescript
async connect(): Promise<void> {
  const headers: Record<string, string> = {};
  if (this.opts.token) {
    headers['Authorization'] = `Bearer ${this.opts.token}`;
  } else if (this.opts.apiKey) {
    headers['Authorization'] = `Bearer ${this.opts.apiKey}`;
  }

  // Node.js WebSocket supports headers; browser WebSocket does not
  // (but in browser, the BFF proxy handles auth transparently)
  const WS = this.opts.WebSocket ?? WebSocket;
  this.ws = new WS(this.url, { headers });
  // Connection ready immediately — no hello to wait for
}
```

- [ ] **Step 3: Remove register() and identify() methods**

- [ ] **Step 4: Remove AuthOptions type**

- [ ] **Step 5: Update tests — remove hello/register/identify**

- [ ] **Step 6: Run tests**

Run: `cd clients/ts && npm test`

- [ ] **Step 7: Commit**

```bash
git add clients/ts/
git commit -m "refactor(client-ts): remove register/identify, add token/apiKey auth"
```

## Chunk 6: Tests

### Task 23: E2E Harness — Rewrite for Passport Auth

**Files:**
- Modify: `tests/e2e/harness/harness.go`

The e2e tests need a way to authenticate without a real Passport instance. **Approach:** Start a minimal JWKS stub HTTP server alongside the daemon. The stub serves a static JWKS endpoint and issues test JWTs signed with a known key.

- [ ] **Step 1: Create test JWT helper**

Create `tests/e2e/harness/jwks_stub.go` (separate file — distinct concern from the MCP harness) that:
1. Generates an RSA key pair at test time
2. Serves `GET /v1/jwks` returning the public key in JWKS format
3. Provides a `SignJWT(id, username, displayName, userType string) string` function that creates a signed JWT with the expected claims

Use `github.com/lestrrat-go/jwx/v2` (already a transitive dep via Passport SDK) for JWT signing and JWKS serialization.

- [ ] **Step 2: Update StartDaemon — add --passport-url pointing to stub**

```go
func StartDaemon(binary, addr string, opts ...DaemonOption) (*Daemon, error) {
	// ... existing code ...
	// Start JWKS stub server
	stubAddr, stubStop, signJWT := StartJWKSStub()

	args = append(args, "--passport-url", "http://"+stubAddr)
	// ... existing cmd setup ...

	return &Daemon{..., stubStop: stubStop, signJWT: signJWT}, nil
}
```

Add `SignJWT` method to `Daemon` so tests can generate tokens.

- [ ] **Step 3: Update Client — remove token/register/identify flow**

```go
type Client struct {
	addr      string
	sessionID string
	authToken string // JWT for this client
	nextID    int
	mu        sync.Mutex
}

func NewClient(daemonAddr string, authToken string) *Client {
	return &Client{addr: daemonAddr, authToken: authToken, nextID: 1}
}
```

Update `RawMCPRequest` to add `Authorization: Bearer` header. Remove `ConnectPresence`, `Token()`, `Register()`, `Identify()`, `RegisterFlow()`, `IdentifyFlow()` methods.

No separate `ProvisionUser` method needed — the first authenticated MCP tool call auto-provisions the identity via the authMiddleware.

- [ ] **Step 4: Update WSClient — remove hello handshake**

```go
func NewWSClient(daemonAddr string, authToken string) (*WSClient, error) {
	url := fmt.Sprintf("ws://%s/ws", daemonAddr)
	header := http.Header{}
	header.Set("Authorization", "Bearer "+authToken)
	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		return nil, fmt.Errorf("dial ws: %w", err)
	}
	// No hello to read — connection ready immediately
	return &WSClient{conn: conn}, nil
}
```

Remove `WSRegister()`, `WSIdentify()`.

- [ ] **Step 5: Update PresenceClient — pass auth header, remove token reading**

```go
func NewPresenceClient(daemonAddr string, authToken string) (*PresenceClient, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+authToken)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	// No token to read — connection authenticated at upgrade
	// ... start read loop immediately
}
```

Remove `Token()` method.

- [ ] **Step 6: Update Bridge harness — pass API key**

```go
func StartBridge(binary, daemonAddr, xdgDir, apiKey string) (*Bridge, error) {
	cmd := exec.Command(binary,
		"mcp-bridge",
		"--daemon", daemonAddr,
		"--api-key", apiKey,
		"--log-level", "disabled",
	)
	// ...
}
```

- [ ] **Step 7: Commit**

```bash
git add tests/e2e/harness/
git commit -m "refactor(e2e): rewrite harness for Passport auth"
```

### Task 24: E2E Tests — Update All Tests

**Files:**
- Modify: `tests/e2e/sharkfin_test.go`

- [ ] **Step 1: Update every test to use JWT auth**

Replace all `RegisterFlow("alice")` calls with:
```go
aliceToken := daemon.SignJWT("uuid-alice", "alice", "Alice", "user")
c := harness.NewClient(daemon.Addr(), aliceToken)
c.Initialize()
// First tool call auto-provisions the identity
```

Replace all `WSClient` creation:
```go
ws, err := harness.NewWSClient(daemon.Addr(), aliceToken)
```

Replace all `PresenceClient` creation:
```go
pc, err := harness.NewPresenceClient(daemon.Addr(), aliceToken)
```

- [ ] **Step 2: Update bridge tests to pass API key**

The JWKS stub must also serve `POST /v1/verify-api-key` — accept any key from the test and return a canned identity. This is a simple HTTP handler alongside the JWKS endpoint.

- [ ] **Step 3: Run e2e tests**

Run: `mise run e2e`
Expected: All tests pass.

- [ ] **Step 4: Commit**

```bash
git add tests/e2e/sharkfin_test.go
git commit -m "test(e2e): update all tests for Passport auth"
```

### Task 25: Unit Tests — Update ws_handler_test.go

**Files:**
- Modify: `pkg/daemon/ws_handler_test.go`

- [ ] **Step 1: Update unit tests**

Update the mock server / test helpers to inject identity into the request context before calling `ServeHTTP`:

```go
// In test helper that dials the WS handler:
identity := auth.Identity{ID: "test-uuid", Username: "alice", DisplayName: "Alice", Type: "user"}
ctx := auth.ContextWithIdentity(r.Context(), identity)
r = r.WithContext(ctx)
```

- Remove hello/register/identify test cases
- Add test for `notifications_only` query parameter
- Add test for auto-provisioning on first connect

- [ ] **Step 2: Run unit tests**

Run: `mise run test`

- [ ] **Step 3: Commit**

```bash
git add pkg/daemon/
git commit -m "test: update unit tests for Passport auth"
```

### Task 26: Full Verification

- [ ] **Step 1: Run all checks**

Run: `mise run ci`
Expected: All unit tests, lints, and e2e tests pass.

- [ ] **Step 2: Verify build produces working binary**

Run: `mise run build`

- [ ] **Step 3: Commit any final fixes**

```bash
git add -A
git commit -m "fix: final adjustments from CI run"
```
