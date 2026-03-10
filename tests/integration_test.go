// SPDX-License-Identifier: AGPL-3.0-or-later
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

	"github.com/gorilla/websocket"

	pkgdaemon "github.com/Work-Fort/sharkfin/pkg/daemon"
	"github.com/Work-Fort/sharkfin/pkg/infra/sqlite"
)

// jsonrpcResponse is a minimal JSON-RPC 2.0 response for integration tests.
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// testEnv holds a running server and helpers for integration tests.
type testEnv struct {
	t      *testing.T
	srv    *pkgdaemon.Server
	addr   string
	cancel context.CancelFunc
}

func startTestServer(t *testing.T) *testEnv {
	t.Helper()

	// Find a free port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	srv, err := pkgdaemon.NewServer(addr, store, 20*time.Second, "", nil)
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
func (e *testEnv) mcpRequest(sessionID string, method string, id int, params interface{}) (*http.Response, jsonrpcResponse) {
	e.t.Helper()

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"id":      id,
	}
	if params != nil {
		req["params"] = params
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

	var rpcResp jsonrpcResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &rpcResp); err != nil {
			e.t.Fatalf("unmarshal response: %v (body: %s)", err, string(respBody))
		}
	}
	return resp, rpcResp
}

// toolCall is a convenience for calling tools/call.
func (e *testEnv) toolCall(sessionID string, id int, name string, args interface{}) (*http.Response, jsonrpcResponse) {
	e.t.Helper()
	return e.mcpRequest(sessionID, "tools/call", id, map[string]interface{}{
		"name":      name,
		"arguments": args,
	})
}

// toolResultText extracts the text from a successful tool result.
func toolResultText(t *testing.T, resp jsonrpcResponse) string {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("expected success, got error: %s", resp.Error.Message)
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal tool result: %v", err)
	}
	if result.IsError {
		if len(result.Content) > 0 {
			t.Fatalf("expected success, got tool error: %s", result.Content[0].Text)
		}
		t.Fatal("expected success, got tool error")
	}
	if len(result.Content) == 0 {
		t.Fatal("empty tool result content")
	}
	return result.Content[0].Text
}

// toolResultIsError checks if a tool result is an error and returns the error message.
func toolResultIsError(t *testing.T, resp jsonrpcResponse) (bool, string) {
	t.Helper()
	if resp.Error != nil {
		return true, resp.Error.Message
	}
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal tool result: %v", err)
	}
	if result.IsError && len(result.Content) > 0 {
		return true, result.Content[0].Text
	}
	return result.IsError, ""
}

// connectPresence establishes a WebSocket presence connection and returns the identity token.
// The cancel function closes the connection (marks user offline).
func (e *testEnv) connectPresence() (token string, cancelPresence func()) {
	e.t.Helper()

	wsURL := fmt.Sprintf("ws://%s/presence", e.addr)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		e.t.Fatalf("dial presence: %v", err)
	}

	// Read token (first message from server)
	_, msg, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		e.t.Fatalf("read presence token: %v", err)
	}
	token = string(msg)

	// Read loop: processes server pings (gorilla auto-responds with pong)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	cancelPresence = func() {
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		conn.Close()
		<-readDone
	}
	return token, cancelPresence
}

// registerUser performs the full identity handshake: presence → initialize → register.
// Returns the MCP session ID and a cancel function to disconnect presence.
func (e *testEnv) registerUser(username string, id int) (sessionID string, cancelPresence func()) {
	e.t.Helper()

	// 1. Connect to presence (gets token)
	token, cancelPresence := e.connectPresence()

	// 2. Initialize — mcp-go returns the session ID here
	httpResp, _ := e.mcpRequest("", "initialize", id, map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]string{"name": "test", "version": "0.1"},
	})
	sessionID = httpResp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		e.t.Fatal("no Mcp-Session-Id in initialize response")
	}

	// 3. Register (pass session ID so the server can map it to the user)
	_, regResp := e.toolCall(sessionID, id+2, "register", map[string]interface{}{
		"token": token, "username": username, "password": "",
	})
	if isErr, msg := toolResultIsError(e.t, regResp); isErr {
		e.t.Fatalf("register %s failed: %s", username, msg)
	}

	return sessionID, cancelPresence
}

// identifyUser performs the full identity handshake for an existing user: presence → initialize → identify.
func (e *testEnv) identifyUser(username string, id int) (sessionID string, cancelPresence func()) {
	e.t.Helper()

	// 1. Connect to presence (gets token)
	token, cancelPresence := e.connectPresence()

	// 2. Initialize — mcp-go returns the session ID here
	httpResp, _ := e.mcpRequest("", "initialize", id, map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]string{"name": "test", "version": "0.1"},
	})
	sessionID = httpResp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		e.t.Fatal("no Mcp-Session-Id in initialize response")
	}

	// 3. Identify (pass session ID so the server can map it to the user)
	_, identResp := e.toolCall(sessionID, id+2, "identify", map[string]interface{}{
		"token": token, "username": username, "password": "",
	})
	if isErr, msg := toolResultIsError(e.t, identResp); isErr {
		e.t.Fatalf("identify %s failed: %s", username, msg)
	}

	return sessionID, cancelPresence
}

// grantAdmin promotes a user to admin role via the server's Store handle.
func (e *testEnv) grantAdmin(username string) {
	e.t.Helper()
	if err := e.srv.Store().SetUserRole(username, "admin"); err != nil {
		e.t.Fatalf("grant admin to %s: %v", username, err)
	}
}

// --- Test Scenarios ---

func TestScenario1_IdentityHandshakeAndPresence(t *testing.T) {
	env := startTestServer(t)

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

	// The mcp-go session survives presence disconnect (it's managed independently).
	// The SharkfinMCP username mapping also persists. Only the SessionManager's
	// onlineUsers is cleaned up, so alice should appear offline.
	_, ulResp2 := env.toolCall(sessionID, 11, "user_list", map[string]interface{}{})
	isErr, _ := toolResultIsError(t, ulResp2)
	if isErr {
		// Auth mapping was cleaned up — re-identify to check user list.
		sessionID2, cancelPresence2 := env.identifyUser("alice", 20)
		defer cancelPresence2()

		_, ulResp3 := env.toolCall(sessionID2, 30, "user_list", map[string]interface{}{})
		users2 := toolResultText(t, ulResp3)
		var ul2 []struct {
			Username string `json:"username"`
			Online   bool   `json:"online"`
		}
		json.Unmarshal([]byte(users2), &ul2)
		if len(ul2) != 1 || ul2[0].Username != "alice" {
			t.Fatalf("expected alice in user list, got: %v", ul2)
		}
	} else {
		// Session survived — check that alice is offline
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
	env := startTestServer(t)

	sessionA, cancelA := env.registerUser("alice", 1)
	defer cancelA()
	env.grantAdmin("alice")
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
	env := startTestServer(t)

	sessionA, cancelA := env.registerUser("alice", 1)
	defer cancelA()
	env.grantAdmin("alice")
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

func TestScenario4_ChannelCreatePermissionDenied(t *testing.T) {
	env := startTestServer(t)

	sessionA, cancelA := env.registerUser("alice", 1)
	defer cancelA()
	// alice has "user" role which lacks create_channel permission

	_, chResp := env.toolCall(sessionA, 10, "channel_create", map[string]interface{}{
		"name": "test-channel", "public": true,
	})
	isErr, msg := toolResultIsError(t, chResp)
	if !isErr {
		t.Fatal("expected error: user role lacks create_channel permission")
	}
	if msg != "permission denied: create_channel" {
		t.Fatalf("unexpected error: %s", msg)
	}
}

func TestScenario5_SessionStateConstraints(t *testing.T) {
	env := startTestServer(t)

	sessionA, cancelA := env.registerUser("alice", 1)
	defer cancelA()

	// Try register again — should fail
	_, regResp := env.toolCall(sessionA, 10, "register", map[string]interface{}{
		"token": "fake-token", "username": "alice2", "password": "",
	})
	if isErr, _ := toolResultIsError(t, regResp); !isErr {
		t.Fatal("expected error on second register")
	}

	// Try identify — should also fail
	_, identResp := env.toolCall(sessionA, 11, "identify", map[string]interface{}{
		"token": "fake-token", "username": "alice", "password": "",
	})
	if isErr, _ := toolResultIsError(t, identResp); !isErr {
		t.Fatal("expected error on identify after register")
	}
}

func TestScenario6_DoubleLoginPrevention(t *testing.T) {
	env := startTestServer(t)

	_, cancelA := env.registerUser("alice", 1)
	defer cancelA()

	// Get a second presence connection (simulates a second bridge)
	token2, cancelPresence2 := env.connectPresence()
	defer cancelPresence2()

	// Initialize a second session
	httpResp2, _ := env.mcpRequest("", "initialize", 20, map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]string{"name": "test2", "version": "0.1"},
	})
	sessionID2 := httpResp2.Header.Get("Mcp-Session-Id")

	// Try to identify as alice — should fail because alice is already online
	_, identResp := env.toolCall(sessionID2, 22, "identify", map[string]interface{}{
		"token": token2, "username": "alice", "password": "",
	})
	isErr, msg := toolResultIsError(t, identResp)
	if !isErr {
		t.Fatal("expected error: alice is already online")
	}
	if msg != "user already online: alice" {
		t.Fatalf("unexpected error: %s", msg)
	}
}

func TestScenario7_ChannelInvite(t *testing.T) {
	env := startTestServer(t)

	sessionA, cancelA := env.registerUser("alice", 1)
	defer cancelA()
	env.grantAdmin("alice")
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
	env := startTestServer(t)

	sessionA, cancelA := env.registerUser("alice", 1)
	defer cancelA()
	env.grantAdmin("alice")
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
	isErr, msg := toolResultIsError(t, sendResp)
	if !isErr {
		t.Fatal("expected error: non-participant should not be able to send")
	}
	if msg != "you are not a participant of this channel" {
		t.Fatalf("unexpected error: %s", msg)
	}
}
