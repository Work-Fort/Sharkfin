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
