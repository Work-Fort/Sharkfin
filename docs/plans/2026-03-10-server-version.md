# Server Version Query Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Expose the server version to WS and MCP clients so they can identify which daemon version they're connected to.

**Architecture:** Pass `cmd.Version` through the constructor chain (`NewServer` → `NewSharkfinMCP` + `NewWSHandler`). MCP uses the existing `serverInfo` field. WS adds `version` to the hello envelope and adds a `version` request type. Default to `"dev"` when the version is empty.

**Tech Stack:** Go, gorilla/websocket, mcp-go

---

### Task 1: Plumb version through constructors

**Files:**
- Modify: `cmd/daemon/daemon.go:49-50`
- Modify: `pkg/daemon/server.go:28`
- Modify: `pkg/daemon/mcp_server.go:58,73`
- Modify: `pkg/daemon/ws_handler.go:17-32`

**Step 1: Add version field to WSHandler and pass it through**

In `pkg/daemon/ws_handler.go`, add `version string` field to `WSHandler` struct and `NewWSHandler` parameter:

```go
type WSHandler struct {
	sessions    *SessionManager
	store       domain.Store
	hub         *Hub
	pongTimeout time.Duration
	version     string
}

func NewWSHandler(sessions *SessionManager, store domain.Store, hub *Hub, pongTimeout time.Duration, version string) *WSHandler {
	return &WSHandler{
		sessions:    sessions,
		store:       store,
		hub:         hub,
		pongTimeout: pongTimeout,
		version:     version,
	}
}
```

**Step 2: Add version parameter to NewSharkfinMCP**

In `pkg/daemon/mcp_server.go`, add `version string` parameter to `NewSharkfinMCP` and replace `"0.1.0"`:

```go
func NewSharkfinMCP(sm *SessionManager, store domain.Store, hub *Hub, version string) *SharkfinMCP {
```

And on line 73:

```go
s.mcpServer = server.NewMCPServer("sharkfin", version,
```

**Step 3: Add version parameter to NewServer and pass through**

In `pkg/daemon/server.go`, add `version string` parameter:

```go
func NewServer(addr string, store domain.Store, pongTimeout time.Duration, webhookURL string, bus domain.EventBus, version string) (*Server, error) {
```

Update the constructor calls inside `NewServer`:

```go
sharkfinMCP := NewSharkfinMCP(sm, store, hub, version)
wsHandler := NewWSHandler(sm, store, hub, pongTimeout, version)
```

**Step 4: Pass cmd.Version from daemon command**

In `cmd/daemon/daemon.go`, import the `cmd` package and pass `cmd.Version`:

```go
import "github.com/Work-Fort/sharkfin/cmd"
```

Add version defaulting before the `NewServer` call:

```go
version := cmd.Version
if version == "" {
	version = "dev"
}
srv, err := pkgdaemon.NewServer(addr, store, pongTimeout, webhookURL, bus, version)
```

**Step 5: Verify it compiles**

Run: `mise run build`
Expected: BUILD OK

**Step 6: Commit**

```bash
git add cmd/daemon/daemon.go pkg/daemon/server.go pkg/daemon/mcp_server.go pkg/daemon/ws_handler.go
git commit -m "refactor: plumb version string through server constructors"
```

---

### Task 2: Add version to WS hello and version request type

**Files:**
- Modify: `pkg/daemon/ws_handler.go:44-48,121-191,197-200`
- Test: `pkg/daemon/ws_handler_test.go`

**Step 1: Write failing test for version in hello**

In `pkg/daemon/ws_handler_test.go`, find the existing hello test or add a new one that checks for the `version` field in the hello envelope:

```go
func TestWSHelloIncludesVersion(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()

	hello := h.readEnvelope(t)
	require.Equal(t, "hello", hello.Type)

	d := hello.D.(map[string]interface{})
	require.Contains(t, d, "version")
	require.Equal(t, "test", d["version"])
}
```

Note: The test harness creates a `WSHandler` — update the harness's `NewWSHandler` call to pass `"test"` as the version.

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/daemon/ -run TestWSHelloIncludesVersion -v`
Expected: FAIL — no `version` field in hello

**Step 3: Add version to hello envelope**

In `ws_handler.go`, update the hello message construction:

```go
hello := wsEnvelope{
	Type: "hello",
	D: map[string]interface{}{
		"heartbeat_interval": int(pingInterval.Seconds()),
		"version":            h.version,
	},
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/daemon/ -run TestWSHelloIncludesVersion -v`
Expected: PASS

**Step 5: Write failing test for version request type**

```go
func TestWSVersionRequest(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()

	h.readEnvelope(t) // consume hello

	h.register(t, "alice")

	h.send(t, wsEnvelope{Type: "version", Ref: "v1"})
	reply := h.readEnvelope(t)
	require.Equal(t, "reply", reply.Type)
	require.Equal(t, "v1", reply.Ref)
	require.True(t, *reply.OK)

	d := reply.D.(map[string]interface{})
	require.Equal(t, "test", d["version"])
}
```

Also test that `version` works before identification (alongside `ping`):

```go
func TestWSVersionBeforeIdentify(t *testing.T) {
	h := newTestHarness(t)
	defer h.close()

	h.readEnvelope(t) // consume hello

	h.send(t, wsEnvelope{Type: "version", Ref: "v1"})
	reply := h.readEnvelope(t)
	require.Equal(t, "reply", reply.Type)
	require.True(t, *reply.OK)

	d := reply.D.(map[string]interface{})
	require.Equal(t, "test", d["version"])
}
```

**Step 6: Run tests to verify they fail**

Run: `go test ./pkg/daemon/ -run "TestWSVersion" -v`
Expected: FAIL — unknown type

**Step 7: Add version request handler**

In the pre-identification switch block (around line 122), add `"version"` alongside `"ping"`:

```go
case "ping":
	sendPong(sendCh, req.Ref)
case "version":
	sendReply(sendCh, req.Ref, true, map[string]string{"version": h.version})
```

In the post-identification switch block (around line 197), add `"version"` alongside `"ping"`:

```go
case "ping":
	sendPong(sendCh, req.Ref)
case "version":
	sendReply(sendCh, req.Ref, true, map[string]string{"version": h.version})
```

**Step 8: Run tests to verify they pass**

Run: `go test ./pkg/daemon/ -run "TestWSVersion|TestWSHello" -v`
Expected: PASS

**Step 9: Run full unit test suite**

Run: `mise run test`
Expected: All tests pass

**Step 10: Commit**

```bash
git add pkg/daemon/ws_handler.go pkg/daemon/ws_handler_test.go
git commit -m "feat: add version to WS hello envelope and version request type"
```

---

### Task 3: E2E test and CI

**Files:**
- Modify: `tests/e2e/sharkfin_test.go`

**Step 1: Add e2e test for WS version**

Add a test that connects via WS and verifies the hello includes `version` and the `version` request works:

```go
func TestWSVersion(t *testing.T) {
	h := harness.StartDaemon(t)
	ws := h.NewWSClient(t)
	defer ws.Close()

	// hello should include version
	hello := ws.ReadEnvelope(t)
	require.Equal(t, "hello", hello.Type)
	d := hello.D.(map[string]interface{})
	require.Contains(t, d, "version")
	require.NotEmpty(t, d["version"])

	// version request before identify
	ws.Send(t, map[string]interface{}{"type": "version", "ref": "v1"})
	reply := ws.ReadByRef(t, "v1")
	require.True(t, *reply.OK)
}
```

Note: Adjust to match the actual e2e harness API — the WS client helpers may differ from unit test helpers.

**Step 2: Run e2e tests**

Run: `mise run e2e`
Expected: All pass (including new test)

**Step 3: Run full CI**

Run: `mise run ci`
Expected: All pass

**Step 4: Commit**

```bash
git add tests/e2e/sharkfin_test.go
git commit -m "test: add e2e test for WS version query"
```
