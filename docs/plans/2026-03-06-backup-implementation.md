# Encrypted S3 Backup Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `sharkfin backup {export,import,list}` for encrypted, backend-portable S3 backups.

**Architecture:** Export queries all data through `domain.Store` interface, serializes to JSON with natural keys (usernames, channel names), tars with config.yaml, compresses with xz, encrypts with age, uploads to S3. Import reverses the pipeline. Thread references use sequential export IDs mapped to real IDs on import. Read cursors are not backed up (ephemeral state). A `BackupStore` interface extends `domain.Store` with `ImportMessage` (preserves timestamps) and `IsEmpty`.

**Tech Stack:** `filippo.io/age`, `github.com/ulikunitz/xz`, `github.com/aws/aws-sdk-go-v2`

**Design doc:** `docs/plans/2026-03-06-backup-design.md`

---

### Task 1: Add Dependencies

**Files:**
- Modify: `go.mod`

**Step 1: Add new dependencies**

```bash
cd /home/kazw/Work/WorkFort/sharkfin
go get filippo.io/age
go get github.com/ulikunitz/xz
go get github.com/aws/aws-sdk-go-v2
go get github.com/aws/aws-sdk-go-v2/config
go get github.com/aws/aws-sdk-go-v2/credentials
go get github.com/aws/aws-sdk-go-v2/service/s3
```

**Step 2: Tidy**

```bash
go mod tidy
```

**Step 3: Verify build**

Run: `mise run build`
Expected: PASS

**Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add age, xz, and aws-sdk-go-v2 for backup feature"
```

---

### Task 2: Backup Data Types and BackupStore Interface

**Files:**
- Create: `pkg/backup/data.go`

**Context:** This file defines the JSON-serializable backup format and the `BackupStore` interface that extends `domain.Store` with two backup-specific methods. The existing `domain.Store` interface is NOT modified — `BackupStore` is defined here in the backup package and implemented by both SQLite and Postgres backends (added in Task 3).

**Step 1: Create `pkg/backup/data.go`**

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package backup

import (
	"time"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

// BackupStore extends domain.Store with backup-specific methods.
// Both sqlite.Store and postgres.Store implement this interface.
type BackupStore interface {
	domain.Store
	// ImportMessage inserts a message with a specific created_at timestamp.
	// Used during backup import to preserve original message timestamps.
	ImportMessage(channelID, userID int64, body string, threadID *int64, mentionUserIDs []int64, createdAt time.Time) (int64, error)
	// IsEmpty returns true if the database has no users.
	IsEmpty() (bool, error)
}

// Backup is the top-level JSON structure written to data.json inside the archive.
type Backup struct {
	Version    int       `json:"version"`
	ExportedAt time.Time `json:"exported_at"`

	Users           []BackupUser             `json:"users"`
	Channels        []BackupChannel          `json:"channels"`
	ChannelMembers  map[string][]string      `json:"channel_members"`
	Messages        []BackupMessage          `json:"messages"`
	Roles           []BackupRole             `json:"roles"`
	RolePermissions map[string][]string      `json:"role_permissions"`
	Settings        map[string]string        `json:"settings"`
	DMs             []BackupDM               `json:"dms"`
}

type BackupUser struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
	Type     string `json:"type"`
}

type BackupChannel struct {
	Name   string `json:"name"`
	Public bool   `json:"public"`
	Type   string `json:"type"`
}

type BackupMessage struct {
	ID        int       `json:"id"`
	Channel   string    `json:"channel"`
	From      string    `json:"from"`
	Body      string    `json:"body"`
	ThreadID  *int      `json:"thread_id"`
	Mentions  []string  `json:"mentions"`
	CreatedAt time.Time `json:"created_at"`
}

type BackupRole struct {
	Name    string `json:"name"`
	BuiltIn bool   `json:"built_in"`
}

type BackupDM struct {
	User1       string `json:"user1"`
	User2       string `json:"user2"`
	ChannelName string `json:"channel_name"`
}
```

**Step 2: Verify build**

Run: `go vet ./pkg/backup/...`
Expected: PASS (no errors)

**Step 3: Commit**

```bash
git add pkg/backup/data.go
git commit -m "feat: add backup data types and BackupStore interface"
```

---

### Task 3: Add BackupStore Methods to Both Backends

**Files:**
- Modify: `pkg/infra/sqlite/messages.go`
- Modify: `pkg/infra/sqlite/users.go`
- Modify: `pkg/infra/postgres/messages.go`
- Modify: `pkg/infra/postgres/users.go`
- Modify: `pkg/infra/sqlite/store_test.go`

**Context:** Add `ImportMessage` and `IsEmpty` to both backends so they satisfy the `backup.BackupStore` interface. `ImportMessage` is identical to `SendMessage` except it accepts a `createdAt` parameter. `IsEmpty` checks `SELECT COUNT(*) FROM users`.

**Step 1: Write the test for ImportMessage and IsEmpty in SQLite**

Add to `pkg/infra/sqlite/store_test.go`:

```go
func TestImportMessage(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	uid, _ := s.CreateUser("alice", "")
	chID, _ := s.CreateChannel("general", true, []int64{uid}, "channel")

	ts := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	msgID, err := s.ImportMessage(chID, uid, "old message", nil, nil, ts)
	if err != nil {
		t.Fatalf("import message: %v", err)
	}
	if msgID == 0 {
		t.Fatal("expected non-zero message ID")
	}

	msgs, err := s.GetMessages(chID, nil, nil, 50, nil)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !msgs[0].CreatedAt.Equal(ts) {
		t.Errorf("created_at = %v, want %v", msgs[0].CreatedAt, ts)
	}
}

func TestIsEmpty(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	empty, err := s.IsEmpty()
	if err != nil {
		t.Fatalf("is empty: %v", err)
	}
	if !empty {
		t.Error("expected empty store")
	}

	s.CreateUser("alice", "")
	empty, err = s.IsEmpty()
	if err != nil {
		t.Fatalf("is empty: %v", err)
	}
	if empty {
		t.Error("expected non-empty store")
	}
}
```

**Step 2: Run the test to verify it fails**

Run: `go test ./pkg/infra/sqlite/... -run TestImportMessage -v`
Expected: FAIL — `s.ImportMessage undefined`

**Step 3: Implement ImportMessage and IsEmpty for SQLite**

Add to `pkg/infra/sqlite/messages.go`:

```go
// ImportMessage inserts a message with a specific created_at timestamp.
// Used during backup import to preserve original timestamps.
func (s *Store) ImportMessage(channelID, userID int64, body string, threadID *int64, mentionUserIDs []int64, createdAt time.Time) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		"INSERT INTO messages (channel_id, user_id, body, thread_id, created_at) VALUES (?, ?, ?, ?, ?)",
		channelID, userID, body, threadID, createdAt,
	)
	if err != nil {
		return 0, fmt.Errorf("import message: %w", err)
	}

	msgID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}

	for _, uid := range mentionUserIDs {
		if _, err := tx.Exec(
			"INSERT OR IGNORE INTO message_mentions (message_id, user_id) VALUES (?, ?)",
			msgID, uid,
		); err != nil {
			return 0, fmt.Errorf("insert mention: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return msgID, nil
}
```

Add to `pkg/infra/sqlite/users.go`:

```go
// IsEmpty returns true if the database has no users.
func (s *Store) IsEmpty() (bool, error) {
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return false, fmt.Errorf("is empty: %w", err)
	}
	return count == 0, nil
}
```

**Step 4: Run the tests**

Run: `go test ./pkg/infra/sqlite/... -run "TestImportMessage|TestIsEmpty" -v`
Expected: PASS

**Step 5: Implement ImportMessage and IsEmpty for Postgres**

Add to `pkg/infra/postgres/messages.go` (uses `$N` placeholders and `RETURNING id`):

```go
// ImportMessage inserts a message with a specific created_at timestamp.
func (s *Store) ImportMessage(channelID, userID int64, body string, threadID *int64, mentionUserIDs []int64, createdAt time.Time) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var msgID int64
	err = tx.QueryRow(
		"INSERT INTO messages (channel_id, user_id, body, thread_id, created_at) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		channelID, userID, body, threadID, createdAt,
	).Scan(&msgID)
	if err != nil {
		return 0, fmt.Errorf("import message: %w", err)
	}

	for i, uid := range mentionUserIDs {
		if _, err := tx.Exec(
			fmt.Sprintf("INSERT INTO message_mentions (message_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING"),
			msgID, uid,
		); err != nil {
			return 0, fmt.Errorf("insert mention %d: %w", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return msgID, nil
}
```

Add to `pkg/infra/postgres/users.go`:

```go
// IsEmpty returns true if the database has no users.
func (s *Store) IsEmpty() (bool, error) {
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return false, fmt.Errorf("is empty: %w", err)
	}
	return count == 0, nil
}
```

**Step 6: Verify build**

Run: `go vet ./pkg/infra/...`
Expected: PASS

**Step 7: Commit**

```bash
git add pkg/infra/sqlite/messages.go pkg/infra/sqlite/users.go pkg/infra/postgres/messages.go pkg/infra/postgres/users.go pkg/infra/sqlite/store_test.go
git commit -m "feat: add ImportMessage and IsEmpty to both backends"
```

---

### Task 4: Export Function

**Files:**
- Create: `pkg/backup/export.go`
- Create: `pkg/backup/export_test.go`

**Context:** ExportData queries everything from the Store via the domain interfaces and assembles a `Backup` struct. Messages are assigned sequential `ID` values (1, 2, 3, ...) so thread references remain portable across backends. All channels are fetched via `ListAllChannelsWithMembership(0)` (non-DM) and `ListAllDMs()` (DM). Messages are paginated with `GetMessages` using the `after` cursor.

**Step 1: Write the test**

Create `pkg/backup/export_test.go`:

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package backup

import (
	"testing"
	"time"

	"github.com/Work-Fort/sharkfin/pkg/infra/sqlite"
)

func TestExportData(t *testing.T) {
	s, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	// Populate test data
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")
	_ = s.SetUserRole("alice", "admin")
	_ = s.SetUserType("bob", "agent")

	chID, _ := s.CreateChannel("general", true, []int64{aliceID, bobID}, "channel")
	_, _ = s.SendMessage(chID, aliceID, "hello", nil, []int64{bobID})
	_ = s.SetSetting("motd", "welcome")

	// Open a DM
	_, _, _ = s.OpenDM(aliceID, bobID, "bob")

	data, err := ExportData(s)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	if data.Version != 1 {
		t.Errorf("version = %d, want 1", data.Version)
	}
	if len(data.Users) != 2 {
		t.Errorf("users = %d, want 2", len(data.Users))
	}
	if len(data.Channels) != 1 {
		t.Errorf("channels = %d, want 1", len(data.Channels))
	}
	if len(data.Messages) != 1 {
		t.Errorf("messages = %d, want 1", len(data.Messages))
	}
	if data.Messages[0].ID != 1 {
		t.Errorf("message export ID = %d, want 1", data.Messages[0].ID)
	}
	if len(data.Messages[0].Mentions) != 1 || data.Messages[0].Mentions[0] != "bob" {
		t.Errorf("mentions = %v, want [bob]", data.Messages[0].Mentions)
	}
	if members, ok := data.ChannelMembers["general"]; !ok || len(members) != 2 {
		t.Errorf("channel members = %v", data.ChannelMembers)
	}
	if data.Settings["motd"] != "welcome" {
		t.Errorf("settings = %v", data.Settings)
	}
	if len(data.DMs) != 1 {
		t.Errorf("dms = %d, want 1", len(data.DMs))
	}
	if data.ExportedAt.Before(time.Now().Add(-1 * time.Minute)) {
		t.Errorf("exported_at too old: %v", data.ExportedAt)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/backup/... -run TestExportData -v`
Expected: FAIL — `ExportData undefined`

**Step 3: Implement ExportData**

Create `pkg/backup/export.go`:

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package backup

import (
	"fmt"
	"time"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

// ExportData reads all data from the store and returns a portable Backup struct.
func ExportData(s domain.Store) (*Backup, error) {
	b := &Backup{
		Version:        1,
		ExportedAt:     time.Now().UTC(),
		ChannelMembers: make(map[string][]string),
		RolePermissions: make(map[string][]string),
	}

	// Users
	users, err := s.ListUsers()
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	for _, u := range users {
		b.Users = append(b.Users, BackupUser{
			Username: u.Username,
			Password: u.Password,
			Role:     u.Role,
			Type:     u.Type,
		})
	}

	// Channels (non-DM) — pass userID=0 so LEFT JOIN matches no one; we just need metadata
	channels, err := s.ListAllChannelsWithMembership(0)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	for _, ch := range channels {
		b.Channels = append(b.Channels, BackupChannel{
			Name:   ch.Name,
			Public: ch.Public,
			Type:   ch.Type,
		})
		members, err := s.ChannelMemberUsernames(ch.ID)
		if err != nil {
			return nil, fmt.Errorf("channel members %q: %w", ch.Name, err)
		}
		b.ChannelMembers[ch.Name] = members
	}

	// DMs
	dms, err := s.ListAllDMs()
	if err != nil {
		return nil, fmt.Errorf("list dms: %w", err)
	}
	for _, dm := range dms {
		b.DMs = append(b.DMs, BackupDM{
			User1:       dm.User1Username,
			User2:       dm.User2Username,
			ChannelName: dm.ChannelName,
		})
		// Also collect DM members and messages
		b.ChannelMembers[dm.ChannelName] = []string{dm.User1Username, dm.User2Username}
	}

	// Messages — iterate all channels (regular + DM), paginate with after cursor
	exportID := 0
	dbIDToExportID := make(map[int64]int)

	allChannels := make([]struct{ id int64; name string }, 0)
	for _, ch := range channels {
		allChannels = append(allChannels, struct{ id int64; name string }{ch.ID, ch.Name})
	}
	for _, dm := range dms {
		allChannels = append(allChannels, struct{ id int64; name string }{dm.ChannelID, dm.ChannelName})
	}

	for _, ch := range allChannels {
		var afterID *int64
		for {
			msgs, err := s.GetMessages(ch.id, nil, afterID, 100, nil)
			if err != nil {
				return nil, fmt.Errorf("get messages %q: %w", ch.name, err)
			}
			if len(msgs) == 0 {
				break
			}
			for _, m := range msgs {
				exportID++
				dbIDToExportID[m.ID] = exportID

				var threadRef *int
				if m.ThreadID != nil {
					if ref, ok := dbIDToExportID[*m.ThreadID]; ok {
						threadRef = &ref
					}
				}

				b.Messages = append(b.Messages, BackupMessage{
					ID:        exportID,
					Channel:   ch.name,
					From:      m.From,
					Body:      m.Body,
					ThreadID:  threadRef,
					Mentions:  m.Mentions,
					CreatedAt: m.CreatedAt,
				})
			}
			last := msgs[len(msgs)-1].ID
			afterID = &last
		}
	}

	// Roles
	roles, err := s.ListRoles()
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	for _, r := range roles {
		b.Roles = append(b.Roles, BackupRole{
			Name:    r.Name,
			BuiltIn: r.BuiltIn,
		})
		perms, err := s.GetRolePermissions(r.Name)
		if err != nil {
			return nil, fmt.Errorf("role permissions %q: %w", r.Name, err)
		}
		b.RolePermissions[r.Name] = perms
	}

	// Settings
	settings, err := s.ListSettings()
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	b.Settings = settings

	return b, nil
}
```

**Step 4: Run the test**

Run: `go test ./pkg/backup/... -run TestExportData -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/backup/export.go pkg/backup/export_test.go
git commit -m "feat: add ExportData function for backup"
```

---

### Task 5: Import Function

**Files:**
- Create: `pkg/backup/import.go`
- Modify: `pkg/backup/export_test.go` (add roundtrip test)

**Context:** `ImportData` takes a `BackupStore` and a `Backup` struct, inserts everything in dependency order. Uses `BackupStore.ImportMessage` to preserve timestamps and `BackupStore.IsEmpty` for safety check. Thread IDs are resolved via the export-ID-to-real-ID map. Built-in roles already exist from migrations — only custom roles and non-default permissions are imported.

**Step 1: Write the roundtrip test**

Add to `pkg/backup/export_test.go`:

```go
func TestExportImportRoundtrip(t *testing.T) {
	// Export from source
	src, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open src: %v", err)
	}
	defer src.Close()

	aliceID, _ := src.CreateUser("alice", "secret")
	bobID, _ := src.CreateUser("bob", "")
	_ = src.SetUserRole("alice", "admin")
	_ = src.SetUserType("bob", "agent")

	chID, _ := src.CreateChannel("general", true, []int64{aliceID, bobID}, "channel")
	privID, _ := src.CreateChannel("private", false, []int64{aliceID}, "channel")
	parentID, _ := src.SendMessage(chID, aliceID, "hello world", nil, []int64{bobID})
	_, _ = src.SendMessage(chID, bobID, "reply", &parentID, nil)
	_, _ = src.SendMessage(privID, aliceID, "secret stuff", nil, nil)
	_ = src.SetSetting("motd", "welcome")
	_, _, _ = src.OpenDM(aliceID, bobID, "bob")
	_, _ = src.SendMessage(chID, aliceID, "dm test", nil, nil) // extra message

	data, err := ExportData(src)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Import to destination
	dst, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open dst: %v", err)
	}
	defer dst.Close()

	if err := ImportData(dst, data, false); err != nil {
		t.Fatalf("import: %v", err)
	}

	// Verify users
	users, _ := dst.ListUsers()
	if len(users) != 2 {
		t.Fatalf("users = %d, want 2", len(users))
	}

	alice, _ := dst.GetUserByUsername("alice")
	if alice.Role != "admin" {
		t.Errorf("alice role = %q, want admin", alice.Role)
	}
	bob, _ := dst.GetUserByUsername("bob")
	if bob.Type != "agent" {
		t.Errorf("bob type = %q, want agent", bob.Type)
	}

	// Verify channels
	ch, _ := dst.GetChannelByName("general")
	if ch == nil || !ch.Public {
		t.Error("general channel missing or not public")
	}
	members, _ := dst.ChannelMemberUsernames(ch.ID)
	if len(members) != 2 {
		t.Errorf("general members = %d, want 2", len(members))
	}

	priv, _ := dst.GetChannelByName("private")
	if priv == nil || priv.Public {
		t.Error("private channel missing or is public")
	}

	// Verify messages (general has 3: hello, reply, dm test; private has 1)
	msgs, _ := dst.GetMessages(ch.ID, nil, nil, 50, nil)
	if len(msgs) != 3 {
		t.Fatalf("general messages = %d, want 3", len(msgs))
	}
	if msgs[0].Body != "hello world" {
		t.Errorf("msg[0] body = %q", msgs[0].Body)
	}
	if msgs[1].ThreadID == nil {
		t.Error("msg[1] should be a thread reply")
	}
	if len(msgs[0].Mentions) != 1 || msgs[0].Mentions[0] != "bob" {
		t.Errorf("msg[0] mentions = %v, want [bob]", msgs[0].Mentions)
	}

	// Verify settings
	motd, _ := dst.GetSetting("motd")
	if motd != "welcome" {
		t.Errorf("motd = %q, want welcome", motd)
	}

	// Verify DMs
	allDMs, _ := dst.ListAllDMs()
	if len(allDMs) != 1 {
		t.Errorf("dms = %d, want 1", len(allDMs))
	}
}

func TestImportNonEmptyRejectsWithoutForce(t *testing.T) {
	s, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	s.CreateUser("existing", "")

	data := &Backup{Version: 1, Users: []BackupUser{{Username: "alice"}}}
	err = ImportData(s, data, false)
	if err == nil {
		t.Fatal("expected error for non-empty store without force")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/backup/... -run "TestExportImportRoundtrip|TestImportNonEmpty" -v`
Expected: FAIL — `ImportData undefined`

**Step 3: Implement ImportData**

Create `pkg/backup/import.go`:

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package backup

import (
	"fmt"
)

// ImportData restores a Backup into the given store.
// If force is false and the store is not empty, returns an error.
func ImportData(s BackupStore, data *Backup, force bool) error {
	if data.Version != 1 {
		return fmt.Errorf("unsupported backup version: %d", data.Version)
	}

	empty, err := s.IsEmpty()
	if err != nil {
		return fmt.Errorf("check empty: %w", err)
	}
	if !empty && !force {
		return fmt.Errorf("database is not empty; use --force to overwrite")
	}

	// 1. Users
	userIDs := make(map[string]int64) // username → new ID
	for _, u := range data.Users {
		id, err := s.CreateUser(u.Username, u.Password)
		if err != nil {
			return fmt.Errorf("create user %q: %w", u.Username, err)
		}
		userIDs[u.Username] = id
		if u.Role != "" && u.Role != "user" {
			if err := s.SetUserRole(u.Username, u.Role); err != nil {
				return fmt.Errorf("set role for %q: %w", u.Username, err)
			}
		}
		if u.Type != "" && u.Type != "user" {
			if err := s.SetUserType(u.Username, u.Type); err != nil {
				return fmt.Errorf("set type for %q: %w", u.Username, err)
			}
		}
	}

	// 2. Custom roles (non-built-in)
	for _, r := range data.Roles {
		if r.BuiltIn {
			continue
		}
		if err := s.CreateRole(r.Name); err != nil {
			return fmt.Errorf("create role %q: %w", r.Name, err)
		}
	}

	// 3. Role permissions — grant any that differ from migration defaults
	for role, perms := range data.RolePermissions {
		existing, _ := s.GetRolePermissions(role)
		existingSet := make(map[string]bool)
		for _, p := range existing {
			existingSet[p] = true
		}
		for _, p := range perms {
			if !existingSet[p] {
				if err := s.GrantPermission(role, p); err != nil {
					return fmt.Errorf("grant %q to %q: %w", p, role, err)
				}
			}
		}
	}

	// 4. Channels (non-DM)
	channelIDs := make(map[string]int64) // channel name → new ID
	for _, ch := range data.Channels {
		memberNames := data.ChannelMembers[ch.Name]
		var memberIDs []int64
		for _, name := range memberNames {
			if uid, ok := userIDs[name]; ok {
				memberIDs = append(memberIDs, uid)
			}
		}
		id, err := s.CreateChannel(ch.Name, ch.Public, memberIDs, ch.Type)
		if err != nil {
			return fmt.Errorf("create channel %q: %w", ch.Name, err)
		}
		channelIDs[ch.Name] = id
	}

	// 5. DMs
	for _, dm := range data.DMs {
		uid1, ok1 := userIDs[dm.User1]
		uid2, ok2 := userIDs[dm.User2]
		if !ok1 || !ok2 {
			return fmt.Errorf("DM references unknown users: %q, %q", dm.User1, dm.User2)
		}
		name, _, err := s.OpenDM(uid1, uid2, dm.User2)
		if err != nil {
			return fmt.Errorf("open DM %q-%q: %w", dm.User1, dm.User2, err)
		}
		ch, err := s.GetChannelByName(name)
		if err != nil {
			return fmt.Errorf("get DM channel %q: %w", name, err)
		}
		channelIDs[name] = ch.ID
	}

	// 6. Messages — in order, mapping export IDs to real IDs for threads
	exportIDToRealID := make(map[int]int64)
	for _, m := range data.Messages {
		chID, ok := channelIDs[m.Channel]
		if !ok {
			return fmt.Errorf("message references unknown channel %q", m.Channel)
		}
		uid, ok := userIDs[m.From]
		if !ok {
			return fmt.Errorf("message references unknown user %q", m.From)
		}

		var threadID *int64
		if m.ThreadID != nil {
			if realID, ok := exportIDToRealID[*m.ThreadID]; ok {
				threadID = &realID
			}
		}

		var mentionIDs []int64
		for _, name := range m.Mentions {
			if uid, ok := userIDs[name]; ok {
				mentionIDs = append(mentionIDs, uid)
			}
		}

		realID, err := s.ImportMessage(chID, uid, m.Body, threadID, mentionIDs, m.CreatedAt)
		if err != nil {
			return fmt.Errorf("import message %d: %w", m.ID, err)
		}
		exportIDToRealID[m.ID] = realID
	}

	// 7. Settings
	for k, v := range data.Settings {
		if err := s.SetSetting(k, v); err != nil {
			return fmt.Errorf("set setting %q: %w", k, err)
		}
	}

	return nil
}
```

**Step 4: Run the tests**

Run: `go test ./pkg/backup/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/backup/import.go pkg/backup/export_test.go
git commit -m "feat: add ImportData function with roundtrip test"
```

---

### Task 6: Archive Pipeline (tar + xz + age)

**Files:**
- Create: `pkg/backup/archive.go`
- Create: `pkg/backup/archive_test.go`

**Context:** `Pack` takes a map of filename→content, creates a tar archive, compresses with xz, encrypts with age passphrase. `Unpack` reverses the process. These are streaming pipelines — data flows through tar → xz → age writer, and age reader → xz reader → tar reader.

**Step 1: Write the test**

Create `pkg/backup/archive_test.go`:

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package backup

import (
	"bytes"
	"testing"
)

func TestPackUnpackRoundtrip(t *testing.T) {
	files := map[string][]byte{
		"config.yaml": []byte("daemon: 127.0.0.1:16000\n"),
		"data.json":   []byte(`{"version":1,"users":[]}`),
	}
	passphrase := "test-passphrase-123"

	packed, err := Pack(files, passphrase)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if len(packed) == 0 {
		t.Fatal("packed output is empty")
	}

	unpacked, err := Unpack(packed, passphrase)
	if err != nil {
		t.Fatalf("unpack: %v", err)
	}

	for name, want := range files {
		got, ok := unpacked[name]
		if !ok {
			t.Errorf("missing file %q in unpacked output", name)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("file %q: got %q, want %q", name, got, want)
		}
	}
}

func TestUnpackWrongPassphrase(t *testing.T) {
	files := map[string][]byte{"test.txt": []byte("hello")}
	packed, err := Pack(files, "correct")
	if err != nil {
		t.Fatalf("pack: %v", err)
	}

	_, err = Unpack(packed, "wrong")
	if err == nil {
		t.Fatal("expected error for wrong passphrase")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/backup/... -run TestPack -v`
Expected: FAIL — `Pack undefined`

**Step 3: Implement Pack and Unpack**

Create `pkg/backup/archive.go`:

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package backup

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"time"

	"filippo.io/age"
	"github.com/ulikunitz/xz"
)

// Pack creates a tar.xz.age archive from a map of filename → content.
func Pack(files map[string][]byte, passphrase string) ([]byte, error) {
	// tar → xz → age → output buffer
	var out bytes.Buffer

	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("age recipient: %w", err)
	}

	ageWriter, err := age.Encrypt(&out, recipient)
	if err != nil {
		return nil, fmt.Errorf("age encrypt: %w", err)
	}

	xzWriter, err := xz.NewWriter(ageWriter)
	if err != nil {
		return nil, fmt.Errorf("xz writer: %w", err)
	}

	tw := tar.NewWriter(xzWriter)
	for name, data := range files {
		hdr := &tar.Header{
			Name:    name,
			Size:    int64(len(data)),
			Mode:    0644,
			ModTime: time.Now().UTC(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("tar header %q: %w", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			return nil, fmt.Errorf("tar write %q: %w", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("tar close: %w", err)
	}
	if err := xzWriter.Close(); err != nil {
		return nil, fmt.Errorf("xz close: %w", err)
	}
	if err := ageWriter.Close(); err != nil {
		return nil, fmt.Errorf("age close: %w", err)
	}

	return out.Bytes(), nil
}

// Unpack decrypts and decompresses a tar.xz.age archive, returning filename → content.
func Unpack(data []byte, passphrase string) (map[string][]byte, error) {
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("age identity: %w", err)
	}

	ageReader, err := age.Decrypt(bytes.NewReader(data), identity)
	if err != nil {
		return nil, fmt.Errorf("age decrypt: %w", err)
	}

	xzReader, err := xz.NewReader(ageReader)
	if err != nil {
		return nil, fmt.Errorf("xz reader: %w", err)
	}

	tr := tar.NewReader(xzReader)
	files := make(map[string][]byte)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar next: %w", err)
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("tar read %q: %w", hdr.Name, err)
		}
		files[hdr.Name] = content
	}

	return files, nil
}
```

**Step 4: Run the tests**

Run: `go test ./pkg/backup/... -run "TestPack|TestUnpack" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/backup/archive.go pkg/backup/archive_test.go
git commit -m "feat: add archive pipeline (tar + xz + age)"
```

---

### Task 7: S3 Client

**Files:**
- Create: `pkg/backup/s3.go`

**Context:** Thin wrapper around AWS SDK v2 for upload, download, and list operations. Uses explicit credentials from config (not the default chain). Supports custom endpoint for MinIO/R2/Cloudflare. No unit tests — tested via E2E with a real S3-compatible bucket.

**Step 1: Implement the S3 client**

Create `pkg/backup/s3.go`:

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Config holds the configuration for S3 operations.
type S3Config struct {
	Bucket    string
	Region    string
	Endpoint  string // optional, for MinIO/R2/etc.
	AccessKey string
	SecretKey string
}

// Validate returns an error if required fields are missing.
func (c *S3Config) Validate() error {
	if c.Bucket == "" {
		return fmt.Errorf("backup.s3-bucket is required")
	}
	if c.Region == "" {
		return fmt.Errorf("backup.s3-region is required")
	}
	if c.AccessKey == "" {
		return fmt.Errorf("backup.s3-access-key is required")
	}
	if c.SecretKey == "" {
		return fmt.Errorf("backup.s3-secret-key is required")
	}
	return nil
}

func (c *S3Config) client(ctx context.Context) (*s3.Client, error) {
	creds := credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, "")
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(c.Region),
		awsconfig.WithCredentialsProvider(creds),
	)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}

	opts := func(o *s3.Options) {}
	if c.Endpoint != "" {
		opts = func(o *s3.Options) {
			o.BaseEndpoint = aws.String(c.Endpoint)
			o.UsePathStyle = true
		}
	}
	return s3.NewFromConfig(cfg, opts), nil
}

// Upload puts an object into S3.
func (c *S3Config) Upload(ctx context.Context, key string, data []byte) error {
	client, err := c.client(ctx)
	if err != nil {
		return err
	}

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &c.Bucket,
		Key:    &key,
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("s3 upload %q: %w", key, err)
	}
	return nil
}

// Download gets an object from S3.
func (c *S3Config) Download(ctx context.Context, key string) ([]byte, error) {
	client, err := c.client(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &c.Bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 download %q: %w", key, err)
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// ObjectInfo describes a backup object in S3.
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// List returns all objects with the sharkfin-backup- prefix.
func (c *S3Config) List(ctx context.Context) ([]ObjectInfo, error) {
	client, err := c.client(ctx)
	if err != nil {
		return nil, err
	}

	prefix := "sharkfin-backup-"
	resp, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: &c.Bucket,
		Prefix: &prefix,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 list: %w", err)
	}

	var objects []ObjectInfo
	for _, obj := range resp.Contents {
		objects = append(objects, ObjectInfo{
			Key:          *obj.Key,
			Size:         *obj.Size,
			LastModified: *obj.LastModified,
		})
	}
	return objects, nil
}
```

**Step 2: Verify build**

Run: `go vet ./pkg/backup/...`
Expected: PASS

**Step 3: Commit**

```bash
git add pkg/backup/s3.go
git commit -m "feat: add S3 client for backup upload/download/list"
```

---

### Task 8: Config Defaults and CLI Commands

**Files:**
- Modify: `pkg/config/config.go`
- Create: `cmd/backup/backup.go`
- Modify: `cmd/root.go`

**Context:** Add Viper defaults for backup S3 config. Create the `backup` command with `export`, `import`, and `list` subcommands. Wire into root. The passphrase is read from `--passphrase` flag, `SHARKFIN_BACKUP_PASSPHRASE` env var, or interactive prompt (in that order). The `--force` flag on import allows overwriting a non-empty database. The `--restore-config` flag on import writes the backed-up config.yaml to disk.

**Step 1: Add Viper defaults**

Add to `pkg/config/config.go` inside `InitViper()`, after the existing defaults:

```go
	viper.SetDefault("backup.s3-bucket", "")
	viper.SetDefault("backup.s3-region", "")
	viper.SetDefault("backup.s3-endpoint", "")
	viper.SetDefault("backup.s3-access-key", "")
	viper.SetDefault("backup.s3-secret-key", "")
```

**Step 2: Create the backup command**

Create `cmd/backup/backup.go`:

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	bk "github.com/Work-Fort/sharkfin/pkg/backup"
	"github.com/Work-Fort/sharkfin/pkg/config"
	"github.com/Work-Fort/sharkfin/pkg/infra"
)

func s3cfg() *bk.S3Config {
	return &bk.S3Config{
		Bucket:    viper.GetString("backup.s3-bucket"),
		Region:    viper.GetString("backup.s3-region"),
		Endpoint:  viper.GetString("backup.s3-endpoint"),
		AccessKey: viper.GetString("backup.s3-access-key"),
		SecretKey: viper.GetString("backup.s3-secret-key"),
	}
}

func getPassphrase(cmd *cobra.Command) (string, error) {
	p, _ := cmd.Flags().GetString("passphrase")
	if p != "" {
		return p, nil
	}
	p = os.Getenv("SHARKFIN_BACKUP_PASSPHRASE")
	if p != "" {
		return p, nil
	}
	return "", fmt.Errorf("passphrase required: use --passphrase flag or SHARKFIN_BACKUP_PASSPHRASE env var")
}

func openDSN() string {
	dsn := viper.GetString("db")
	if dsn == "" {
		dsn = filepath.Join(config.GlobalPaths.StateDir, "sharkfin.db")
	}
	return dsn
}

// NewBackupCmd creates the backup subcommand with export, import, and list.
func NewBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Export, import, and list encrypted S3 backups",
	}

	cmd.PersistentFlags().String("db", "", "Database DSN (postgres://... or path to SQLite file)")
	cmd.PersistentFlags().String("passphrase", "", "Encryption passphrase (or set SHARKFIN_BACKUP_PASSPHRASE)")
	cmd.PersistentFlags().String("s3-bucket", "", "S3 bucket name")
	cmd.PersistentFlags().String("s3-region", "", "S3 region")
	cmd.PersistentFlags().String("s3-endpoint", "", "S3 endpoint (for MinIO/R2/etc.)")
	cmd.PersistentFlags().String("s3-access-key", "", "S3 access key")
	cmd.PersistentFlags().String("s3-secret-key", "", "S3 secret key")

	_ = viper.BindPFlag("db", cmd.PersistentFlags().Lookup("db"))
	_ = viper.BindPFlag("backup.s3-bucket", cmd.PersistentFlags().Lookup("s3-bucket"))
	_ = viper.BindPFlag("backup.s3-region", cmd.PersistentFlags().Lookup("s3-region"))
	_ = viper.BindPFlag("backup.s3-endpoint", cmd.PersistentFlags().Lookup("s3-endpoint"))
	_ = viper.BindPFlag("backup.s3-access-key", cmd.PersistentFlags().Lookup("s3-access-key"))
	_ = viper.BindPFlag("backup.s3-secret-key", cmd.PersistentFlags().Lookup("s3-secret-key"))

	cmd.AddCommand(
		newExportCmd(),
		newImportCmd(),
		newListCmd(),
	)

	return cmd
}

func newExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export",
		Short: "Export config and database to an encrypted S3 backup",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			passphrase, err := getPassphrase(cmd)
			if err != nil {
				return err
			}

			cfg := s3cfg()
			if err := cfg.Validate(); err != nil {
				return err
			}

			dsn := openDSN()
			store, err := infra.Open(dsn)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer store.Close()

			// Export data
			data, err := bk.ExportData(store)
			if err != nil {
				return fmt.Errorf("export data: %w", err)
			}

			dataJSON, err := json.MarshalIndent(data, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal json: %w", err)
			}

			// Read config.yaml
			configPath := filepath.Join(config.GlobalPaths.ConfigDir, "config.yaml")
			configData, err := os.ReadFile(configPath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("read config: %w", err)
			}
			if configData == nil {
				configData = []byte{}
			}

			// Pack
			files := map[string][]byte{
				"data.json":   dataJSON,
				"config.yaml": configData,
			}
			packed, err := bk.Pack(files, passphrase)
			if err != nil {
				return fmt.Errorf("pack: %w", err)
			}

			// Upload
			key := fmt.Sprintf("sharkfin-backup-%s.tar.xz.age",
				time.Now().UTC().Format(time.RFC3339))
			ctx := context.Background()
			if err := cfg.Upload(ctx, key, packed); err != nil {
				return fmt.Errorf("upload: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Uploaded: %s (%s)\n", key, humanSize(int64(len(packed))))
			return nil
		},
	}
}

func newImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import <key>",
		Short: "Download and restore an encrypted S3 backup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			passphrase, err := getPassphrase(cmd)
			if err != nil {
				return err
			}
			force, _ := cmd.Flags().GetBool("force")
			restoreConfig, _ := cmd.Flags().GetBool("restore-config")

			cfg := s3cfg()
			if err := cfg.Validate(); err != nil {
				return err
			}

			// Download
			ctx := context.Background()
			packed, err := cfg.Download(ctx, key)
			if err != nil {
				return fmt.Errorf("download: %w", err)
			}

			// Unpack
			files, err := bk.Unpack(packed, passphrase)
			if err != nil {
				return fmt.Errorf("unpack: %w", err)
			}

			// Parse data.json
			dataJSON, ok := files["data.json"]
			if !ok {
				return fmt.Errorf("backup archive missing data.json")
			}
			var data bk.Backup
			if err := json.Unmarshal(dataJSON, &data); err != nil {
				return fmt.Errorf("parse data.json: %w", err)
			}

			// Open store
			dsn := openDSN()
			store, err := infra.Open(dsn)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer store.Close()

			bs, ok := store.(bk.BackupStore)
			if !ok {
				return fmt.Errorf("store does not support backup import")
			}

			// Import
			if err := bk.ImportData(bs, &data, force); err != nil {
				return fmt.Errorf("import: %w", err)
			}

			// Optionally restore config
			if restoreConfig {
				if configData, ok := files["config.yaml"]; ok && len(configData) > 0 {
					configPath := filepath.Join(config.GlobalPaths.ConfigDir, "config.yaml")
					if err := os.WriteFile(configPath, configData, 0644); err != nil {
						return fmt.Errorf("write config: %w", err)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "Restored config.yaml\n")
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Import complete: %d users, %d channels, %d messages\n",
				len(data.Users), len(data.Channels)+len(data.DMs), len(data.Messages))
			return nil
		},
	}

	cmd.Flags().Bool("force", false, "Overwrite non-empty database")
	cmd.Flags().Bool("restore-config", false, "Restore config.yaml from backup")

	return cmd
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List backups in the S3 bucket",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := s3cfg()
			if err := cfg.Validate(); err != nil {
				return err
			}

			ctx := context.Background()
			objects, err := cfg.List(ctx)
			if err != nil {
				return fmt.Errorf("list: %w", err)
			}

			if len(objects) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No backups found.")
				return nil
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%-60s  %10s  %s\n", "KEY", "SIZE", "MODIFIED")
			for _, obj := range objects {
				fmt.Fprintf(out, "%-60s  %10s  %s\n",
					obj.Key, humanSize(obj.Size), obj.LastModified.Format(time.RFC3339))
			}
			return nil
		},
	}
}

func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
```

**Step 3: Wire into root.go**

Add import for `"github.com/Work-Fort/sharkfin/cmd/backup"` and add `rootCmd.AddCommand(backup.NewBackupCmd())` in `init()`.

**Step 4: Verify build**

Run: `mise run build`
Expected: PASS

**Step 5: Verify help output**

Run: `./build/sharkfin backup --help`
Expected: Shows export, import, list subcommands

Run: `./build/sharkfin backup export --help`
Expected: Shows --passphrase, --s3-bucket, etc.

**Step 6: Commit**

```bash
git add pkg/config/config.go cmd/backup/backup.go cmd/root.go
git commit -m "feat: add sharkfin backup CLI commands (export, import, list)"
```

---

### Task 9: Unit Tests — Full CI Pass

**Files:**
- Modify: various (fix any issues)

**Step 1: Run full CI**

Run: `mise run ci`
Expected: All lint, unit tests, and e2e tests pass

If any failures occur, fix them before proceeding.

**Step 2: Commit any fixes**

```bash
git commit -m "fix: resolve CI issues from backup feature"
```

---

### Task 10: E2E Test

**Files:**
- Modify: `tests/e2e/sharkfin_test.go`

**Context:** This tests the full export→import roundtrip against a real daemon process. It uses a local MinIO instance or skips if `SHARKFIN_BACKUP_TEST_BUCKET` is not set. The test:
1. Starts daemon A, creates users/channels/messages via MCP
2. Runs `sharkfin backup export` against daemon A's DB
3. Starts daemon B with a fresh DB
4. Runs `sharkfin backup import` against daemon B's DB
5. Verifies data via MCP queries against daemon B

**Note:** This test requires S3 credentials set via env vars. It is skipped in CI unless credentials are available:
- `SHARKFIN_BACKUP_TEST_BUCKET`
- `SHARKFIN_BACKUP_TEST_REGION`
- `SHARKFIN_BACKUP_TEST_ENDPOINT` (optional)
- `SHARKFIN_BACKUP_TEST_ACCESS_KEY`
- `SHARKFIN_BACKUP_TEST_SECRET_KEY`
- `SHARKFIN_BACKUP_TEST_PASSPHRASE`

**Step 1: Add the E2E test**

Add to `tests/e2e/sharkfin_test.go`:

```go
func TestBackupExportImport(t *testing.T) {
	bucket := os.Getenv("SHARKFIN_BACKUP_TEST_BUCKET")
	if bucket == "" {
		t.Skip("SHARKFIN_BACKUP_TEST_BUCKET not set, skipping backup e2e")
	}
	region := os.Getenv("SHARKFIN_BACKUP_TEST_REGION")
	endpoint := os.Getenv("SHARKFIN_BACKUP_TEST_ENDPOINT")
	accessKey := os.Getenv("SHARKFIN_BACKUP_TEST_ACCESS_KEY")
	secretKey := os.Getenv("SHARKFIN_BACKUP_TEST_SECRET_KEY")
	passphrase := os.Getenv("SHARKFIN_BACKUP_TEST_PASSPHRASE")
	if passphrase == "" {
		passphrase = "test-passphrase"
	}

	// Start daemon A and populate data
	dA := harness.StartDaemon(t)
	cA := harness.NewClient(t, dA.Addr)
	cA.Register("alice")
	cA.Register("bob")
	cA.GrantAdmin(t, dA, "alice")
	cA.Identify("alice")
	cA.CreateChannel("general", true)
	cA.SendMessage("general", "hello from alice")

	cB := harness.NewClient(t, dA.Addr)
	cB.Identify("bob")
	cB.JoinChannel("general")
	cB.SendMessage("general", "hello from bob")
	dA.Stop()

	// Export
	backupArgs := []string{
		"backup", "export",
		"--db", dA.DBPath(),
		"--passphrase", passphrase,
		"--s3-bucket", bucket,
		"--s3-region", region,
		"--s3-access-key", accessKey,
		"--s3-secret-key", secretKey,
	}
	if endpoint != "" {
		backupArgs = append(backupArgs, "--s3-endpoint", endpoint)
	}
	out := harness.RunCLI(t, backupArgs...)
	// Parse the key from output like "Uploaded: sharkfin-backup-...tar.xz.age (1.2 KB)"
	key := harness.ParseUploadedKey(t, out)

	// Start daemon B with fresh DB, import
	dB := harness.StartDaemon(t)
	importArgs := []string{
		"backup", "import", key,
		"--db", dB.DBPath(),
		"--passphrase", passphrase,
		"--s3-bucket", bucket,
		"--s3-region", region,
		"--s3-access-key", accessKey,
		"--s3-secret-key", secretKey,
	}
	if endpoint != "" {
		importArgs = append(importArgs, "--s3-endpoint", endpoint)
	}
	harness.RunCLI(t, importArgs...)

	// Verify via MCP
	cC := harness.NewClient(t, dB.Addr)
	cC.Identify("alice")

	users := cC.UserList()
	if len(users) < 2 {
		t.Errorf("expected >=2 users, got %d", len(users))
	}

	msgs := cC.History("general", 50)
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}

	dB.Stop()
}
```

**Step 2: Add harness helpers if needed**

Add `RunCLI`, `ParseUploadedKey`, and `DBPath` to `tests/e2e/harness/harness.go` if they don't exist. `RunCLI` runs the sharkfin binary with the given args and returns stdout. `DBPath` returns the daemon's database file path. `ParseUploadedKey` extracts the S3 key from the export output.

**Step 3: Run the test (if S3 is available)**

Run: `SHARKFIN_BACKUP_TEST_BUCKET=... go test ./tests/e2e/... -run TestBackupExportImport -v -timeout 120s`
Expected: PASS

Or if no S3 bucket is available, verify the test skips:

Run: `go test ./tests/e2e/... -run TestBackupExportImport -v`
Expected: SKIP with "SHARKFIN_BACKUP_TEST_BUCKET not set"

**Step 4: Verify full CI still passes**

Run: `mise run ci`
Expected: PASS (backup e2e test skipped without S3 env vars)

**Step 5: Commit**

```bash
git add tests/e2e/sharkfin_test.go tests/e2e/harness/harness.go
git commit -m "test: add e2e test for backup export/import roundtrip"
```
