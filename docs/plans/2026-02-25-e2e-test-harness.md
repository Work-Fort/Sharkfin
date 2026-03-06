# E2E Test Harness Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build an external E2E test harness that tests the compiled sharkfin binary as a subprocess over HTTP/WebSocket — the same way an LLM agent would use it.

**Architecture:** Separate Go module in `tests/e2e/` with a reusable `harness/` package exporting `Daemon`, `Client`, `Bridge` types. `TestMain` builds the binary. Each test starts its own daemon with XDG isolation. No sharkfin module imports.

**Tech Stack:** Go 1.25+, `github.com/gorilla/websocket`, `os/exec`, `net/http`

---

### Task 1: Add configurable presence timeout to daemon

The presence handler hardcodes `pongTimeout = 20s` and `pingInterval = 10s`. E2E tests need fast timeouts (1-2s). Add `presence-timeout` as a config/env var.

**Files:**
- Modify: `pkg/config/config.go`
- Modify: `pkg/daemon/server.go`
- Modify: `pkg/daemon/presence_handler.go`
- Modify: `pkg/daemon/presence_handler_test.go`

**Step 1: Add viper default and binding in config.go**

In `pkg/config/config.go`, add to `InitViper()`:

```go
viper.SetDefault("presence-timeout", "20s")
```

Add to `BindFlags`:

```go
_ = viper.BindPFlag("presence-timeout", flags.Lookup("presence-timeout"))
```

Note: The env var `SHARKFIN_PRESENCE_TIMEOUT` works automatically via `viper.SetEnvPrefix("SHARKFIN")` + `viper.AutomaticEnv()`. Viper maps `presence-timeout` to `SHARKFIN_PRESENCE_TIMEOUT` (dashes become underscores, uppercased).

**Step 2: Make PresenceHandler accept timeout as a parameter**

In `pkg/daemon/presence_handler.go`:

Remove the constants:
```go
const (
	pingInterval = 10 * time.Second
	pongTimeout  = 20 * time.Second
)
```

Add fields to the struct:
```go
type PresenceHandler struct {
	sessions     *SessionManager
	pongTimeout  time.Duration
	pingInterval time.Duration
}
```

Update the constructor:
```go
func NewPresenceHandler(sessions *SessionManager, pongTimeout time.Duration) *PresenceHandler {
	pingInterval := pongTimeout / 2
	return &PresenceHandler{
		sessions:     sessions,
		pongTimeout:  pongTimeout,
		pingInterval: pingInterval,
	}
}
```

Update `ServeHTTP` to use `h.pongTimeout` instead of `pongTimeout` and `h.pingInterval` instead of `pingInterval`. The three places:
- `conn.SetReadDeadline(time.Now().Add(h.pongTimeout))` (line 53)
- Inside PongHandler: `conn.SetReadDeadline(time.Now().Add(h.pongTimeout))` (line 55)
- `ticker := time.NewTicker(h.pingInterval)` (line 72)
- `conn.SetWriteDeadline(time.Now().Add(h.pongTimeout))` (line 78, was hardcoded 10s)

**Step 3: Wire up in server.go**

In `pkg/daemon/server.go`, update `NewServer` signature and body:

```go
func NewServer(addr, dbPath string, allowChannelCreation bool, pongTimeout time.Duration) (*Server, error) {
```

Pass to handler:
```go
presenceHandler := NewPresenceHandler(sm, pongTimeout)
```

**Step 4: Read config in daemon command**

In `cmd/daemon/daemon.go`, in the `RunE` function, after reading `addr`:

```go
timeoutStr := viper.GetString("presence-timeout")
pongTimeout, err := time.ParseDuration(timeoutStr)
if err != nil {
	return fmt.Errorf("invalid presence-timeout %q: %w", timeoutStr, err)
}
```

Pass to `NewServer`:
```go
srv, err := pkgdaemon.NewServer(addr, dbPath, allowChannelCreation, pongTimeout)
```

**Step 5: Fix existing tests**

Update all callers of `NewServer` and `NewPresenceHandler` to pass the timeout:

In `pkg/daemon/presence_handler_test.go`, every call to `NewPresenceHandler(sm)` becomes:
```go
NewPresenceHandler(sm, 20*time.Second)
```

In `tests/integration_test.go`, the call to `pkgdaemon.NewServer(addr, ":memory:", allowChannelCreation)` becomes:
```go
pkgdaemon.NewServer(addr, ":memory:", allowChannelCreation, 20*time.Second)
```

**Step 6: Run all tests**

Run: `cd /home/kazw/Work/WorkFort/sharkfin && go test -race ./...`
Expected: All 57 tests pass.

**Step 7: Commit**

```bash
git add pkg/config/config.go pkg/daemon/server.go pkg/daemon/presence_handler.go pkg/daemon/presence_handler_test.go cmd/daemon/daemon.go tests/integration_test.go
git commit -m "feat: add configurable presence-timeout (env/config/default 20s)"
```

---

### Task 2: Create the e2e module and harness package skeleton

**Files:**
- Create: `tests/e2e/go.mod`
- Create: `tests/e2e/harness/harness.go`
- Create: `tests/e2e/sharkfin_test.go` (TestMain only)
- Modify: `.gitignore`
- Create: `tests/e2e/.gitignore`

**Step 1: Create go.mod**

```bash
mkdir -p /home/kazw/Work/WorkFort/sharkfin/tests/e2e/harness
```

Write `tests/e2e/go.mod`:
```
module github.com/Work-Fort/sharkfin-e2e

go 1.25.0

require github.com/gorilla/websocket v1.5.3
```

Run:
```bash
cd /home/kazw/Work/WorkFort/sharkfin/tests/e2e && go mod tidy
```

**Step 2: Write the harness skeleton**

Write `tests/e2e/harness/harness.go` with the three exported types and their constructors/methods. Start with stubs that compile but panic("not implemented"):

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package harness

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// --- Daemon ---

type daemonConfig struct {
	allowChannelCreation bool
	presenceTimeout      time.Duration
}

type DaemonOption func(*daemonConfig)

func WithAllowChannelCreation(allow bool) DaemonOption {
	return func(c *daemonConfig) { c.allowChannelCreation = allow }
}

func WithPresenceTimeout(d time.Duration) DaemonOption {
	return func(c *daemonConfig) { c.presenceTimeout = d }
}

type Daemon struct {
	cmd    *exec.Cmd
	addr   string
	xdgDir string
}

func StartDaemon(binary, addr string, opts ...DaemonOption) (*Daemon, error) {
	cfg := &daemonConfig{
		allowChannelCreation: true,
		presenceTimeout:      20 * time.Second,
	}
	for _, o := range opts {
		o(cfg)
	}

	xdgDir, err := os.MkdirTemp("", "sharkfin-e2e-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	args := []string{
		"daemon",
		"--daemon", addr,
		"--log-level", "disabled",
		fmt.Sprintf("--allow-channel-creation=%t", cfg.allowChannelCreation),
	}

	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+xdgDir+"/config",
		"XDG_STATE_HOME="+xdgDir+"/state",
		fmt.Sprintf("SHARKFIN_PRESENCE_TIMEOUT=%s", cfg.presenceTimeout),
	)
	cmd.Stdout = os.Stderr // forward daemon output for debugging
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		os.RemoveAll(xdgDir)
		return nil, fmt.Errorf("start daemon: %w", err)
	}

	// Wait for TCP ready
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return &Daemon{cmd: cmd, addr: addr, xdgDir: xdgDir}, nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	cmd.Process.Kill()
	cmd.Wait()
	os.RemoveAll(xdgDir)
	return nil, fmt.Errorf("daemon did not become ready on %s", addr)
}

func (d *Daemon) Addr() string { return d.addr }

func (d *Daemon) Stop() error {
	if d.cmd.Process == nil {
		return nil
	}
	d.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- d.cmd.Wait() }()
	select {
	case err := <-done:
		os.RemoveAll(d.xdgDir)
		return err
	case <-time.After(5 * time.Second):
		d.cmd.Process.Kill()
		<-done
		os.RemoveAll(d.xdgDir)
		return fmt.Errorf("daemon did not exit after SIGTERM")
	}
}

// --- Client ---

type ToolResult struct {
	Text  string
	Error *RPCError
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Client struct {
	addr      string
	sessionID string
	token     string
	wsConn    *websocket.Conn
	wsDone    chan struct{}
	mu        sync.Mutex
	nextID    int
}

func NewClient(daemonAddr string) *Client {
	return &Client{addr: daemonAddr, nextID: 1}
}

func (c *Client) ConnectPresence() error {
	wsURL := fmt.Sprintf("ws://%s/presence", c.addr)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial presence: %w", err)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("read token: %w", err)
	}
	c.token = string(msg)
	c.wsConn = conn
	c.wsDone = make(chan struct{})

	go func() {
		defer close(c.wsDone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	return nil
}

func (c *Client) DisconnectPresence() {
	if c.wsConn == nil {
		return
	}
	c.wsConn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	c.wsConn.Close()
	<-c.wsDone
	c.wsConn = nil
}

func (c *Client) Token() string    { return c.token }
func (c *Client) SessionID() string { return c.sessionID }

func (c *Client) allocID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	return id
}

// RawMCPRequest sends a JSON-RPC request and returns the raw result and error.
func (c *Client) RawMCPRequest(method string, id int, params any) (json.RawMessage, *RPCError, http.Header, error) {
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"id":      id,
	}
	if params != nil {
		reqBody["params"] = params
	}

	body, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("http://%s/mcp", c.addr)
	httpReq, err := http.NewRequest("POST", url, strings.NewReader(string(body)))
	if err != nil {
		return nil, nil, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", c.sessionID)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	// Capture session ID from response
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.sessionID = sid
	}

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, nil, resp.Header, fmt.Errorf("decode response: %w", err)
	}

	return rpcResp.Result, rpcResp.Error, resp.Header, nil
}

// RawPost sends a raw HTTP request and returns status code and body.
func (c *Client) RawPost(path, body string) (int, []byte, error) {
	url := fmt.Sprintf("http://%s%s", c.addr, path)
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := fmt.Fprintf(nil, "") // placeholder
	_ = b
	respBody := make([]byte, 0)
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		respBody = append(respBody, buf[:n]...)
		if err != nil {
			break
		}
	}
	return resp.StatusCode, respBody, nil
}

// RawGet sends a raw HTTP GET and returns status code.
func (c *Client) RawGet(path string) (int, error) {
	url := fmt.Sprintf("http://%s%s", c.addr, path)
	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

// Initialize sends the MCP initialize request.
func (c *Client) Initialize() error {
	id := c.allocID()
	_, rpcErr, _, err := c.RawMCPRequest("initialize", id, map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "e2e-test", "version": "0.1"},
	})
	if err != nil {
		return err
	}
	if rpcErr != nil {
		return fmt.Errorf("initialize error: %s", rpcErr.Message)
	}
	return nil
}

// ToolCall calls an MCP tool and returns the result.
func (c *Client) ToolCall(name string, args any) (ToolResult, error) {
	id := c.allocID()
	result, rpcErr, _, err := c.RawMCPRequest("tools/call", id, map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return ToolResult{}, err
	}
	if rpcErr != nil {
		return ToolResult{Error: rpcErr}, nil
	}

	var parsed struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return ToolResult{}, fmt.Errorf("unmarshal tool result: %w", err)
	}
	if len(parsed.Content) == 0 {
		return ToolResult{}, nil
	}
	return ToolResult{Text: parsed.Content[0].Text}, nil
}

// Register performs the register tool call using the client's token.
func (c *Client) Register(username, password string) error {
	r, err := c.ToolCall("register", map[string]any{
		"token": c.token, "username": username, "password": password,
	})
	if err != nil {
		return err
	}
	if r.Error != nil {
		return fmt.Errorf("register: %s", r.Error.Message)
	}
	return nil
}

// Identify performs the identify tool call using the client's token.
func (c *Client) Identify(username, password string) error {
	r, err := c.ToolCall("identify", map[string]any{
		"token": c.token, "username": username, "password": password,
	})
	if err != nil {
		return err
	}
	if r.Error != nil {
		return fmt.Errorf("identify: %s", r.Error.Message)
	}
	return nil
}

// RegisterFlow does ConnectPresence + Initialize + Register.
func (c *Client) RegisterFlow(username string) error {
	if err := c.ConnectPresence(); err != nil {
		return err
	}
	if err := c.Initialize(); err != nil {
		return err
	}
	return c.Register(username, "")
}

// IdentifyFlow does ConnectPresence + Initialize + Identify.
func (c *Client) IdentifyFlow(username string) error {
	if err := c.ConnectPresence(); err != nil {
		return err
	}
	if err := c.Initialize(); err != nil {
		return err
	}
	return c.Identify(username, "")
}

// --- Bridge ---

type Bridge struct {
	cmd    *exec.Cmd
	stdin  *json.Encoder
	stdout *bufio.Scanner
	xdgDir string
}

func StartBridge(binary, daemonAddr, xdgDir string) (*Bridge, error) {
	cmd := exec.Command(binary,
		"mcp-bridge",
		"--daemon", daemonAddr,
		"--log-level", "disabled",
	)
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+xdgDir+"/config",
		"XDG_STATE_HOME="+xdgDir+"/state",
	)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start bridge: %w", err)
	}

	// Give bridge a moment to connect presence
	time.Sleep(500 * time.Millisecond)

	return &Bridge{
		cmd:    cmd,
		stdin:  json.NewEncoder(stdinPipe),
		stdout: bufio.NewScanner(stdoutPipe),
		xdgDir: xdgDir,
	}, nil
}

// Send writes a JSON-RPC request to bridge stdin and reads the response line.
func (b *Bridge) Send(request any) (json.RawMessage, error) {
	if err := b.stdin.Encode(request); err != nil {
		return nil, fmt.Errorf("write to bridge: %w", err)
	}
	if !b.stdout.Scan() {
		if err := b.stdout.Err(); err != nil {
			return nil, fmt.Errorf("read from bridge: %w", err)
		}
		return nil, fmt.Errorf("bridge closed stdout")
	}
	return json.RawMessage(b.stdout.Bytes()), nil
}

func (b *Bridge) Stop() error {
	if b.cmd.Process == nil {
		return nil
	}
	b.cmd.Process.Signal(syscall.SIGTERM)
	return b.cmd.Wait()
}

func (b *Bridge) Kill() error {
	if b.cmd.Process == nil {
		return nil
	}
	return b.cmd.Process.Kill()
}

// --- Helpers ---

// FreePort returns a free TCP port on localhost.
func FreePort() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr, nil
}
```

**Step 3: Write TestMain**

Write `tests/e2e/sharkfin_test.go`:

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var sharkfinBin string

func TestMain(m *testing.M) {
	// Build the sharkfin binary
	tmpDir, err := os.MkdirTemp("", "sharkfin-e2e-bin-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	binPath := filepath.Join(tmpDir, "sharkfin")
	cmd := exec.Command("go", "build", "-o", binPath, "../../")
	cmd.Dir = "."
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build sharkfin: %v\n", err)
		os.Exit(1)
	}

	sharkfinBin = binPath
	os.Exit(m.Run())
}
```

**Step 4: Update .gitignore**

Append to `.gitignore`:
```
tests/e2e/testbin/
```

Write `tests/e2e/.gitignore`:
```
testbin/
```

**Step 5: Verify it compiles**

Run:
```bash
cd /home/kazw/Work/WorkFort/sharkfin/tests/e2e && go build ./...
```
Expected: compiles with no errors.

**Step 6: Commit**

```bash
cd /home/kazw/Work/WorkFort/sharkfin
git add tests/e2e/ .gitignore
git commit -m "feat: add e2e test harness skeleton with Daemon, Client, Bridge types"
```

---

### Task 3: Presence tests (4 tests)

**Files:**
- Modify: `tests/e2e/sharkfin_test.go`

**Step 1: Write TestPresenceConnect**

```go
func TestPresenceConnect(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	c := harness.NewClient(addr)
	if err := c.ConnectPresence(); err != nil {
		t.Fatalf("connect presence: %v", err)
	}
	defer c.DisconnectPresence()

	token := c.Token()
	if len(token) != 64 {
		t.Errorf("token length = %d, want 64 hex chars", len(token))
	}
}
```

**Step 2: Write TestPresenceDisconnectMarksOffline**

```go
func TestPresenceDisconnectMarksOffline(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	// Register alice
	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}

	// Verify online
	r, err := alice.ToolCall("user_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Text, `"online":true`) {
		t.Fatalf("expected alice online, got: %s", r.Text)
	}

	// Disconnect
	alice.DisconnectPresence()
	time.Sleep(200 * time.Millisecond)

	// New client to check (alice's session is gone after disconnect)
	checker := harness.NewClient(addr)
	if err := checker.RegisterFlow("bob"); err != nil {
		t.Fatal(err)
	}
	defer checker.DisconnectPresence()

	r, err = checker.ToolCall("user_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}

	var users []struct {
		Username string `json:"username"`
		Online   bool   `json:"online"`
	}
	json.Unmarshal([]byte(r.Text), &users)

	for _, u := range users {
		if u.Username == "alice" && u.Online {
			t.Error("alice should be offline after disconnect")
		}
	}
}
```

**Step 3: Write TestPresenceRejectsPlainHTTP**

```go
func TestPresenceRejectsPlainHTTP(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	// Plain HTTP GET (not a WebSocket upgrade)
	resp, err := http.Get(fmt.Sprintf("http://%s/presence", addr))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusSwitchingProtocols || resp.StatusCode == http.StatusOK {
		t.Errorf("expected rejection, got %d", resp.StatusCode)
	}
}
```

**Step 4: Write TestPresenceHardKillMarksOffline**

```go
func TestPresenceHardKillMarksOffline(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr,
		harness.WithPresenceTimeout(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	// Start a bridge, register through it
	bridge, err := harness.StartBridge(sharkfinBin, addr, d.XDGDir())
	if err != nil {
		t.Fatal(err)
	}

	// Initialize through bridge
	initReq := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]string{"name": "test", "version": "0.1"},
		},
	}
	if _, err := bridge.Send(initReq); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	// get_identity_token through bridge (intercepted locally)
	tokenReq := map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{
			"name": "get_identity_token", "arguments": map[string]any{},
		},
	}
	tokenResp, err := bridge.Send(tokenReq)
	if err != nil {
		t.Fatalf("get_identity_token: %v", err)
	}

	// Parse token from response
	var tokenResult struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	json.Unmarshal(tokenResp, &tokenResult)
	token := tokenResult.Result.Content[0].Text

	// Register through bridge
	regReq := map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{
			"name": "register",
			"arguments": map[string]any{
				"token": token, "username": "alice", "password": "",
			},
		},
	}
	if _, err := bridge.Send(regReq); err != nil {
		t.Fatalf("register: %v", err)
	}

	// SIGKILL the bridge (no clean shutdown)
	bridge.Kill()

	// Wait for pong timeout (2s) + margin
	time.Sleep(4 * time.Second)

	// Check with a new client
	checker := harness.NewClient(addr)
	if err := checker.RegisterFlow("bob"); err != nil {
		t.Fatal(err)
	}
	defer checker.DisconnectPresence()

	r, err := checker.ToolCall("user_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var users []struct {
		Username string `json:"username"`
		Online   bool   `json:"online"`
	}
	json.Unmarshal([]byte(r.Text), &users)
	for _, u := range users {
		if u.Username == "alice" && u.Online {
			t.Error("alice should be offline after bridge SIGKILL")
		}
	}
}
```

Note: The `Daemon` needs to expose `XDGDir()` so the bridge can share the same env. Add to harness:
```go
func (d *Daemon) XDGDir() string { return d.xdgDir }
```

**Step 5: Run tests**

Run: `cd /home/kazw/Work/WorkFort/sharkfin/tests/e2e && go test -v -race -run TestPresence -timeout 30s`
Expected: 4 tests pass.

**Step 6: Commit**

```bash
cd /home/kazw/Work/WorkFort/sharkfin
git add tests/e2e/
git commit -m "test: add e2e presence tests (connect, disconnect, plain HTTP, hard kill)"
```

---

### Task 4: Identity tests (6 tests)

**Files:**
- Modify: `tests/e2e/sharkfin_test.go`

**Step 1: Write all 6 identity tests**

```go
func TestRegisterAndIdentify(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	// Register alice
	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	alice.DisconnectPresence()
	time.Sleep(100 * time.Millisecond)

	// Re-identify as alice
	alice2 := harness.NewClient(addr)
	if err := alice2.IdentifyFlow("alice"); err != nil {
		t.Fatalf("identify: %v", err)
	}
	defer alice2.DisconnectPresence()
}

func TestDoubleRegisterFails(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	// Try register again on same session
	r, err := alice.ToolCall("register", map[string]any{
		"token": "fake", "username": "bob", "password": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error == nil {
		t.Error("expected error on second register")
	}
}

func TestIdentifyAfterRegisterFails(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	r, err := alice.ToolCall("identify", map[string]any{
		"token": "fake", "username": "alice", "password": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error == nil {
		t.Error("expected error: already identified")
	}
}

func TestDoubleLoginPrevention(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	// Second client tries to identify as alice (while alice is online)
	alice2 := harness.NewClient(addr)
	err = alice2.IdentifyFlow("alice")
	if err == nil {
		alice2.DisconnectPresence()
		t.Fatal("expected error: user already online")
	}
	if !strings.Contains(err.Error(), "already online") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegisterDuplicateUsername(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	// Second client tries to register as "alice" (duplicate username)
	alice2 := harness.NewClient(addr)
	err = alice2.RegisterFlow("alice")
	if err == nil {
		alice2.DisconnectPresence()
		t.Fatal("expected error: duplicate username or already online")
	}
}

func TestIdentifyNonexistentUser(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	c := harness.NewClient(addr)
	err = c.IdentifyFlow("nonexistent")
	if err == nil {
		c.DisconnectPresence()
		t.Fatal("expected error: user not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

**Step 2: Run tests**

Run: `cd /home/kazw/Work/WorkFort/sharkfin/tests/e2e && go test -v -race -run "TestRegister|TestIdentify|TestDouble" -timeout 30s`
Expected: 6 tests pass.

**Step 3: Commit**

```bash
cd /home/kazw/Work/WorkFort/sharkfin
git add tests/e2e/
git commit -m "test: add e2e identity tests (register, identify, double-login, duplicates)"
```

---

### Task 5: MCP protocol tests (7 tests)

**Files:**
- Modify: `tests/e2e/sharkfin_test.go`

**Step 1: Write all 7 protocol tests**

```go
func TestInitializeResponse(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	c := harness.NewClient(addr)
	result, rpcErr, _, err := c.RawMCPRequest("initialize", 1, map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "test", "version": "0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rpcErr != nil {
		t.Fatalf("rpc error: %s", rpcErr.Message)
	}

	var parsed struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
		Capabilities struct {
			Tools map[string]any `json:"tools"`
		} `json:"capabilities"`
	}
	json.Unmarshal(result, &parsed)

	if parsed.ProtocolVersion != "2025-03-26" {
		t.Errorf("protocolVersion = %q, want 2025-03-26", parsed.ProtocolVersion)
	}
	if parsed.ServerInfo.Name != "sharkfin" {
		t.Errorf("serverInfo.name = %q, want sharkfin", parsed.ServerInfo.Name)
	}
	if c.SessionID() == "" {
		t.Error("expected Mcp-Session-Id to be set")
	}
}

func TestToolsList(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	c := harness.NewClient(addr)
	result, rpcErr, _, err := c.RawMCPRequest("tools/list", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rpcErr != nil {
		t.Fatalf("rpc error: %s", rpcErr.Message)
	}

	var parsed struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	json.Unmarshal(result, &parsed)

	expected := map[string]bool{
		"register": true, "identify": true, "user_list": true,
		"channel_list": true, "channel_create": true, "channel_invite": true,
		"send_message": true, "unread_messages": true,
	}
	if len(parsed.Tools) != 8 {
		t.Errorf("got %d tools, want 8", len(parsed.Tools))
	}
	for _, tool := range parsed.Tools {
		if !expected[tool.Name] {
			t.Errorf("unexpected tool: %s", tool.Name)
		}
	}
}

func TestToolCallBeforeIdentify(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	c := harness.NewClient(addr)
	c.Initialize()

	r, err := c.ToolCall("user_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error == nil {
		t.Error("expected error: not identified")
	}
	if !strings.Contains(r.Error.Message, "not identified") {
		t.Errorf("unexpected error: %s", r.Error.Message)
	}
}

func TestUnknownMethod(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	c := harness.NewClient(addr)
	_, rpcErr, _, err := c.RawMCPRequest("nonexistent/method", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rpcErr == nil {
		t.Error("expected error for unknown method")
	}
}

func TestUnknownTool(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	c := harness.NewClient(addr)
	r, err := c.ToolCall("nonexistent_tool", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestInvalidJSON(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	c := harness.NewClient(addr)
	status, _, err := c.RawPost("/mcp", "this is not json{{{")
	if err != nil {
		t.Fatal(err)
	}
	// Should get 200 with a JSON-RPC parse error, not a 4xx
	if status != 200 {
		t.Logf("status = %d (server may return non-200 for parse errors, that's acceptable)", status)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	c := harness.NewClient(addr)
	status, err := c.RawGet("/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusMethodNotAllowed {
		t.Errorf("GET /mcp status = %d, want 405", status)
	}
}
```

**Step 2: Run tests**

Run: `cd /home/kazw/Work/WorkFort/sharkfin/tests/e2e && go test -v -race -run "TestInitialize|TestTools|TestToolCall|TestUnknown|TestInvalid|TestMethod" -timeout 30s`
Expected: 7 tests pass.

**Step 3: Commit**

```bash
cd /home/kazw/Work/WorkFort/sharkfin
git add tests/e2e/
git commit -m "test: add e2e MCP protocol tests (initialize, tools/list, errors)"
```

---

### Task 6: Channel tests (6 tests)

**Files:**
- Modify: `tests/e2e/sharkfin_test.go`

**Step 1: Write all 6 channel tests**

```go
func TestChannelCreateAndList(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	r, err := alice.ToolCall("channel_create", map[string]any{
		"name": "general", "public": true, "members": []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("create: %s", r.Error.Message)
	}

	r, err = alice.ToolCall("channel_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}

	var channels []struct {
		Name   string `json:"name"`
		Public bool   `json:"public"`
	}
	json.Unmarshal([]byte(r.Text), &channels)
	if len(channels) != 1 || channels[0].Name != "general" || !channels[0].Public {
		t.Fatalf("expected [general, public], got: %v", channels)
	}
}

func TestPrivateChannelVisibility(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	bob := harness.NewClient(addr)
	if err := bob.RegisterFlow("bob"); err != nil {
		t.Fatal(err)
	}
	defer bob.DisconnectPresence()

	charlie := harness.NewClient(addr)
	if err := charlie.RegisterFlow("charlie"); err != nil {
		t.Fatal(err)
	}
	defer charlie.DisconnectPresence()

	// Alice creates private channel with bob
	alice.ToolCall("channel_create", map[string]any{
		"name": "secret", "public": false, "members": []string{"bob"},
	})

	// Charlie should NOT see it
	r, _ := charlie.ToolCall("channel_list", map[string]any{})
	if strings.Contains(r.Text, "secret") {
		t.Error("charlie should not see private channel")
	}

	// Bob should see it
	r, _ = bob.ToolCall("channel_list", map[string]any{})
	if !strings.Contains(r.Text, "secret") {
		t.Error("bob should see private channel")
	}
}

func TestPublicChannelVisibleToAll(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	bob := harness.NewClient(addr)
	if err := bob.RegisterFlow("bob"); err != nil {
		t.Fatal(err)
	}
	defer bob.DisconnectPresence()

	alice.ToolCall("channel_create", map[string]any{
		"name": "public-room", "public": true, "members": []string{},
	})

	// Bob (non-member) can see it
	r, _ := bob.ToolCall("channel_list", map[string]any{})
	if !strings.Contains(r.Text, "public-room") {
		t.Error("bob should see public channel")
	}
}

func TestChannelCreationDisabled(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr,
		harness.WithAllowChannelCreation(false))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	r, err := alice.ToolCall("channel_create", map[string]any{
		"name": "test", "public": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error == nil {
		t.Error("expected error: channel creation disabled")
	}
	if r.Error.Message != "channel creation is disabled" {
		t.Errorf("unexpected error: %s", r.Error.Message)
	}
}

func TestChannelInvite(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	bob := harness.NewClient(addr)
	if err := bob.RegisterFlow("bob"); err != nil {
		t.Fatal(err)
	}
	defer bob.DisconnectPresence()

	charlie := harness.NewClient(addr)
	if err := charlie.RegisterFlow("charlie"); err != nil {
		t.Fatal(err)
	}
	defer charlie.DisconnectPresence()

	// Alice creates private channel with bob
	alice.ToolCall("channel_create", map[string]any{
		"name": "project", "public": false, "members": []string{"bob"},
	})

	// Bob invites charlie
	r, _ := bob.ToolCall("channel_invite", map[string]any{
		"channel": "project", "username": "charlie",
	})
	if r.Error != nil {
		t.Fatalf("invite: %s", r.Error.Message)
	}

	// Charlie can now see and send
	r, _ = charlie.ToolCall("channel_list", map[string]any{})
	if !strings.Contains(r.Text, "project") {
		t.Error("charlie should see channel after invite")
	}

	r, _ = charlie.ToolCall("send_message", map[string]any{
		"channel": "project", "message": "hello!",
	})
	if r.Error != nil {
		t.Fatalf("charlie send: %s", r.Error.Message)
	}
}

func TestChannelInviteByNonMember(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	bob := harness.NewClient(addr)
	if err := bob.RegisterFlow("bob"); err != nil {
		t.Fatal(err)
	}
	defer bob.DisconnectPresence()

	charlie := harness.NewClient(addr)
	if err := charlie.RegisterFlow("charlie"); err != nil {
		t.Fatal(err)
	}
	defer charlie.DisconnectPresence()

	// Alice creates private channel (only alice)
	alice.ToolCall("channel_create", map[string]any{
		"name": "secret", "public": false, "members": []string{},
	})

	// Bob (not a member) tries to invite charlie
	r, _ := bob.ToolCall("channel_invite", map[string]any{
		"channel": "secret", "username": "charlie",
	})
	if r.Error == nil {
		t.Error("expected error: bob is not a participant")
	}
}
```

**Step 2: Run tests**

Run: `cd /home/kazw/Work/WorkFort/sharkfin/tests/e2e && go test -v -race -run "TestChannel|TestPublic|TestPrivate" -timeout 30s`
Expected: 6 tests pass.

**Step 3: Commit**

```bash
cd /home/kazw/Work/WorkFort/sharkfin
git add tests/e2e/
git commit -m "test: add e2e channel tests (create, visibility, invite, non-member)"
```

---

### Task 7: Messaging tests (6 tests)

**Files:**
- Modify: `tests/e2e/sharkfin_test.go`

**Step 1: Write all 6 messaging tests**

```go
func TestSendAndReceiveMessage(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	bob := harness.NewClient(addr)
	if err := bob.RegisterFlow("bob"); err != nil {
		t.Fatal(err)
	}
	defer bob.DisconnectPresence()

	alice.ToolCall("channel_create", map[string]any{
		"name": "chat", "public": false, "members": []string{"bob"},
	})

	alice.ToolCall("send_message", map[string]any{
		"channel": "chat", "message": "hello bob!",
	})

	r, _ := bob.ToolCall("unread_messages", map[string]any{})
	var msgs []struct {
		Channel string `json:"channel"`
		From    string `json:"from"`
		Body    string `json:"body"`
	}
	json.Unmarshal([]byte(r.Text), &msgs)

	if len(msgs) != 1 || msgs[0].From != "alice" || msgs[0].Body != "hello bob!" || msgs[0].Channel != "chat" {
		t.Fatalf("expected 1 message from alice, got: %v", msgs)
	}
}

func TestUnreadMessagesAreConsumed(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	bob := harness.NewClient(addr)
	if err := bob.RegisterFlow("bob"); err != nil {
		t.Fatal(err)
	}
	defer bob.DisconnectPresence()

	alice.ToolCall("channel_create", map[string]any{
		"name": "dm", "public": false, "members": []string{"bob"},
	})
	alice.ToolCall("send_message", map[string]any{
		"channel": "dm", "message": "first",
	})

	// Read once
	r, _ := bob.ToolCall("unread_messages", map[string]any{})
	var msgs []struct{ Body string `json:"body"` }
	json.Unmarshal([]byte(r.Text), &msgs)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	// Read again — empty
	r, _ = bob.ToolCall("unread_messages", map[string]any{})
	if r.Text != "null" && r.Text != "[]" {
		var msgs2 []any
		json.Unmarshal([]byte(r.Text), &msgs2)
		if len(msgs2) != 0 {
			t.Fatalf("expected no messages, got: %s", r.Text)
		}
	}

	// New message arrives
	alice.ToolCall("send_message", map[string]any{
		"channel": "dm", "message": "second",
	})
	r, _ = bob.ToolCall("unread_messages", map[string]any{})
	json.Unmarshal([]byte(r.Text), &msgs)
	if len(msgs) != 1 || msgs[0].Body != "second" {
		t.Fatalf("expected 'second', got: %v", msgs)
	}
}

func TestUnreadFilterByChannel(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	bob := harness.NewClient(addr)
	if err := bob.RegisterFlow("bob"); err != nil {
		t.Fatal(err)
	}
	defer bob.DisconnectPresence()

	// Two channels
	alice.ToolCall("channel_create", map[string]any{
		"name": "ch1", "public": false, "members": []string{"bob"},
	})
	alice.ToolCall("channel_create", map[string]any{
		"name": "ch2", "public": false, "members": []string{"bob"},
	})

	alice.ToolCall("send_message", map[string]any{"channel": "ch1", "message": "in ch1"})
	alice.ToolCall("send_message", map[string]any{"channel": "ch2", "message": "in ch2"})

	// Filter by ch1
	r, _ := bob.ToolCall("unread_messages", map[string]any{"channel": "ch1"})
	var msgs []struct {
		Channel string `json:"channel"`
		Body    string `json:"body"`
	}
	json.Unmarshal([]byte(r.Text), &msgs)
	if len(msgs) != 1 || msgs[0].Channel != "ch1" {
		t.Fatalf("expected 1 message in ch1, got: %v", msgs)
	}

	// ch2 still unread
	r, _ = bob.ToolCall("unread_messages", map[string]any{"channel": "ch2"})
	json.Unmarshal([]byte(r.Text), &msgs)
	if len(msgs) != 1 || msgs[0].Channel != "ch2" {
		t.Fatalf("expected 1 message in ch2, got: %v", msgs)
	}
}

func TestSendToNonexistentChannel(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	r, _ := alice.ToolCall("send_message", map[string]any{
		"channel": "doesnt-exist", "message": "hello",
	})
	if r.Error == nil {
		t.Error("expected error for nonexistent channel")
	}
}

func TestNonParticipantCannotSend(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	bob := harness.NewClient(addr)
	if err := bob.RegisterFlow("bob"); err != nil {
		t.Fatal(err)
	}
	defer bob.DisconnectPresence()

	// Alice creates private channel (only alice)
	alice.ToolCall("channel_create", map[string]any{
		"name": "private", "public": false, "members": []string{},
	})

	// Bob tries to send
	r, _ := bob.ToolCall("send_message", map[string]any{
		"channel": "private", "message": "sneaky",
	})
	if r.Error == nil {
		t.Error("expected error: not a participant")
	}
	if r.Error.Message != "you are not a participant of this channel" {
		t.Errorf("unexpected error: %s", r.Error.Message)
	}
}

func TestMultipleMessagesOrdering(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	bob := harness.NewClient(addr)
	if err := bob.RegisterFlow("bob"); err != nil {
		t.Fatal(err)
	}
	defer bob.DisconnectPresence()

	alice.ToolCall("channel_create", map[string]any{
		"name": "chat", "public": false, "members": []string{"bob"},
	})

	// Send 5 messages
	for i := 0; i < 5; i++ {
		alice.ToolCall("send_message", map[string]any{
			"channel": "chat", "message": fmt.Sprintf("msg-%d", i),
		})
	}

	r, _ := bob.ToolCall("unread_messages", map[string]any{})
	var msgs []struct{ Body string `json:"body"` }
	json.Unmarshal([]byte(r.Text), &msgs)

	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(msgs))
	}
	for i, m := range msgs {
		expected := fmt.Sprintf("msg-%d", i)
		if m.Body != expected {
			t.Errorf("message %d = %q, want %q", i, m.Body, expected)
		}
	}
}
```

**Step 2: Run tests**

Run: `cd /home/kazw/Work/WorkFort/sharkfin/tests/e2e && go test -v -race -run "TestSend|TestUnread|TestNonParticipant|TestMultiple" -timeout 30s`
Expected: 6 tests pass.

**Step 3: Commit**

```bash
cd /home/kazw/Work/WorkFort/sharkfin
git add tests/e2e/
git commit -m "test: add e2e messaging tests (send, unread, filter, ordering, errors)"
```

---

### Task 8: Integration tests (bridge smoke + presence exit)

**Files:**
- Modify: `tests/e2e/sharkfin_test.go`

**Step 1: Write TestBridgeEndToEnd**

```go
func TestBridgeEndToEnd(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	bridge, err := harness.StartBridge(sharkfinBin, addr, d.XDGDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Stop()

	// 1. Initialize
	initResp, err := bridge.Send(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]string{"name": "bridge-test", "version": "0.1"},
		},
	})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}

	var initResult struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
		Error *harness.RPCError `json:"error"`
	}
	json.Unmarshal(initResp, &initResult)
	if initResult.Error != nil {
		t.Fatalf("init error: %s", initResult.Error.Message)
	}
	if initResult.Result.ProtocolVersion != "2025-03-26" {
		t.Errorf("protocol = %q", initResult.Result.ProtocolVersion)
	}

	// 2. get_identity_token (intercepted by bridge)
	tokenResp, err := bridge.Send(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{
			"name": "get_identity_token", "arguments": map[string]any{},
		},
	})
	if err != nil {
		t.Fatalf("get_identity_token: %v", err)
	}
	var tokenResult struct {
		Result struct {
			Content []struct{ Text string `json:"text"` } `json:"content"`
		} `json:"result"`
	}
	json.Unmarshal(tokenResp, &tokenResult)
	token := tokenResult.Result.Content[0].Text
	if len(token) != 64 {
		t.Errorf("token length = %d, want 64", len(token))
	}

	// 3. Register
	regResp, err := bridge.Send(map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{
			"name": "register",
			"arguments": map[string]any{
				"token": token, "username": "bridge-alice", "password": "",
			},
		},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	var regResult struct {
		Error *harness.RPCError `json:"error"`
	}
	json.Unmarshal(regResp, &regResult)
	if regResult.Error != nil {
		t.Fatalf("register error: %s", regResult.Error.Message)
	}

	// 4. Create channel + send message
	bridge.Send(map[string]any{
		"jsonrpc": "2.0", "id": 4, "method": "tools/call",
		"params": map[string]any{
			"name": "channel_create",
			"arguments": map[string]any{
				"name": "bridge-ch", "public": true, "members": []string{},
			},
		},
	})

	sendResp, err := bridge.Send(map[string]any{
		"jsonrpc": "2.0", "id": 5, "method": "tools/call",
		"params": map[string]any{
			"name": "send_message",
			"arguments": map[string]any{
				"channel": "bridge-ch", "message": "from bridge",
			},
		},
	})
	if err != nil {
		t.Fatalf("send_message: %v", err)
	}
	var sendResult struct {
		Error *harness.RPCError `json:"error"`
	}
	json.Unmarshal(sendResp, &sendResult)
	if sendResult.Error != nil {
		t.Fatalf("send error: %s", sendResult.Error.Message)
	}
}
```

**Step 2: Write TestPresenceExitsOnDaemonRestart**

```go
func TestPresenceExitsOnDaemonRestart(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}

	// Start daemon
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}

	// Start presence subprocess
	xdgDir := d.XDGDir()
	presenceCmd := exec.Command(sharkfinBin,
		"presence",
		"--daemon", addr,
		"--log-level", "disabled",
	)
	presenceCmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+xdgDir+"/config",
		"XDG_STATE_HOME="+xdgDir+"/state",
	)
	if err := presenceCmd.Start(); err != nil {
		d.Stop()
		t.Fatal(err)
	}

	// Give presence time to connect
	time.Sleep(500 * time.Millisecond)

	// Stop daemon
	d.Stop()

	// Presence should exit within a few seconds
	done := make(chan error, 1)
	go func() { done <- presenceCmd.Wait() }()
	select {
	case <-done:
		// Good — process exited
	case <-time.After(10 * time.Second):
		presenceCmd.Process.Kill()
		t.Fatal("presence did not exit after daemon stopped")
	}

	// Restart daemon on same address — should work
	d2, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatalf("restart daemon: %v", err)
	}
	defer d2.Stop()

	// New client can connect
	c := harness.NewClient(addr)
	if err := c.ConnectPresence(); err != nil {
		t.Fatalf("new presence after restart: %v", err)
	}
	c.DisconnectPresence()
}
```

**Step 3: Run all tests**

Run: `cd /home/kazw/Work/WorkFort/sharkfin/tests/e2e && go test -v -race -timeout 120s`
Expected: All 28 tests pass.

**Step 4: Commit**

```bash
cd /home/kazw/Work/WorkFort/sharkfin
git add tests/e2e/
git commit -m "test: add e2e bridge smoke test and presence-exits-on-restart test"
```

---

### Task 9: Add Taskfile e2e target and update existing tests

**Files:**
- Modify: `Taskfile.dist.yaml`
- Modify: `tests/integration_test.go` (if compile broke from Task 1)

**Step 1: Add e2e task to Taskfile**

Add to `Taskfile.dist.yaml`:

```yaml
  e2e:
    desc: Run end-to-end tests
    cmds:
      - cd tests/e2e && go test -v -race -timeout 120s
```

**Step 2: Run full CI**

Run: `cd /home/kazw/Work/WorkFort/sharkfin && task ci`
Expected: All existing tests pass (lint + unit + integration).

Run: `cd /home/kazw/Work/WorkFort/sharkfin && task e2e`
Expected: All 28 e2e tests pass.

**Step 3: Commit**

```bash
cd /home/kazw/Work/WorkFort/sharkfin
git add Taskfile.dist.yaml
git commit -m "feat: add 'task e2e' target for end-to-end tests"
```

---

### Task 10: Final verification

**Step 1: Run everything from clean state**

```bash
cd /home/kazw/Work/WorkFort/sharkfin
task ci
task e2e
```

Expected: All green.

**Step 2: Verify gitignore works**

```bash
git status
```

Expected: No untracked files in `tests/e2e/testbin/` or temp directories.

**Step 3: Verify XDG isolation**

Check that `~/.config/sharkfin` and `~/.local/state/sharkfin` were NOT touched by tests:
```bash
ls -la ~/.config/sharkfin/ ~/.local/state/sharkfin/ 2>&1
```

Expected: Either doesn't exist or has no files modified during the test run.
