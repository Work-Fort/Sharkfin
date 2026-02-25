// SPDX-License-Identifier: GPL-2.0-only
package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Work-Fort/sharkfin/pkg/db"
	"github.com/Work-Fort/sharkfin/pkg/protocol"
)

type testEnv struct {
	handler *MCPHandler
	sm      *SessionManager
	db      *db.DB
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	sm := NewSessionManager(d, true)
	h := NewMCPHandler(sm, d)
	return &testEnv{handler: h, sm: sm, db: d}
}

func mcpCall(t *testing.T, handler http.Handler, sessionID string, method string, id int, params interface{}) (*http.Response, protocol.Response) {
	t.Helper()
	var paramsJSON json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
		paramsJSON = b
	}

	reqBody := protocol.Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsJSON,
		ID:      &protocol.RequestID{IntValue: int64(id)},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	respBody, _ := io.ReadAll(resp.Body)

	var rpcResp protocol.Response
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		t.Fatalf("unmarshal response: %v (body: %s)", err, string(respBody))
	}
	return resp, rpcResp
}

// registerUser creates a user through the MCP flow and returns the session ID.
// Simulates the bridge: creates token + attaches presence directly.
func registerUser(t *testing.T, env *testEnv, username string) string {
	t.Helper()

	// Initialize
	httpResp, _ := mcpCall(t, env.handler, "", "initialize", 1, map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"clientInfo":      map[string]string{"name": "test"},
		"capabilities":    map[string]interface{}{},
	})
	sessionID := httpResp.Header.Get("Mcp-Session-Id")

	// Simulate bridge: create token and attach presence directly
	token := env.sm.CreateIdentityToken()
	env.sm.AttachPresence(token)

	// Register — captures the new session ID from the response
	regResp, rpcResp := mcpCall(t, env.handler, sessionID, "tools/call", 3, map[string]interface{}{
		"name": "register",
		"arguments": map[string]interface{}{
			"token":    token,
			"username": username,
			"password": "",
		},
	})
	if rpcResp.Error != nil {
		t.Fatalf("register failed: %s", rpcResp.Error.Message)
	}

	newSessionID := regResp.Header.Get("Mcp-Session-Id")
	if newSessionID != "" {
		return newSessionID
	}
	return sessionID
}

// --- Tests ---

func TestInitialize(t *testing.T) {
	env := newTestEnv(t)
	httpResp, rpcResp := mcpCall(t, env.handler, "", "initialize", 1, map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"clientInfo":      map[string]string{"name": "test"},
		"capabilities":    map[string]interface{}{},
	})

	if rpcResp.Error != nil {
		t.Fatalf("error: %s", rpcResp.Error.Message)
	}

	sessionID := httpResp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Error("expected Mcp-Session-Id header")
	}

	var result map[string]interface{}
	json.Unmarshal(rpcResp.Result, &result)
	if result["protocolVersion"] != "2025-03-26" {
		t.Errorf("protocolVersion = %v, want 2025-03-26", result["protocolVersion"])
	}
}

func TestToolsList(t *testing.T) {
	env := newTestEnv(t)
	_, rpcResp := mcpCall(t, env.handler, "", "tools/list", 1, nil)

	if rpcResp.Error != nil {
		t.Fatalf("error: %s", rpcResp.Error.Message)
	}

	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	json.Unmarshal(rpcResp.Result, &result)

	expected := map[string]bool{
		"get_identity_token": true, "register": true, "identify": true,
		"user_list": true, "channel_list": true, "channel_create": true,
		"channel_invite": true, "send_message": true, "unread_messages": true,
	}
	if len(result.Tools) != len(expected) {
		t.Errorf("got %d tools, want %d", len(result.Tools), len(expected))
	}
	for _, tool := range result.Tools {
		if !expected[tool.Name] {
			t.Errorf("unexpected tool: %s", tool.Name)
		}
	}
}

func TestRegister(t *testing.T) {
	env := newTestEnv(t)
	sessionID := registerUser(t, env, "alice")
	if sessionID == "" {
		t.Error("expected non-empty session ID")
	}
}

func TestRegisterAfterIdentifiedViaHandler(t *testing.T) {
	env := newTestEnv(t)
	sessionID := registerUser(t, env, "alice")

	_, rpcResp := mcpCall(t, env.handler, sessionID, "tools/call", 10, map[string]interface{}{
		"name": "register",
		"arguments": map[string]interface{}{
			"token":    "whatever",
			"username": "bob",
			"password": "",
		},
	})
	if rpcResp.Error == nil {
		t.Error("expected error: already identified")
	}
}

func TestIdentifyUserAlreadyOnline(t *testing.T) {
	env := newTestEnv(t)
	registerUser(t, env, "alice")

	// Simulate a second bridge: create token and attach presence directly
	token := env.sm.CreateIdentityToken()
	env.sm.AttachPresence(token)

	_, rpcResp := mcpCall(t, env.handler, "", "tools/call", 3, map[string]interface{}{
		"name": "identify",
		"arguments": map[string]interface{}{
			"token":    token,
			"username": "alice",
			"password": "",
		},
	})
	if rpcResp.Error == nil {
		t.Error("expected error: user already online")
	}
}

func TestUserList(t *testing.T) {
	env := newTestEnv(t)
	sessionID := registerUser(t, env, "alice")

	_, rpcResp := mcpCall(t, env.handler, sessionID, "tools/call", 10, map[string]interface{}{
		"name":      "user_list",
		"arguments": map[string]interface{}{},
	})
	if rpcResp.Error != nil {
		t.Fatalf("error: %s", rpcResp.Error.Message)
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.Unmarshal(rpcResp.Result, &result)
	if len(result.Content) == 0 {
		t.Fatal("expected content")
	}
	// Should contain alice
	text := result.Content[0].Text
	if text == "" {
		t.Error("expected non-empty user list")
	}
}

func TestChannelCreate(t *testing.T) {
	env := newTestEnv(t)
	sessionID := registerUser(t, env, "alice")

	_, rpcResp := mcpCall(t, env.handler, sessionID, "tools/call", 10, map[string]interface{}{
		"name": "channel_create",
		"arguments": map[string]interface{}{
			"name":    "general",
			"public":  true,
			"members": []string{},
		},
	})
	if rpcResp.Error != nil {
		t.Fatalf("error: %s", rpcResp.Error.Message)
	}
}

func TestChannelCreateDisabled(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	sm := NewSessionManager(d, false)
	h := NewMCPHandler(sm, d)
	env := &testEnv{handler: h, sm: sm, db: d}

	sessionID := registerUser(t, env, "alice")

	_, rpcResp := mcpCall(t, env.handler, sessionID, "tools/call", 10, map[string]interface{}{
		"name": "channel_create",
		"arguments": map[string]interface{}{
			"name":    "secret",
			"public":  false,
			"members": []string{},
		},
	})
	if rpcResp.Error == nil {
		t.Error("expected error: channel creation disabled")
	}
}

func TestChannelInvite(t *testing.T) {
	env := newTestEnv(t)
	aliceSession := registerUser(t, env, "alice")
	registerUser(t, env, "bob")

	// Alice creates a channel
	_, rpcResp := mcpCall(t, env.handler, aliceSession, "tools/call", 10, map[string]interface{}{
		"name": "channel_create",
		"arguments": map[string]interface{}{
			"name":    "project",
			"public":  false,
			"members": []string{},
		},
	})
	if rpcResp.Error != nil {
		t.Fatalf("create channel: %s", rpcResp.Error.Message)
	}

	// Get channel ID from response
	var createResult struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.Unmarshal(rpcResp.Result, &createResult)

	// Alice invites bob
	_, rpcResp = mcpCall(t, env.handler, aliceSession, "tools/call", 11, map[string]interface{}{
		"name": "channel_invite",
		"arguments": map[string]interface{}{
			"channel":  "project",
			"username": "bob",
		},
	})
	if rpcResp.Error != nil {
		t.Fatalf("invite: %s", rpcResp.Error.Message)
	}
}

func TestChannelInviteNonParticipant(t *testing.T) {
	env := newTestEnv(t)
	aliceSession := registerUser(t, env, "alice")
	bobSession := registerUser(t, env, "bob")
	registerUser(t, env, "charlie")

	// Alice creates a channel (only alice is member)
	mcpCall(t, env.handler, aliceSession, "tools/call", 10, map[string]interface{}{
		"name": "channel_create",
		"arguments": map[string]interface{}{
			"name":    "secret",
			"public":  false,
			"members": []string{},
		},
	})

	// Bob tries to invite charlie — should fail (bob is not a participant)
	_, rpcResp := mcpCall(t, env.handler, bobSession, "tools/call", 11, map[string]interface{}{
		"name": "channel_invite",
		"arguments": map[string]interface{}{
			"channel":  "secret",
			"username": "charlie",
		},
	})
	if rpcResp.Error == nil {
		t.Error("expected error: bob is not a participant")
	}
}

func TestSendMessage(t *testing.T) {
	env := newTestEnv(t)
	aliceSession := registerUser(t, env, "alice")

	mcpCall(t, env.handler, aliceSession, "tools/call", 10, map[string]interface{}{
		"name": "channel_create",
		"arguments": map[string]interface{}{
			"name":    "general",
			"public":  true,
			"members": []string{},
		},
	})

	_, rpcResp := mcpCall(t, env.handler, aliceSession, "tools/call", 11, map[string]interface{}{
		"name": "send_message",
		"arguments": map[string]interface{}{
			"channel": "general",
			"message": "hello world",
		},
	})
	if rpcResp.Error != nil {
		t.Fatalf("send message: %s", rpcResp.Error.Message)
	}
}

func TestSendMessageNonParticipant(t *testing.T) {
	env := newTestEnv(t)
	aliceSession := registerUser(t, env, "alice")
	bobSession := registerUser(t, env, "bob")

	mcpCall(t, env.handler, aliceSession, "tools/call", 10, map[string]interface{}{
		"name": "channel_create",
		"arguments": map[string]interface{}{
			"name":    "secret",
			"public":  false,
			"members": []string{},
		},
	})

	_, rpcResp := mcpCall(t, env.handler, bobSession, "tools/call", 11, map[string]interface{}{
		"name": "send_message",
		"arguments": map[string]interface{}{
			"channel": "secret",
			"message": "sneaky",
		},
	})
	if rpcResp.Error == nil {
		t.Error("expected error: bob is not a participant")
	}
}

func TestUnreadMessages(t *testing.T) {
	env := newTestEnv(t)
	aliceSession := registerUser(t, env, "alice")
	bobSession := registerUser(t, env, "bob")

	// Alice creates channel with bob
	mcpCall(t, env.handler, aliceSession, "tools/call", 10, map[string]interface{}{
		"name": "channel_create",
		"arguments": map[string]interface{}{
			"name":    "dm",
			"public":  false,
			"members": []string{"bob"},
		},
	})

	// Alice sends a message
	mcpCall(t, env.handler, aliceSession, "tools/call", 11, map[string]interface{}{
		"name": "send_message",
		"arguments": map[string]interface{}{
			"channel": "dm",
			"message": "hey bob",
		},
	})

	// Bob reads unread
	_, rpcResp := mcpCall(t, env.handler, bobSession, "tools/call", 12, map[string]interface{}{
		"name":      "unread_messages",
		"arguments": map[string]interface{}{},
	})
	if rpcResp.Error != nil {
		t.Fatalf("unread: %s", rpcResp.Error.Message)
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.Unmarshal(rpcResp.Result, &result)
	if len(result.Content) == 0 {
		t.Fatal("expected messages")
	}
	fmt.Printf("Unread messages: %s\n", result.Content[0].Text)
}

func TestUnidentifiedSessionCannotUseProtectedTools(t *testing.T) {
	env := newTestEnv(t)

	// Initialize but don't identify
	httpResp, _ := mcpCall(t, env.handler, "", "initialize", 1, map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"clientInfo":      map[string]string{"name": "test"},
		"capabilities":    map[string]interface{}{},
	})
	sessionID := httpResp.Header.Get("Mcp-Session-Id")

	tools := []string{"user_list", "channel_list", "channel_create", "channel_invite", "send_message", "unread_messages"}
	for _, tool := range tools {
		_, rpcResp := mcpCall(t, env.handler, sessionID, "tools/call", 10, map[string]interface{}{
			"name":      tool,
			"arguments": map[string]interface{}{},
		})
		if rpcResp.Error == nil {
			t.Errorf("%s: expected error for unidentified session", tool)
		}
	}
}

func TestUnknownToolName(t *testing.T) {
	env := newTestEnv(t)
	_, rpcResp := mcpCall(t, env.handler, "", "tools/call", 1, map[string]interface{}{
		"name":      "nonexistent_tool",
		"arguments": map[string]interface{}{},
	})
	if rpcResp.Error == nil {
		t.Error("expected error for unknown tool")
	}
}
