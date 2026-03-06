# Database Abstraction Layer Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refactor the monolithic `pkg/db` package into a hexagonal architecture with `pkg/domain` interfaces and separate `pkg/infra/sqlite` and `pkg/infra/postgres` backends using sqlc.

**Architecture:** Domain interfaces (ports) in `pkg/domain/` define the contract. Each backend (`pkg/infra/sqlite/`, `pkg/infra/postgres/`) implements those interfaces using sqlc-generated query code and goose migrations. A factory in `pkg/infra/open.go` auto-detects the backend from the DSN. The daemon layer consumes domain interfaces via dependency injection.

**Tech Stack:** Go, sqlc, goose v3, modernc.org/sqlite, jackc/pgx/v5, domain-driven hexagonal architecture

---

### Task 1: Create Domain Types and Interfaces

**Files:**
- Create: `pkg/domain/types.go`
- Create: `pkg/domain/ports.go`

**Context:** These are the shared types and store interfaces that both backends implement. Types are extracted from the existing `pkg/db/*.go` structs. Interfaces group the 36 existing DB methods into 5 granular stores.

**Step 1: Create `pkg/domain/types.go`**

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package domain

import "time"

type User struct {
	ID        int64
	Username  string
	Password  string
	Role      string
	Type      string
	CreatedAt time.Time
}

type Channel struct {
	ID        int64
	Name      string
	Public    bool
	Type      string // "channel" or "dm"
	CreatedAt time.Time
}

type ChannelWithMembership struct {
	Channel
	Member bool
}

type Message struct {
	ID        int64
	ChannelID int64
	UserID    int64
	From      string // username
	Body      string
	ThreadID  *int64
	Mentions  []string
	CreatedAt time.Time
}

type UnreadCount struct {
	ChannelID    int64
	Channel      string
	UnreadCount  int
	MentionCount int
	Type         string
}

type DMInfo struct {
	ChannelID     int64
	ChannelName   string
	OtherUserID   int64
	OtherUsername string
}

type AllDMInfo struct {
	ChannelID     int64
	ChannelName   string
	User1ID       int64
	User1Username string
	User2ID       int64
	User2Username string
}

type Role struct {
	Name    string
	BuiltIn bool
}
```

**Step 2: Create `pkg/domain/ports.go`**

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package domain

type UserStore interface {
	CreateUser(username, password string) (int64, error)
	GetUserByUsername(username string) (*User, error)
	ListUsers() ([]User, error)
}

type ChannelStore interface {
	CreateChannel(name string, public bool, memberIDs []int64, channelType string) (int64, error)
	GetChannelByID(id int64) (*Channel, error)
	GetChannelByName(name string) (*Channel, error)
	ListChannelsForUser(userID int64) ([]ChannelWithMembership, error)
	ListAllChannelsWithMembership(userID int64) ([]ChannelWithMembership, error)
	AddChannelMember(channelID, userID int64) error
	ChannelMemberUsernames(channelID int64) ([]string, error)
	IsChannelMember(channelID, userID int64) (bool, error)
	ListDMsForUser(userID int64) ([]DMInfo, error)
	ListAllDMs() ([]AllDMInfo, error)
	OpenDM(userID, otherUserID int64, otherUsername string) (string, bool, error)
}

type MessageStore interface {
	SendMessage(channelID, userID int64, body string, threadID *int64, mentionUserIDs []int64) (int64, error)
	GetMessages(channelID int64, before *int64, after *int64, limit int, threadID *int64) ([]Message, error)
	GetUnreadMessages(userID int64, channelID *int64, mentionsOnly bool, threadID *int64) ([]Message, error)
	GetUnreadCounts(userID int64) ([]UnreadCount, error)
	MarkRead(userID, channelID int64, messageID *int64) error
}

type RoleStore interface {
	CreateRole(name string) error
	DeleteRole(name string) error
	ListRoles() ([]Role, error)
	GrantPermission(role, permission string) error
	RevokePermission(role, permission string) error
	GetRolePermissions(role string) ([]string, error)
	GetUserPermissions(username string) ([]string, error)
	HasPermission(username, permission string) (bool, error)
	SetUserRole(username, role string) error
	SetUserType(username, userType string) error
}

type SettingsStore interface {
	GetSetting(key string) (string, error)
	SetSetting(key, value string) error
	ListSettings() (map[string]string, error)
}

// Store is the composite interface for convenient wiring.
type Store interface {
	UserStore
	ChannelStore
	MessageStore
	RoleStore
	SettingsStore
	Close() error
}
```

**Step 3: Verify it compiles**

Run: `go build ./pkg/domain/...`
Expected: PASS (no errors — pure types and interfaces)

**Step 4: Commit**

```bash
git add pkg/domain/
git commit -m "feat: add domain types and store interfaces for hexagonal architecture"
```

---

### Task 2: Install sqlc and Create SQLite Backend Scaffolding

**Files:**
- Create: `pkg/infra/sqlite/sqlc.yaml`
- Create: `pkg/infra/sqlite/queries/users.sql`
- Create: `pkg/infra/sqlite/queries/channels.sql`
- Create: `pkg/infra/sqlite/queries/messages.sql`
- Create: `pkg/infra/sqlite/queries/roles.sql`
- Create: `pkg/infra/sqlite/queries/settings.sql`
- Copy: `pkg/db/migrations/*.sql` → `pkg/infra/sqlite/migrations/`
- Create: `pkg/infra/sqlite/store.go`

**Context:** The SQLite backend uses sqlc for type-safe query generation and goose for migrations. The existing migration files are moved as-is. The queries are the same SQL currently hand-written in `pkg/db/*.go`, but extracted into `.sql` files for sqlc.

**Step 1: Install sqlc**

Run: `go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`

Verify: `sqlc version`

**Step 2: Copy migrations**

```bash
mkdir -p pkg/infra/sqlite/migrations
cp pkg/db/migrations/*.sql pkg/infra/sqlite/migrations/
```

**Step 3: Create `pkg/infra/sqlite/sqlc.yaml`**

```yaml
version: "2"
sql:
  - engine: "sqlite"
    queries: "queries"
    schema: "migrations"
    gen:
      go:
        package: "sqlite"
        out: "."
        emit_exact_table_names: true
```

**Step 4: Write sqlc query files**

Write all query `.sql` files in `pkg/infra/sqlite/queries/`. Each query has a `-- name:` annotation for sqlc. Translate every hand-written query from the current `pkg/db/*.go` files into annotated SQL.

Key patterns to preserve:
- `INSERT OR IGNORE` for mentions and permissions
- `ON CONFLICT(...) DO UPDATE` for settings and read cursors
- `?` placeholders (SQLite native)
- `COALESCE` for null handling in unread queries
- `MAX()` in conflict clause for forward-only cursors

**Important:** Some queries are too dynamic for sqlc (e.g., `GetMessages` has 3 different query shapes based on before/after/neither; `loadMentions` builds `IN (?, ?, ...)` dynamically; `GetUnreadMessages` has complex conditional logic). These must be hand-written in Go using the `DBTX` interface from sqlc's generated `db.go`. The store will use sqlc-generated code where possible and raw `database/sql` for the rest.

Queries that CAN be sqlc-generated:
- All user queries (simple CRUD)
- `GetChannelByID`, `GetChannelByName`, `AddChannelMember`, `ChannelMemberUsernames`, `IsChannelMember`
- `CreateRole`, `DeleteRole`, `ListRoles`, `GrantPermission`, `RevokePermission`, `GetRolePermissions`, `GetUserPermissions`, `HasPermission`, `SetUserRole`, `SetUserType`
- `GetSetting`, `SetSetting`, `ListSettings`

Queries that need hand-written Go (too dynamic for sqlc):
- `CreateChannel` (transaction: insert channel + N members)
- `ListChannelsForUser`, `ListAllChannelsWithMembership` (complex joins with membership subquery)
- `ListDMsForUser`, `ListAllDMs`, `OpenDM` (complex DM logic)
- `SendMessage` (transaction: insert message + N mentions + thread validation)
- `GetMessages` (3 query variants based on cursor position)
- `GetUnreadMessages` (conditional filters, transaction with cursor advance)
- `GetUnreadCounts` (complex aggregation)
- `MarkRead` (ON CONFLICT with MAX)
- `loadMentions` (dynamic IN clause)

**Step 5: Run sqlc generate**

Run: `cd pkg/infra/sqlite && sqlc generate`
Expected: Generates `db.go`, `models.go`, `queries/*.sql.go`

**Step 6: Create `pkg/infra/sqlite/store.go`**

This is the main file that:
- Opens the SQLite connection with pragmas
- Runs goose migrations
- Implements all `domain.Store` methods
- Uses sqlc-generated code where possible, hand-written SQL for complex queries
- Embeds migrations via `//go:embed`

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

type Store struct {
	db *sql.DB
	q  *Queries // sqlc-generated
}

func Open(path string) (*Store, error) {
	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	sqldb.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA wal_autocheckpoint = 1000",
	} {
		if _, err := sqldb.Exec(pragma); err != nil {
			sqldb.Close()
			return nil, fmt.Errorf("exec %s: %w", pragma, err)
		}
	}

	if err := runMigrations(sqldb); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &Store{db: sqldb, q: New(sqldb)}, nil
}

func runMigrations(db *sql.DB) error {
	fsys, err := fs.Sub(embedMigrations, "migrations")
	if err != nil {
		return fmt.Errorf("migrations fs: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, fsys)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Compile-time interface check.
var _ domain.Store = (*Store)(nil)
```

**Step 7: Implement all store methods**

Port every method from `pkg/db/*.go` into `Store` methods in `pkg/infra/sqlite/`. Use sqlc-generated queries where possible; hand-write the rest using `s.db` directly. The SQL stays identical — only the wiring changes.

Split across files mirroring the domain interfaces:
- `pkg/infra/sqlite/users.go` — UserStore methods
- `pkg/infra/sqlite/channels.go` — ChannelStore methods
- `pkg/infra/sqlite/messages.go` — MessageStore methods
- `pkg/infra/sqlite/roles.go` — RoleStore methods
- `pkg/infra/sqlite/settings.go` — SettingsStore methods

**Step 8: Write unit tests**

Create `pkg/infra/sqlite/store_test.go` that opens `:memory:` and exercises the same scenarios as the current `pkg/db/db_test.go`. Verify every method works.

Run: `go test ./pkg/infra/sqlite/... -v -count=1`
Expected: PASS

**Step 9: Commit**

```bash
git add pkg/infra/sqlite/
git commit -m "feat: add SQLite backend with sqlc queries and domain.Store implementation"
```

---

### Task 3: Create PostgreSQL Backend

**Files:**
- Create: `pkg/infra/postgres/sqlc.yaml`
- Create: `pkg/infra/postgres/queries/` (same query files, Postgres dialect)
- Create: `pkg/infra/postgres/migrations/` (same schemas, Postgres dialect)
- Create: `pkg/infra/postgres/store.go`
- Create: `pkg/infra/postgres/users.go`, `channels.go`, `messages.go`, `roles.go`, `settings.go`

**Context:** The Postgres backend mirrors the SQLite backend structurally, but uses Postgres-specific SQL dialect. The schemas are logically identical.

**Step 1: Create `pkg/infra/postgres/sqlc.yaml`**

```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "queries"
    schema: "migrations"
    gen:
      go:
        package: "postgres"
        out: "."
        emit_exact_table_names: true
```

**Step 2: Write Postgres migration files**

Translate each SQLite migration to Postgres dialect:

| SQLite | Postgres |
|--------|----------|
| `INTEGER PRIMARY KEY AUTOINCREMENT` | `BIGSERIAL PRIMARY KEY` |
| `BOOLEAN NOT NULL DEFAULT 0` | `BOOLEAN NOT NULL DEFAULT FALSE` |
| `TIMESTAMP DEFAULT CURRENT_TIMESTAMP` | `TIMESTAMPTZ DEFAULT NOW()` |
| `INSERT OR IGNORE` | `INSERT ... ON CONFLICT DO NOTHING` |
| `INSERT OR REPLACE` | `INSERT ... ON CONFLICT DO UPDATE` |
| Table recreation for column removal | `ALTER TABLE ... DROP COLUMN` |

Numbering and goose markers remain identical.

**Step 3: Write Postgres query files**

Translate queries from `?` to `$1, $2, ...` placeholders and adjust dialect:
- `INSERT OR IGNORE INTO x (...) VALUES (?, ?)` → `INSERT INTO x (...) VALUES ($1, $2) ON CONFLICT DO NOTHING`
- Boolean literals: `FALSE` instead of `0`
- String functions remain the same (both support `||`, `COALESCE`, etc.)

**Step 4: Run sqlc generate**

Run: `cd pkg/infra/postgres && sqlc generate`

**Step 5: Create `pkg/infra/postgres/store.go`**

```go
package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

type Store struct {
	db *sql.DB
	q  *Queries
}

func Open(dsn string) (*Store, error) {
	sqldb, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	sqldb.SetMaxOpenConns(25)
	sqldb.SetMaxIdleConns(5)
	sqldb.SetConnMaxLifetime(5 * time.Minute)

	if err := runMigrations(sqldb); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &Store{db: sqldb, q: New(sqldb)}, nil
}

func runMigrations(db *sql.DB) error {
	fsys, err := fs.Sub(embedMigrations, "migrations")
	if err != nil {
		return fmt.Errorf("migrations fs: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

var _ domain.Store = (*Store)(nil)
```

**Step 6: Implement all store methods**

Port every method, adapting SQL to Postgres dialect. The logic stays identical — only SQL syntax differs.

**Step 7: Write unit tests (skip without Postgres)**

```go
func TestMain(m *testing.M) {
	dsn := os.Getenv("SHARKFIN_DB")
	if dsn == "" || !strings.HasPrefix(dsn, "postgres") {
		fmt.Println("skipping postgres tests: SHARKFIN_DB not set")
		os.Exit(0)
	}
	os.Exit(m.Run())
}
```

Run locally against a Postgres instance if available.

**Step 8: Add pgx dependency**

Run: `go get github.com/jackc/pgx/v5`

**Step 9: Commit**

```bash
git add pkg/infra/postgres/ go.mod go.sum
git commit -m "feat: add PostgreSQL backend with sqlc queries and domain.Store implementation"
```

---

### Task 4: Create Infra Factory with DSN Auto-Detect

**Files:**
- Create: `pkg/infra/open.go`

**Step 1: Create factory**

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package infra

import (
	"strings"

	"github.com/Work-Fort/sharkfin/pkg/domain"
	"github.com/Work-Fort/sharkfin/pkg/infra/postgres"
	"github.com/Work-Fort/sharkfin/pkg/infra/sqlite"
)

// Open auto-detects the database backend from the DSN and returns a Store.
//
// DSN formats:
//   - postgres://... or postgresql://...  → PostgreSQL
//   - Any file path or :memory:           → SQLite
func Open(dsn string) (domain.Store, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return postgres.Open(dsn)
	}
	return sqlite.Open(dsn)
}
```

**Step 2: Verify it compiles**

Run: `go build ./pkg/infra/...`
Expected: PASS

**Step 3: Commit**

```bash
git add pkg/infra/open.go
git commit -m "feat: add infra.Open() factory with DSN auto-detect"
```

---

### Task 5: Update Daemon Layer to Use Domain Interfaces

**Files:**
- Modify: `pkg/daemon/server.go`
- Modify: `pkg/daemon/ws_handler.go`
- Modify: `pkg/daemon/mcp_server.go`
- Modify: `pkg/daemon/mcp_tools.go`
- Modify: `pkg/daemon/hub.go`
- Modify: `pkg/daemon/mentions.go`
- Modify: `pkg/daemon/session_manager.go`

**Context:** This is the largest task. Every `*db.DB` reference in the daemon layer becomes a `domain.Store` (or specific sub-interface). All type references change from `db.User` to `domain.User`, `db.Message` to `domain.Message`, etc.

**Step 1: Update `server.go`**

- Change `NewServer(addr, dbPath string, ...)` to `NewServer(addr string, store domain.Store, ...)`
- Remove `db.Open(dbPath)` call — caller provides the store
- Change `db *db.DB` field to `store domain.Store`
- Update `DB()` accessor to return `domain.Store`
- Pass `store` to `NewSessionManager`, `NewWSHandler`, `NewSharkfinMCP`
- Remove the `allow_channel_creation` setting seed (already done)
- Keep the webhook_url setting via `store.SetSetting()`

**Step 2: Update `ws_handler.go`**

- Change field `db *db.DB` to `store domain.Store`
- Replace all `h.db.XXX()` calls with `h.store.XXX()`
- Replace all `db.User`, `db.Message` etc. type references with `domain.User`, `domain.Message`

**Step 3: Update `mcp_server.go`**

- Change field `db *db.DB` to `store domain.Store`
- Replace all `s.db.XXX()` calls with `s.store.XXX()`
- Replace all type references
- Update `hub.BroadcastMessage(..., database)` → `hub.BroadcastMessage(..., s.store)`

**Step 4: Update `hub.go`**

- Change `BroadcastMessage(... database *db.DB)` to use `domain.Store` (or the specific sub-interfaces needed: `ChannelStore`, `SettingsStore`, `UserStore`)
- Change `BroadcastToRole(... database *db.DB)` similarly

**Step 5: Update `mentions.go`**

- Change `resolveMentions(database *db.DB, ...)` to `resolveMentions(users domain.UserStore, ...)`

**Step 6: Update `session_manager.go`**

- Change `db *db.DB` field to use `domain.UserStore` (or whatever subset it needs)

**Step 7: Update import paths**

Replace all `"github.com/Work-Fort/sharkfin/pkg/db"` imports with `"github.com/Work-Fort/sharkfin/pkg/domain"` in the daemon package.

**Step 8: Verify it compiles**

Run: `go build ./pkg/daemon/...`
Expected: PASS

**Step 9: Commit**

```bash
git add pkg/daemon/
git commit -m "refactor: update daemon layer to use domain.Store interfaces"
```

---

### Task 6: Update Command Layer and Configuration

**Files:**
- Modify: `cmd/daemon/daemon.go`
- Modify: `cmd/admin/admin.go`
- Modify: `cmd/root.go`
- Modify: `pkg/config/config.go`

**Step 1: Add `--db` flag to daemon command**

In `cmd/daemon/daemon.go`:
- Add `--db` flag bound to Viper key `db`
- Resolve DSN: if `--db` / `SHARKFIN_DB` is set, use it; otherwise default to `filepath.Join(config.GlobalPaths.StateDir, "sharkfin.db")`
- Call `infra.Open(dsn)` instead of `db.Open(dbPath)`
- Pass the `domain.Store` to `NewServer()` instead of a path

```go
dsn := viper.GetString("db")
if dsn == "" {
	dsn = filepath.Join(config.GlobalPaths.StateDir, "sharkfin.db")
}
store, err := infra.Open(dsn)
if err != nil {
	return fmt.Errorf("open database: %w", err)
}
defer store.Close()

srv, err := pkgdaemon.NewServer(addr, store, pongTimeout, webhookURL)
```

**Step 2: Update admin command**

In `cmd/admin/admin.go`:
- Add `--db` flag (same pattern)
- Change `openDB()` to use `infra.Open(dsn)`
- Returns `domain.Store` instead of `*db.DB`

**Step 3: Add Viper defaults**

In `pkg/config/config.go`:
- Add default: `viper.SetDefault("db", "")` (empty = use default SQLite path)

**Step 4: Verify build and tests**

Run: `mise run build`
Expected: PASS

Run: `go test ./pkg/daemon/... -count=1`
Expected: PASS (unit tests use `:memory:` which hits SQLite backend)

**Step 5: Commit**

```bash
git add cmd/ pkg/config/
git commit -m "feat: add --db flag with DSN auto-detect for backend selection"
```

---

### Task 7: Update Unit and Integration Tests

**Files:**
- Modify: `pkg/daemon/ws_handler_test.go`
- Modify: `tests/integration_test.go`
- Create: `pkg/infra/sqlite/store_test.go` (if not done in Task 2)

**Step 1: Update `ws_handler_test.go`**

- Change `newWSTestEnv` to open via `sqlite.Open(":memory:")` instead of `db.Open(":memory:")`
- Change type references from `db.DB` to `domain.Store`
- The `grantAdmin` helper now calls `env.store.SetUserRole(...)` instead of `env.db.SetUserRole(...)`

**Step 2: Update `tests/integration_test.go`**

- Change `startTestServer` to open via `sqlite.Open(":memory:")` and pass `domain.Store` to `NewServer`
- Change `grantAdmin` to use `domain.Store` methods
- Update all type references

**Step 3: Run full test suite**

Run: `go test ./pkg/daemon/... -count=1 && go test ./tests/... -count=1`
Expected: PASS

**Step 4: Commit**

```bash
git add pkg/daemon/ws_handler_test.go tests/
git commit -m "test: update unit and integration tests for domain.Store interfaces"
```

---

### Task 8: Update E2E Harness for Dual-Backend Support

**Files:**
- Modify: `tests/e2e/harness/harness.go`
- Modify: `tests/e2e/sharkfin_test.go`

**Context:** The e2e harness starts a daemon as an external process. It needs to pass `SHARKFIN_DB` to the daemon so it uses the right backend. When `SHARKFIN_DB` is set in the test runner's environment, forward it to the daemon process.

**Step 1: Update `StartDaemon`**

Add logic to pass through `SHARKFIN_DB`:

```go
func StartDaemon(binary, addr string, opts ...DaemonOption) (*Daemon, error) {
	// ...existing setup...

	args := []string{
		"daemon",
		"--daemon", addr,
		"--log-level", "disabled",
	}

	env := append(os.Environ(),
		"XDG_CONFIG_HOME="+xdgDir+"/config",
		"XDG_STATE_HOME="+xdgDir+"/state",
		fmt.Sprintf("SHARKFIN_PRESENCE_TIMEOUT=%s", cfg.presenceTimeout),
	)

	// Forward SHARKFIN_DB if set (e.g., for Postgres e2e)
	if dbDSN := os.Getenv("SHARKFIN_DB"); dbDSN != "" {
		args = append(args, "--db", dbDSN)
	}

	// ...rest unchanged...
}
```

**Step 2: Update `GrantAdmin`**

The admin CLI also needs the DSN. Forward `SHARKFIN_DB` or pass `--db`:

```go
func (d *Daemon) GrantAdmin(binary, username string) error {
	args := []string{"admin", "set-role", username, "admin"}
	env := append(os.Environ(),
		"XDG_CONFIG_HOME="+d.xdgDir+"/config",
		"XDG_STATE_HOME="+d.xdgDir+"/state",
	)

	// Forward SHARKFIN_DB if set
	if dbDSN := os.Getenv("SHARKFIN_DB"); dbDSN != "" {
		args = append(args, "--db", dbDSN)
	}

	cmd := exec.Command(binary, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("grant admin to %s: %w (output: %s)", username, err, out)
	}
	return nil
}
```

**Step 3: Run e2e tests (SQLite)**

Run: `mise run e2e`
Expected: PASS (all existing tests still work with default SQLite)

**Step 4: Test with Postgres locally (if available)**

Run: `SHARKFIN_DB=postgres://user:pass@localhost/sharkfin_test mise run e2e`
Expected: PASS

**Step 5: Commit**

```bash
git add tests/e2e/
git commit -m "feat: update e2e harness to forward SHARKFIN_DB for dual-backend testing"
```

---

### Task 9: Update CI for Dual-Backend E2E

**Files:**
- Modify: `.github/workflows/ci.yaml`

**Step 1: Add Postgres e2e job**

```yaml
# SPDX-License-Identifier: AGPL-3.0-or-later
name: CI

on:
  push:
    branches: [master]
  pull_request:

jobs:
  ci:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: jdx/mise-action@v3
      - run: mise run ci

  e2e-postgres:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:17
        env:
          POSTGRES_DB: sharkfin_test
          POSTGRES_USER: sharkfin
          POSTGRES_PASSWORD: sharkfin
        ports:
          - 5432:5432
        options: >-
          --health-cmd pg_isready
          --health-interval 5s
          --health-timeout 5s
          --health-retries 5
    steps:
      - uses: actions/checkout@v4
      - uses: jdx/mise-action@v3
      - run: mise run build
      - run: mise run e2e
        env:
          SHARKFIN_DB: postgres://sharkfin:sharkfin@localhost:5432/sharkfin_test?sslmode=disable
```

**Step 2: Commit**

```bash
git add .github/workflows/ci.yaml
git commit -m "ci: add Postgres e2e test job"
```

---

### Task 10: Delete Old `pkg/db` Package

**Files:**
- Delete: `pkg/db/` (entire directory)

**Context:** At this point all code references `pkg/domain` and `pkg/infra`. The old `pkg/db` is dead code.

**Step 1: Verify no remaining references**

Run: `grep -r "pkg/db" --include="*.go" . | grep -v vendor | grep -v .git`
Expected: No matches (or only in generated files that need regeneration)

**Step 2: Delete**

```bash
rm -rf pkg/db/
```

**Step 3: Run full CI**

Run: `mise run ci`
Expected: PASS (lint, test, e2e all green)

**Step 4: Commit**

```bash
git add -A
git commit -m "refactor: remove old pkg/db package (replaced by pkg/domain + pkg/infra)"
```

---

## Verification

After all tasks:

```bash
mise run ci                                                               # SQLite CI
SHARKFIN_DB=postgres://user:pass@localhost/sharkfin_test mise run e2e     # Postgres e2e (if available)
```

Both must pass.
