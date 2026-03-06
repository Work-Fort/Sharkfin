# sharkfin Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build sharkfin, a local LLM IPC messaging system with MCP integration.

**Architecture:** Single Go binary with three subcommands (`daemon`, `mcp-bridge`, `presence`). The daemon serves a JSON-RPC 2.0 MCP endpoint and a presence long-poll endpoint over HTTP. The CLI bridges stdio MCP to the daemon's HTTP API. SQLite stores users, channels, messages, and read cursors. In-memory state tracks identity tokens and presence connections.

**Tech Stack:** Go, Cobra, Viper, charmbracelet/log, modernc.org/sqlite, net/http, go-task, mise

---

### Task 1: Project Scaffolding

**Files:**
- Create: `go.mod`
- Create: `main.go`
- Create: `cmd/root.go`
- Create: `LICENSE`
- Create: `Taskfile.dist.yaml`
- Modify: `mise.toml`

**Step 1: Initialize Go module and install dependencies**

Run:
```bash
cd /home/kazw/Work/WorkFort/sharkfin
mise install
go mod init github.com/Work-Fort/sharkfin
go get github.com/spf13/cobra@latest
go get github.com/spf13/viper@latest
go get github.com/charmbracelet/log@latest
go get modernc.org/sqlite@latest
```

**Step 2: Create LICENSE file**

Write the GPL-v2.0-Only license text to `LICENSE`.

**Step 3: Update mise.toml**

```toml
[tools]
go = "latest"
"go:github.com/go-task/task/v3/cmd/task" = "latest"
```

**Step 4: Create main.go**

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"github.com/Work-Fort/sharkfin/cmd"
)

func main() {
	cmd.Execute()
}
```

**Step 5: Create cmd/root.go**

Follow anvil's pattern: root command with `PersistentPreRunE` that initializes
config and logging. Add global `--log-level` and `--daemon` flags. Bind to
viper with `SHARKFIN_` env prefix. Silence errors and usage (handle them
ourselves). No TUI, no glamour — keep it minimal for now.

Key differences from anvil:
- `--daemon` flag (default `127.0.0.1:16000`) for daemon address
- No TUI flags
- SPDX header: `GPL-2.0-only` not `Apache-2.0`

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	Version   string
	daemonAddr string
	logLevel   string
)

var rootCmd = &cobra.Command{
	Use:   "sharkfin",
	Short: "LLM IPC tool for coding agent collaboration",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&daemonAddr, "daemon", "127.0.0.1:16000", "Daemon address")
	rootCmd.PersistentFlags().StringVarP(&logLevel, "log-level", "l", "debug", "Log level: disabled, debug, info, warn, error")
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
}
```

**Step 6: Create Taskfile.dist.yaml**

```yaml
# SPDX-License-Identifier: AGPL-3.0-or-later
# yaml-language-server: $schema=https://taskfile.dev/schema.json
version: '3'

vars:
  BINARY_NAME: sharkfin
  BUILD_DIR: build
  GIT_SHORT_SHA:
    sh: git rev-parse --short HEAD 2>/dev/null || echo "unknown"
  VERSION: '{{.VERSION | default (printf "dev-%s" .GIT_SHORT_SHA)}}'

tasks:
  default:
    desc: Show available tasks
    silent: true
    cmds:
      - task --list

  build:
    desc: Build sharkfin
    silent: true
    sources:
      - "**/*.go"
      - go.mod
      - go.sum
    generates:
      - "{{.BUILD_DIR}}/{{.BINARY_NAME}}"
    cmds:
      - mkdir -p {{.BUILD_DIR}}
      - go build -ldflags "-X github.com/Work-Fort/sharkfin/cmd.Version={{.VERSION}}" -o {{.BUILD_DIR}}/{{.BINARY_NAME}}

  test:
    desc: Run tests
    silent: true
    cmds:
      - mkdir -p {{.BUILD_DIR}}
      - go test -v -race -coverprofile={{.BUILD_DIR}}/coverage.out ./...

  lint:
    desc: Run linters
    silent: true
    cmds:
      - test -z "$(gofmt -l .)" || (gofmt -l . && exit 1)
      - go vet ./...

  clean:
    desc: Clean build artifacts
    silent: true
    cmds:
      - rm -rf {{.BUILD_DIR}}

  ci:
    desc: Run all CI checks
    silent: true
    cmds:
      - task: lint
      - task: test
```

**Step 7: Verify it builds**

Run: `cd /home/kazw/Work/WorkFort/sharkfin && task build`
Expected: Binary at `build/sharkfin`

Run: `./build/sharkfin --help`
Expected: Help output showing "LLM IPC tool for coding agent collaboration"

**Step 8: Commit**

```bash
git add main.go cmd/root.go go.mod go.sum LICENSE mise.toml Taskfile.dist.yaml
git commit -m "feat: project scaffolding with cobra, viper, go-task"
```

---

### Task 2: Config Package

**Files:**
- Create: `pkg/config/config.go`
- Modify: `cmd/root.go`

**Step 1: Create pkg/config/config.go**

XDG-compliant paths for sharkfin. Follow anvil's pattern from
`pkg/config/config.go`. Paths needed:

- `$XDG_CONFIG_HOME/sharkfin/` — config dir (config.yaml)
- `$XDG_STATE_HOME/sharkfin/` — state dir (sharkfin.db, debug.log)

Constants: `EnvPrefix = "SHARKFIN"`, `ConfigFileName = "config"`,
`ConfigType = "yaml"`, `DefaultDaemonAddr = "127.0.0.1:16000"`.

Struct:
```go
type Paths struct {
	ConfigDir string
	StateDir  string
}
```

Functions:
- `GetPaths() *Paths` — resolve XDG paths
- `InitDirs() error` — create directories
- `InitViper()` — set defaults, config file search, env prefix
- `LoadConfig() error` — read config file
- `BindFlags(flags *pflag.FlagSet)` — bind cobra flags to viper

Note: Use `$XDG_STATE_HOME` (not `$XDG_DATA_HOME`) for the database and logs.
Default: `~/.local/state`.

**Step 2: Wire config into cmd/root.go**

Add `PersistentPreRunE` that calls `config.InitDirs()`, `config.LoadConfig()`,
and sets up charmbracelet/log to file at `<StateDir>/debug.log`. Follow anvil's
logging setup pattern (JSON formatter, timestamps, caller info).

**Step 3: Verify**

Run: `task build && ./build/sharkfin --help`
Expected: Works, creates `~/.config/sharkfin/` and `~/.local/state/sharkfin/`

**Step 4: Commit**

```bash
git add pkg/config/config.go cmd/root.go
git commit -m "feat: add config package with XDG paths and viper setup"
```

---

### Task 3: JSON-RPC 2.0 Protocol Types

**Files:**
- Create: `pkg/protocol/jsonrpc.go`
- Create: `pkg/protocol/jsonrpc_test.go`

**Step 1: Write the tests**

Test JSON marshaling/unmarshaling of:
- Request with string ID
- Request with integer ID
- Notification (no ID)
- Successful response
- Error response with data
- Batch request (array of requests)

**Step 2: Run tests to verify they fail**

Run: `go test -v ./pkg/protocol/`
Expected: FAIL — types don't exist yet

**Step 3: Implement protocol types**

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package protocol
```

Types needed:
- `RequestID` — can be string or int, needs custom JSON marshal/unmarshal
- `Request` — `jsonrpc`, `method`, `params` (json.RawMessage), `id`
- `Response` — `jsonrpc`, `result` (json.RawMessage), `error`, `id`
- `Error` — `code`, `message`, `data` (json.RawMessage)
- `Notification` — `jsonrpc`, `method`, `params`

Constants for standard error codes:
- `ParseError = -32700`
- `InvalidRequest = -32600`
- `MethodNotFound = -32601`
- `InvalidParams = -32602`
- `InternalError = -32603`

Helper constructors:
- `NewResponse(id, result)` — success response
- `NewErrorResponse(id, code, message)` — error response

**Step 4: Run tests to verify they pass**

Run: `go test -v ./pkg/protocol/`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/protocol/
git commit -m "feat: add JSON-RPC 2.0 protocol types"
```

---

### Task 4: Database Layer

**Files:**
- Create: `pkg/db/db.go`
- Create: `pkg/db/users.go`
- Create: `pkg/db/channels.go`
- Create: `pkg/db/messages.go`
- Create: `pkg/db/db_test.go`

**Step 1: Write integration tests for the database layer**

Test file `pkg/db/db_test.go` should use `":memory:"` SQLite for fast tests.
Test cases:

Users:
- Create user, verify it exists
- Create duplicate username returns error
- Get user by username
- List all users

Channels:
- Create channel with members
- List channels visible to user (public + participant)
- Private channel not visible to non-participant
- Add member to channel
- Verify caller is participant before invite

Messages:
- Send message to channel
- Get unread messages (no cursor = all messages)
- Get unread messages advances cursor
- Second call returns only new messages
- Messages from non-participant rejected

**Step 2: Run tests to verify they fail**

Run: `go test -v ./pkg/db/`
Expected: FAIL

**Step 3: Implement db.go**

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package db
```

- `type DB struct { db *sql.DB }`
- `func Open(path string) (*DB, error)` — open SQLite, run migrations
- `func (d *DB) Close() error`
- Migration: create all 5 tables (users, channels, channel_members, messages,
  read_cursors) with foreign keys enabled via `PRAGMA foreign_keys = ON`

**Step 4: Implement users.go**

- `func (d *DB) CreateUser(username, password string) (int64, error)`
- `func (d *DB) GetUserByUsername(username string) (*User, error)`
- `func (d *DB) ListUsers() ([]User, error)`
- `type User struct { ID int64; Username string; Password string; CreatedAt time.Time }`

**Step 5: Implement channels.go**

- `type Channel struct { ID int64; Name string; Public bool; CreatedAt time.Time }`
- `type ChannelMember struct { ChannelID int64; UserID int64; JoinedAt time.Time }`
- `func (d *DB) CreateChannel(name string, public bool, memberIDs []int64) (int64, error)` — transaction: insert channel + members
- `func (d *DB) ListChannelsForUser(userID int64) ([]Channel, error)` — public channels UNION channels where user is member
- `func (d *DB) AddChannelMember(channelID, userID int64) error`
- `func (d *DB) IsChannelMember(channelID, userID int64) (bool, error)`

**Step 6: Implement messages.go**

- `type Message struct { ID int64; ChannelID int64; UserID int64; Body string; CreatedAt time.Time; Username string }`
- `func (d *DB) SendMessage(channelID, userID int64, body string) (int64, error)`
- `func (d *DB) GetUnreadMessages(userID int64, channelID *int64) ([]Message, error)`
  — if channelID is nil, get across all channels the user is a member of
  — compare against read_cursors to find unread
  — advance cursor after fetch
- The cursor advance and message fetch must be in a transaction

**Step 7: Run tests to verify they pass**

Run: `go test -v ./pkg/db/`
Expected: PASS

**Step 8: Commit**

```bash
git add pkg/db/
git commit -m "feat: add SQLite database layer with users, channels, messages"
```

---

### Task 5: Session Manager

**Files:**
- Create: `pkg/daemon/session.go`
- Create: `pkg/daemon/session_test.go`

**Step 1: Write tests for session management**

Test cases:
- Create identity token, verify it's pending
- Register with token — creates user, marks token as identified
- Identify with token — marks token as identified
- Identify when already online — returns error
- Register after already identified — returns error
- Identify after already identified — returns error
- Get session by MCP session ID — returns correct user
- Remove presence — marks user offline
- Orphaned token cleanup

**Step 2: Run tests to verify they fail**

Run: `go test -v ./pkg/daemon/`
Expected: FAIL

**Step 3: Implement session.go**

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon
```

In-memory state:
```go
type SessionManager struct {
	mu             sync.RWMutex
	tokens         map[string]*IdentityToken  // token string → state
	mcpSessions    map[string]*MCPSession     // mcp session ID → session
	onlineUsers    map[string]string           // username → token
	db             *db.DB
	allowChannelCreation bool
}

type IdentityToken struct {
	Token        string
	Username     string    // empty until identified
	Identified   bool
	MCPSessionID string
	PresenceConn chan struct{} // closed when presence disconnects
	CreatedAt    time.Time
}

type MCPSession struct {
	ID       string
	TokenID  string
	Username string
}
```

Methods:
- `func NewSessionManager(db *db.DB, allowChannelCreation bool) *SessionManager`
- `func (sm *SessionManager) CreateIdentityToken() string` — generate crypto random token, store in map
- `func (sm *SessionManager) AttachPresence(token string) (<-chan struct{}, error)` — called by presence handler, returns done channel
- `func (sm *SessionManager) Register(token, username, password string) (string, error)` — create user in DB, associate token, return MCP session ID
- `func (sm *SessionManager) Identify(token, username, password string) (string, error)` — look up user in DB, associate token, return MCP session ID
- `func (sm *SessionManager) GetSession(mcpSessionID string) (*MCPSession, error)` — look up session
- `func (sm *SessionManager) DisconnectPresence(token string)` — remove from online users, clean up

**Step 4: Run tests to verify they pass**

Run: `go test -v ./pkg/daemon/`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/daemon/session.go pkg/daemon/session_test.go
git commit -m "feat: add session manager with identity token lifecycle"
```

---

### Task 6: MCP Handler

**Files:**
- Create: `pkg/daemon/mcp_handler.go`
- Create: `pkg/daemon/mcp_handler_test.go`

**Step 1: Write integration tests for the MCP handler**

Test the handler as an `http.Handler` using `httptest`. Each test POSTs
JSON-RPC requests and asserts on JSON-RPC responses.

Test cases:
- `initialize` — returns capabilities and server info, sets Mcp-Session-Id header
- `tools/list` — returns all 9 tools with schemas
- `tools/call` `get_identity_token` — returns a token string
- `tools/call` `register` — with valid token, creates user, returns success
- `tools/call` `register` — after already identified, returns error
- `tools/call` `identify` — with valid token and existing user, returns success
- `tools/call` `identify` — user already online, returns error
- `tools/call` `user_list` — returns users with presence status
- `tools/call` `channel_create` — creates channel, returns channel info
- `tools/call` `channel_create` — when disabled globally, returns error
- `tools/call` `channel_invite` — adds user to channel
- `tools/call` `channel_invite` — non-participant, returns error
- `tools/call` `send_message` — sends message
- `tools/call` `send_message` — non-participant, returns error
- `tools/call` `unread_messages` — returns unread messages
- Unidentified session calling identified-only tool — returns error
- Unknown tool name — returns error

**Step 2: Run tests to verify they fail**

Run: `go test -v ./pkg/daemon/`
Expected: FAIL

**Step 3: Implement mcp_handler.go**

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon
```

- `type MCPHandler struct { sessions *SessionManager; db *db.DB }`
- `func NewMCPHandler(sessions *SessionManager, db *db.DB) *MCPHandler`
- `func (h *MCPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request)`
  — parse JSON-RPC request from body
  — extract `Mcp-Session-Id` header
  — dispatch on `method`:
    - `initialize` → return capabilities, generate and set `Mcp-Session-Id` header
    - `tools/list` → return tool definitions with JSON schemas
    - `tools/call` → dispatch to tool handler based on tool name
  — check session state (identified vs unidentified) before tool execution
  — return JSON-RPC response

Tool implementations — each is a private method:
- `handleGetIdentityToken()`
- `handleRegister(params)`
- `handleIdentify(params)`
- `handleUserList(session)`
- `handleChannelList(session)`
- `handleChannelCreate(session, params)`
- `handleChannelInvite(session, params)`
- `handleSendMessage(session, params)`
- `handleUnreadMessages(session, params)`

MCP tool definitions for `tools/list`:
Each tool needs `name`, `description`, and `inputSchema` (JSON Schema object).
The schemas define required parameters and types.

**Step 4: Run tests to verify they pass**

Run: `go test -v ./pkg/daemon/`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/daemon/mcp_handler.go pkg/daemon/mcp_handler_test.go
git commit -m "feat: add MCP handler with all 9 tools"
```

---

### Task 7: Presence Handler

**Files:**
- Create: `pkg/daemon/presence_handler.go`
- Create: `pkg/daemon/presence_handler_test.go`

**Step 1: Write tests for presence handler**

Test as `http.Handler` with `httptest`:
- POST with valid token — connection stays open until client disconnects
- POST with invalid token — returns error immediately
- Client disconnect — session manager is notified, user goes offline
- Verify presence is reflected in `user_list` tool

**Step 2: Run tests to verify they fail**

Run: `go test -v ./pkg/daemon/`
Expected: FAIL

**Step 3: Implement presence_handler.go**

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon
```

- `type PresenceHandler struct { sessions *SessionManager }`
- `func NewPresenceHandler(sessions *SessionManager) *PresenceHandler`
- `func (h *PresenceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request)`
  — extract token from request body (JSON: `{"token": "..."}`)
  — call `sessions.AttachPresence(token)` — returns done channel
  — if error (invalid token), return 400
  — block until either:
    - `r.Context().Done()` (client disconnected)
    - done channel closed (server-side disconnect)
  — on exit, call `sessions.DisconnectPresence(token)`

**Step 4: Run tests to verify they pass**

Run: `go test -v ./pkg/daemon/`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/daemon/presence_handler.go pkg/daemon/presence_handler_test.go
git commit -m "feat: add presence handler with long-poll connection"
```

---

### Task 8: HTTP Server & Daemon Command

**Files:**
- Create: `pkg/daemon/server.go`
- Create: `cmd/daemon/daemon.go`

**Step 1: Implement server.go**

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon
```

- `type Server struct { addr string; db *db.DB; sessions *SessionManager; httpServer *http.Server }`
- `func NewServer(addr, dbPath string, allowChannelCreation bool) (*Server, error)` — open DB, create session manager, wire routes
- `func (s *Server) Start() error` — start HTTP server
- `func (s *Server) Shutdown(ctx context.Context) error` — graceful shutdown

Routes:
- `POST /mcp` → MCPHandler
- `POST /presence` → PresenceHandler

**Step 2: Implement cmd/daemon/daemon.go**

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon
```

- `func NewDaemonCmd() *cobra.Command`
- Command: `sharkfin daemon`
- Flags: `--addr` (default from viper/config), `--allow-channel-creation` (default true)
- `RunE`: create server, start it, handle SIGINT/SIGTERM for graceful shutdown

**Step 3: Wire into cmd/root.go**

Add `rootCmd.AddCommand(daemon.NewDaemonCmd())` in `init()`.

**Step 4: Verify**

Run: `task build && ./build/sharkfin daemon --help`
Expected: Help for daemon subcommand

Run: `./build/sharkfin daemon &` then `curl -X POST http://127.0.0.1:16000/mcp -d '{"jsonrpc":"2.0","method":"initialize","id":1}'`
Expected: JSON-RPC response with capabilities

**Step 5: Commit**

```bash
git add pkg/daemon/server.go cmd/daemon/daemon.go cmd/root.go
git commit -m "feat: add daemon command with HTTP server"
```

---

### Task 9: MCP Bridge Command

**Files:**
- Create: `cmd/mcpbridge/mcp_bridge.go`

**Step 1: Implement mcp_bridge.go**

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package mcpbridge
```

- `func NewMCPBridgeCmd() *cobra.Command`
- Command: `sharkfin mcp-bridge`
- `RunE`:
  — create HTTP client
  — build MCP URL: `http://<daemon_addr>/mcp`
  — read stdin line by line (bufio.Scanner)
  — for each line: POST to MCP URL with body
  — capture `Mcp-Session-Id` from first response, include on subsequent requests
  — write response body to stdout, flush
  — on stdin EOF, exit

**Step 2: Wire into cmd/root.go**

Add `rootCmd.AddCommand(mcpbridge.NewMCPBridgeCmd())`.

**Step 3: Verify**

Run daemon in background, then:
```bash
echo '{"jsonrpc":"2.0","method":"initialize","id":1}' | ./build/sharkfin mcp-bridge
```
Expected: JSON-RPC response on stdout

**Step 4: Commit**

```bash
git add cmd/mcpbridge/mcp_bridge.go cmd/root.go
git commit -m "feat: add MCP bridge command (stdio to HTTP proxy)"
```

---

### Task 10: Presence Command

**Files:**
- Create: `cmd/presence/presence.go`

**Step 1: Implement presence.go**

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package presence
```

- `func NewPresenceCmd() *cobra.Command`
- Command: `sharkfin presence <token>`
- Args: exactly 1 (the identity token)
- `RunE`:
  — POST to `http://<daemon_addr>/presence` with JSON body `{"token": "<token>"}`
  — the request blocks (long-poll) until disconnected
  — handle SIGINT/SIGTERM to close gracefully
  — on connection close, exit

**Step 2: Wire into cmd/root.go**

Add `rootCmd.AddCommand(presence.NewPresenceCmd())`.

**Step 3: Commit**

```bash
git add cmd/presence/presence.go cmd/root.go
git commit -m "feat: add presence command for long-poll connection"
```

---

### Task 11: Systemd Service

**Files:**
- Create: `dist/sharkfin.service`

**Step 1: Create service unit**

```ini
[Unit]
Description=sharkfin daemon
After=network.target

[Service]
Type=simple
ExecStart=sharkfin daemon
Restart=on-failure

[Install]
WantedBy=default.target
```

**Step 2: Commit**

```bash
git add dist/sharkfin.service
git commit -m "feat: add systemd user service unit"
```

---

### Task 12: Full Integration Tests

**Files:**
- Create: `tests/integration_test.go`

**Step 1: Write end-to-end integration tests**

These tests start a real sharkfind server in-process on a random port, then
exercise the full flow via HTTP. Use `":memory:"` SQLite. Use `httptest` or
direct HTTP client calls.

Test scenarios:

**Scenario 1: Identity handshake and presence**
1. POST `initialize` to /mcp — get session ID
2. Call `get_identity_token` — get token
3. POST to /presence with token in background goroutine
4. Call `register` with token, username, password
5. Call `user_list` — verify user is online
6. Cancel presence goroutine (simulate disconnect)
7. Call `user_list` — verify user is offline

**Scenario 2: Messaging between two users**
1. Register user A and user B (each with their own token + presence + session)
2. User A creates a private channel with user B
3. User A sends a message
4. User B calls unread_messages — gets the message
5. User B calls unread_messages again — no new messages
6. User A sends another message
7. User B calls unread_messages — gets only the new message

**Scenario 3: Channel visibility**
1. Register user A, user B, user C
2. User A creates public channel, adds user B
3. User C calls channel_list — sees the public channel
4. User A creates private channel with user B
5. User C calls channel_list — does not see the private channel
6. User B calls channel_list — sees both channels

**Scenario 4: Channel creation disabled**
1. Start server with `allowChannelCreation = false`
2. Register user A
3. User A calls channel_create — gets error

**Scenario 5: Session state constraints**
1. Register user A
2. Call register again — error (already identified)
3. Call identify — error (already identified)

**Scenario 6: Double login prevention**
1. Register user A with token1 + presence1
2. Get token2, start presence2
3. Identify as user A with token2 — error (already online)

**Scenario 7: Channel invite**
1. Register user A, user B, user C
2. User A creates private channel with user B
3. User B invites user C
4. User C calls channel_list — sees the channel
5. User C sends a message — succeeds

**Scenario 8: Non-participant rejection**
1. Register user A, user B
2. User A creates private channel (alone or with another user)
3. User B calls send_message to that channel — error

**Step 2: Run tests to verify they fail**

Run: `go test -v ./tests/`
Expected: FAIL

**Step 3: Fix any issues discovered by integration tests**

Iterate until all tests pass.

**Step 4: Run full test suite**

Run: `task test`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add tests/
git commit -m "feat: add full integration test suite"
```

---

### Task 13: Final Verification

**Step 1: Run CI checks**

Run: `task ci`
Expected: lint + test all pass

**Step 2: Manual smoke test**

```bash
# Terminal 1: start daemon
./build/sharkfin daemon

# Terminal 2: use MCP bridge
echo '{"jsonrpc":"2.0","method":"initialize","id":1}' | ./build/sharkfin mcp-bridge
# get identity token
echo '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"get_identity_token","arguments":{}},"id":2}' | ./build/sharkfin mcp-bridge

# Terminal 3: start presence with token from above
./build/sharkfin presence <token>

# Terminal 2: identify
echo '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"register","arguments":{"token":"<token>","username":"agent1","password":""}},"id":3}' | ./build/sharkfin mcp-bridge
```

**Step 3: Commit any final fixes**

```bash
git add -A
git commit -m "chore: final cleanup and verification"
```
