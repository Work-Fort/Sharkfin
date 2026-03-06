// SPDX-License-Identifier: AGPL-3.0-or-later
package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Work-Fort/sharkfin-e2e/harness"
)

var sharkfinBin string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "sharkfin-e2e-bin-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	binPath := filepath.Join(tmpDir, "sharkfin")

	// Build the sharkfin binary from the project root module.
	wd, err2 := os.Getwd()
	if err2 != nil {
		fmt.Fprintf(os.Stderr, "getwd: %v\n", err2)
		os.Exit(1)
	}
	projectRoot := filepath.Join(wd, "..", "..")
	cmd := exec.Command("go", "build", "-race", "-o", binPath, ".")
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build sharkfin: %v\n", err)
		os.Exit(1)
	}

	sharkfinBin = binPath
	os.Exit(m.Run())
}

// --- Presence tests ---

func TestPresenceConnect(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

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

func TestPresenceDisconnectMarksOffline(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}

	r, err := alice.ToolCall("user_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Text, `"online":true`) {
		t.Fatalf("expected alice online, got: %s", r.Text)
	}

	alice.DisconnectPresence()
	time.Sleep(200 * time.Millisecond)

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

func TestPresenceRejectsPlainHTTP(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	resp, err := http.Get(fmt.Sprintf("http://%s/presence", addr))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusSwitchingProtocols || resp.StatusCode == http.StatusOK {
		t.Errorf("expected rejection, got %d", resp.StatusCode)
	}
}

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
	defer d.StopFatal(t)

	bridge, err := harness.StartBridge(sharkfinBin, addr, d.XDGDir())
	if err != nil {
		t.Fatal(err)
	}

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

	var tokenResult struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(tokenResp, &tokenResult); err != nil {
		t.Fatalf("unmarshal token response: %v (raw: %s)", err, tokenResp)
	}
	if len(tokenResult.Result.Content) == 0 {
		t.Fatalf("get_identity_token returned empty content (raw: %s)", tokenResp)
	}
	token := tokenResult.Result.Content[0].Text

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

	bridge.Kill()
	time.Sleep(4 * time.Second)

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

// --- Identity tests ---

func TestRegisterAndIdentify(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	alice.DisconnectPresence()
	time.Sleep(100 * time.Millisecond)

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
	defer d.StopFatal(t)

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

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
	defer d.StopFatal(t)

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
	defer d.StopFatal(t)

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

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
	defer d.StopFatal(t)

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

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
	defer d.StopFatal(t)

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

// --- MCP Protocol tests ---

func TestInitializeResponse(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	c := harness.NewClient(addr)
	id := 1
	result, rpcErr, headers, err := c.RawMCPRequest("initialize", id, map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "e2e-test", "version": "0.1"},
	})
	if err != nil {
		t.Fatalf("initialize request: %v", err)
	}
	if rpcErr != nil {
		t.Fatalf("initialize RPC error: %s", rpcErr.Message)
	}

	var initResult struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(result, &initResult); err != nil {
		t.Fatalf("unmarshal initialize result: %v", err)
	}
	if initResult.ProtocolVersion != "2025-03-26" {
		t.Errorf("protocolVersion = %q, want %q", initResult.ProtocolVersion, "2025-03-26")
	}
	if initResult.ServerInfo.Name != "sharkfin" {
		t.Errorf("serverInfo.name = %q, want %q", initResult.ServerInfo.Name, "sharkfin")
	}

	sessionID := headers.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Error("Mcp-Session-Id header not set")
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
	defer d.StopFatal(t)

	c := harness.NewClient(addr)
	if err := c.Initialize(); err != nil {
		t.Fatal(err)
	}

	id := 100
	result, rpcErr, _, err := c.RawMCPRequest("tools/list", id, nil)
	if err != nil {
		t.Fatalf("tools/list request: %v", err)
	}
	if rpcErr != nil {
		t.Fatalf("tools/list RPC error: %s", rpcErr.Message)
	}

	var listResult struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &listResult); err != nil {
		t.Fatalf("unmarshal tools/list: %v", err)
	}

	expected := []string{
		"get_identity_token", "register", "identify", "user_list", "channel_list",
		"channel_create", "channel_invite", "channel_join", "send_message", "unread_messages",
		"unread_counts", "mark_read", "history", "dm_list", "dm_open",
		"capabilities", "set_state", "set_role", "create_role", "delete_role",
		"grant_permission", "revoke_permission", "list_roles",
	}
	if len(listResult.Tools) != len(expected) {
		names := make([]string, len(listResult.Tools))
		for i, tool := range listResult.Tools {
			names[i] = tool.Name
		}
		t.Fatalf("got %d tools %v, want %d %v", len(listResult.Tools), names, len(expected), expected)
	}

	got := make(map[string]bool)
	for _, tool := range listResult.Tools {
		got[tool.Name] = true
	}
	for _, name := range expected {
		if !got[name] {
			t.Errorf("missing tool %q", name)
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
	defer d.StopFatal(t)

	c := harness.NewClient(addr)
	if err := c.ConnectPresence(); err != nil {
		t.Fatal(err)
	}
	defer c.DisconnectPresence()

	if err := c.Initialize(); err != nil {
		t.Fatal(err)
	}

	r, err := c.ToolCall("user_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error == nil {
		t.Fatal("expected error for tool call before identify")
	}
	if !strings.Contains(strings.ToLower(r.Error.Message), "not identified") {
		t.Errorf("error message = %q, want it to contain 'not identified'", r.Error.Message)
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
	defer d.StopFatal(t)

	c := harness.NewClient(addr)
	if err := c.Initialize(); err != nil {
		t.Fatal(err)
	}

	_, rpcErr, _, err := c.RawMCPRequest("nonexistent/method", 99, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if rpcErr == nil {
		t.Fatal("expected RPC error for unknown method")
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
	defer d.StopFatal(t)

	c := harness.NewClient(addr)
	if err := c.Initialize(); err != nil {
		t.Fatal(err)
	}

	r, err := c.ToolCall("nonexistent_tool", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error == nil {
		t.Fatal("expected error for unknown tool")
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
	defer d.StopFatal(t)

	c := harness.NewClient(addr)
	status, body, err := c.RawPost("/mcp", "this is not json{{{")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	t.Logf("status=%d body=%s", status, string(body))

	// mcp-go returns 400 for invalid JSON (Bad Request).
	if status != http.StatusBadRequest && status != http.StatusOK {
		t.Errorf("status = %d, want 400 or 200", status)
	}
	if !strings.Contains(string(body), "error") && !strings.Contains(string(body), "invalid") {
		t.Errorf("response body should contain error info: %s", string(body))
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
	defer d.StopFatal(t)

	c := harness.NewClient(addr)
	status, err := c.RawGet("/mcp")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	// mcp-go's StreamableHTTPServer accepts GET for SSE streaming, so GET
	// returns 200 (or possibly another valid status) instead of 405.
	// Just verify the server responds without error.
	if status >= 500 {
		t.Errorf("status = %d, want non-5xx", status)
	}
}

// --- Channel tests ---

func TestChannelCreateAndList(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	r, err := alice.ToolCall("channel_create", map[string]any{
		"name":   "general",
		"public": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("channel_create error: %s", r.Error.Message)
	}

	r, err = alice.ToolCall("channel_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("channel_list error: %s", r.Error.Message)
	}

	var channels []struct {
		Name   string `json:"name"`
		Public bool   `json:"public"`
	}
	if err := json.Unmarshal([]byte(r.Text), &channels); err != nil {
		t.Fatalf("unmarshal channel_list: %v (raw: %s)", err, r.Text)
	}

	found := false
	for _, ch := range channels {
		if ch.Name == "general" {
			found = true
			if !ch.Public {
				t.Error("channel 'general' should be public")
			}
		}
	}
	if !found {
		t.Errorf("channel 'general' not found in list: %s", r.Text)
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
	defer d.StopFatal(t)

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

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	// Alice creates a private channel with bob
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name":    "secret",
		"public":  false,
		"members": []string{"bob"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("channel_create error: %s", r.Error.Message)
	}

	// Charlie should NOT see the private channel
	r, err = charlie.ToolCall("channel_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.Text, "secret") {
		t.Errorf("charlie should not see private channel 'secret': %s", r.Text)
	}

	// Bob should see the private channel
	r, err = bob.ToolCall("channel_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Text, "secret") {
		t.Errorf("bob should see private channel 'secret': %s", r.Text)
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
	defer d.StopFatal(t)

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

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	r, err := alice.ToolCall("channel_create", map[string]any{
		"name":   "lobby",
		"public": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("channel_create error: %s", r.Error.Message)
	}

	// Bob (non-member) should see the public channel via MCP
	r, err = bob.ToolCall("channel_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Text, "lobby") {
		t.Errorf("bob should see public channel 'lobby': %s", r.Text)
	}
}

func TestChannelCreatePermissionDenied(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	// alice has "user" role which lacks create_channel permission
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name":   "forbidden",
		"public": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error == nil {
		t.Fatal("expected error when user lacks create_channel permission")
	}
	if r.Error.Message != "permission denied: create_channel" {
		t.Errorf("error message = %q, want %q", r.Error.Message, "permission denied: create_channel")
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
	defer d.StopFatal(t)

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

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	// Alice creates a private channel with bob
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name":    "team",
		"public":  false,
		"members": []string{"bob"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("channel_create error: %s", r.Error.Message)
	}

	// Bob invites charlie
	r, err = bob.ToolCall("channel_invite", map[string]any{
		"channel":  "team",
		"username": "charlie",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("channel_invite error: %s", r.Error.Message)
	}

	// Charlie can now see the channel
	r, err = charlie.ToolCall("channel_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Text, "team") {
		t.Errorf("charlie should see channel 'team' after invite: %s", r.Text)
	}

	// Charlie can send a message
	r, err = charlie.ToolCall("send_message", map[string]any{
		"channel": "team",
		"text":    "hello from charlie",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Errorf("charlie send_message error: %s", r.Error.Message)
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
	defer d.StopFatal(t)

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

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	// Alice creates a private channel (only alice is a member)
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name":   "private-only-alice",
		"public": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("channel_create error: %s", r.Error.Message)
	}

	// Bob (not a member) tries to invite charlie
	r, err = bob.ToolCall("channel_invite", map[string]any{
		"channel":  "private-only-alice",
		"username": "charlie",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error == nil {
		t.Fatal("expected error when non-member invites to private channel")
	}
}

// --- Messaging tests ---

func TestSendAndReceiveMessage(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

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

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	// Create a private channel with both users
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name":    "chat",
		"public":  false,
		"members": []string{"bob"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("channel_create error: %s", r.Error.Message)
	}

	// Alice sends a message
	r, err = alice.ToolCall("send_message", map[string]any{
		"channel": "chat",
		"message": "hello bob!",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("send_message error: %s", r.Error.Message)
	}

	// Bob reads unread messages
	r, err = bob.ToolCall("unread_messages", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("unread_messages error: %s", r.Error.Message)
	}

	var msgs []struct {
		Channel string `json:"channel"`
		From    string `json:"from"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal([]byte(r.Text), &msgs); err != nil {
		t.Fatalf("unmarshal messages: %v (raw: %s)", err, r.Text)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d: %s", len(msgs), r.Text)
	}
	if msgs[0].From != "alice" {
		t.Errorf("from = %q, want %q", msgs[0].From, "alice")
	}
	if msgs[0].Body != "hello bob!" {
		t.Errorf("body = %q, want %q", msgs[0].Body, "hello bob!")
	}
	if msgs[0].Channel != "chat" {
		t.Errorf("channel = %q, want %q", msgs[0].Channel, "chat")
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
	defer d.StopFatal(t)

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

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	r, err := alice.ToolCall("channel_create", map[string]any{
		"name":    "consumed-test",
		"public":  false,
		"members": []string{"bob"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("channel_create error: %s", r.Error.Message)
	}

	// Alice sends "first"
	r, err = alice.ToolCall("send_message", map[string]any{
		"channel": "consumed-test",
		"message": "first",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("send_message error: %s", r.Error.Message)
	}

	// Bob reads -> should get 1 message
	r, err = bob.ToolCall("unread_messages", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var msgs []struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(r.Text), &msgs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message on first read, got %d", len(msgs))
	}

	// Bob reads again -> should be empty
	r, err = bob.ToolCall("unread_messages", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "null" && r.Text != "[]" {
		var msgs2 []any
		json.Unmarshal([]byte(r.Text), &msgs2)
		if len(msgs2) != 0 {
			t.Fatalf("expected empty on second read, got: %s", r.Text)
		}
	}

	// Alice sends "second"
	r, err = alice.ToolCall("send_message", map[string]any{
		"channel": "consumed-test",
		"message": "second",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("send_message error: %s", r.Error.Message)
	}

	// Bob reads -> should get only "second"
	r, err = bob.ToolCall("unread_messages", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var msgs3 []struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(r.Text), &msgs3); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(msgs3) != 1 || msgs3[0].Body != "second" {
		t.Fatalf("expected only 'second', got: %s", r.Text)
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
	defer d.StopFatal(t)

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

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	// Create two channels
	for _, chName := range []string{"ch1", "ch2"} {
		r, err := alice.ToolCall("channel_create", map[string]any{
			"name":    chName,
			"public":  false,
			"members": []string{"bob"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if r.Error != nil {
			t.Fatalf("channel_create %s error: %s", chName, r.Error.Message)
		}
	}

	// Alice sends a message to each channel
	for _, chName := range []string{"ch1", "ch2"} {
		r, err := alice.ToolCall("send_message", map[string]any{
			"channel": chName,
			"message": "msg-in-" + chName,
		})
		if err != nil {
			t.Fatal(err)
		}
		if r.Error != nil {
			t.Fatalf("send_message to %s error: %s", chName, r.Error.Message)
		}
	}

	// Bob reads unread filtered by ch1
	r, err := bob.ToolCall("unread_messages", map[string]any{
		"channel": "ch1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var msgs1 []struct {
		Channel string `json:"channel"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal([]byte(r.Text), &msgs1); err != nil {
		t.Fatalf("unmarshal ch1: %v", err)
	}
	if len(msgs1) != 1 || msgs1[0].Channel != "ch1" || msgs1[0].Body != "msg-in-ch1" {
		t.Fatalf("expected 1 message from ch1, got: %s", r.Text)
	}

	// Bob reads unread filtered by ch2
	r, err = bob.ToolCall("unread_messages", map[string]any{
		"channel": "ch2",
	})
	if err != nil {
		t.Fatal(err)
	}
	var msgs2 []struct {
		Channel string `json:"channel"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal([]byte(r.Text), &msgs2); err != nil {
		t.Fatalf("unmarshal ch2: %v", err)
	}
	if len(msgs2) != 1 || msgs2[0].Channel != "ch2" || msgs2[0].Body != "msg-in-ch2" {
		t.Fatalf("expected 1 message from ch2, got: %s", r.Text)
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
	defer d.StopFatal(t)

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	r, err := alice.ToolCall("send_message", map[string]any{
		"channel": "doesnt-exist",
		"message": "hello?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error == nil {
		t.Fatal("expected error when sending to non-existent channel")
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
	defer d.StopFatal(t)

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

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	// Alice creates a private channel (bob is not a member)
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name":   "alice-only",
		"public": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("channel_create error: %s", r.Error.Message)
	}

	// Bob tries to send to the private channel
	r, err = bob.ToolCall("send_message", map[string]any{
		"channel": "alice-only",
		"message": "sneaky!",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error == nil {
		t.Fatal("expected error: non-participant should not be able to send")
	}
	if r.Error.Message != "you are not a participant of this channel" {
		t.Errorf("error message = %q, want %q", r.Error.Message, "you are not a participant of this channel")
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
	defer d.StopFatal(t)

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

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	r, err := alice.ToolCall("channel_create", map[string]any{
		"name":    "ordering",
		"public":  false,
		"members": []string{"bob"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("channel_create error: %s", r.Error.Message)
	}

	// Alice sends 5 messages
	for i := 0; i < 5; i++ {
		r, err = alice.ToolCall("send_message", map[string]any{
			"channel": "ordering",
			"message": fmt.Sprintf("msg-%d", i),
		})
		if err != nil {
			t.Fatal(err)
		}
		if r.Error != nil {
			t.Fatalf("send_message msg-%d error: %s", i, r.Error.Message)
		}
	}

	// Bob reads all 5
	r, err = bob.ToolCall("unread_messages", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}

	var msgs []struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(r.Text), &msgs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d: %s", len(msgs), r.Text)
	}
	for i, m := range msgs {
		expected := fmt.Sprintf("msg-%d", i)
		if m.Body != expected {
			t.Errorf("message[%d].body = %q, want %q", i, m.Body, expected)
		}
	}
}

func TestMCPHistory(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	// Create channel
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name": "general", "public": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("channel_create: %s", r.Error.Message)
	}

	// Send 3 messages
	for i := 1; i <= 3; i++ {
		r, err := alice.ToolCall("send_message", map[string]any{
			"channel": "general",
			"message": fmt.Sprintf("msg-%d", i),
		})
		if err != nil {
			t.Fatal(err)
		}
		if r.Error != nil {
			t.Fatalf("send_message: %s", r.Error.Message)
		}
	}

	// Fetch history
	r, err = alice.ToolCall("history", map[string]any{
		"channel": "general",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("history: %s", r.Error.Message)
	}

	var messages []struct {
		ID   int64  `json:"id"`
		From string `json:"from"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(r.Text), &messages); err != nil {
		t.Fatalf("unmarshal history: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("got %d messages, want 3", len(messages))
	}
	// Messages are in chronological order (oldest first)
	if messages[0].Body != "msg-1" {
		t.Errorf("first message = %q, want msg-1", messages[0].Body)
	}
	if messages[2].Body != "msg-3" {
		t.Errorf("last message = %q, want msg-3", messages[2].Body)
	}

	// Test pagination with "before" — get messages before msg-3
	r, err = alice.ToolCall("history", map[string]any{
		"channel": "general",
		"before":  messages[2].ID, // before newest
		"limit":   2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("history before: %s", r.Error.Message)
	}

	var page []struct {
		Body string `json:"body"`
	}
	json.Unmarshal([]byte(r.Text), &page)
	if len(page) != 2 {
		t.Fatalf("got %d messages, want 2", len(page))
	}
}

// --- Integration tests ---

func TestBridgeEndToEnd(t *testing.T) {
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
	initReq := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]string{"name": "bridge-e2e", "version": "0.1"},
		},
	}
	initResp, err := bridge.Send(initReq)
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
	if initResult.Result.ProtocolVersion != "2025-03-26" {
		t.Errorf("protocolVersion = %q, want %q", initResult.Result.ProtocolVersion, "2025-03-26")
	}

	// 2. Get identity token
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
	var tokenResult struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(tokenResp, &tokenResult); err != nil {
		t.Fatalf("unmarshal token: %v (raw: %s)", err, tokenResp)
	}
	if len(tokenResult.Result.Content) == 0 {
		t.Fatalf("get_identity_token returned empty content (raw: %s)", tokenResp)
	}
	token := tokenResult.Result.Content[0].Text
	if len(token) != 64 {
		t.Errorf("token length = %d, want 64", len(token))
	}

	// 3. Register
	regReq := map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{
			"name": "register",
			"arguments": map[string]any{
				"token": token, "username": "bridge-alice", "password": "",
			},
		},
	}
	regResp, err := bridge.Send(regReq)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	var regResult struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(regResp, &regResult); err != nil {
		t.Fatalf("unmarshal register: %v (raw: %s)", err, regResp)
	}
	if regResult.Error != nil {
		t.Fatalf("register error: %s", regResult.Error.Message)
	}

	if err := d.GrantAdmin(sharkfinBin, "bridge-alice"); err != nil {
		t.Fatal(err)
	}

	// 4. Create channel
	chReq := map[string]any{
		"jsonrpc": "2.0", "id": 4, "method": "tools/call",
		"params": map[string]any{
			"name": "channel_create",
			"arguments": map[string]any{
				"name": "bridge-chan", "public": true,
			},
		},
	}
	chResp, err := bridge.Send(chReq)
	if err != nil {
		t.Fatalf("channel_create: %v", err)
	}
	var chResult struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(chResp, &chResult); err != nil {
		t.Fatalf("unmarshal channel_create: %v (raw: %s)", err, chResp)
	}
	if chResult.Error != nil {
		t.Fatalf("channel_create error: %s", chResult.Error.Message)
	}

	// 5. Send message
	msgReq := map[string]any{
		"jsonrpc": "2.0", "id": 5, "method": "tools/call",
		"params": map[string]any{
			"name": "send_message",
			"arguments": map[string]any{
				"channel": "bridge-chan", "message": "hello from bridge",
			},
		},
	}
	msgResp, err := bridge.Send(msgReq)
	if err != nil {
		t.Fatalf("send_message: %v", err)
	}
	var msgResult struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(msgResp, &msgResult); err != nil {
		t.Fatalf("unmarshal send_message: %v (raw: %s)", err, msgResp)
	}
	if msgResult.Error != nil {
		t.Fatalf("send_message error: %s", msgResult.Error.Message)
	}
}

func TestPresenceExitsOnDaemonRestart(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}

	// Start a presence subprocess
	presenceCmd := exec.Command(sharkfinBin, "presence", "--daemon", addr, "--log-level", "disabled")
	presenceCmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+d.XDGDir()+"/config",
		"XDG_STATE_HOME="+d.XDGDir()+"/state",
	)
	presenceCmd.Stdout = os.Stderr
	presenceCmd.Stderr = os.Stderr
	if err := presenceCmd.Start(); err != nil {
		d.StopFatal(t)
		t.Fatalf("start presence: %v", err)
	}

	// Give the presence process time to connect
	time.Sleep(500 * time.Millisecond)

	// Stop the daemon
	d.StopFatal(t)

	// Verify the presence process exits within 10 seconds
	done := make(chan error, 1)
	go func() { done <- presenceCmd.Wait() }()

	select {
	case <-done:
		// Presence exited as expected
	case <-time.After(10 * time.Second):
		presenceCmd.Process.Kill()
		t.Fatal("presence process did not exit within 10s after daemon stop")
	}

	// Start a new daemon on the same address
	d2, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatalf("restart daemon: %v", err)
	}
	defer d2.StopFatal(t)

	// Verify a new client can connect
	c := harness.NewClient(addr)
	if err := c.RegisterFlow("restart-user"); err != nil {
		t.Fatalf("register after restart: %v", err)
	}
	defer c.DisconnectPresence()
}

// --- WebSocket chat tests ---

func TestWSRegisterAndIdentify(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Register via WS
	ws1, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws1.WSRegister("alice"); err != nil {
		t.Fatal(err)
	}

	// Verify alice appears in user_list
	env, err := ws1.Req("user_list", map[string]any{}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("user_list failed: %s", string(env.D))
	}
	if !strings.Contains(string(env.D), `"alice"`) {
		t.Errorf("expected alice in user list: %s", string(env.D))
	}

	// Disconnect
	ws1.Close()
	time.Sleep(100 * time.Millisecond)

	// Re-identify on new connection
	ws2, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ws2.Close()

	env, err = ws2.Req("identify", map[string]string{"username": "alice"}, "i1")
	if err != nil {
		t.Fatal(err)
	}
	if env.OK == nil || !*env.OK {
		t.Errorf("identify failed: %s", string(env.D))
	}
}

func TestWSChannelCreateAndInvite(t *testing.T) {
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
	if err := alice.WSRegister("alice"); err != nil {
		t.Fatal(err)
	}

	bob, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()
	if err := bob.WSRegister("bob"); err != nil {
		t.Fatal(err)
	}

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	// Create private channel
	env, err := alice.Req("channel_create", map[string]any{
		"name": "project", "public": false,
	}, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("channel_create failed: %s", string(env.D))
	}

	// Invite bob
	env, err = alice.Req("channel_invite", map[string]any{
		"channel": "project", "username": "bob",
	}, "inv1")
	if err != nil {
		t.Fatal(err)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("channel_invite failed: %s", string(env.D))
	}

	// Bob can send to the channel
	env, err = bob.Req("send_message", map[string]any{
		"channel": "project", "body": "hello from bob",
	}, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if env.OK == nil || !*env.OK {
		t.Errorf("bob send failed: %s", string(env.D))
	}
}

func TestWSSendAndBroadcast(t *testing.T) {
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
	if err := alice.WSRegister("alice"); err != nil {
		t.Fatal(err)
	}

	bob, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()
	if err := bob.WSRegister("bob"); err != nil {
		t.Fatal(err)
	}

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	// Create channel with both
	alice.Req("channel_create", map[string]any{
		"name": "general", "public": true,
	}, "c1")
	alice.Req("channel_invite", map[string]any{
		"channel": "general", "username": "bob",
	}, "inv1")

	// Alice sends
	env, err := alice.Req("send_message", map[string]any{
		"channel": "general", "body": "hello everyone",
	}, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("send failed: %s", string(env.D))
	}

	// Bob receives broadcast
	bcast, err := bob.Read()
	if err != nil {
		t.Fatalf("read broadcast: %v", err)
	}
	if bcast.Type != "message.new" {
		t.Fatalf("type = %q, want message.new", bcast.Type)
	}

	var msg struct {
		Channel string `json:"channel"`
		From    string `json:"from"`
		Body    string `json:"body"`
		ID      int64  `json:"id"`
	}
	json.Unmarshal(bcast.D, &msg)
	if msg.Channel != "general" {
		t.Errorf("channel = %q, want general", msg.Channel)
	}
	if msg.From != "alice" {
		t.Errorf("from = %q, want alice", msg.From)
	}
	if msg.Body != "hello everyone" {
		t.Errorf("body = %q, want 'hello everyone'", msg.Body)
	}
	if msg.ID == 0 {
		t.Error("expected non-zero message ID in broadcast")
	}
}

func TestWSHistory(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	ws, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := ws.WSRegister("alice"); err != nil {
		t.Fatal(err)
	}

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	ws.Req("channel_create", map[string]any{
		"name": "general", "public": true,
	}, "c1")

	// Send 3 messages
	for i := 0; i < 3; i++ {
		ws.Req("send_message", map[string]any{
			"channel": "general", "body": fmt.Sprintf("msg-%d", i),
		}, fmt.Sprintf("m%d", i))
	}

	// Fetch history
	env, err := ws.Req("history", map[string]any{
		"channel": "general",
	}, "h1")
	if err != nil {
		t.Fatal(err)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("history failed: %s", string(env.D))
	}

	var result struct {
		Messages []struct {
			Body string `json:"body"`
		} `json:"messages"`
	}
	json.Unmarshal(env.D, &result)
	if len(result.Messages) != 3 {
		t.Fatalf("got %d messages, want 3", len(result.Messages))
	}
	if result.Messages[0].Body != "msg-0" {
		t.Errorf("first message = %q, want msg-0", result.Messages[0].Body)
	}
	if result.Messages[2].Body != "msg-2" {
		t.Errorf("last message = %q, want msg-2", result.Messages[2].Body)
	}
}

func TestWSUnreadMessages(t *testing.T) {
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
	if err := alice.WSRegister("alice"); err != nil {
		t.Fatal(err)
	}

	bob, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()
	if err := bob.WSRegister("bob"); err != nil {
		t.Fatal(err)
	}

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	alice.Req("channel_create", map[string]any{
		"name": "chat", "public": true,
	}, "c1")
	alice.Req("channel_invite", map[string]any{
		"channel": "chat", "username": "bob",
	}, "inv1")

	alice.Req("send_message", map[string]any{
		"channel": "chat", "body": "hey bob",
	}, "m1")
	// Drain bob's broadcast
	bob.Read()

	// Bob reads unread
	env, err := bob.Req("unread_messages", map[string]any{}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("unread failed: %s", string(env.D))
	}

	var result struct {
		Messages []struct {
			Body    string `json:"body"`
			Channel string `json:"channel"`
		} `json:"messages"`
	}
	json.Unmarshal(env.D, &result)
	if len(result.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(result.Messages))
	}
	if result.Messages[0].Body != "hey bob" {
		t.Errorf("body = %q, want 'hey bob'", result.Messages[0].Body)
	}

	// Second call should return empty (consumed)
	env, err = bob.Req("unread_messages", map[string]any{}, "u2")
	if err != nil {
		t.Fatal(err)
	}
	var result2 struct {
		Messages []struct{} `json:"messages"`
	}
	json.Unmarshal(env.D, &result2)
	if len(result2.Messages) != 0 {
		t.Errorf("expected 0 messages on second read, got %d", len(result2.Messages))
	}
}

func TestWSMentions(t *testing.T) {
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
	if err := alice.WSRegister("alice"); err != nil {
		t.Fatal(err)
	}

	bob, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()
	if err := bob.WSRegister("bob"); err != nil {
		t.Fatal(err)
	}

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	alice.Req("channel_create", map[string]any{
		"name": "general", "public": true,
	}, "c1")
	alice.Req("channel_invite", map[string]any{
		"channel": "general", "username": "bob",
	}, "inv1")

	// Send with mention
	env, err := alice.Req("send_message", map[string]any{
		"channel":  "general",
		"body":     "hey @bob look at this",
		"mentions": []string{"bob"},
	}, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("send failed: %s", string(env.D))
	}

	// Bob receives broadcast with mentions
	bcast, err := bob.Read()
	if err != nil {
		t.Fatalf("read broadcast: %v", err)
	}
	var msg struct {
		Mentions []string `json:"mentions"`
	}
	json.Unmarshal(bcast.D, &msg)
	if len(msg.Mentions) != 1 || msg.Mentions[0] != "bob" {
		t.Errorf("mentions = %v, want [bob]", msg.Mentions)
	}

	// Also send a non-mention message
	alice.Req("send_message", map[string]any{
		"channel": "general", "body": "just chatting",
	}, "m2")
	bob.Read() // drain broadcast

	// Bob filters with mentions_only
	env, err = bob.Req("unread_messages", map[string]any{
		"mentions_only": true,
	}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Messages []struct {
			Body     string   `json:"body"`
			Mentions []string `json:"mentions"`
		} `json:"messages"`
	}
	json.Unmarshal(env.D, &result)
	if len(result.Messages) != 1 {
		t.Fatalf("got %d mention messages, want 1", len(result.Messages))
	}
	if result.Messages[0].Body != "hey @bob look at this" {
		t.Errorf("body = %q", result.Messages[0].Body)
	}
}

func TestWSMentionInvalidUser(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	ws, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := ws.WSRegister("alice"); err != nil {
		t.Fatal(err)
	}

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	ws.Req("channel_create", map[string]any{
		"name": "general", "public": true,
	}, "c1")

	env, err := ws.Req("send_message", map[string]any{
		"channel":  "general",
		"body":     "hey @ghost",
		"mentions": []string{"ghost"},
	}, "m1")
	if err != nil {
		t.Fatal(err)
	}
	// Invalid mentions are silently ignored
	if env.OK == nil || !*env.OK {
		t.Error("expected ok: invalid mentions should be silently ignored")
	}
}

func TestWSThreadReply(t *testing.T) {
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
	if err := alice.WSRegister("alice"); err != nil {
		t.Fatal(err)
	}

	bob, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()
	if err := bob.WSRegister("bob"); err != nil {
		t.Fatal(err)
	}

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	alice.Req("channel_create", map[string]any{
		"name": "general", "public": true,
	}, "c1")
	alice.Req("channel_invite", map[string]any{
		"channel": "general", "username": "bob",
	}, "inv1")

	// Send parent
	parentEnv, err := alice.Req("send_message", map[string]any{
		"channel": "general", "body": "parent message",
	}, "m1")
	if err != nil {
		t.Fatal(err)
	}
	bob.Read() // drain parent broadcast

	var parentResult struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(parentEnv.D, &parentResult)

	// Reply in thread
	env, err := alice.Req("send_message", map[string]any{
		"channel":   "general",
		"body":      "thread reply",
		"thread_id": parentResult.ID,
	}, "m2")
	if err != nil {
		t.Fatal(err)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("thread reply failed: %s", string(env.D))
	}

	// Bob receives broadcast with thread_id
	bcast, err := bob.Read()
	if err != nil {
		t.Fatalf("read broadcast: %v", err)
	}
	var msg struct {
		ThreadID int64 `json:"thread_id"`
	}
	json.Unmarshal(bcast.D, &msg)
	if msg.ThreadID != parentResult.ID {
		t.Errorf("thread_id = %d, want %d", msg.ThreadID, parentResult.ID)
	}

	// History filtered by thread_id returns only the reply
	env, err = alice.Req("history", map[string]any{
		"channel":   "general",
		"thread_id": parentResult.ID,
	}, "h1")
	if err != nil {
		t.Fatal(err)
	}
	var histResult struct {
		Messages []struct {
			Body string `json:"body"`
		} `json:"messages"`
	}
	json.Unmarshal(env.D, &histResult)
	if len(histResult.Messages) != 1 {
		t.Fatalf("got %d thread messages, want 1", len(histResult.Messages))
	}
	if histResult.Messages[0].Body != "thread reply" {
		t.Errorf("body = %q, want 'thread reply'", histResult.Messages[0].Body)
	}
}

func TestWSNestedReplyRejected(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	ws, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := ws.WSRegister("alice"); err != nil {
		t.Fatal(err)
	}

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	ws.Req("channel_create", map[string]any{
		"name": "general", "public": true,
	}, "c1")

	// Send parent
	parentEnv, _ := ws.Req("send_message", map[string]any{
		"channel": "general", "body": "parent",
	}, "m1")
	var pr struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(parentEnv.D, &pr)

	// Send reply
	replyEnv, _ := ws.Req("send_message", map[string]any{
		"channel": "general", "body": "reply", "thread_id": pr.ID,
	}, "m2")
	var rr struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(replyEnv.D, &rr)

	// Nested reply should fail
	env, err := ws.Req("send_message", map[string]any{
		"channel": "general", "body": "nested", "thread_id": rr.ID,
	}, "m3")
	if err != nil {
		t.Fatal(err)
	}
	if env.OK != nil && *env.OK {
		t.Error("expected error for nested reply")
	}
}

func TestWSAndMCPInterop(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Alice on MCP
	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	// Bob on WS
	bob, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()
	if err := bob.WSRegister("bob"); err != nil {
		t.Fatal(err)
	}

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	// Alice creates channel and invites bob via MCP
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name": "cross", "public": false, "members": []string{"bob"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("channel_create: %s", r.Error.Message)
	}

	// Alice sends via MCP
	r, err = alice.ToolCall("send_message", map[string]any{
		"channel": "cross", "message": "from mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("send_message: %s", r.Error.Message)
	}

	// Bob receives broadcast on WS
	bcast, err := bob.Read()
	if err != nil {
		t.Fatalf("read broadcast: %v", err)
	}
	if bcast.Type != "message.new" {
		t.Fatalf("type = %q, want message.new", bcast.Type)
	}

	var msg struct {
		From string `json:"from"`
		Body string `json:"body"`
	}
	json.Unmarshal(bcast.D, &msg)
	if msg.From != "alice" {
		t.Errorf("from = %q, want alice", msg.From)
	}
	if msg.Body != "from mcp" {
		t.Errorf("body = %q, want 'from mcp'", msg.Body)
	}
}

// --- Unread counts and mark_read tests ---

func TestUnreadCounts(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

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

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	// Create channel with both users
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name": "counts-test", "public": false, "members": []string{"bob"},
	})
	if err != nil || r.Error != nil {
		t.Fatalf("create channel: err=%v rpc=%+v", err, r.Error)
	}

	// Alice sends 3 messages, one mentioning bob
	for i, body := range []string{"hello", "world", "hey @bob check this"} {
		r, err := alice.ToolCall("send_message", map[string]any{
			"channel": "counts-test", "message": body,
		})
		if err != nil || r.Error != nil {
			t.Fatalf("send message %d: err=%v rpc=%+v", i, err, r.Error)
		}
	}

	// Bob checks unread counts
	r, err = bob.ToolCall("unread_counts", map[string]any{})
	if err != nil || r.Error != nil {
		t.Fatalf("unread_counts: err=%v rpc=%+v", err, r.Error)
	}

	var counts []struct {
		Channel      string `json:"channel"`
		UnreadCount  int    `json:"unread_count"`
		MentionCount int    `json:"mention_count"`
	}
	if err := json.Unmarshal([]byte(r.Text), &counts); err != nil {
		t.Fatalf("unmarshal counts: %v (text: %s)", err, r.Text)
	}

	found := false
	for _, c := range counts {
		if c.Channel == "counts-test" {
			found = true
			if c.UnreadCount != 3 {
				t.Errorf("unread_count = %d, want 3", c.UnreadCount)
			}
			if c.MentionCount != 1 {
				t.Errorf("mention_count = %d, want 1", c.MentionCount)
			}
		}
	}
	if !found {
		t.Errorf("counts-test not in unread_counts response: %s", r.Text)
	}

	// Bob marks channel as read
	r, err = bob.ToolCall("mark_read", map[string]any{"channel": "counts-test"})
	if err != nil || r.Error != nil {
		t.Fatalf("mark_read: err=%v rpc=%+v", err, r.Error)
	}

	// Unread counts should now be empty for this channel
	r, err = bob.ToolCall("unread_counts", map[string]any{})
	if err != nil || r.Error != nil {
		t.Fatalf("unread_counts after mark_read: err=%v rpc=%+v", err, r.Error)
	}

	if r.Text != "null" && r.Text != "[]" {
		var countsAfter []struct {
			Channel string `json:"channel"`
		}
		if err := json.Unmarshal([]byte(r.Text), &countsAfter); err != nil {
			t.Fatalf("unmarshal counts after: %v", err)
		}
		for _, c := range countsAfter {
			if c.Channel == "counts-test" {
				t.Error("counts-test still in unread_counts after mark_read")
			}
		}
	}
}

func TestMarkReadForwardOnly(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

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

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	// Create channel
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name": "fwd-test", "public": false, "members": []string{"bob"},
	})
	if err != nil || r.Error != nil {
		t.Fatalf("create channel: err=%v rpc=%+v", err, r.Error)
	}

	// Alice sends 3 messages
	var msgIDs []int64
	for i := 0; i < 3; i++ {
		r, err := alice.ToolCall("send_message", map[string]any{
			"channel": "fwd-test", "message": fmt.Sprintf("msg %d", i),
		})
		if err != nil || r.Error != nil {
			t.Fatalf("send message %d: err=%v rpc=%+v", i, err, r.Error)
		}
		var id int64
		fmt.Sscanf(r.Text, "message sent (id: %d)", &id)
		msgIDs = append(msgIDs, id)
	}

	// Bob marks read up to message 3 (latest)
	r, err = bob.ToolCall("mark_read", map[string]any{
		"channel": "fwd-test", "message_id": msgIDs[2],
	})
	if err != nil || r.Error != nil {
		t.Fatalf("mark_read latest: err=%v rpc=%+v", err, r.Error)
	}

	// Bob tries to mark read at message 1 (older) — should be a no-op
	r, err = bob.ToolCall("mark_read", map[string]any{
		"channel": "fwd-test", "message_id": msgIDs[0],
	})
	if err != nil || r.Error != nil {
		t.Fatalf("mark_read older: err=%v rpc=%+v", err, r.Error)
	}

	// Alice sends a 4th message
	r, err = alice.ToolCall("send_message", map[string]any{
		"channel": "fwd-test", "message": "msg 3",
	})
	if err != nil || r.Error != nil {
		t.Fatalf("send msg 3: err=%v rpc=%+v", err, r.Error)
	}

	// Bob should have exactly 1 unread (the 4th message)
	r, err = bob.ToolCall("unread_counts", map[string]any{})
	if err != nil || r.Error != nil {
		t.Fatalf("unread_counts: err=%v rpc=%+v", err, r.Error)
	}

	var counts []struct {
		Channel     string `json:"channel"`
		UnreadCount int    `json:"unread_count"`
	}
	if err := json.Unmarshal([]byte(r.Text), &counts); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	found := false
	for _, c := range counts {
		if c.Channel == "fwd-test" {
			found = true
			if c.UnreadCount != 1 {
				t.Errorf("unread_count = %d, want 1 (forward-only cursor should not have regressed)", c.UnreadCount)
			}
		}
	}
	if !found {
		t.Error("fwd-test not in unread_counts (expected 1 unread)")
	}
}

func TestWSUnreadCountsAndMarkRead(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Register alice via MCP
	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	// Bob connects via WS
	ws, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := ws.WSRegister("bob"); err != nil {
		t.Fatal(err)
	}

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	// Create channel with both users
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name": "ws-counts", "public": false, "members": []string{"bob"},
	})
	if err != nil || r.Error != nil {
		t.Fatalf("create channel: err=%v rpc=%+v", err, r.Error)
	}

	// Alice sends 2 messages
	for _, body := range []string{"hello", "world"} {
		alice.ToolCall("send_message", map[string]any{
			"channel": "ws-counts", "message": body,
		})
	}

	// Bob checks unread_counts via WS
	env, err := ws.Req("unread_counts", nil, "uc1")
	if err != nil {
		t.Fatalf("ws unread_counts: %v", err)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("ws unread_counts failed: %s", string(env.D))
	}

	// Verify counts
	var resp struct {
		Counts []struct {
			Channel      string `json:"channel"`
			UnreadCount  int    `json:"unread_count"`
			MentionCount int    `json:"mention_count"`
		} `json:"counts"`
	}
	json.Unmarshal(env.D, &resp)
	found := false
	for _, c := range resp.Counts {
		if c.Channel == "ws-counts" {
			found = true
			if c.UnreadCount != 2 {
				t.Errorf("ws unread_count = %d, want 2", c.UnreadCount)
			}
		}
	}
	if !found {
		t.Errorf("ws-counts not in response: %s", string(env.D))
	}

	// Bob marks read via WS
	env, err = ws.Req("mark_read", map[string]string{"channel": "ws-counts"}, "mr1")
	if err != nil {
		t.Fatalf("ws mark_read: %v", err)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("ws mark_read failed: %s", string(env.D))
	}

	// Verify cleared
	env, err = ws.Req("unread_counts", nil, "uc2")
	if err != nil {
		t.Fatalf("ws unread_counts after mark: %v", err)
	}

	var resp2 struct {
		Counts []struct {
			Channel string `json:"channel"`
		} `json:"counts"`
	}
	json.Unmarshal(env.D, &resp2)
	for _, c := range resp2.Counts {
		if c.Channel == "ws-counts" {
			t.Error("ws-counts still in unread_counts after mark_read")
		}
	}
}

// --- Webhook notification tests ---

func TestWebhookOnMention(t *testing.T) {
	var mu sync.Mutex
	var received []map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]interface{}
		json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		received = append(received, p)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr, harness.WithWebhookURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Register alice and bob
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

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	// Create channel with both
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name": "webhook-test", "public": true, "members": []string{"bob"},
	})
	if err != nil || r.Error != nil {
		t.Fatalf("create channel: err=%v rpc=%+v", err, r.Error)
	}

	// Alice sends message mentioning bob
	r, err = alice.ToolCall("send_message", map[string]any{
		"channel": "webhook-test", "message": "Hey @bob check this out",
	})
	if err != nil || r.Error != nil {
		t.Fatalf("send message: err=%v rpc=%+v", err, r.Error)
	}

	// Wait for async webhook
	time.Sleep(1 * time.Second)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(received))
	}

	p := received[0]
	if p["event"] != "message.new" {
		t.Errorf("event = %v, want message.new", p["event"])
	}
	if p["recipient"] != "bob" {
		t.Errorf("recipient = %v, want bob", p["recipient"])
	}
	if p["from"] != "alice" {
		t.Errorf("from = %v, want alice", p["from"])
	}
	if p["channel"] != "webhook-test" {
		t.Errorf("channel = %v, want webhook-test", p["channel"])
	}
	if p["channel_type"] != "channel" {
		t.Errorf("channel_type = %v, want channel", p["channel_type"])
	}
}

func TestWebhookOnDM(t *testing.T) {
	var mu sync.Mutex
	var received []map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]interface{}
		json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		received = append(received, p)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr, harness.WithWebhookURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Register alice and bob
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

	// Open DM
	r, err := alice.ToolCall("dm_open", map[string]any{"username": "bob"})
	if err != nil || r.Error != nil {
		t.Fatalf("dm_open: err=%v rpc=%+v", err, r.Error)
	}

	// Alice sends DM (no explicit mention)
	r, err = alice.ToolCall("send_message", map[string]any{
		"channel": "dm-alice-bob", "message": "hey bob, private msg",
	})
	if err != nil || r.Error != nil {
		t.Fatalf("send dm: err=%v rpc=%+v", err, r.Error)
	}

	// Wait for async webhook
	time.Sleep(1 * time.Second)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 DM webhook, got %d", len(received))
	}

	p := received[0]
	if p["event"] != "message.new" {
		t.Errorf("event = %v, want message.new", p["event"])
	}
	if p["recipient"] != "bob" {
		t.Errorf("recipient = %v, want bob", p["recipient"])
	}
	if p["from"] != "alice" {
		t.Errorf("from = %v, want alice", p["from"])
	}
	if p["channel_type"] != "dm" {
		t.Errorf("channel_type = %v, want dm", p["channel_type"])
	}
}

// --- RBAC, Presence, and Agent tests ---

func TestRBACDefaultPermissions(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Register an admin to create a channel
	admin := harness.NewClient(addr)
	if err := admin.RegisterFlow("admin-user"); err != nil {
		t.Fatal(err)
	}
	defer admin.DisconnectPresence()

	if err := d.GrantAdmin(sharkfinBin, "admin-user"); err != nil {
		t.Fatal(err)
	}

	r, err := admin.ToolCall("channel_create", map[string]any{
		"name": "general", "public": true,
	})
	if err != nil || r.Error != nil {
		t.Fatalf("channel_create: err=%v rpc=%+v", err, r.Error)
	}

	// Register a normal user via MCP (gets "agent" role, same perms as "user")
	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	// Alice should be able to join the channel and send a message
	r, err = alice.ToolCall("channel_join", map[string]any{"channel": "general"})
	if err != nil || r.Error != nil {
		t.Fatalf("channel_join: err=%v rpc=%+v", err, r.Error)
	}

	r, err = alice.ToolCall("send_message", map[string]any{
		"channel": "general", "message": "hello from alice",
	})
	if err != nil || r.Error != nil {
		t.Fatalf("send_message should succeed: err=%v rpc=%+v", err, r.Error)
	}

	// Alice should NOT be able to create a channel (lacks create_channel)
	r, err = alice.ToolCall("channel_create", map[string]any{
		"name": "forbidden", "public": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error == nil {
		t.Fatal("expected error: default user should not have create_channel permission")
	}
	if !strings.Contains(r.Error.Message, "permission denied") {
		t.Errorf("error message = %q, want it to contain 'permission denied'", r.Error.Message)
	}
}

func TestRBACAdminCanManageRoles(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Register a user
	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	// Verify alice cannot create channels initially
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name": "test-ch", "public": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error == nil {
		t.Fatal("expected error: alice should not have create_channel permission before promotion")
	}

	// Promote alice to admin via CLI
	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	// Now alice should be able to create channels
	r, err = alice.ToolCall("channel_create", map[string]any{
		"name": "admin-created", "public": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("admin alice should be able to create channels: %s", r.Error.Message)
	}
}

func TestCapabilitiesQueryMCP(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	r, err := alice.Capabilities()
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("capabilities error: %s", r.Error.Message)
	}

	var perms []string
	if err := json.Unmarshal([]byte(r.Text), &perms); err != nil {
		t.Fatalf("unmarshal capabilities: %v (raw: %s)", err, r.Text)
	}

	// Agent role should have everything except create_channel and manage_roles
	permSet := make(map[string]bool)
	for _, p := range perms {
		permSet[p] = true
	}

	// Should have these
	for _, expected := range []string{
		"send_message", "join_channel", "invite_channel", "history",
		"unread_messages", "unread_counts", "mark_read", "user_list",
		"channel_list", "dm_open", "dm_list",
	} {
		if !permSet[expected] {
			t.Errorf("missing expected permission: %s", expected)
		}
	}

	// Should NOT have these
	for _, forbidden := range []string{"create_channel", "manage_roles"} {
		if permSet[forbidden] {
			t.Errorf("should not have permission: %s", forbidden)
		}
	}
}

func TestCapabilitiesQueryWS(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	ws, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := ws.WSRegister("alice"); err != nil {
		t.Fatal(err)
	}

	env, err := ws.Capabilities()
	if err != nil {
		t.Fatal(err)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("capabilities failed: %s", string(env.D))
	}

	var result struct {
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(env.D, &result); err != nil {
		t.Fatalf("unmarshal capabilities: %v (raw: %s)", err, string(env.D))
	}

	permSet := make(map[string]bool)
	for _, p := range result.Permissions {
		permSet[p] = true
	}

	// User role should have these
	if !permSet["send_message"] {
		t.Error("missing expected permission: send_message")
	}
	if !permSet["user_list"] {
		t.Error("missing expected permission: user_list")
	}

	// Should NOT have these
	if permSet["create_channel"] {
		t.Error("should not have permission: create_channel")
	}
	if permSet["manage_roles"] {
		t.Error("should not have permission: manage_roles")
	}
}

func TestPresenceBroadcastOnWS(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Register first user
	ws1, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ws1.Close()
	if err := ws1.WSRegister("alice"); err != nil {
		t.Fatal(err)
	}

	// Register second user — first should receive presence broadcast
	ws2, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws2.WSRegister("bob"); err != nil {
		t.Fatal(err)
	}

	// Read broadcasts on ws1 until we get bob's online presence
	var foundOnline bool
	for i := 0; i < 10; i++ {
		env, err := ws1.ReadWithTimeout(2 * time.Second)
		if err != nil {
			break
		}
		if env.Type == "presence" {
			var p struct {
				Username string `json:"username"`
				Status   string `json:"status"`
			}
			json.Unmarshal(env.D, &p)
			if p.Username == "bob" && p.Status == "online" {
				foundOnline = true
				break
			}
		}
	}
	if !foundOnline {
		t.Error("expected presence broadcast with bob online")
	}

	// Disconnect second user — first should receive offline presence
	ws2.Close()
	time.Sleep(200 * time.Millisecond)

	var foundOffline bool
	for i := 0; i < 10; i++ {
		env, err := ws1.ReadWithTimeout(2 * time.Second)
		if err != nil {
			break
		}
		if env.Type == "presence" {
			var p struct {
				Username string `json:"username"`
				Status   string `json:"status"`
			}
			json.Unmarshal(env.D, &p)
			if p.Username == "bob" && p.Status == "offline" {
				foundOffline = true
				break
			}
		}
	}
	if !foundOffline {
		t.Error("expected presence broadcast with bob offline after disconnect")
	}
}

func TestActiveIdleState(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Register first WS user and set to active
	ws1, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ws1.Close()
	if err := ws1.WSRegister("alice"); err != nil {
		t.Fatal(err)
	}

	env, err := ws1.SetState("active")
	if err != nil {
		t.Fatalf("set_state active: %v", err)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("set_state failed: %s", string(env.D))
	}

	// Register second WS user
	ws2, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ws2.Close()
	if err := ws2.WSRegister("bob"); err != nil {
		t.Fatal(err)
	}

	// Drain alice's presence broadcast on ws2 (from register)
	// We should see alice's presence broadcast when bob connects;
	// alice set state to active so the initial presence for alice came before bob existed.
	// bob only sees own connect, not alice's state change.
	// Instead, let's have alice change state to idle and check bob receives the broadcast.

	// Alice changes state to idle
	env, err = ws1.SetState("idle")
	if err != nil {
		t.Fatalf("set_state idle: %v", err)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("set_state idle failed: %s", string(env.D))
	}

	// Bob should receive a presence broadcast with alice's state change
	var foundIdleBroadcast bool
	for i := 0; i < 10; i++ {
		bcast, err := ws2.ReadWithTimeout(2 * time.Second)
		if err != nil {
			break
		}
		if bcast.Type == "presence" {
			var p struct {
				Username string `json:"username"`
				Status   string `json:"status"`
				State    string `json:"state"`
			}
			json.Unmarshal(bcast.D, &p)
			if p.Username == "alice" && p.State == "idle" {
				foundIdleBroadcast = true
				break
			}
		}
	}
	if !foundIdleBroadcast {
		t.Error("expected presence broadcast with alice idle state")
	}

	// Also test via MCP set_state tool
	mcpUser := harness.NewClient(addr)
	if err := mcpUser.RegisterFlow("charlie"); err != nil {
		t.Fatal(err)
	}
	defer mcpUser.DisconnectPresence()

	r, err := mcpUser.SetState("active")
	if err != nil {
		t.Fatalf("MCP set_state: %v", err)
	}
	if r.Error != nil {
		t.Fatalf("MCP set_state error: %s", r.Error.Message)
	}
	if !strings.Contains(r.Text, "active") {
		t.Errorf("MCP set_state response = %q, want it to contain 'active'", r.Text)
	}
}

func TestNotificationsOnlyMode(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Register a normal user first (to create channels and send messages)
	admin := harness.NewClient(addr)
	if err := admin.RegisterFlow("admin-user"); err != nil {
		t.Fatal(err)
	}
	defer admin.DisconnectPresence()
	if err := d.GrantAdmin(sharkfinBin, "admin-user"); err != nil {
		t.Fatal(err)
	}
	r, err := admin.ToolCall("channel_create", map[string]any{
		"name": "general", "public": true,
	})
	if err != nil || r.Error != nil {
		t.Fatalf("channel_create: err=%v rpc=%+v", err, r.Error)
	}

	// Connect a WS user with notifications_only
	ws, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	// Register with notifications_only: true
	env, err := ws.Req("register", map[string]any{
		"username":           "notif-user",
		"notifications_only": true,
	}, "reg")
	if err != nil {
		t.Fatal(err)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("register notifications_only failed: %s", string(env.D))
	}

	// (a) send_message should return error "notification-only connection"
	env, err = ws.Req("send_message", map[string]any{
		"channel": "general", "message": "should fail",
	}, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if env.OK != nil && *env.OK {
		t.Error("expected send_message to fail in notification-only mode")
	}
	if !strings.Contains(string(env.D), "notification-only connection") {
		t.Errorf("expected 'notification-only connection' error, got: %s", string(env.D))
	}

	// (b) set_state should work
	env, err = ws.SetState("active")
	if err != nil {
		t.Fatal(err)
	}
	if env.OK == nil || !*env.OK {
		t.Errorf("set_state should work in notification-only mode: %s", string(env.D))
	}

	// (c) capabilities should work
	env, err = ws.Capabilities()
	if err != nil {
		t.Fatal(err)
	}
	if env.OK == nil || !*env.OK {
		t.Errorf("capabilities should work in notification-only mode: %s", string(env.D))
	}

	// (d) unread_counts should work (needed by agent sidecar)
	env, err = ws.Req("unread_counts", nil, "uc1")
	if err != nil {
		t.Fatal(err)
	}
	if env.OK == nil || !*env.OK {
		t.Errorf("unread_counts should work in notification-only mode: %s", string(env.D))
	}

	// (e) user still receives message.new broadcasts from other users
	// Admin joins general and invites notif-user to receive broadcasts
	r, err = admin.ToolCall("channel_invite", map[string]any{
		"channel": "general", "username": "notif-user",
	})
	if err != nil || r.Error != nil {
		t.Fatalf("invite notif-user: err=%v rpc=%+v", err, r.Error)
	}

	// Admin sends a message
	r, err = admin.ToolCall("send_message", map[string]any{
		"channel": "general", "message": "hello from admin",
	})
	if err != nil || r.Error != nil {
		t.Fatalf("admin send: err=%v rpc=%+v", err, r.Error)
	}

	// notif-user should receive the broadcast
	var foundBroadcast bool
	for i := 0; i < 10; i++ {
		bcast, err := ws.ReadWithTimeout(2 * time.Second)
		if err != nil {
			break
		}
		if bcast.Type == "message.new" {
			var msg struct {
				Channel string `json:"channel"`
				Body    string `json:"body"`
			}
			json.Unmarshal(bcast.D, &msg)
			if msg.Channel == "general" && msg.Body == "hello from admin" {
				foundBroadcast = true
				break
			}
		}
	}
	if !foundBroadcast {
		t.Error("notification-only user should still receive message.new broadcasts")
	}
}

func TestAgentTypeOnMCPRegister(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

	r, err := alice.ToolCall("user_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("user_list error: %s", r.Error.Message)
	}

	var users []struct {
		Username string `json:"username"`
		Type     string `json:"type"`
	}
	if err := json.Unmarshal([]byte(r.Text), &users); err != nil {
		t.Fatalf("unmarshal user_list: %v (raw: %s)", err, r.Text)
	}

	found := false
	for _, u := range users {
		if u.Username == "alice" {
			found = true
			if u.Type != "agent" {
				t.Errorf("alice type = %q, want %q", u.Type, "agent")
			}
		}
	}
	if !found {
		t.Error("alice not found in user_list")
	}
}

func TestUserTypeOnWSRegister(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	ws, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := ws.WSRegister("alice"); err != nil {
		t.Fatal(err)
	}

	// Query user_list via WS
	env, err := ws.Req("user_list", map[string]any{}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("user_list failed: %s", string(env.D))
	}

	var result struct {
		Users []struct {
			Username string `json:"username"`
			Type     string `json:"type"`
		} `json:"users"`
	}
	if err := json.Unmarshal(env.D, &result); err != nil {
		t.Fatalf("unmarshal user_list: %v (raw: %s)", err, string(env.D))
	}

	found := false
	for _, u := range result.Users {
		if u.Username == "alice" {
			found = true
			if u.Type != "user" {
				t.Errorf("alice type = %q, want %q", u.Type, "user")
			}
		}
	}
	if !found {
		t.Errorf("alice not found in WS user_list: %s", string(env.D))
	}
}

func TestWebhookNotFiredWithoutMention(t *testing.T) {
	var mu sync.Mutex
	var received []map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]interface{}
		json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		received = append(received, p)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr, harness.WithWebhookURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Register alice and bob
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

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	// Create channel with both
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name": "no-webhook", "public": true, "members": []string{"bob"},
	})
	if err != nil || r.Error != nil {
		t.Fatalf("create channel: err=%v rpc=%+v", err, r.Error)
	}

	// Alice sends message WITHOUT mentioning bob
	r, err = alice.ToolCall("send_message", map[string]any{
		"channel": "no-webhook", "message": "just a regular message",
	})
	if err != nil || r.Error != nil {
		t.Fatalf("send message: err=%v rpc=%+v", err, r.Error)
	}

	// Wait to verify no webhook fires
	time.Sleep(1 * time.Second)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 0 {
		t.Errorf("expected 0 webhooks for non-mention message, got %d", len(received))
	}
}

// --- Backup tests ---

func TestBackupExportImport(t *testing.T) {
	bucket := os.Getenv("SHARKFIN_BACKUP_TEST_BUCKET")
	if bucket == "" {
		t.Skip("SHARKFIN_BACKUP_TEST_BUCKET not set")
	}
	if os.Getenv("SHARKFIN_DB") != "" {
		t.Skip("backup e2e requires SQLite (SHARKFIN_DB must not be set)")
	}
	region := os.Getenv("SHARKFIN_BACKUP_TEST_REGION")
	endpoint := os.Getenv("SHARKFIN_BACKUP_TEST_ENDPOINT")
	accessKey := os.Getenv("SHARKFIN_BACKUP_TEST_ACCESS_KEY")
	secretKey := os.Getenv("SHARKFIN_BACKUP_TEST_SECRET_KEY")
	passphrase := os.Getenv("SHARKFIN_BACKUP_TEST_PASSPHRASE")
	if passphrase == "" {
		passphrase = "test-passphrase"
	}

	// --- Daemon A: populate data ---
	addrA, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	dA, err := harness.StartDaemon(sharkfinBin, addrA)
	if err != nil {
		t.Fatal(err)
	}
	defer dA.Cleanup()

	alice := harness.NewClient(addrA)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	bob := harness.NewClient(addrA)
	if err := bob.RegisterFlow("bob"); err != nil {
		t.Fatal(err)
	}

	if err := dA.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	r, err := alice.ToolCall("channel_create", map[string]any{
		"name": "general", "public": true, "members": []string{"bob"},
	})
	if err != nil || r.Error != nil {
		t.Fatalf("create channel: err=%v rpc=%+v", err, r.Error)
	}

	r, err = alice.ToolCall("send_message", map[string]any{
		"channel": "general", "message": "hello from alice", "mentions": []string{"bob"},
	})
	if err != nil || r.Error != nil {
		t.Fatalf("send: err=%v rpc=%+v", err, r.Error)
	}

	r, err = bob.ToolCall("send_message", map[string]any{
		"channel": "general", "message": "hello from bob",
	})
	if err != nil || r.Error != nil {
		t.Fatalf("send: err=%v rpc=%+v", err, r.Error)
	}

	alice.DisconnectPresence()
	bob.DisconnectPresence()
	dA.StopNoClean(t)

	// --- Export ---
	s3Flags := []string{
		"--s3-bucket", bucket,
		"--s3-region", region,
		"--s3-access-key", accessKey,
		"--s3-secret-key", secretKey,
	}
	if endpoint != "" {
		s3Flags = append(s3Flags, "--s3-endpoint", endpoint)
	}

	exportArgs := append([]string{
		"backup", "export",
		"--db", dA.DBPath(),
		"--passphrase", passphrase,
	}, s3Flags...)

	exportOut, err := exec.Command(sharkfinBin, exportArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("export: %v\n%s", err, exportOut)
	}

	// Parse key from "Uploaded: sharkfin-backup-...tar.xz.age (123 B)"
	var key string
	for _, line := range strings.Split(string(exportOut), "\n") {
		if strings.HasPrefix(line, "Uploaded: ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				key = fields[1]
			}
		}
	}
	if key == "" {
		t.Fatalf("could not parse key from export output: %s", exportOut)
	}
	t.Logf("exported key: %s", key)

	// --- Import into fresh DB, start daemon B ---
	tmpDir, err := os.MkdirTemp("", "sharkfin-backup-import-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	dbPathB := filepath.Join(tmpDir, "imported.db")

	importArgs := append([]string{
		"backup", "import", key,
		"--db", dbPathB,
		"--passphrase", passphrase,
	}, s3Flags...)

	importOut, err := exec.Command(sharkfinBin, importArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("import: %v\n%s", err, importOut)
	}
	t.Logf("import output: %s", importOut)

	// Start daemon B using the imported DB
	addrB, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	dB, err := harness.StartDaemon(sharkfinBin, addrB, harness.WithDB(dbPathB))
	if err != nil {
		t.Fatal(err)
	}
	defer dB.StopFatal(t)

	// --- Verify via MCP ---
	verifier := harness.NewClient(addrB)
	if err := verifier.IdentifyFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer verifier.DisconnectPresence()

	// Verify users
	ur, err := verifier.ToolCall("user_list", map[string]any{})
	if err != nil || ur.Error != nil {
		t.Fatalf("user_list: err=%v rpc=%+v", err, ur.Error)
	}
	if !strings.Contains(ur.Text, "alice") || !strings.Contains(ur.Text, "bob") {
		t.Errorf("expected alice and bob in user_list, got: %s", ur.Text)
	}

	// Verify messages in general channel
	hr, err := verifier.ToolCall("history", map[string]any{
		"channel": "general", "limit": 50,
	})
	if err != nil || hr.Error != nil {
		t.Fatalf("history: err=%v rpc=%+v", err, hr.Error)
	}
	if !strings.Contains(hr.Text, "hello from alice") {
		t.Errorf("expected 'hello from alice' in history, got: %s", hr.Text)
	}
	if !strings.Contains(hr.Text, "hello from bob") {
		t.Errorf("expected 'hello from bob' in history, got: %s", hr.Text)
	}
}
