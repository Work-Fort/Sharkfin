# Bridge StreamableHTTP Robustness Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the MCP bridge to correctly handle all StreamableHTTP response types — SSE streams, 202 notification acknowledgments, and standard JSON — so the bridge works reliably with mcp-go's `StreamableHTTPServer`.

**Architecture:** Extract a `readResponseBody` helper that inspects `Content-Type` and status code to dispatch to the right reader (JSON `io.ReadAll`, SSE `data:` line parser, or 202 no-op). Use it in both `processStdin` (stdout forwarding) and `callUnreadMessages` (internal tool call). Unit test the helper with a mock HTTP server; e2e test with `notifications/initialized` through the real bridge.

**Tech Stack:** Go, mcp-go v0.44.1 (StreamableHTTP), net/http/httptest

**Bugs being fixed:**
1. **SSE responses:** `io.ReadAll` returns raw SSE event framing (`event: message\ndata: {...}\n\n`) which gets written to stdout as-is, causing the client to receive non-JSON lines instead of the actual JSON payload.
2. **202 notification responses:** Notifications like `notifications/initialized` return 202 with no body. The bridge writes an empty line to stdout, which the client may read as a malformed response.

---

### Task 1: Add readResponseBody helper with unit tests

**Files:**
- Modify: `cmd/mcpbridge/mcp_bridge.go` (add helper function + imports)
- Create: `cmd/mcpbridge/mcp_bridge_test.go`

- [ ] **Step 1: Write failing tests for readResponseBody**

Create `cmd/mcpbridge/mcp_bridge_test.go`:

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package mcpbridge

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadResponseBody_JSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)

	messages, err := readResponseBody(resp)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.JSONEq(t, `{"jsonrpc":"2.0","id":1,"result":{}}`, string(messages[0]))
}

func TestReadResponseBody_202(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)

	messages, err := readResponseBody(resp)
	require.NoError(t, err)
	require.Nil(t, messages)
}

func TestReadResponseBody_SSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n"))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)

	messages, err := readResponseBody(resp)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.JSONEq(t, `{"jsonrpc":"2.0","id":1,"result":{}}`, string(messages[0]))
}

func TestReadResponseBody_SSEMultipleMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(
			"event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"progress\"}\n\n" +
				"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n",
		))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)

	messages, err := readResponseBody(resp)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	require.Contains(t, string(messages[0]), "progress")
	require.Contains(t, string(messages[1]), "result")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/mcpbridge/ -run TestReadResponseBody -v`
Expected: FAIL — `readResponseBody` undefined

- [ ] **Step 3: Implement readResponseBody**

Add to `cmd/mcpbridge/mcp_bridge.go`:

```go
// readResponseBody reads the HTTP response body, handling JSON, SSE, and 202.
// Returns nil for 202 (notification accepted, no body).
// For SSE, returns each data: line as a separate message.
// For JSON, returns the body as a single-element slice.
func readResponseBody(resp *http.Response) ([][]byte, error) {
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusAccepted {
		return nil, nil
	}

	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/event-stream") {
		var messages [][]byte
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				messages = append(messages, []byte(strings.TrimPrefix(line, "data: ")))
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read SSE: %w", err)
		}
		return messages, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return [][]byte{body}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/mcpbridge/ -run TestReadResponseBody -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/mcpbridge/mcp_bridge.go cmd/mcpbridge/mcp_bridge_test.go
git commit -m "feat: add readResponseBody helper for StreamableHTTP response handling"
```

---

### Task 2: Use readResponseBody in processStdin and callUnreadMessages

**Files:**
- Modify: `cmd/mcpbridge/mcp_bridge.go:110-154` (processStdin)
- Modify: `cmd/mcpbridge/mcp_bridge.go:265-316` (callUnreadMessages)

- [ ] **Step 1: Update processStdin to use readResponseBody**

Replace `processStdin` with:

```go
func (b *bridge) processStdin() error {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if b.interceptGetIdentityToken(line) {
			continue
		}

		if b.interceptWaitForMessages(line) {
			continue
		}

		req, err := http.NewRequest("POST", b.mcpURL, strings.NewReader(line))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if b.sessionID != "" {
			req.Header.Set("Mcp-Session-Id", b.sessionID)
		}

		resp, err := b.client.Do(req)
		if err != nil {
			return fmt.Errorf("forward request: %w", err)
		}

		if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
			b.sessionID = sid
		}

		messages, err := readResponseBody(resp)
		if err != nil {
			return fmt.Errorf("read response: %w", err)
		}

		for _, msg := range messages {
			os.Stdout.Write(bytes.TrimRight(msg, "\n"))
			os.Stdout.Write([]byte("\n"))
		}
	}
	return scanner.Err()
}
```

`readResponseBody` closes `resp.Body` via defer, so no explicit `resp.Body.Close()` needed.

- [ ] **Step 2: Update callUnreadMessages to use readResponseBody**

Replace `callUnreadMessages` with:

```go
func (b *bridge) callUnreadMessages() (string, error) {
	rpcReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "unread_messages",
		},
	}
	body, err := json.Marshal(rpcReq)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", b.mcpURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if b.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", b.sessionID)
	}

	resp, err := b.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("forward request: %w", err)
	}

	messages, err := readResponseBody(resp)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if len(messages) == 0 {
		return "", fmt.Errorf("no response body")
	}

	// Use the last message (the actual response, skipping any intermediate notifications).
	respBody := messages[len(messages)-1]

	var rpcResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if len(rpcResp.Result.Content) > 0 {
		return rpcResp.Result.Content[0].Text, nil
	}
	return "[]", nil
}
```

- [ ] **Step 3: Verify imports**

After the refactor, confirm all imports are still used. `io` is needed by `readResponseBody`, `bytes` by `callUnreadMessages` and `processStdin`, `bufio` by both `processStdin` (stdin scanner) and `readResponseBody` (SSE scanner). The `context` import is used by `startPresence`.

- [ ] **Step 4: Run unit tests**

Run: `go test ./cmd/mcpbridge/ -v`
Expected: PASS

- [ ] **Step 5: Run full unit test suite**

Run: `mise run test`
Expected: All pass

- [ ] **Step 6: Commit**

```bash
git add cmd/mcpbridge/mcp_bridge.go
git commit -m "fix: handle SSE and 202 responses in MCP bridge"
```

---

### Task 3: E2E test — bridge handles notifications/initialized

**Files:**
- Modify: `tests/e2e/harness/harness.go` (add SendNotification to Bridge)
- Modify: `tests/e2e/sharkfin_test.go` (add test; `encoding/json` is already imported)

- [ ] **Step 1: Add SendNotification to bridge harness**

`Bridge.Send` blocks reading stdout, but notifications (202) produce no output.
Add a `SendNotification` method that writes to stdin without reading stdout.

Add to `tests/e2e/harness/harness.go` after the existing `Send` method:

```go
// SendNotification sends a JSON-RPC notification (no id) to the bridge.
// Notifications produce no stdout output (server returns 202).
func (b *Bridge) SendNotification(request any) error {
	return b.stdin.Encode(request)
}
```

- [ ] **Step 2: Write e2e test**

Add to `tests/e2e/sharkfin_test.go`:

```go
func TestBridgeNotification(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	bridge, err := harness.StartBridge(sharkfinBin, addr, d.XDGDir())
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Kill()

	// 1. Initialize
	initResp, err := bridge.Send(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]string{"name": "notif-test", "version": "0.1"},
		},
	})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	var initResult struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if err := json.Unmarshal(initResp, &initResult); err != nil {
		t.Fatalf("unmarshal initialize: %v (raw: %s)", err, initResp)
	}

	// 2. Send notifications/initialized (no id — this is a JSON-RPC notification).
	// The server returns 202 with no body. The bridge must NOT write anything to stdout.
	err = bridge.SendNotification(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	if err != nil {
		t.Fatalf("send notification: %v", err)
	}

	// 3. Verify bridge still works — send a tools/list request.
	// If the bridge wrote garbage to stdout for the 202, this Send would
	// read the garbage instead of the actual response and fail.
	listResp, err := bridge.Send(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/list",
	})
	if err != nil {
		t.Fatalf("tools/list after notification: %v", err)
	}
	var listResult struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(listResp, &listResult); err != nil {
		t.Fatalf("unmarshal tools/list: %v (raw: %s)", err, listResp)
	}
	if len(listResult.Result.Tools) == 0 {
		t.Fatal("expected tools in list response")
	}
}
```

- [ ] **Step 3: Run e2e test**

Run: `go test -v -race -timeout 60s -run TestBridgeNotification ./tests/e2e/`
Expected: PASS

- [ ] **Step 4: Run full e2e suite**

Run: `mise run e2e`
Expected: All pass

- [ ] **Step 5: Commit**

```bash
git add tests/e2e/harness/harness.go tests/e2e/sharkfin_test.go
git commit -m "test: add e2e test for bridge notification handling"
```

---

### Task 4: Full CI pass

- [ ] **Step 1: Run CI**

Run: `mise run ci`
Expected: All lint, unit, and e2e tests pass

- [ ] **Step 2: Verify no regressions**

Check that:
- `TestBridgeEndToEnd` still passes (existing bridge functionality)
- `TestBridgeNotification` passes (new test)
- All presence notification tests pass
- All other tests pass
