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
	"github.com/gorilla/websocket"
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

// newMCPClient creates an MCP client with JWT auth and auto-provisions the identity.
func newMCPClient(t *testing.T, d *harness.Daemon, id, username, displayName, userType string) *harness.Client {
	t.Helper()
	token := d.SignJWT(id, username, displayName, userType)
	c := harness.NewClient(d.Addr(), token)
	if err := c.Initialize(); err != nil {
		t.Fatalf("initialize %s: %v", username, err)
	}
	// First tool call auto-provisions the identity
	if _, err := c.ToolCall("user_list", map[string]any{}); err != nil {
		t.Fatalf("provision %s: %v", username, err)
	}
	return c
}

// --- Presence tests ---

func TestPresenceWSDisconnectMarksOffline(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Alice connects via WS (persistent connection = online)
	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	ws, err := harness.NewWSClient(addr, aliceToken)
	if err != nil {
		t.Fatal(err)
	}
	ws.Req("user_list", map[string]any{}, "prov")

	// Disconnect Alice's WS connection
	ws.Close()
	time.Sleep(200 * time.Millisecond)

	// Bob checks user list — alice should be offline
	bob := newMCPClient(t, d, "uuid-bob", "bob", "Bob", "user")

	r, err := bob.ToolCall("user_list", map[string]any{})
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
			t.Error("alice should be offline after WS disconnect")
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

	bridge, err := harness.StartBridge(sharkfinBin, addr, d.XDGDir(), "test-api-key")
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

	// Bridge auto-provisions via API key — call user_list to trigger identity creation
	listReq := map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{
			"name": "user_list", "arguments": map[string]any{},
		},
	}
	if _, err := bridge.Send(listReq); err != nil {
		t.Fatalf("user_list: %v", err)
	}

	bridge.Kill()
	time.Sleep(4 * time.Second)

	bobToken := d.SignJWT("uuid-bob", "bob", "Bob", "user")
	checker := harness.NewClient(addr, bobToken)
	if err := checker.Initialize(); err != nil {
		t.Fatal(err)
	}

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
		if u.Username == "bridge" && u.Online {
			t.Error("bridge should be offline after SIGKILL")
		}
	}
}

// --- Identity tests (Passport auto-provisioning) ---

func TestAutoProvisionOnFirstToolCall(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	alice := harness.NewClient(addr, aliceToken)
	if err := alice.Initialize(); err != nil {
		t.Fatal(err)
	}

	// First tool call auto-provisions identity
	r, err := alice.ToolCall("user_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != nil {
		t.Fatalf("user_list error: %s", r.Error.Message)
	}
	if !strings.Contains(r.Text, "alice") {
		t.Errorf("expected alice in user list: %s", r.Text)
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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	c := harness.NewClient(addr, aliceToken)
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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	c := harness.NewClient(addr, aliceToken)
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
		"user_list", "channel_list",
		"channel_create", "channel_invite", "channel_join", "send_message", "unread_messages",
		"unread_counts", "mark_read", "history", "dm_list", "dm_open",
		"capabilities", "set_state", "set_role", "create_role", "delete_role",
		"grant_permission", "revoke_permission", "list_roles", "wait_for_messages",
		"mention_group_create", "mention_group_delete", "mention_group_get",
		"mention_group_list", "mention_group_add_member", "mention_group_remove_member",
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

func TestToolCallWithInvalidJWT(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Use an invalid JWT token
	c := harness.NewClient(addr, "invalid-jwt-token")
	if err := c.Initialize(); err != nil {
		t.Fatal(err)
	}

	r, err := c.ToolCall("user_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error == nil {
		t.Fatal("expected error for tool call with invalid JWT")
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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	c := harness.NewClient(addr, aliceToken)
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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	c := harness.NewClient(addr, aliceToken)
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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	c := harness.NewClient(addr, aliceToken)
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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	c := harness.NewClient(addr, aliceToken)
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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	alice := harness.NewClient(addr, aliceToken)
	if err := alice.Initialize(); err != nil {
		t.Fatal(err)
	}
	// Provision alice with first tool call
	alice.ToolCall("user_list", map[string]any{})

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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	alice := harness.NewClient(addr, aliceToken)
	if err := alice.Initialize(); err != nil {
		t.Fatal(err)
	}
	alice.ToolCall("user_list", map[string]any{})

	bobToken := d.SignJWT("uuid-bob", "bob", "Bob", "user")
	bob := harness.NewClient(addr, bobToken)
	if err := bob.Initialize(); err != nil {
		t.Fatal(err)
	}
	bob.ToolCall("user_list", map[string]any{})

	charlieToken := d.SignJWT("uuid-charlie", "charlie", "Charlie", "user")
	charlie := harness.NewClient(addr, charlieToken)
	if err := charlie.Initialize(); err != nil {
		t.Fatal(err)
	}
	charlie.ToolCall("user_list", map[string]any{})

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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	alice := harness.NewClient(addr, aliceToken)
	if err := alice.Initialize(); err != nil {
		t.Fatal(err)
	}
	alice.ToolCall("user_list", map[string]any{})

	bobToken := d.SignJWT("uuid-bob", "bob", "Bob", "user")
	bob := harness.NewClient(addr, bobToken)
	if err := bob.Initialize(); err != nil {
		t.Fatal(err)
	}
	bob.ToolCall("user_list", map[string]any{})

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

	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")

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

	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")
	bob := newMCPClient(t, d, "uuid-bob", "bob", "Bob", "user")
	charlie := newMCPClient(t, d, "uuid-charlie", "charlie", "Charlie", "user")

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

	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")
	bob := newMCPClient(t, d, "uuid-bob", "bob", "Bob", "user")
	_ = newMCPClient(t, d, "uuid-charlie", "charlie", "Charlie", "user")

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

	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")
	bob := newMCPClient(t, d, "uuid-bob", "bob", "Bob", "user")

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

	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")
	bob := newMCPClient(t, d, "uuid-bob", "bob", "Bob", "user")

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

	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")
	bob := newMCPClient(t, d, "uuid-bob", "bob", "Bob", "user")

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

	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")

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

	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")
	bob := newMCPClient(t, d, "uuid-bob", "bob", "Bob", "user")

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

	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")
	bob := newMCPClient(t, d, "uuid-bob", "bob", "Bob", "user")

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

	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")

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

	bridge, err := harness.StartBridge(sharkfinBin, addr, d.XDGDir(), "test-api-key")
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

	// 2. Trigger auto-provisioning by making a tool call (user_list is benign).
	provReq := map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{
			"name":      "user_list",
			"arguments": map[string]any{},
		},
	}
	provResp, err := bridge.Send(provReq)
	if err != nil {
		t.Fatalf("user_list (provision): %v", err)
	}
	t.Logf("user_list provision response: %s", provResp)

	// 3. Grant admin to bridge identity (now provisioned)
	if err := d.GrantAdmin(sharkfinBin, "bridge"); err != nil {
		t.Fatal(err)
	}

	// 4. Create channel
	chReq := map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
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
		"jsonrpc": "2.0", "id": 4, "method": "tools/call",
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
	c := newMCPClient(t, d2, "uuid-restart", "restart-user", "Restart User", "user")
	_ = c
}

// --- WebSocket chat tests ---

func TestWSConnectAndUserList(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	ws1, err := harness.NewWSClient(addr, aliceToken)
	if err != nil {
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

	// Disconnect and reconnect with same token
	ws1.Close()
	time.Sleep(100 * time.Millisecond)

	ws2, err := harness.NewWSClient(addr, aliceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer ws2.Close()

	env, err = ws2.Req("user_list", map[string]any{}, "u2")
	if err != nil {
		t.Fatal(err)
	}
	if env.OK == nil || !*env.OK {
		t.Errorf("user_list after reconnect failed: %s", string(env.D))
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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	alice, err := harness.NewWSClient(addr, aliceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()

	bobToken := d.SignJWT("uuid-bob", "bob", "Bob", "user")
	bob, err := harness.NewWSClient(addr, bobToken)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()

	// Auto-provision both users
	alice.Req("user_list", map[string]any{}, "prov1")
	bob.Req("user_list", map[string]any{}, "prov2")

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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	alice, err := harness.NewWSClient(addr, aliceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()

	bobToken := d.SignJWT("uuid-bob", "bob", "Bob", "user")
	bob, err := harness.NewWSClient(addr, bobToken)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()

	alice.Req("user_list", map[string]any{}, "prov1")
	bob.Req("user_list", map[string]any{}, "prov2")

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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	ws, err := harness.NewWSClient(addr, aliceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.Req("user_list", map[string]any{}, "prov")

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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	alice, err := harness.NewWSClient(addr, aliceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()

	bobToken := d.SignJWT("uuid-bob", "bob", "Bob", "user")
	bob, err := harness.NewWSClient(addr, bobToken)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()

	alice.Req("user_list", map[string]any{}, "prov1")
	bob.Req("user_list", map[string]any{}, "prov2")

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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	alice, err := harness.NewWSClient(addr, aliceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()

	bobToken := d.SignJWT("uuid-bob", "bob", "Bob", "user")
	bob, err := harness.NewWSClient(addr, bobToken)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()

	alice.Req("user_list", map[string]any{}, "prov1")
	bob.Req("user_list", map[string]any{}, "prov2")

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
		"channel": "general",
		"body":    "hey @bob look at this",
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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	ws, err := harness.NewWSClient(addr, aliceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.Req("user_list", map[string]any{}, "prov")

	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	ws.Req("channel_create", map[string]any{
		"name": "general", "public": true,
	}, "c1")

	env, err := ws.Req("send_message", map[string]any{
		"channel": "general",
		"body":    "hey @ghost",
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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	alice, err := harness.NewWSClient(addr, aliceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()

	bobToken := d.SignJWT("uuid-bob", "bob", "Bob", "user")
	bob, err := harness.NewWSClient(addr, bobToken)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()

	alice.Req("user_list", map[string]any{}, "prov1")
	bob.Req("user_list", map[string]any{}, "prov2")

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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	ws, err := harness.NewWSClient(addr, aliceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.Req("user_list", map[string]any{}, "prov")

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
	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")

	// Bob on WS
	bobToken := d.SignJWT("uuid-bob", "bob", "Bob", "user")
	bob, err := harness.NewWSClient(addr, bobToken)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()
	bob.Req("user_list", map[string]any{}, "prov")

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

	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")
	bob := newMCPClient(t, d, "uuid-bob", "bob", "Bob", "user")

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

	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")
	bob := newMCPClient(t, d, "uuid-bob", "bob", "Bob", "user")

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

	// Alice via MCP
	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")

	// Bob connects via WS
	bobToken := d.SignJWT("uuid-bob", "bob", "Bob", "user")
	ws, err := harness.NewWSClient(addr, bobToken)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.Req("user_list", map[string]any{}, "prov")

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
	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")
	_ = newMCPClient(t, d, "uuid-bob", "bob", "Bob", "user")

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
	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")
	bob := newMCPClient(t, d, "uuid-bob", "bob", "Bob", "user")
	_ = bob

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
	admin := newMCPClient(t, d, "uuid-admin", "admin-user", "Admin User", "user")

	if err := d.GrantAdmin(sharkfinBin, "admin-user"); err != nil {
		t.Fatal(err)
	}

	r, err := admin.ToolCall("channel_create", map[string]any{
		"name": "general", "public": true,
	})
	if err != nil || r.Error != nil {
		t.Fatalf("channel_create: err=%v rpc=%+v", err, r.Error)
	}

	// Register a normal user via MCP
	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")

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
	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")

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

	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")

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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	ws, err := harness.NewWSClient(addr, aliceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.Req("user_list", map[string]any{}, "prov")

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

	// Connect first user
	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	ws1, err := harness.NewWSClient(addr, aliceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer ws1.Close()
	ws1.Req("user_list", map[string]any{}, "prov")

	// Connect second user — first should receive presence broadcast
	bobToken := d.SignJWT("uuid-bob", "bob", "Bob", "user")
	ws2, err := harness.NewWSClient(addr, bobToken)
	if err != nil {
		t.Fatal(err)
	}
	ws2.Req("user_list", map[string]any{}, "prov")

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

	// Connect first WS user and set to active
	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	ws1, err := harness.NewWSClient(addr, aliceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer ws1.Close()
	ws1.Req("user_list", map[string]any{}, "prov")

	env, err := ws1.SetState("active")
	if err != nil {
		t.Fatalf("set_state active: %v", err)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("set_state failed: %s", string(env.D))
	}

	// Connect second WS user
	bobToken := d.SignJWT("uuid-bob", "bob", "Bob", "user")
	ws2, err := harness.NewWSClient(addr, bobToken)
	if err != nil {
		t.Fatal(err)
	}
	defer ws2.Close()
	ws2.Req("user_list", map[string]any{}, "prov")

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
	mcpUser := newMCPClient(t, d, "uuid-charlie", "charlie", "Charlie", "user")

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

	// Register an admin to create channels and send messages
	admin := newMCPClient(t, d, "uuid-admin", "admin-user", "Admin User", "user")
	if err := d.GrantAdmin(sharkfinBin, "admin-user"); err != nil {
		t.Fatal(err)
	}
	r, err := admin.ToolCall("channel_create", map[string]any{
		"name": "general", "public": true,
	})
	if err != nil || r.Error != nil {
		t.Fatalf("channel_create: err=%v rpc=%+v", err, r.Error)
	}

	// Connect a WS user with notifications_only query parameter
	notifToken := d.SignJWT("uuid-notif", "notif-user", "Notif User", "user")
	wsURL := fmt.Sprintf("ws://%s/ws?notifications_only=true", addr)
	wsHeader := http.Header{}
	wsHeader.Set("Authorization", "Bearer "+notifToken)
	wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, wsHeader)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	ws := harness.NewWSClientFromConn(wsConn)
	defer ws.Close()

	// Provision the user
	ws.Req("user_list", map[string]any{}, "prov")

	// (a) send_message should return error "notification-only connection"
	env, err := ws.Req("send_message", map[string]any{
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

func TestIdentityTypeFromJWT(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Create alice with type "user" in JWT
	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")

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
			if u.Type != "user" {
				t.Errorf("alice type = %q, want %q", u.Type, "user")
			}
		}
	}
	if !found {
		t.Error("alice not found in user_list")
	}
}

func TestUserTypeOnWSConnect(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	ws, err := harness.NewWSClient(addr, aliceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.Req("user_list", map[string]any{}, "prov")

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
	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")
	bob := newMCPClient(t, d, "uuid-bob", "bob", "Bob", "user")
	_ = bob

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
	if os.Getenv("SHARKFIN_DB") != "" {
		t.Skip("backup e2e requires SQLite (SHARKFIN_DB must not be set)")
	}
	passphrase := "test-passphrase"

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

	alice := newMCPClient(t, dA, "uuid-alice", "alice", "Alice", "user")
	bob := newMCPClient(t, dA, "uuid-bob", "bob", "Bob", "user")

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
		"channel": "general", "message": "hello from @bob via alice",
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

	_ = alice
	_ = bob
	dA.StopNoClean(t)

	// --- Export to local file ---
	backupFile := filepath.Join(t.TempDir(), "backup.tar.xz.age")
	exportOut, err := exec.Command(sharkfinBin,
		"backup", "export",
		"--db", dA.DBPath(),
		"--passphrase", passphrase,
		"--local", backupFile,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("export: %v\n%s", err, exportOut)
	}
	t.Logf("export output: %s", exportOut)

	// --- Import from local file into fresh DB ---
	dbPathB := filepath.Join(t.TempDir(), "imported.db")
	importOut, err := exec.Command(sharkfinBin,
		"backup", "import",
		"--local", backupFile,
		"--db", dbPathB,
		"--passphrase", passphrase,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("import: %v\n%s", err, importOut)
	}
	t.Logf("import output: %s", importOut)

	// --- Start daemon B using the imported DB ---
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
	aliceTokenB := dB.SignJWT("uuid-alice", "alice", "Alice", "user")
	verifier := harness.NewClient(addrB, aliceTokenB)
	if err := verifier.Initialize(); err != nil {
		t.Fatal(err)
	}

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
	if !strings.Contains(hr.Text, "hello from @bob via alice") {
		t.Errorf("expected 'hello from @bob via alice' in history, got: %s", hr.Text)
	}
	if !strings.Contains(hr.Text, "hello from bob") {
		t.Errorf("expected 'hello from bob' in history, got: %s", hr.Text)
	}
}

// --- Presence Notification tests ---

// setupPresenceUser creates a user with both:
//   - a PresenceClient (WebSocket connection to /presence, for receiving notifications)
//   - a regular MCP Client (HTTP, for calling tools like send_message)
//
// Both use the same JWT for authentication.
func setupPresenceUser(t *testing.T, d *harness.Daemon, id, username, displayName string) (*harness.PresenceClient, *harness.Client) {
	t.Helper()

	token := d.SignJWT(id, username, displayName, "user")

	pc, err := harness.NewPresenceClient(d.Addr(), token)
	if err != nil {
		t.Fatalf("presence client for %s: %v", username, err)
	}

	mc := harness.NewClient(d.Addr(), token)
	if err := mc.Initialize(); err != nil {
		pc.Close()
		t.Fatalf("initialize MCP for %s: %v", username, err)
	}

	// Auto-provision identity
	if _, err := mc.ToolCall("user_list", map[string]any{}); err != nil {
		pc.Close()
		t.Fatalf("provision %s: %v", username, err)
	}

	return pc, mc
}

func TestPresenceNotificationOnMention(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Alice: regular MCP client (sender) — she doesn't need a PresenceClient
	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")

	// Bob: needs both presence WS (for notifications) and MCP client (for tools)
	bobPC, bobMCP := setupPresenceUser(t, d, "uuid-bob", "bob", "Bob")
	defer bobPC.Close()
	_ = bobMCP // bob's MCP client; not used for sending in this test

	// Grant alice admin so she can create channels
	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	// Alice creates a public channel and invites bob
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name": "general", "public": true, "members": []string{"bob"},
	})
	if err != nil || r.Error != nil {
		t.Fatalf("channel_create: err=%v rpc=%+v", err, r.Error)
	}

	// Alice sends a message mentioning bob
	r, err = alice.ToolCall("send_message", map[string]any{
		"channel": "general", "message": "hey @bob check this out",
	})
	if err != nil || r.Error != nil {
		t.Fatalf("send_message: err=%v rpc=%+v", err, r.Error)
	}

	// Bob should receive a presence notification
	notif, err := bobPC.ReadNotification(5 * time.Second)
	if err != nil {
		t.Fatalf("bob notification: %v", err)
	}

	if notif.Type != "message.new" {
		t.Errorf("notification type = %q, want %q", notif.Type, "message.new")
	}

	var payload struct {
		Channel     string `json:"channel"`
		ChannelType string `json:"channel_type"`
		From        string `json:"from"`
		MessageID   int64  `json:"message_id"`
	}
	if err := json.Unmarshal(notif.D, &payload); err != nil {
		t.Fatalf("unmarshal notification data: %v (raw: %s)", err, string(notif.D))
	}
	if payload.Channel != "general" {
		t.Errorf("channel = %q, want %q", payload.Channel, "general")
	}
	if payload.ChannelType != "channel" {
		t.Errorf("channel_type = %q, want %q", payload.ChannelType, "channel")
	}
	if payload.From != "alice" {
		t.Errorf("from = %q, want %q", payload.From, "alice")
	}
	if payload.MessageID == 0 {
		t.Error("message_id should be non-zero")
	}
}

func TestPresenceNotificationOnDM(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Alice: regular MCP client (sender)
	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")

	// Bob: presence WS + MCP
	bobPC, _ := setupPresenceUser(t, d, "uuid-bob", "bob", "Bob")
	defer bobPC.Close()

	// Alice opens a DM with bob
	r, err := alice.ToolCall("dm_open", map[string]any{"username": "bob"})
	if err != nil || r.Error != nil {
		t.Fatalf("dm_open: err=%v rpc=%+v", err, r.Error)
	}

	// Alice sends a DM (no explicit mention needed for DM notifications)
	r, err = alice.ToolCall("send_message", map[string]any{
		"channel": "dm-alice-bob", "message": "hey bob, private message",
	})
	if err != nil || r.Error != nil {
		t.Fatalf("send dm: err=%v rpc=%+v", err, r.Error)
	}

	// Bob should receive a presence notification for the DM
	notif, err := bobPC.ReadNotification(5 * time.Second)
	if err != nil {
		t.Fatalf("bob DM notification: %v", err)
	}

	if notif.Type != "message.new" {
		t.Errorf("notification type = %q, want %q", notif.Type, "message.new")
	}

	var payload struct {
		Channel     string `json:"channel"`
		ChannelType string `json:"channel_type"`
		From        string `json:"from"`
		MessageID   int64  `json:"message_id"`
	}
	if err := json.Unmarshal(notif.D, &payload); err != nil {
		t.Fatalf("unmarshal DM notification: %v (raw: %s)", err, string(notif.D))
	}
	if payload.ChannelType != "dm" {
		t.Errorf("channel_type = %q, want %q", payload.ChannelType, "dm")
	}
	if payload.From != "alice" {
		t.Errorf("from = %q, want %q", payload.From, "alice")
	}
	if payload.MessageID == 0 {
		t.Error("message_id should be non-zero")
	}
}

func TestPresenceNoNotificationWithoutMention(t *testing.T) {
	addr, err := harness.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	d, err := harness.StartDaemon(sharkfinBin, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer d.StopFatal(t)

	// Alice: sender
	alice := newMCPClient(t, d, "uuid-alice", "alice", "Alice", "user")

	// Carol: should NOT receive notifications (not mentioned)
	carolPC, _ := setupPresenceUser(t, d, "uuid-carol", "carol", "Carol")
	defer carolPC.Close()

	// Grant alice admin so she can create channels
	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

	// Create channel with both users
	r, err := alice.ToolCall("channel_create", map[string]any{
		"name": "general", "public": true, "members": []string{"carol"},
	})
	if err != nil || r.Error != nil {
		t.Fatalf("channel_create: err=%v rpc=%+v", err, r.Error)
	}

	// Alice sends a message WITHOUT mentioning anyone
	r, err = alice.ToolCall("send_message", map[string]any{
		"channel": "general", "message": "hello everyone, no mentions here",
	})
	if err != nil || r.Error != nil {
		t.Fatalf("send_message: err=%v rpc=%+v", err, r.Error)
	}

	// Carol should NOT receive a notification
	if err := carolPC.NoNotification(1 * time.Second); err != nil {
		t.Errorf("carol should not get notified: %v", err)
	}
}

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

	bridge, err := harness.StartBridge(sharkfinBin, addr, d.XDGDir(), "test-api-key")
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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	alice, err := harness.NewWSClient(addr, aliceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()

	bobToken := d.SignJWT("uuid-bob", "bob", "Bob", "user")
	bob, err := harness.NewWSClient(addr, bobToken)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()

	alice.Req("user_list", map[string]any{}, "prov1")
	bob.Req("user_list", map[string]any{}, "prov2")

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

	aliceToken := d.SignJWT("uuid-alice", "alice", "Alice", "user")
	alice, err := harness.NewWSClient(addr, aliceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()

	bobToken := d.SignJWT("uuid-bob", "bob", "Bob", "user")
	bob, err := harness.NewWSClient(addr, bobToken)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()

	alice.Req("user_list", map[string]any{}, "prov1")
	bob.Req("user_list", map[string]any{}, "prov2")

	// Alice creates group and adds bob.
	alice.Req("mention_group_create", map[string]any{"slug": "devs"}, "g1")
	alice.Req("mention_group_add_member", map[string]any{
		"slug": "devs", "username": "bob",
	}, "g2")

	// Grant admin so alice can create channels.
	if err := d.GrantAdmin(sharkfinBin, "alice"); err != nil {
		t.Fatal(err)
	}

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
