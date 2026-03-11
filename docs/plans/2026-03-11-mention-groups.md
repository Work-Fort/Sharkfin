# Mention Groups Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add mention groups — named sets of users that can be `@mentioned` in messages, expanding to notify all members.

**Architecture:** New `mention_groups` and `mention_group_members` tables, a `MentionGroupStore` interface with SQLite/Postgres implementations, group expansion in `resolveMentions`, and CRUD exposed via MCP tools and WS request types. Groups are expanded to individual `message_mentions` rows at write time, so the entire read path (unread counts, `mentions_only`, presence, webhooks) is unchanged.

**Tech Stack:** Go, mcp-go, gorilla/websocket, goose migrations, modernc.org/sqlite, lib/pq

**Prerequisite:** [Body-only mentions](2026-03-11-mentions-body-only.md) must be implemented first — `resolveMentions` must already have the simplified `(store, body)` signature.

---

### File Structure

| File | Responsibility | Change |
|------|---------------|--------|
| `pkg/domain/types.go` | Domain types | Add `MentionGroup` struct |
| `pkg/domain/ports.go` | Store interfaces | Add `MentionGroupStore`, embed in `Store` |
| `pkg/infra/sqlite/migrations/007_mention_groups.sql` | SQLite migration | Create tables |
| `pkg/infra/postgres/migrations/007_mention_groups.sql` | Postgres migration | Create tables |
| `pkg/infra/sqlite/mention_groups.go` | SQLite store impl | New file: all `MentionGroupStore` methods |
| `pkg/infra/postgres/mention_groups.go` | Postgres store impl | New file: all `MentionGroupStore` methods |
| `pkg/daemon/mentions.go` | Mention resolution | Extend `resolveMentions` for group expansion |
| `pkg/daemon/mcp_tools.go` | MCP tool definitions | Add 6 mention group tools |
| `pkg/daemon/mcp_server.go` | MCP handlers | Add 6 handlers, register tools |
| `pkg/daemon/ws_handler.go` | WS handlers | Add 6 handlers, add switch cases |
| `pkg/infra/sqlite/store_test.go` | SQLite store tests | Add mention group tests |
| `pkg/infra/postgres/store_test.go` | Postgres store tests | Add mention group tests |
| `pkg/daemon/ws_handler_test.go` | WS handler tests | Add mention group + resolution tests |
| `tests/e2e/sharkfin_test.go` | E2e tests | Add mention group e2e tests |

---

### Task 1: Migration + domain types + store interface

Foundation layer. No implementations yet — just the schema, types, and interface
that everything else builds on.

**Files:**
- Create: `pkg/infra/sqlite/migrations/007_mention_groups.sql`
- Create: `pkg/infra/postgres/migrations/007_mention_groups.sql`
- Modify: `pkg/domain/types.go`
- Modify: `pkg/domain/ports.go`

- [ ] **Step 1: Create SQLite migration**

Create `pkg/infra/sqlite/migrations/007_mention_groups.sql`:

```sql
-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
CREATE TABLE IF NOT EXISTS mention_groups (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    slug       TEXT    NOT NULL UNIQUE,
    created_by INTEGER NOT NULL REFERENCES users(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mention_group_members (
    group_id INTEGER NOT NULL REFERENCES mention_groups(id) ON DELETE CASCADE,
    user_id  INTEGER NOT NULL REFERENCES users(id),
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX idx_mention_group_members_user ON mention_group_members(user_id);

-- +goose Down
DROP TABLE IF EXISTS mention_group_members;
DROP TABLE IF EXISTS mention_groups;
```

- [ ] **Step 2: Create Postgres migration**

Create `pkg/infra/postgres/migrations/007_mention_groups.sql`:

```sql
-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
CREATE TABLE IF NOT EXISTS mention_groups (
    id         SERIAL PRIMARY KEY,
    slug       TEXT   NOT NULL UNIQUE,
    created_by INTEGER NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS mention_group_members (
    group_id INTEGER NOT NULL REFERENCES mention_groups(id) ON DELETE CASCADE,
    user_id  INTEGER NOT NULL REFERENCES users(id),
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX idx_mention_group_members_user ON mention_group_members(user_id);

-- +goose Down
DROP TABLE IF EXISTS mention_group_members;
DROP TABLE IF EXISTS mention_groups;
```

- [ ] **Step 3: Add MentionGroup domain type**

In `pkg/domain/types.go`, add after the `Role` struct (line 66):

```go
type MentionGroup struct {
	ID        int64
	Slug      string
	CreatedBy string   // username of creator
	Members   []string // member usernames
	CreatedAt time.Time
}
```

- [ ] **Step 4: Add MentionGroupStore interface**

In `pkg/domain/ports.go`, add after the `RoleStore` interface (after line 43):

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

- [ ] **Step 5: Embed MentionGroupStore in Store**

In `pkg/domain/ports.go`, add `MentionGroupStore` to the composite `Store` interface (line 52-58):

```go
type Store interface {
	UserStore
	ChannelStore
	MessageStore
	RoleStore
	MentionGroupStore
	SettingsStore
	Close() error
}
```

- [ ] **Step 6: Verify compilation fails**

Run: `mise run build`
Expected: FAIL — SQLite and Postgres `Store` types don't implement `MentionGroupStore` yet. This confirms the interface is wired correctly.

- [ ] **Step 7: Commit**

```bash
git add pkg/domain/types.go pkg/domain/ports.go \
  pkg/infra/sqlite/migrations/007_mention_groups.sql \
  pkg/infra/postgres/migrations/007_mention_groups.sql
git commit -m "feat: add mention group schema, types, and store interface"
```

---

### Task 2: SQLite store implementation + tests

**Files:**
- Create: `pkg/infra/sqlite/mention_groups.go`
- Modify: `pkg/infra/sqlite/store_test.go`

- [ ] **Step 1: Write failing tests**

Add to `pkg/infra/sqlite/store_test.go`:

```go
func TestCreateMentionGroup(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")

	id, err := s.CreateMentionGroup("backend-team", aliceID)
	require.NoError(t, err)
	require.Greater(t, id, int64(0))

	// Duplicate slug should fail.
	_, err = s.CreateMentionGroup("backend-team", aliceID)
	require.Error(t, err)
}

func TestGetMentionGroup(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")

	id, _ := s.CreateMentionGroup("backend-team", aliceID)
	require.NoError(t, s.AddMentionGroupMember(id, aliceID))
	require.NoError(t, s.AddMentionGroupMember(id, bobID))

	g, err := s.GetMentionGroup("backend-team")
	require.NoError(t, err)
	require.Equal(t, "backend-team", g.Slug)
	require.Equal(t, "alice", g.CreatedBy)
	require.ElementsMatch(t, []string{"alice", "bob"}, g.Members)
}

func TestListMentionGroups(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")

	s.CreateMentionGroup("team-a", aliceID)
	s.CreateMentionGroup("team-b", aliceID)

	groups, err := s.ListMentionGroups()
	require.NoError(t, err)
	require.Len(t, groups, 2)
}

func TestDeleteMentionGroup(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")

	id, _ := s.CreateMentionGroup("temp-team", aliceID)
	require.NoError(t, s.DeleteMentionGroup(id))

	_, err := s.GetMentionGroup("temp-team")
	require.Error(t, err)
}

func TestMentionGroupMembers(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")

	id, _ := s.CreateMentionGroup("team", aliceID)
	require.NoError(t, s.AddMentionGroupMember(id, aliceID))
	require.NoError(t, s.AddMentionGroupMember(id, bobID))

	members, err := s.GetMentionGroupMembers(id)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"alice", "bob"}, members)

	// Remove bob.
	require.NoError(t, s.RemoveMentionGroupMember(id, bobID))
	members, err = s.GetMentionGroupMembers(id)
	require.NoError(t, err)
	require.Equal(t, []string{"alice"}, members)

	// Duplicate add is idempotent.
	require.NoError(t, s.AddMentionGroupMember(id, aliceID))
}

func TestExpandMentionGroups(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")

	id, _ := s.CreateMentionGroup("backend", aliceID)
	s.AddMentionGroupMember(id, aliceID)
	s.AddMentionGroupMember(id, bobID)

	result, err := s.ExpandMentionGroups([]string{"backend", "nonexistent"})
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.ElementsMatch(t, []int64{aliceID, bobID}, result["backend"])
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/infra/sqlite/ -run TestCreateMentionGroup -v`
Expected: FAIL — methods not implemented

- [ ] **Step 3: Implement SQLite store methods**

Create `pkg/infra/sqlite/mention_groups.go`:

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package sqlite

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

func (s *Store) CreateMentionGroup(slug string, createdBy int64) (int64, error) {
	res, err := s.db.Exec("INSERT INTO mention_groups (slug, created_by) VALUES (?, ?)", slug, createdBy)
	if err != nil {
		return 0, fmt.Errorf("create mention group: %w", err)
	}
	return res.LastInsertId()
}

func (s *Store) DeleteMentionGroup(id int64) error {
	res, err := s.db.Exec("DELETE FROM mention_groups WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete mention group: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("mention group not found: %d", id)
	}
	return nil
}

func (s *Store) GetMentionGroup(slug string) (*domain.MentionGroup, error) {
	var g domain.MentionGroup
	err := s.db.QueryRow(
		"SELECT mg.id, mg.slug, u.username, mg.created_at FROM mention_groups mg JOIN users u ON mg.created_by = u.id WHERE mg.slug = ?",
		slug,
	).Scan(&g.ID, &g.Slug, &g.CreatedBy, &g.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("mention group not found: %s", slug)
	}
	if err != nil {
		return nil, fmt.Errorf("get mention group: %w", err)
	}

	members, err := s.GetMentionGroupMembers(g.ID)
	if err != nil {
		return nil, err
	}
	g.Members = members
	return &g, nil
}

func (s *Store) ListMentionGroups() ([]domain.MentionGroup, error) {
	rows, err := s.db.Query(
		"SELECT mg.id, mg.slug, u.username, mg.created_at FROM mention_groups mg JOIN users u ON mg.created_by = u.id ORDER BY mg.slug",
	)
	if err != nil {
		return nil, fmt.Errorf("list mention groups: %w", err)
	}
	defer rows.Close()

	var groups []domain.MentionGroup
	for rows.Next() {
		var g domain.MentionGroup
		if err := rows.Scan(&g.ID, &g.Slug, &g.CreatedBy, &g.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan mention group: %w", err)
		}
		members, err := s.GetMentionGroupMembers(g.ID)
		if err != nil {
			return nil, err
		}
		g.Members = members
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (s *Store) AddMentionGroupMember(groupID, userID int64) error {
	_, err := s.db.Exec(
		"INSERT OR IGNORE INTO mention_group_members (group_id, user_id) VALUES (?, ?)",
		groupID, userID,
	)
	if err != nil {
		return fmt.Errorf("add mention group member: %w", err)
	}
	return nil
}

func (s *Store) RemoveMentionGroupMember(groupID, userID int64) error {
	_, err := s.db.Exec(
		"DELETE FROM mention_group_members WHERE group_id = ? AND user_id = ?",
		groupID, userID,
	)
	if err != nil {
		return fmt.Errorf("remove mention group member: %w", err)
	}
	return nil
}

func (s *Store) GetMentionGroupMembers(groupID int64) ([]string, error) {
	rows, err := s.db.Query(
		"SELECT u.username FROM mention_group_members mgm JOIN users u ON mgm.user_id = u.id WHERE mgm.group_id = ? ORDER BY u.username",
		groupID,
	)
	if err != nil {
		return nil, fmt.Errorf("get mention group members: %w", err)
	}
	defer rows.Close()

	var members []string
	for rows.Next() {
		var username string
		if err := rows.Scan(&username); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		members = append(members, username)
	}
	return members, rows.Err()
}

func (s *Store) ExpandMentionGroups(slugs []string) (map[string][]int64, error) {
	if len(slugs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(slugs))
	args := make([]interface{}, len(slugs))
	for i, slug := range slugs {
		placeholders[i] = "?"
		args[i] = slug
	}

	rows, err := s.db.Query(
		fmt.Sprintf(
			"SELECT mg.slug, mgm.user_id FROM mention_groups mg JOIN mention_group_members mgm ON mg.id = mgm.group_id WHERE mg.slug IN (%s)",
			strings.Join(placeholders, ","),
		),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("expand mention groups: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]int64)
	for rows.Next() {
		var slug string
		var userID int64
		if err := rows.Scan(&slug, &userID); err != nil {
			return nil, fmt.Errorf("scan expansion: %w", err)
		}
		result[slug] = append(result[slug], userID)
	}
	return result, rows.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/infra/sqlite/ -run "TestCreateMentionGroup|TestGetMentionGroup|TestListMentionGroups|TestDeleteMentionGroup|TestMentionGroupMembers|TestExpandMentionGroups" -v`
Expected: PASS

- [ ] **Step 5: Run full unit tests**

Run: `mise run test`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/infra/sqlite/mention_groups.go pkg/infra/sqlite/store_test.go
git commit -m "feat: add SQLite mention group store implementation with tests"
```

---

### Task 3: Postgres store implementation + tests

**Files:**
- Create: `pkg/infra/postgres/mention_groups.go`
- Modify: `pkg/infra/postgres/store_test.go`

- [ ] **Step 1: Write failing tests**

Add the same test functions to `pkg/infra/postgres/store_test.go` as Task 2
Step 1, but using `newTestStore(t)` from the postgres package (which connects
to a real Postgres instance). The test code is identical — copy from Task 2.

- [ ] **Step 2: Implement Postgres store methods**

Create `pkg/infra/postgres/mention_groups.go`. Copy from the SQLite version
with these dialect changes:

- `INSERT OR IGNORE` → `INSERT ... ON CONFLICT DO NOTHING`
- `?` placeholders → `$1, $2, ...` numbered placeholders
- `LastInsertId()` → use `RETURNING id` with `QueryRow` instead of `Exec`
- `fmt.Sprintf` for IN clause → use numbered placeholders `$1, $2, ...`

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package postgres

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

func (s *Store) CreateMentionGroup(slug string, createdBy int64) (int64, error) {
	var id int64
	err := s.db.QueryRow(
		"INSERT INTO mention_groups (slug, created_by) VALUES ($1, $2) RETURNING id",
		slug, createdBy,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create mention group: %w", err)
	}
	return id, nil
}

func (s *Store) DeleteMentionGroup(id int64) error {
	res, err := s.db.Exec("DELETE FROM mention_groups WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete mention group: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("mention group not found: %d", id)
	}
	return nil
}

func (s *Store) GetMentionGroup(slug string) (*domain.MentionGroup, error) {
	var g domain.MentionGroup
	err := s.db.QueryRow(
		"SELECT mg.id, mg.slug, u.username, mg.created_at FROM mention_groups mg JOIN users u ON mg.created_by = u.id WHERE mg.slug = $1",
		slug,
	).Scan(&g.ID, &g.Slug, &g.CreatedBy, &g.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("mention group not found: %s", slug)
	}
	if err != nil {
		return nil, fmt.Errorf("get mention group: %w", err)
	}
	members, err := s.GetMentionGroupMembers(g.ID)
	if err != nil {
		return nil, err
	}
	g.Members = members
	return &g, nil
}

func (s *Store) ListMentionGroups() ([]domain.MentionGroup, error) {
	rows, err := s.db.Query(
		"SELECT mg.id, mg.slug, u.username, mg.created_at FROM mention_groups mg JOIN users u ON mg.created_by = u.id ORDER BY mg.slug",
	)
	if err != nil {
		return nil, fmt.Errorf("list mention groups: %w", err)
	}
	defer rows.Close()

	var groups []domain.MentionGroup
	for rows.Next() {
		var g domain.MentionGroup
		if err := rows.Scan(&g.ID, &g.Slug, &g.CreatedBy, &g.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan mention group: %w", err)
		}
		members, err := s.GetMentionGroupMembers(g.ID)
		if err != nil {
			return nil, err
		}
		g.Members = members
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (s *Store) AddMentionGroupMember(groupID, userID int64) error {
	_, err := s.db.Exec(
		"INSERT INTO mention_group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		groupID, userID,
	)
	if err != nil {
		return fmt.Errorf("add mention group member: %w", err)
	}
	return nil
}

func (s *Store) RemoveMentionGroupMember(groupID, userID int64) error {
	_, err := s.db.Exec(
		"DELETE FROM mention_group_members WHERE group_id = $1 AND user_id = $2",
		groupID, userID,
	)
	if err != nil {
		return fmt.Errorf("remove mention group member: %w", err)
	}
	return nil
}

func (s *Store) GetMentionGroupMembers(groupID int64) ([]string, error) {
	rows, err := s.db.Query(
		"SELECT u.username FROM mention_group_members mgm JOIN users u ON mgm.user_id = u.id WHERE mgm.group_id = $1 ORDER BY u.username",
		groupID,
	)
	if err != nil {
		return nil, fmt.Errorf("get mention group members: %w", err)
	}
	defer rows.Close()

	var members []string
	for rows.Next() {
		var username string
		if err := rows.Scan(&username); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		members = append(members, username)
	}
	return members, rows.Err()
}

func (s *Store) ExpandMentionGroups(slugs []string) (map[string][]int64, error) {
	if len(slugs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(slugs))
	args := make([]interface{}, len(slugs))
	for i, slug := range slugs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = slug
	}

	rows, err := s.db.Query(
		fmt.Sprintf(
			"SELECT mg.slug, mgm.user_id FROM mention_groups mg JOIN mention_group_members mgm ON mg.id = mgm.group_id WHERE mg.slug IN (%s)",
			strings.Join(placeholders, ","),
		),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("expand mention groups: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]int64)
	for rows.Next() {
		var slug string
		var userID int64
		if err := rows.Scan(&slug, &userID); err != nil {
			return nil, fmt.Errorf("scan expansion: %w", err)
		}
		result[slug] = append(result[slug], userID)
	}
	return result, rows.Err()
}
```

- [ ] **Step 3: Run Postgres tests**

Run: `go test ./pkg/infra/postgres/ -run "TestCreateMentionGroup|TestGetMentionGroup|TestListMentionGroups|TestDeleteMentionGroup|TestMentionGroupMembers|TestExpandMentionGroups" -v`
Expected: PASS (requires running Postgres instance)

- [ ] **Step 4: Run full unit tests**

Run: `mise run test`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/infra/postgres/mention_groups.go pkg/infra/postgres/store_test.go
git commit -m "feat: add Postgres mention group store implementation with tests"
```

---

### Task 4: Extend resolveMentions for group expansion

After the body-only prerequisite, `resolveMentions` takes `(store, body)`. Now
extend it: if a candidate doesn't match a username, check if it's a group slug
and expand to member user IDs.

**Files:**
- Modify: `pkg/daemon/mentions.go`
- Modify: `pkg/daemon/ws_handler_test.go`

The `resolveMentions` function needs access to `MentionGroupStore` (for
`ExpandMentionGroups`). Currently it takes `domain.UserStore`. Change it to
take `domain.Store` (which embeds both `UserStore` and `MentionGroupStore`).

- [ ] **Step 1: Write failing test**

Add to `pkg/daemon/ws_handler_test.go`:

```go
func TestWSSendMessageWithMentionGroup(t *testing.T) {
	env := newWSTestEnv(t)
	aliceConn := registerWSUser(t, env, "alice")
	grantAdmin(t, env, "alice")
	bobConn := registerWSUser(t, env, "bob")

	// Create a mention group.
	resp := wsReq(t, aliceConn, "mention_group_create", map[string]interface{}{
		"slug": "backend",
	}, "mg1")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("create group: %+v", resp)
	}
	wsReq(t, aliceConn, "mention_group_add_member", map[string]interface{}{
		"slug": "backend", "username": "bob",
	}, "mg2")

	// Create channel and invite bob.
	wsReq(t, aliceConn, "channel_create", map[string]interface{}{
		"name": "general", "public": true,
	}, "c1")
	wsReq(t, aliceConn, "channel_invite", map[string]interface{}{
		"channel": "general", "username": "bob",
	}, "inv1")

	// Send with group mention.
	resp = wsReq(t, aliceConn, "send_message", map[string]interface{}{
		"channel": "general",
		"body":    "hey @backend check this",
	}, "m1")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("send: %+v", resp)
	}

	// Bob should receive broadcast with mentions (expanded from group).
	bcast := readWSEnvelope(t, bobConn)
	if bcast.Type != "message.new" {
		t.Fatalf("type = %q, want message.new", bcast.Type)
	}
	d, _ := json.Marshal(bcast.D)
	var msg struct {
		Mentions []string `json:"mentions"`
	}
	json.Unmarshal(d, &msg)
	if len(msg.Mentions) == 0 {
		t.Error("expected mentions from group expansion")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/daemon/ -run TestWSSendMessageWithMentionGroup -v`
Expected: FAIL — `mention_group_create` is unknown type

- [ ] **Step 3: Update resolveMentions signature**

In `pkg/daemon/mentions.go`, change the store parameter type and add group
expansion. The function needs a two-pass approach:

1. First pass: collect all `@candidates` from the body via regex
2. Try to resolve each as a username
3. Collect unresolved candidates
4. Batch-expand unresolved candidates as group slugs
5. Add expanded user IDs (deduplicated)

```go
// resolveMentions extracts @-patterns from the message body, resolves
// usernames directly and expands mention groups to their members.
// Invalid usernames and unknown groups are silently ignored.
func resolveMentions(store domain.Store, body string) ([]int64, []string) {
	seen := make(map[string]bool)
	seenIDs := make(map[int64]bool)
	var userIDs []int64
	var usernames []string
	var unresolved []string

	// Extract all @candidates from body.
	for _, match := range mentionRe.FindAllStringSubmatch(body, -1) {
		u := match[1]
		if seen[u] {
			continue
		}
		seen[u] = true

		user, err := store.GetUserByUsername(u)
		if err != nil {
			unresolved = append(unresolved, u)
			continue
		}
		seenIDs[user.ID] = true
		userIDs = append(userIDs, user.ID)
		usernames = append(usernames, user.Username)
	}

	// Expand unresolved candidates as group slugs.
	if len(unresolved) > 0 {
		expanded, err := store.ExpandMentionGroups(unresolved)
		if err == nil {
			for slug, memberIDs := range expanded {
				usernames = append(usernames, slug)
				for _, id := range memberIDs {
					if !seenIDs[id] {
						seenIDs[id] = true
						userIDs = append(userIDs, id)
					}
				}
			}
		}
	}

	return userIDs, usernames
}
```

- [ ] **Step 4: Update resolveMentions call sites**

Both call sites in `mcp_server.go` and `ws_handler.go` already pass
`s.store`/`h.store` which is `domain.Store`. The parameter type change from
`domain.UserStore` to `domain.Store` is compatible — no call site changes
needed.

- [ ] **Step 5: Verify it compiles**

Run: `mise run build`
Expected: PASS

- [ ] **Step 6: Commit (test still fails — group CRUD not wired yet)**

```bash
git add pkg/daemon/mentions.go
git commit -m "feat: extend resolveMentions to expand mention groups"
```

---

### Task 5: MCP tools + handlers

**Files:**
- Modify: `pkg/daemon/mcp_tools.go`
- Modify: `pkg/daemon/mcp_server.go`

- [ ] **Step 1: Add tool definitions**

Add to `pkg/daemon/mcp_tools.go`:

```go
func newMentionGroupCreateTool() mcp.Tool {
	return mcp.NewTool("mention_group_create",
		mcp.WithDescription("Create a new mention group."),
		mcp.WithString("slug", mcp.Required(), mcp.Description("The @-mentionable name for the group")),
	)
}

func newMentionGroupDeleteTool() mcp.Tool {
	return mcp.NewTool("mention_group_delete",
		mcp.WithDescription("Delete a mention group. Only the creator can delete."),
		mcp.WithString("slug", mcp.Required(), mcp.Description("Group slug")),
	)
}

func newMentionGroupGetTool() mcp.Tool {
	return mcp.NewTool("mention_group_get",
		mcp.WithDescription("Get a mention group with its members."),
		mcp.WithString("slug", mcp.Required(), mcp.Description("Group slug")),
	)
}

func newMentionGroupListTool() mcp.Tool {
	return mcp.NewTool("mention_group_list",
		mcp.WithDescription("List all mention groups with their members."),
	)
}

func newMentionGroupAddMemberTool() mcp.Tool {
	return mcp.NewTool("mention_group_add_member",
		mcp.WithDescription("Add a user to a mention group. Only the creator can manage members."),
		mcp.WithString("slug", mcp.Required(), mcp.Description("Group slug")),
		mcp.WithString("username", mcp.Required(), mcp.Description("Username to add")),
	)
}

func newMentionGroupRemoveMemberTool() mcp.Tool {
	return mcp.NewTool("mention_group_remove_member",
		mcp.WithDescription("Remove a user from a mention group. Only the creator can manage members."),
		mcp.WithString("slug", mcp.Required(), mcp.Description("Group slug")),
		mcp.WithString("username", mcp.Required(), mcp.Description("Username to remove")),
	)
}
```

- [ ] **Step 2: Add handlers to mcp_server.go**

Add handler methods. The slug validation regex is `^[a-zA-Z0-9_-]+$`.
Creator-only operations check `group.CreatedBy` against the caller's username.

```go
var slugRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func (s *SharkfinMCP) handleMentionGroupCreate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	slug := req.GetString("slug", "")
	if !slugRe.MatchString(slug) {
		return mcp.NewToolResultError("invalid slug: must match [a-zA-Z0-9_-]+"), nil
	}
	// Reject if slug collides with an existing username.
	if _, err := s.store.GetUserByUsername(slug); err == nil {
		return mcp.NewToolResultError(fmt.Sprintf("slug conflicts with existing username: %s", slug)), nil
	}
	sender, err := s.store.GetUserByUsername(usernameFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	id, err := s.store.CreateMentionGroup(slug, sender.ID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("created mention group @%s (id: %d)", slug, id)), nil
}

func (s *SharkfinMCP) handleMentionGroupDelete(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	slug := req.GetString("slug", "")
	g, err := s.store.GetMentionGroup(slug)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if g.CreatedBy != usernameFromCtx(ctx) {
		return mcp.NewToolResultError("only the group creator can delete it"), nil
	}
	if err := s.store.DeleteMentionGroup(g.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("deleted mention group @%s", slug)), nil
}

func (s *SharkfinMCP) handleMentionGroupGet(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	slug := req.GetString("slug", "")
	g, err := s.store.GetMentionGroup(slug)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	data, _ := json.Marshal(g)
	return mcp.NewToolResultText(string(data)), nil
}

func (s *SharkfinMCP) handleMentionGroupList(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	groups, err := s.store.ListMentionGroups()
	if err != nil {
		return nil, fmt.Errorf("list mention groups: %w", err)
	}
	data, _ := json.Marshal(groups)
	return mcp.NewToolResultText(string(data)), nil
}

func (s *SharkfinMCP) handleMentionGroupAddMember(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	slug := req.GetString("slug", "")
	username := req.GetString("username", "")
	g, err := s.store.GetMentionGroup(slug)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if g.CreatedBy != usernameFromCtx(ctx) {
		return mcp.NewToolResultError("only the group creator can manage members"), nil
	}
	user, err := s.store.GetUserByUsername(username)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("user not found: %s", username)), nil
	}
	if err := s.store.AddMentionGroupMember(g.ID, user.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("added %s to @%s", username, slug)), nil
}

func (s *SharkfinMCP) handleMentionGroupRemoveMember(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	slug := req.GetString("slug", "")
	username := req.GetString("username", "")
	g, err := s.store.GetMentionGroup(slug)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if g.CreatedBy != usernameFromCtx(ctx) {
		return mcp.NewToolResultError("only the group creator can manage members"), nil
	}
	user, err := s.store.GetUserByUsername(username)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("user not found: %s", username)), nil
	}
	if err := s.store.RemoveMentionGroupMember(g.ID, user.ID); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("removed %s from @%s", username, slug)), nil
}
```

Add `"regexp"` to imports if not already present. Add `slugRe` near the top of the file (or in `mentions.go` since it's already used there).

- [ ] **Step 3: Register tools in NewSharkfinMCP**

Add to the `s.mcpServer.AddTools(...)` block in `mcp_server.go` (after line 102):

```go
server.ServerTool{Tool: newMentionGroupCreateTool(), Handler: s.handleMentionGroupCreate},
server.ServerTool{Tool: newMentionGroupDeleteTool(), Handler: s.handleMentionGroupDelete},
server.ServerTool{Tool: newMentionGroupGetTool(), Handler: s.handleMentionGroupGet},
server.ServerTool{Tool: newMentionGroupListTool(), Handler: s.handleMentionGroupList},
server.ServerTool{Tool: newMentionGroupAddMemberTool(), Handler: s.handleMentionGroupAddMember},
server.ServerTool{Tool: newMentionGroupRemoveMemberTool(), Handler: s.handleMentionGroupRemoveMember},
```

No entries needed in `toolPermissions` — the design says any identified user can
use all mention group tools. Creator-only checks are in the handlers.

- [ ] **Step 4: Verify it compiles**

Run: `mise run build`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/daemon/mcp_tools.go pkg/daemon/mcp_server.go pkg/daemon/mentions.go
git commit -m "feat: add MCP tools for mention group CRUD"
```

---

### Task 6: WS handlers

**Files:**
- Modify: `pkg/daemon/ws_handler.go`

- [ ] **Step 1: Add switch cases**

Add before the `default:` case (line 314) in the dispatch switch:

```go
case "mention_group_create":
	if notificationsOnly {
		sendError(sendCh, req.Ref, "notification-only connection")
	} else {
		h.handleWSMentionGroupCreate(sendCh, req.Ref, req.D, userID)
	}
case "mention_group_delete":
	if notificationsOnly {
		sendError(sendCh, req.Ref, "notification-only connection")
	} else {
		h.handleWSMentionGroupDelete(sendCh, req.Ref, req.D, username)
	}
case "mention_group_get":
	if notificationsOnly {
		sendError(sendCh, req.Ref, "notification-only connection")
	} else {
		h.handleWSMentionGroupGet(sendCh, req.Ref, req.D)
	}
case "mention_group_list":
	if notificationsOnly {
		sendError(sendCh, req.Ref, "notification-only connection")
	} else {
		h.handleWSMentionGroupList(sendCh, req.Ref)
	}
case "mention_group_add_member":
	if notificationsOnly {
		sendError(sendCh, req.Ref, "notification-only connection")
	} else {
		h.handleWSMentionGroupAddMember(sendCh, req.Ref, req.D, username)
	}
case "mention_group_remove_member":
	if notificationsOnly {
		sendError(sendCh, req.Ref, "notification-only connection")
	} else {
		h.handleWSMentionGroupRemoveMember(sendCh, req.Ref, req.D, username)
	}
```

No `checkPermission` — any identified user can use all mention group operations.
Creator-only checks are in the handlers.

- [ ] **Step 2: Add handler methods**

Add to `pkg/daemon/ws_handler.go`:

```go
func (h *WSHandler) handleWSMentionGroupCreate(sendCh chan<- []byte, ref string, rawD json.RawMessage, userID int64) {
	var d struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}
	if !slugRe.MatchString(d.Slug) {
		sendError(sendCh, ref, "invalid slug: must match [a-zA-Z0-9_-]+")
		return
	}
	if _, err := h.store.GetUserByUsername(d.Slug); err == nil {
		sendError(sendCh, ref, fmt.Sprintf("slug conflicts with existing username: %s", d.Slug))
		return
	}
	id, err := h.store.CreateMentionGroup(d.Slug, userID)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, map[string]interface{}{"id": id, "slug": d.Slug})
}

func (h *WSHandler) handleWSMentionGroupDelete(sendCh chan<- []byte, ref string, rawD json.RawMessage, username string) {
	var d struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}
	g, err := h.store.GetMentionGroup(d.Slug)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	if g.CreatedBy != username {
		sendError(sendCh, ref, "only the group creator can delete it")
		return
	}
	if err := h.store.DeleteMentionGroup(g.ID); err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, nil)
}

func (h *WSHandler) handleWSMentionGroupGet(sendCh chan<- []byte, ref string, rawD json.RawMessage) {
	var d struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}
	g, err := h.store.GetMentionGroup(d.Slug)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, g)
}

func (h *WSHandler) handleWSMentionGroupList(sendCh chan<- []byte, ref string) {
	groups, err := h.store.ListMentionGroups()
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, map[string]interface{}{"groups": groups})
}

func (h *WSHandler) handleWSMentionGroupAddMember(sendCh chan<- []byte, ref string, rawD json.RawMessage, username string) {
	var d struct {
		Slug     string `json:"slug"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}
	g, err := h.store.GetMentionGroup(d.Slug)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	if g.CreatedBy != username {
		sendError(sendCh, ref, "only the group creator can manage members")
		return
	}
	user, err := h.store.GetUserByUsername(d.Username)
	if err != nil {
		sendError(sendCh, ref, fmt.Sprintf("user not found: %s", d.Username))
		return
	}
	if err := h.store.AddMentionGroupMember(g.ID, user.ID); err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, nil)
}

func (h *WSHandler) handleWSMentionGroupRemoveMember(sendCh chan<- []byte, ref string, rawD json.RawMessage, username string) {
	var d struct {
		Slug     string `json:"slug"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		sendError(sendCh, ref, "invalid arguments")
		return
	}
	g, err := h.store.GetMentionGroup(d.Slug)
	if err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	if g.CreatedBy != username {
		sendError(sendCh, ref, "only the group creator can manage members")
		return
	}
	user, err := h.store.GetUserByUsername(d.Username)
	if err != nil {
		sendError(sendCh, ref, fmt.Sprintf("user not found: %s", d.Username))
		return
	}
	if err := h.store.RemoveMentionGroupMember(g.ID, user.ID); err != nil {
		sendError(sendCh, ref, err.Error())
		return
	}
	sendReply(sendCh, ref, true, nil)
}
```

- [ ] **Step 3: Run the group mention test from Task 4**

Run: `go test ./pkg/daemon/ -run TestWSSendMessageWithMentionGroup -v`
Expected: PASS

- [ ] **Step 4: Run full unit tests**

Run: `mise run test`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/daemon/ws_handler.go
git commit -m "feat: add WS handlers for mention group CRUD"
```

---

### Task 7: E2e tests

**Files:**
- Modify: `tests/e2e/sharkfin_test.go`

- [ ] **Step 1: Add mention group e2e tests**

Add to `tests/e2e/sharkfin_test.go`:

```go
func TestWSMentionGroupCRUD(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	alice, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()
	alice.Register("alice")

	bob, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()
	bob.Register("bob")

	// Create group.
	env, err := alice.Req("mention_group_create", map[string]any{"slug": "team"}, "mg1")
	if err != nil || env.OK == nil || !*env.OK {
		t.Fatalf("create: err=%v env=%+v", err, env)
	}

	// Add member.
	env, err = alice.Req("mention_group_add_member", map[string]any{
		"slug": "team", "username": "bob",
	}, "mg2")
	if err != nil || env.OK == nil || !*env.OK {
		t.Fatalf("add member: err=%v env=%+v", err, env)
	}

	// Get group.
	env, err = alice.Req("mention_group_get", map[string]any{"slug": "team"}, "mg3")
	if err != nil || env.OK == nil || !*env.OK {
		t.Fatalf("get: err=%v env=%+v", err, env)
	}

	// List groups.
	env, err = alice.Req("mention_group_list", nil, "mg4")
	if err != nil || env.OK == nil || !*env.OK {
		t.Fatalf("list: err=%v env=%+v", err, env)
	}

	// Bob cannot delete (not creator).
	env, err = bob.Req("mention_group_delete", map[string]any{"slug": "team"}, "mg5")
	if err != nil {
		t.Fatal(err)
	}
	if env.OK != nil && *env.OK {
		t.Error("expected bob to be denied deletion")
	}

	// Remove member.
	env, err = alice.Req("mention_group_remove_member", map[string]any{
		"slug": "team", "username": "bob",
	}, "mg6")
	if err != nil || env.OK == nil || !*env.OK {
		t.Fatalf("remove member: err=%v env=%+v", err, env)
	}

	// Delete group.
	env, err = alice.Req("mention_group_delete", map[string]any{"slug": "team"}, "mg7")
	if err != nil || env.OK == nil || !*env.OK {
		t.Fatalf("delete: err=%v env=%+v", err, env)
	}
}

func TestWSMentionGroupExpansion(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	alice, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()
	alice.Register("alice")

	bob, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()
	bob.Register("bob")

	// Alice creates group and adds bob.
	alice.Req("mention_group_create", map[string]any{"slug": "devs"}, "g1")
	alice.Req("mention_group_add_member", map[string]any{
		"slug": "devs", "username": "bob",
	}, "g2")

	// Create channel.
	alice.Req("channel_create", map[string]any{
		"name": "general", "public": true,
	}, "c1")
	alice.Req("channel_invite", map[string]any{
		"channel": "general", "username": "bob",
	}, "inv1")

	// Send with @devs.
	env, err := alice.Req("send_message", map[string]any{
		"channel": "general",
		"body":    "hey @devs review this",
	}, "m1")
	if err != nil || env.OK == nil || !*env.OK {
		t.Fatalf("send: err=%v env=%+v", err, env)
	}

	// Bob should get broadcast (group expanded to include bob).
	bcast, err := bob.Read()
	if err != nil {
		t.Fatal(err)
	}
	if bcast.Type != "message.new" {
		t.Fatalf("type = %q, want message.new", bcast.Type)
	}

	// Bob should see the message via mentions_only filter.
	env, err = bob.Req("unread_messages", map[string]any{
		"mentions_only": true,
	}, "u1")
	if err != nil || env.OK == nil || !*env.OK {
		t.Fatalf("unread: err=%v env=%+v", err, env)
	}
}
```

- [ ] **Step 2: Run e2e tests**

Run: `mise run e2e`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/sharkfin_test.go
git commit -m "test: add e2e tests for mention groups"
```

---

### Task 8: Full CI pass

- [ ] **Step 1: Run CI**

Run: `mise run ci`
Expected: All lint, unit, and e2e tests pass.

- [ ] **Step 2: Verify no regressions**

Check that:
- All existing mention tests pass (body-only extraction still works)
- `TestCreateMentionGroup` passes (SQLite store)
- `TestExpandMentionGroups` passes (SQLite store)
- `TestWSSendMessageWithMentionGroup` passes (WS handler + resolution)
- `TestWSMentionGroupCRUD` passes (e2e CRUD)
- `TestWSMentionGroupExpansion` passes (e2e mention expansion)
- All other tests pass
