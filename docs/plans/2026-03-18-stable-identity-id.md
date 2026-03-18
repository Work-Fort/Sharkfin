# Stable Internal Identity ID — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Decouple the internal identity primary key from the Passport-provided auth ID so that Passport user recreation doesn't break FK references.

**Architecture:** Add an `auth_id` column to `identities` that stores the Passport UUID. The existing `id` column becomes the stable internal key — its value never changes after creation. New identities generate a fresh UUID for `id`; `auth_id` is the external lookup key. `UpsertIdentity` resolves by `auth_id` first, then `username`, and logs a warning when Passport's ID changes.

**Tech Stack:** Go, SQLite (modernc.org/sqlite), PostgreSQL (lib/pq), charmbracelet/log

---

## Context

### The problem

The `identities.id` column currently stores the Passport user UUID directly. All FK references (`messages.identity_id`, `channel_members.identity_id`, `read_cursors.identity_id`, `message_mentions.identity_id`, `mention_groups.created_by`, `mention_group_members.identity_id`) point to this value.

When Passport recreates a user (e.g. Bun migration, DB wipe, or re-seed), the same username gets a new UUID. The `UpsertIdentity` tries to INSERT with the new UUID + existing username, violating the `UNIQUE(username)` constraint. Result: 500 on every WS/MCP connection for that user.

### The fix

- `identities.id` — internal UUID, generated once, never changes. Used by all FKs.
- `identities.auth_id` — Passport UUID (`TEXT UNIQUE`). Updated when Passport's ID changes.
- `UpsertIdentity` resolves existing identity by `auth_id` first, then `username`. If found, updates `auth_id` + metadata. If not found, creates new row with fresh internal `id`.
- Existing data: migration copies `id` → `auth_id`; existing `id` values stay (they're already stable UUIDs).

### Files involved

| File | Change |
|------|--------|
| `pkg/infra/sqlite/migrations/008_auth_id.sql` | Create |
| `pkg/infra/postgres/migrations/008_auth_id.sql` | Create |
| `pkg/domain/types.go` | Add `AuthID` field to `Identity` |
| `pkg/domain/ports.go` | Change `UpsertIdentity` signature |
| `pkg/infra/sqlite/identities.go` | Rewrite `UpsertIdentity`, update queries to select `auth_id` |
| `pkg/infra/postgres/identities.go` | Same as SQLite (Postgres dialect) |
| `pkg/daemon/ws_handler.go` | Pass `identity.ID` as `authID` arg |
| `pkg/daemon/mcp_server.go` | Pass `identity.ID` as `authID` arg |
| `pkg/daemon/notification_handler.go` | Pass `identity.ID` as `authID` arg |
| `pkg/backup/import.go` | Generate internal UUID for `id`, Passport-style UUID for `auth_id` |
| `pkg/infra/sqlite/store_test.go` | Update tests |
| `pkg/infra/postgres/store_test.go` | Update tests |

### What does NOT change

- FK column types — all remain `TEXT`, all still reference `identities.id`
- `GetIdentityByID`, `GetIdentityByUsername`, `ListIdentities` — same signatures, just also SELECT `auth_id`
- `ChannelStore`, `MessageStore`, `MentionGroupStore` — no changes (they use `identityID` which is the internal `id`)
- Hub, WSClient, mentions.go — no changes (they pass around the internal `id`)

---

### Task 1: Migration — add `auth_id` column

**Files:**
- Create: `pkg/infra/sqlite/migrations/008_auth_id.sql`
- Create: `pkg/infra/postgres/migrations/008_auth_id.sql`

**Step 1: Write the SQLite migration**

```sql
-- 008_auth_id.sql
-- Adds auth_id column to decouple internal ID from Passport UUID.
ALTER TABLE identities ADD COLUMN auth_id TEXT;
UPDATE identities SET auth_id = id WHERE auth_id IS NULL;
CREATE UNIQUE INDEX idx_identities_auth_id ON identities(auth_id);
```

Write identical content to `pkg/infra/sqlite/migrations/008_auth_id.sql`.

**Step 2: Write the Postgres migration**

```sql
-- 008_auth_id.sql
-- Adds auth_id column to decouple internal ID from Passport UUID.
ALTER TABLE identities ADD COLUMN auth_id TEXT;
UPDATE identities SET auth_id = id WHERE auth_id IS NULL;
CREATE UNIQUE INDEX idx_identities_auth_id ON identities(auth_id);
```

Write identical content to `pkg/infra/postgres/migrations/008_auth_id.sql`.

**Step 3: Run tests to verify migration applies cleanly**

Run: `mise run test`
Expected: existing tests pass (migration adds column with no breaking changes yet)

**Step 4: Commit**

```bash
git add pkg/infra/sqlite/migrations/008_auth_id.sql pkg/infra/postgres/migrations/008_auth_id.sql
git commit -m "feat: add auth_id column to identities (migration 008)"
```

---

### Task 2: Domain types — add `AuthID` to Identity struct

**Files:**
- Modify: `pkg/domain/types.go:6-13`

**Step 1: Add the AuthID field**

Change the `Identity` struct from:

```go
type Identity struct {
	ID          string // Passport UUID
	Username    string
	DisplayName string
	Type        string // "user", "agent", "service"
	Role        string
	CreatedAt   time.Time
}
```

to:

```go
type Identity struct {
	ID          string // Internal stable UUID (never changes after creation)
	AuthID      string // Passport-provided UUID (may change if user is recreated)
	Username    string
	DisplayName string
	Type        string // "user", "agent", "service"
	Role        string
	CreatedAt   time.Time
}
```

**Step 2: Run tests — expect compilation failures**

Run: `go build ./...`
Expected: compilation succeeds (AuthID is additive, no code references it yet)

**Step 3: Commit**

```bash
git add pkg/domain/types.go
git commit -m "feat: add AuthID field to domain.Identity"
```

---

### Task 3: Update store interface — change `UpsertIdentity` signature

**Files:**
- Modify: `pkg/domain/ports.go:4-9`

**Step 1: Change the UpsertIdentity signature**

The first parameter changes from `id` (the Passport UUID used as PK) to `authID` (the Passport UUID stored in the new column). The store resolves the existing identity internally.

Change:

```go
type IdentityStore interface {
	UpsertIdentity(id, username, displayName, identityType, role string) error
	GetIdentityByID(id string) (*Identity, error)
	GetIdentityByUsername(username string) (*Identity, error)
	ListIdentities() ([]Identity, error)
}
```

to:

```go
type IdentityStore interface {
	UpsertIdentity(authID, username, displayName, identityType, role string) (*Identity, error)
	GetIdentityByID(id string) (*Identity, error)
	GetIdentityByUsername(username string) (*Identity, error)
	ListIdentities() ([]Identity, error)
}
```

Note: `UpsertIdentity` now returns `*Identity` so callers get the internal `id` directly without a second lookup.

**Step 2: Verify build fails (expected)**

Run: `go build ./...`
Expected: FAIL — SQLite/Postgres stores don't match new signature

**Step 3: Commit**

```bash
git add pkg/domain/ports.go
git commit -m "feat: change UpsertIdentity to accept authID, return *Identity"
```

---

### Task 4: SQLite store — implement new `UpsertIdentity`

**Files:**
- Modify: `pkg/infra/sqlite/identities.go:11-31`
- Test: `pkg/infra/sqlite/store_test.go`

**Step 1: Write a failing test for auth_id migration**

Add to `pkg/infra/sqlite/store_test.go`:

```go
func TestUpsertIdentityAuthIDChange(t *testing.T) {
	store := newTestStore(t)

	// First provision with Passport UUID "old-passport-id"
	ident1, err := store.UpsertIdentity("old-passport-id", "alice", "Alice", "user", "user")
	require.NoError(t, err)
	require.Equal(t, "alice", ident1.Username)
	require.Equal(t, "old-passport-id", ident1.AuthID)
	internalID := ident1.ID

	// Passport recreates user with new UUID
	ident2, err := store.UpsertIdentity("new-passport-id", "alice", "Alice Updated", "user", "user")
	require.NoError(t, err)
	require.Equal(t, internalID, ident2.ID, "internal ID must not change")
	require.Equal(t, "new-passport-id", ident2.AuthID)
	require.Equal(t, "Alice Updated", ident2.DisplayName)

	// FK references still work — lookup by internal ID
	ident3, err := store.GetIdentityByID(internalID)
	require.NoError(t, err)
	require.Equal(t, "new-passport-id", ident3.AuthID)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/infra/sqlite/ -run TestUpsertIdentityAuthIDChange -v`
Expected: FAIL (signature mismatch / auth_id not handled)

**Step 3: Rewrite UpsertIdentity in SQLite store**

Replace `pkg/infra/sqlite/identities.go` UpsertIdentity with:

```go
func (s *Store) UpsertIdentity(authID, username, displayName, identityType, role string) (*domain.Identity, error) {
	if role == "" {
		role = "user"
	}
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM identities").Scan(&count)
	if count == 0 {
		role = "admin"
	}

	// Try to find existing identity by auth_id first, then by username.
	var existingID string
	err := s.db.QueryRow("SELECT id FROM identities WHERE auth_id = ?", authID).Scan(&existingID)
	if err != nil {
		// No match on auth_id — try username (handles Passport ID change).
		err = s.db.QueryRow("SELECT id FROM identities WHERE username = ?", username).Scan(&existingID)
	}

	if existingID != "" {
		// Update existing identity.
		var existingAuthID string
		s.db.QueryRow("SELECT auth_id FROM identities WHERE id = ?", existingID).Scan(&existingAuthID)
		if existingAuthID != authID {
			log.Warn("identity auth_id changed, updating to match Passport",
				"username", username, "old_auth_id", existingAuthID, "new_auth_id", authID)
		}
		_, err := s.db.Exec(`UPDATE identities SET auth_id = ?, username = ?, display_name = ?, type = ? WHERE id = ?`,
			authID, username, displayName, identityType, existingID)
		if err != nil {
			return nil, fmt.Errorf("update identity: %w", err)
		}
		return s.GetIdentityByID(existingID)
	}

	// New identity — generate internal UUID.
	internalID := newUUID()
	_, err = s.db.Exec(`INSERT INTO identities (id, auth_id, username, display_name, type, role) VALUES (?, ?, ?, ?, ?, ?)`,
		internalID, authID, username, displayName, identityType, role)
	if err != nil {
		return nil, fmt.Errorf("insert identity: %w", err)
	}
	return s.GetIdentityByID(internalID)
}
```

You'll need a `newUUID()` helper. Add at the top of the file:

```go
import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"

	"github.com/charmbracelet/log"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
```

Also update `GetIdentityByID`, `GetIdentityByUsername`, and `ListIdentities` to include `auth_id` in their SELECT statements:

In `GetIdentityByID`:
```go
err := s.db.QueryRow(
	"SELECT id, auth_id, username, display_name, type, role, created_at FROM identities WHERE id = ?", id,
).Scan(&i.ID, &i.AuthID, &i.Username, &i.DisplayName, &i.Type, &i.Role, &i.CreatedAt)
```

Same pattern for `GetIdentityByUsername` and `ListIdentities` (add `auth_id` to SELECT and Scan).

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/infra/sqlite/ -run TestUpsertIdentityAuthIDChange -v`
Expected: PASS

**Step 5: Fix existing tests**

Update `TestUpsertIdentity`, `TestUpsertIdentityFirstUserAutoAdmin`, and `TestUpsertIdentityIdempotent` to match the new signature (returns `*Identity` instead of `error`).

**Step 6: Run all SQLite store tests**

Run: `go test ./pkg/infra/sqlite/ -v`
Expected: all pass

**Step 7: Commit**

```bash
git add pkg/infra/sqlite/identities.go pkg/infra/sqlite/store_test.go
git commit -m "feat: SQLite UpsertIdentity with auth_id resolution"
```

---

### Task 5: Postgres store — implement new `UpsertIdentity`

**Files:**
- Modify: `pkg/infra/postgres/identities.go:11-27`
- Test: `pkg/infra/postgres/store_test.go`

**Step 1: Mirror the SQLite implementation with Postgres dialect**

Same logic as Task 4 but with `$1`-style placeholders instead of `?`.

**Step 2: Update SELECT queries to include `auth_id`**

Same as Task 4 — add `auth_id` to all identity SELECT/Scan calls.

**Step 3: Update Postgres tests to match new signature**

**Step 4: Run tests**

Run: `go test ./pkg/infra/postgres/ -v` (if Postgres is available, otherwise skip)

**Step 5: Commit**

```bash
git add pkg/infra/postgres/identities.go pkg/infra/postgres/store_test.go
git commit -m "feat: Postgres UpsertIdentity with auth_id resolution"
```

---

### Task 6: Update daemon handlers — WS, MCP, notifications

**Files:**
- Modify: `pkg/daemon/ws_handler.go:46-78`
- Modify: `pkg/daemon/mcp_server.go:108-139`
- Modify: `pkg/daemon/notification_handler.go:29-66`

**Step 1: Update ws_handler.go**

The `ServeHTTP` method currently calls `UpsertIdentity(identity.ID, ...)` and then `GetIdentityByID(identity.ID)`. Since `UpsertIdentity` now returns `*Identity`, remove the second lookup.

Change from:

```go
if err := h.store.UpsertIdentity(identity.ID, identity.Username, identity.DisplayName, identity.Type, role); err != nil {
	http.Error(w, "identity provisioning failed", http.StatusInternalServerError)
	return
}
localIdentity, err := h.store.GetIdentityByID(identity.ID)
if err != nil {
	http.Error(w, "identity lookup failed", http.StatusInternalServerError)
	return
}
```

to:

```go
localIdentity, err := h.store.UpsertIdentity(identity.ID, identity.Username, identity.DisplayName, identity.Type, role)
if err != nil {
	http.Error(w, "identity provisioning failed", http.StatusInternalServerError)
	return
}
```

Note: `identity.ID` here is the Passport auth middleware's `Identity.ID` (the Passport UUID), which maps to the `authID` parameter.

**Step 2: Update mcp_server.go**

Same pattern — `UpsertIdentity` now returns `*Identity`. The MCP handler currently ignores the error. Change to use the return value or at minimum handle the new signature:

```go
_ = s.store.UpsertIdentity(identity.ID, identity.Username, identity.DisplayName, identity.Type, role)
```

becomes:

```go
if _, err := s.store.UpsertIdentity(identity.ID, identity.Username, identity.DisplayName, identity.Type, role); err != nil {
	log.Warn("identity provisioning failed", "err", err, "username", identity.Username)
}
```

**Step 3: Update notification_handler.go**

Same pattern as ws_handler.go — collapse `UpsertIdentity` + `GetIdentityByID` into single `UpsertIdentity` call.

**Step 4: Run full test suite**

Run: `mise run test`
Expected: all pass (except the pre-existing TestScenario4 failure)

**Step 5: Commit**

```bash
git add pkg/daemon/ws_handler.go pkg/daemon/mcp_server.go pkg/daemon/notification_handler.go
git commit -m "feat: update daemon handlers for UpsertIdentity return value"
```

---

### Task 7: Update backup import

**Files:**
- Modify: `pkg/backup/import.go`

**Step 1: Update import to use new UpsertIdentity signature**

The import currently generates a synthetic UUID and passes it as the `id` parameter. Now it becomes the `authID` parameter — the store will generate the internal `id` automatically.

The `identityIDByName` map should be populated from the returned `*Identity`:

Change from:

```go
identityID := uuid.New().String()
store.UpsertIdentity(identityID, u.Username, u.Username, identityType, role)
identityIDByName[u.Username] = identityID
```

to:

```go
ident, err := store.UpsertIdentity(uuid.New().String(), u.Username, u.Username, identityType, role)
if err != nil {
	return fmt.Errorf("import identity %s: %w", u.Username, err)
}
identityIDByName[u.Username] = ident.ID
```

**Step 2: Run backup tests**

Run: `go test ./pkg/backup/ -v`
Expected: pass

**Step 3: Commit**

```bash
git add pkg/backup/import.go
git commit -m "feat: update backup import for new UpsertIdentity signature"
```

---

### Task 8: Update integration tests + final verification

**Files:**
- Modify: `tests/integration_test.go` (if it calls UpsertIdentity directly)

**Step 1: Check if integration tests reference UpsertIdentity**

Search for direct UpsertIdentity calls in `tests/`. If found, update to match new signature.

**Step 2: Run full test suite**

Run: `mise run test`
Expected: all pass (except pre-existing TestScenario4)

**Step 3: Manual verification**

1. Stop the daemon: `systemctl --user stop sharkfin`
2. Rebuild + install: `mise run deploy`
3. Verify the daemon starts and migration 008 applies (check logs)
4. Open browser → the WS connection should work without "identity provisioning failed"

**Step 4: Commit**

```bash
git add -A
git commit -m "feat: update integration tests for stable identity ID"
```

---

## Summary

| Task | What | Risk |
|------|------|------|
| 1 | Migration 008 — add `auth_id` column | Low — additive |
| 2 | `Identity.AuthID` field | Low — additive |
| 3 | `UpsertIdentity` signature change | Medium — breaks all callers |
| 4 | SQLite store implementation | Medium — core logic |
| 5 | Postgres store implementation | Medium — mirrors SQLite |
| 6 | Daemon handlers | Low — simplification |
| 7 | Backup import | Low — minor change |
| 8 | Integration tests + verification | Low — cleanup |

Total: ~8 tasks, each with 3-5 steps. The signature change in Task 3 creates a compile-time checkpoint — nothing builds until Tasks 4-7 are done, which prevents partial implementations.
