// SPDX-License-Identifier: GPL-2.0-only
package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	pkgdaemon "github.com/Work-Fort/sharkfin/pkg/daemon"
	"github.com/Work-Fort/sharkfin/pkg/protocol"
)

// testEnv holds a running server and helpers for integration tests.
type testEnv struct {
	t      *testing.T
	srv    *pkgdaemon.Server
	addr   string
	cancel context.CancelFunc
}

func startTestServer(t *testing.T, allowChannelCreation bool) *testEnv {
	t.Helper()

	// Find a free port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	srv, err := pkgdaemon.NewServer(addr, ":memory:", allowChannelCreation)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Wait for server to be ready
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		select {
		case err := <-errCh:
			t.Fatalf("server start failed: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Cleanup(func() {
		cancel()
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutCancel()
		srv.Shutdown(shutCtx)
	})

	_ = ctx
	return &testEnv{t: t, srv: srv, addr: addr, cancel: cancel}
}

// mcpRequest sends a JSON-RPC request to /mcp and returns the HTTP response and parsed JSON-RPC response.
func (e *testEnv) mcpRequest(sessionID string, method string, id int, params interface{}) (*http.Response, protocol.Response) {
	e.t.Helper()

	var paramsJSON json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			e.t.Fatalf("marshal params: %v", err)
		}
		paramsJSON = data
	}

	reqID := protocol.RequestID{IntValue: int64(id)}
	req := protocol.Request{
		JSONRPC: "2.0",
		Method:  method,
		ID:      &reqID,
		Params:  paramsJSON,
	}

	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequest("POST", fmt.Sprintf("http://%s/mcp", e.addr), bytes.NewReader(body))
	if err != nil {
		e.t.Fatalf("create request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", sessionID)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		e.t.Fatalf("do request: %v", err)
	}

	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var rpcResp protocol.Response
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &rpcResp); err != nil {
			e.t.Fatalf("unmarshal response: %v (body: %s)", err, string(respBody))
		}
	}
	return resp, rpcResp
}

// toolCall is a convenience for calling tools/call.
func (e *testEnv) toolCall(sessionID string, id int, name string, args interface{}) (*http.Response, protocol.Response) {
	e.t.Helper()
	return e.mcpRequest(sessionID, "tools/call", id, map[string]interface{}{
		"name":      name,
		"arguments": args,
	})
}

// toolResultText extracts the text from a successful tool result.
func toolResultText(t *testing.T, resp protocol.Response) string {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("expected success, got error: %s", resp.Error.Message)
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal tool result: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("empty tool result content")
	}
	return result.Content[0].Text
}

// registerUser performs the full identity handshake: initialize → get_identity_token → presence → register.
// Returns the MCP session ID and a cancel function to disconnect presence.
func (e *testEnv) registerUser(username string, id int) (sessionID string, cancelPresence func()) {
	e.t.Helper()

	// 1. Initialize
	_, _ = e.mcpRequest("", "initialize", id, map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]string{"name": "test", "version": "0.1"},
	})

	// 2. Get identity token
	_, tokenResp := e.toolCall("", id+1, "get_identity_token", map[string]interface{}{})
	token := toolResultText(e.t, tokenResp)

	// 3. Start presence in background
	ctx, cancel := context.WithCancel(context.Background())
	presenceDone := make(chan struct{})
	go func() {
		defer close(presenceDone)
		body, _ := json.Marshal(map[string]string{"token": token})
		req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("http://%s/presence", e.addr), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}()

	// Wait for presence to attach
	time.Sleep(50 * time.Millisecond)

	// 4. Register
	httpResp, regResp := e.toolCall("", id+2, "register", map[string]interface{}{
		"token": token, "username": username, "password": "",
	})
	if regResp.Error != nil {
		e.t.Fatalf("register %s failed: %s", username, regResp.Error.Message)
	}
	sessionID = httpResp.Header.Get("Mcp-Session-Id")

	cancelPresence = func() {
		cancel()
		<-presenceDone
	}

	return sessionID, cancelPresence
}

// identifyUser performs the full identity handshake for an existing user: initialize → get_identity_token → presence → identify.
func (e *testEnv) identifyUser(username string, id int) (sessionID string, cancelPresence func()) {
	e.t.Helper()

	// 1. Initialize
	_, _ = e.mcpRequest("", "initialize", id, map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]string{"name": "test", "version": "0.1"},
	})

	// 2. Get identity token
	_, tokenResp := e.toolCall("", id+1, "get_identity_token", map[string]interface{}{})
	token := toolResultText(e.t, tokenResp)

	// 3. Start presence
	ctx, cancel := context.WithCancel(context.Background())
	presenceDone := make(chan struct{})
	go func() {
		defer close(presenceDone)
		body, _ := json.Marshal(map[string]string{"token": token})
		req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("http://%s/presence", e.addr), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}()
	time.Sleep(50 * time.Millisecond)

	// 4. Identify
	httpResp, identResp := e.toolCall("", id+2, "identify", map[string]interface{}{
		"token": token, "username": username, "password": "",
	})
	if identResp.Error != nil {
		e.t.Fatalf("identify %s failed: %s", username, identResp.Error.Message)
	}
	sessionID = httpResp.Header.Get("Mcp-Session-Id")

	cancelPresence = func() {
		cancel()
		<-presenceDone
	}
	return sessionID, cancelPresence
}

// --- Test Scenarios ---

func TestScenario1_IdentityHandshakeAndPresence(t *testing.T) {
	env := startTestServer(t, true)

	sessionID, cancelPresence := env.registerUser("alice", 1)

	// Verify user is online
	_, ulResp := env.toolCall(sessionID, 10, "user_list", map[string]interface{}{})
	users := toolResultText(t, ulResp)

	var userList []struct {
		Username string `json:"username"`
		Online   bool   `json:"online"`
	}
	if err := json.Unmarshal([]byte(users), &userList); err != nil {
		t.Fatalf("unmarshal user list: %v", err)
	}
	if len(userList) != 1 || userList[0].Username != "alice" || !userList[0].Online {
		t.Fatalf("expected alice online, got: %v", userList)
	}

	// Disconnect presence
	cancelPresence()
	time.Sleep(100 * time.Millisecond)

	// Verify user is offline (need to re-register since session was cleaned up on disconnect)
	// Actually, the session should still work for a bit — let's check
	_, ulResp2 := env.toolCall(sessionID, 11, "user_list", map[string]interface{}{})
	if ulResp2.Error != nil {
		// Session was cleaned up on disconnect — this is expected behavior.
		// Register a new session to check the user list.
		sessionID2, cancelPresence2 := env.identifyUser("alice", 20)
		defer cancelPresence2()

		_, ulResp3 := env.toolCall(sessionID2, 30, "user_list", map[string]interface{}{})
		users2 := toolResultText(t, ulResp3)
		var ul2 []struct {
			Username string `json:"username"`
			Online   bool   `json:"online"`
		}
		json.Unmarshal([]byte(users2), &ul2)
		// At this point alice is online again because we just identified
		if len(ul2) != 1 || ul2[0].Username != "alice" {
			t.Fatalf("expected alice in user list, got: %v", ul2)
		}
	} else {
		// Session survived disconnect — check that alice is offline
		users2 := toolResultText(t, ulResp2)
		var ul2 []struct {
			Username string `json:"username"`
			Online   bool   `json:"online"`
		}
		json.Unmarshal([]byte(users2), &ul2)
		if len(ul2) != 1 || ul2[0].Username != "alice" || ul2[0].Online {
			t.Fatalf("expected alice offline, got: %v", ul2)
		}
	}
}

func TestScenario2_MessagingBetweenTwoUsers(t *testing.T) {
	env := startTestServer(t, true)

	sessionA, cancelA := env.registerUser("alice", 1)
	defer cancelA()
	sessionB, cancelB := env.registerUser("bob", 10)
	defer cancelB()

	// Alice creates a private channel with Bob
	_, chResp := env.toolCall(sessionA, 20, "channel_create", map[string]interface{}{
		"name": "alice-bob", "public": false, "members": []string{"bob"},
	})
	if chResp.Error != nil {
		t.Fatalf("channel_create: %s", chResp.Error.Message)
	}

	// Alice sends a message
	_, sendResp := env.toolCall(sessionA, 21, "send_message", map[string]interface{}{
		"channel": "alice-bob", "message": "hello bob!",
	})
	if sendResp.Error != nil {
		t.Fatalf("send_message: %s", sendResp.Error.Message)
	}

	// Bob reads unread messages
	_, unreadResp := env.toolCall(sessionB, 22, "unread_messages", map[string]interface{}{})
	unreadText := toolResultText(t, unreadResp)

	var msgs []struct {
		Channel string `json:"channel"`
		From    string `json:"from"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal([]byte(unreadText), &msgs); err != nil {
		t.Fatalf("unmarshal messages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].From != "alice" || msgs[0].Body != "hello bob!" || msgs[0].Channel != "alice-bob" {
		t.Fatalf("expected 1 message from alice, got: %v", msgs)
	}

	// Bob reads again — no new messages
	_, unreadResp2 := env.toolCall(sessionB, 23, "unread_messages", map[string]interface{}{})
	unreadText2 := toolResultText(t, unreadResp2)
	if unreadText2 != "null" && unreadText2 != "[]" {
		var msgs2 []interface{}
		json.Unmarshal([]byte(unreadText2), &msgs2)
		if len(msgs2) != 0 {
			t.Fatalf("expected no new messages, got: %s", unreadText2)
		}
	}

	// Alice sends another message
	_, _ = env.toolCall(sessionA, 24, "send_message", map[string]interface{}{
		"channel": "alice-bob", "message": "are you there?",
	})

	// Bob reads — gets only the new message
	_, unreadResp3 := env.toolCall(sessionB, 25, "unread_messages", map[string]interface{}{})
	unreadText3 := toolResultText(t, unreadResp3)
	var msgs3 []struct {
		Body string `json:"body"`
	}
	json.Unmarshal([]byte(unreadText3), &msgs3)
	if len(msgs3) != 1 || msgs3[0].Body != "are you there?" {
		t.Fatalf("expected 1 new message, got: %v", msgs3)
	}
}

func TestScenario3_ChannelVisibility(t *testing.T) {
	env := startTestServer(t, true)

	sessionA, cancelA := env.registerUser("alice", 1)
	defer cancelA()
	sessionB, cancelB := env.registerUser("bob", 10)
	defer cancelB()
	sessionC, cancelC := env.registerUser("charlie", 20)
	defer cancelC()

	// Alice creates public channel, adds Bob
	_, _ = env.toolCall(sessionA, 30, "channel_create", map[string]interface{}{
		"name": "general", "public": true, "members": []string{"bob"},
	})

	// Charlie sees the public channel
	_, clResp := env.toolCall(sessionC, 31, "channel_list", map[string]interface{}{})
	clText := toolResultText(t, clResp)
	var channels []struct {
		Name   string `json:"name"`
		Public bool   `json:"public"`
	}
	json.Unmarshal([]byte(clText), &channels)
	found := false
	for _, ch := range channels {
		if ch.Name == "general" {
			found = true
		}
	}
	if !found {
		t.Fatalf("charlie should see public channel 'general', got: %v", channels)
	}

	// Alice creates private channel with Bob
	_, _ = env.toolCall(sessionA, 32, "channel_create", map[string]interface{}{
		"name": "secret", "public": false, "members": []string{"bob"},
	})

	// Charlie should NOT see the private channel
	_, clResp2 := env.toolCall(sessionC, 33, "channel_list", map[string]interface{}{})
	clText2 := toolResultText(t, clResp2)
	var channels2 []struct {
		Name string `json:"name"`
	}
	json.Unmarshal([]byte(clText2), &channels2)
	for _, ch := range channels2 {
		if ch.Name == "secret" {
			t.Fatal("charlie should NOT see private channel 'secret'")
		}
	}

	// Bob sees both channels
	_, clResp3 := env.toolCall(sessionB, 34, "channel_list", map[string]interface{}{})
	clText3 := toolResultText(t, clResp3)
	var channels3 []struct {
		Name string `json:"name"`
	}
	json.Unmarshal([]byte(clText3), &channels3)
	foundGeneral := false
	foundSecret := false
	for _, ch := range channels3 {
		if ch.Name == "general" {
			foundGeneral = true
		}
		if ch.Name == "secret" {
			foundSecret = true
		}
	}
	if !foundGeneral || !foundSecret {
		t.Fatalf("bob should see both channels, got: %v", channels3)
	}
}

func TestScenario4_ChannelCreationDisabled(t *testing.T) {
	env := startTestServer(t, false)

	sessionA, cancelA := env.registerUser("alice", 1)
	defer cancelA()

	_, chResp := env.toolCall(sessionA, 10, "channel_create", map[string]interface{}{
		"name": "test-channel", "public": true,
	})
	if chResp.Error == nil {
		t.Fatal("expected error when channel creation is disabled")
	}
	if chResp.Error.Message != "channel creation is disabled" {
		t.Fatalf("unexpected error: %s", chResp.Error.Message)
	}
}

func TestScenario5_SessionStateConstraints(t *testing.T) {
	env := startTestServer(t, true)

	sessionA, cancelA := env.registerUser("alice", 1)
	defer cancelA()

	// Try register again — should fail
	_, regResp := env.toolCall(sessionA, 10, "register", map[string]interface{}{
		"token": "fake-token", "username": "alice2", "password": "",
	})
	if regResp.Error == nil {
		t.Fatal("expected error on second register")
	}

	// Try identify — should also fail
	_, identResp := env.toolCall(sessionA, 11, "identify", map[string]interface{}{
		"token": "fake-token", "username": "alice", "password": "",
	})
	if identResp.Error == nil {
		t.Fatal("expected error on identify after register")
	}
}

func TestScenario6_DoubleLoginPrevention(t *testing.T) {
	env := startTestServer(t, true)

	_, cancelA := env.registerUser("alice", 1)
	defer cancelA()

	// Get a new token and presence for a second connection
	_, _ = env.mcpRequest("", "initialize", 20, map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]string{"name": "test", "version": "0.1"},
	})

	_, tokenResp := env.toolCall("", 21, "get_identity_token", map[string]interface{}{})
	token2 := toolResultText(t, tokenResp)

	// Start presence for token2
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go func() {
		body, _ := json.Marshal(map[string]string{"token": token2})
		req, _ := http.NewRequestWithContext(ctx2, "POST", fmt.Sprintf("http://%s/presence", env.addr), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}()
	time.Sleep(50 * time.Millisecond)

	// Try to identify as alice — should fail because alice is already online
	_, identResp := env.toolCall("", 22, "identify", map[string]interface{}{
		"token": token2, "username": "alice", "password": "",
	})
	if identResp.Error == nil {
		t.Fatal("expected error: alice is already online")
	}
	if identResp.Error.Message != "user already online: alice" {
		t.Fatalf("unexpected error: %s", identResp.Error.Message)
	}
}

func TestScenario7_ChannelInvite(t *testing.T) {
	env := startTestServer(t, true)

	sessionA, cancelA := env.registerUser("alice", 1)
	defer cancelA()
	sessionB, cancelB := env.registerUser("bob", 10)
	defer cancelB()
	sessionC, cancelC := env.registerUser("charlie", 20)
	defer cancelC()

	// Alice creates private channel with Bob
	_, _ = env.toolCall(sessionA, 30, "channel_create", map[string]interface{}{
		"name": "project-x", "public": false, "members": []string{"bob"},
	})

	// Bob invites Charlie
	_, invResp := env.toolCall(sessionB, 31, "channel_invite", map[string]interface{}{
		"channel": "project-x", "username": "charlie",
	})
	if invResp.Error != nil {
		t.Fatalf("invite failed: %s", invResp.Error.Message)
	}

	// Charlie can now see the channel
	_, clResp := env.toolCall(sessionC, 32, "channel_list", map[string]interface{}{})
	clText := toolResultText(t, clResp)
	var channels []struct {
		Name string `json:"name"`
	}
	json.Unmarshal([]byte(clText), &channels)
	found := false
	for _, ch := range channels {
		if ch.Name == "project-x" {
			found = true
		}
	}
	if !found {
		t.Fatalf("charlie should see 'project-x' after invite, got: %v", channels)
	}

	// Charlie can send a message
	_, sendResp := env.toolCall(sessionC, 33, "send_message", map[string]interface{}{
		"channel": "project-x", "message": "hey everyone!",
	})
	if sendResp.Error != nil {
		t.Fatalf("charlie send_message failed: %s", sendResp.Error.Message)
	}
}

func TestScenario8_NonParticipantRejection(t *testing.T) {
	env := startTestServer(t, true)

	sessionA, cancelA := env.registerUser("alice", 1)
	defer cancelA()
	_, cancelB := env.registerUser("bob", 10)
	defer cancelB()

	// Alice creates a private channel (alone)
	_, _ = env.toolCall(sessionA, 20, "channel_create", map[string]interface{}{
		"name": "private-notes", "public": false,
	})

	// Bob tries to send a message to alice's channel — should fail
	sessionB, cancelB2 := env.registerUser("charlie", 30) // register charlie to use as the outsider
	defer cancelB2()

	_, sendResp := env.toolCall(sessionB, 40, "send_message", map[string]interface{}{
		"channel": "private-notes", "message": "sneaky!",
	})
	if sendResp.Error == nil {
		t.Fatal("expected error: non-participant should not be able to send")
	}
	if sendResp.Error.Message != "you are not a participant of this channel" {
		t.Fatalf("unexpected error: %s", sendResp.Error.Message)
	}
}
