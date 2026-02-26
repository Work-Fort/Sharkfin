// SPDX-License-Identifier: GPL-2.0-only
package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	defer d.Stop()

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
	defer d.Stop()

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
	defer d.Stop()

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
	defer d.Stop()

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
	defer d.Stop()

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
	defer d.Stop()

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
		"channel_create", "channel_invite", "send_message", "unread_messages", "history",
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
	defer d.Stop()

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
	defer d.Stop()

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
	defer d.Stop()

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
	defer d.Stop()

	c := harness.NewClient(addr)
	status, body, err := c.RawPost("/mcp", "this is not json{{{")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	t.Logf("status=%d body=%s", status, string(body))

	// Server should return 200 with a JSON-RPC parse error
	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}
	if !strings.Contains(string(body), "error") {
		t.Errorf("response body should contain 'error': %s", string(body))
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
		t.Fatalf("request: %v", err)
	}
	if status != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", status, http.StatusMethodNotAllowed)
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
	defer d.Stop()

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

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
		"name":   "forbidden",
		"public": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Error == nil {
		t.Fatal("expected error when channel creation is disabled")
	}
	if !strings.Contains(strings.ToLower(r.Error.Message), "channel creation is disabled") {
		t.Errorf("error message = %q, want it to contain 'channel creation is disabled'", r.Error.Message)
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
	defer d.Stop()

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
	defer d.Stop()

	alice := harness.NewClient(addr)
	if err := alice.RegisterFlow("alice"); err != nil {
		t.Fatal(err)
	}
	defer alice.DisconnectPresence()

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
	defer d.Stop()

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
		d.Stop()
		t.Fatalf("start presence: %v", err)
	}

	// Give the presence process time to connect
	time.Sleep(500 * time.Millisecond)

	// Stop the daemon
	if err := d.Stop(); err != nil {
		t.Logf("daemon stop (expected): %v", err)
	}

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
	defer d2.Stop()

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
	defer d.Stop()

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
	defer d.Stop()

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
	defer d.Stop()

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
	defer d.Stop()

	ws, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := ws.WSRegister("alice"); err != nil {
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
	defer d.Stop()

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
	defer d.Stop()

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
	defer d.Stop()

	ws, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := ws.WSRegister("alice"); err != nil {
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
	if env.OK != nil && *env.OK {
		t.Error("expected error for invalid mention")
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
	defer d.Stop()

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
	defer d.Stop()

	ws, err := harness.NewWSClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := ws.WSRegister("alice"); err != nil {
		t.Fatal(err)
	}

	ws.Req("channel_create", map[string]any{
		"name": "general", "public": true,
	}, "c1")

	// Send parent
	parentEnv, _ := ws.Req("send_message", map[string]any{
		"channel": "general", "body": "parent",
	}, "m1")
	var pr struct{ ID int64 `json:"id"` }
	json.Unmarshal(parentEnv.D, &pr)

	// Send reply
	replyEnv, _ := ws.Req("send_message", map[string]any{
		"channel": "general", "body": "reply", "thread_id": pr.ID,
	}, "m2")
	var rr struct{ ID int64 `json:"id"` }
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
	defer d.Stop()

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
