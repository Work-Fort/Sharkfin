# External E2E Test Harness Design

## Context

Sharkfin is an LLM IPC tool for coding agent collaboration via MCP. The daemon exposes two endpoints: `POST /mcp` (JSON-RPC 2.0) and `GET /presence` (WebSocket). Agents interact through the `mcp-bridge` CLI command, which manages a presence WebSocket and forwards MCP requests.

The existing integration tests (`tests/integration_test.go`) import Go packages directly and use in-process `httptest.NewServer`. They are fast and useful for unit-level coverage, but they don't test the actual compiled binary, CLI flags, XDG config paths, process lifecycle, or signal handling.

This design describes a **completely external test harness** — a separate Go module that builds the sharkfin binary, starts it as a subprocess, and exercises it exclusively over HTTP, WebSocket, and stdin/stdout. No internal imports. This is how an LLM/Agent application would interact with sharkfin.

## Structure

```
tests/e2e/
  go.mod              # separate module, no dependency on sharkfin
  go.sum
  harness/
    harness.go        # Daemon, Client, Bridge — exported, reusable
  sharkfin_test.go    # TestMain + 28 test functions
```

**Module:** `github.com/Work-Fort/sharkfin-e2e` (or similar). Only external dependency: `github.com/gorilla/websocket`.

**No `replace` directives.** The harness has zero knowledge of sharkfin internals.

## Harness Package API

### Daemon

Manages the sharkfin daemon subprocess.

```go
type DaemonOption func(*daemonConfig)

func WithAllowChannelCreation(allow bool) DaemonOption
func WithPresenceTimeout(d time.Duration) DaemonOption

func StartDaemon(binary, addr string, opts ...DaemonOption) (*Daemon, error)
func (d *Daemon) Addr() string
func (d *Daemon) Stop() error
```

`StartDaemon`:
1. Creates a temp dir for XDG isolation
2. Sets `XDG_CONFIG_HOME=<tmp>/config`, `XDG_STATE_HOME=<tmp>/state` on the child process
3. Runs `<binary> daemon --daemon <addr> --log-level disabled [flags]`
4. Polls TCP until the port is accepting connections (2s timeout)
5. Returns the running daemon handle

`Stop` sends SIGTERM and waits for the process to exit.

### Client

An MCP client that manages its own presence WebSocket.

```go
type ToolResult struct {
    Text  string
    Error *RPCError
}

type RPCError struct {
    Code    int
    Message string
}

func NewClient(daemonAddr string) *Client
func (c *Client) ConnectPresence() error
func (c *Client) DisconnectPresence()
func (c *Client) Token() string
func (c *Client) Initialize() error
func (c *Client) SessionID() string
func (c *Client) ToolCall(name string, args any) (ToolResult, error)
func (c *Client) Register(username, password string) error
func (c *Client) Identify(username, password string) error

// Convenience for common flows
func (c *Client) RegisterFlow(username string) error  // ConnectPresence + Initialize + Register
func (c *Client) IdentifyFlow(username string) error   // ConnectPresence + Initialize + Identify

// Raw access for protocol-level tests
func (c *Client) RawPost(path, body string) (*http.Response, []byte, error)
func (c *Client) RawMCPRequest(method string, id int, params any) (json.RawMessage, *RPCError, error)
```

### Bridge

For smoke testing the mcp-bridge subprocess.

```go
func StartBridge(binary, daemonAddr string, xdgDir string) (*Bridge, error)
func (b *Bridge) Send(jsonrpc string) (string, error)
func (b *Bridge) Stop() error
func (b *Bridge) Kill() error  // SIGKILL — no clean shutdown
```

`StartBridge` runs `<binary> mcp-bridge --daemon <addr> --log-level disabled` with stdin/stdout pipes and the same XDG env vars.

## Test Isolation

**TestMain** (in `sharkfin_test.go`):
1. Runs `go build -o <tempdir>/sharkfin ../../` to build the binary from the repo root
2. Stores the binary path in a package-level var
3. Runs tests
4. Cleans up

Each test function:
1. Picks a free port (`net.Listen("tcp", ":0")`, grab port, close)
2. Starts its own daemon with a fresh temp dir (fresh SQLite DB)
3. Defers `daemon.Stop()`

No shared state between tests. Every test gets a clean daemon.

**gitignore** — add to repo root `.gitignore`:
```
tests/e2e/testbin/
```

And `tests/e2e/.gitignore`:
```
testbin/
```

## Daemon Config Change

Add `presence-timeout` to support fast tests without hardcoding 20s waits.

- **Config file key:** `presence-timeout`
- **Env var:** `SHARKFIN_PRESENCE_TIMEOUT`
- **Default:** `20s`
- **Type:** `time.Duration` (parsed via `time.ParseDuration`)

Changes in sharkfin source:
- `pkg/config/config.go`: add viper default, bind
- `pkg/daemon/server.go`: read config, pass to `PresenceHandler`
- `pkg/daemon/presence_handler.go`: accept timeout as field instead of const

Tests set `SHARKFIN_PRESENCE_TIMEOUT=2s` via the `WithPresenceTimeout` daemon option.

## Test Matrix (28 tests)

### Presence (4 tests)

| Test | Description |
|------|-------------|
| `TestPresenceConnect` | Connect WS, receive 64-char hex token |
| `TestPresenceDisconnectMarksOffline` | Register user, clean disconnect, `user_list` shows offline |
| `TestPresenceRejectsPlainHTTP` | Plain HTTP GET to `/presence` → non-101 status |
| `TestPresenceHardKillMarksOffline` | Register via bridge, SIGKILL bridge, wait for pong timeout, verify offline |

### Identity (5 tests)

| Test | Description |
|------|-------------|
| `TestRegisterAndIdentify` | Register alice, disconnect, re-identify as alice |
| `TestDoubleRegisterFails` | Identified session calls `register` again → error |
| `TestIdentifyAfterRegisterFails` | Identified session calls `identify` → error |
| `TestDoubleLoginPrevention` | Second client `identify` as online user → "user already online" |
| `TestRegisterDuplicateUsername` | Two clients both `register` as "alice" → second fails |
| `TestIdentifyNonexistentUser` | `identify` unknown username → error |

### MCP Protocol (5 tests)

| Test | Description |
|------|-------------|
| `TestInitializeResponse` | Verify `protocolVersion`, `serverInfo`, `capabilities` in response |
| `TestToolsList` | `tools/list` returns 8 tools with expected names |
| `TestToolCallBeforeIdentify` | `user_list` without session → "not identified" error |
| `TestUnknownMethod` | Unknown JSON-RPC method → `MethodNotFound` |
| `TestUnknownTool` | `tools/call` unknown tool → error |
| `TestInvalidJSON` | Malformed body → `ParseError` |
| `TestMethodNotAllowed` | `GET /mcp` → 405 |

### Channels (6 tests)

| Test | Description |
|------|-------------|
| `TestChannelCreateAndList` | Create public channel, verify in `channel_list` |
| `TestPrivateChannelVisibility` | Private channel hidden from non-members, visible to members |
| `TestPublicChannelVisibleToAll` | Public channel visible to non-member |
| `TestChannelCreationDisabled` | Daemon with `--allow-channel-creation=false` → create fails |
| `TestChannelInvite` | Member invites non-member, invitee can see and send |
| `TestChannelInviteByNonMember` | Non-member tries `channel_invite` → error |

### Messaging (6 tests)

| Test | Description |
|------|-------------|
| `TestSendAndReceiveMessage` | Alice sends, Bob reads via `unread_messages` |
| `TestUnreadMessagesAreConsumed` | Read unreads, read again → empty |
| `TestUnreadFilterByChannel` | `unread_messages` with `channel` param filters correctly |
| `TestSendToNonexistentChannel` | `send_message` to unknown channel → error |
| `TestNonParticipantCannotSend` | Non-member sends to private channel → error |
| `TestMultipleMessagesOrdering` | Multiple messages arrive in chronological order |

### Integration (2 tests)

| Test | Description |
|------|-------------|
| `TestBridgeEndToEnd` | Start daemon + bridge. Pipe `initialize` → `get_identity_token` → `register` → `send_message` through stdin/stdout. Verify responses. |
| `TestPresenceExitsOnDaemonRestart` | Start daemon + `presence` subprocess. Stop daemon. Verify presence process exits. Restart daemon, verify accepts new connections. |

## Verification

```bash
cd tests/e2e
go test -v -race -timeout 120s
```

From the repo root, add a Taskfile task:
```yaml
e2e:
  desc: Run end-to-end tests
  deps: [build]
  cmds:
    - cd tests/e2e && go test -v -race -timeout 120s
```
