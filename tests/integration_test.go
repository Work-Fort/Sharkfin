// SPDX-License-Identifier: AGPL-3.0-or-later
package tests

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"

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

// jwksStub holds a test JWKS server and JWT signing function.
type jwksStub struct {
	addr    string
	stop    func()
	privJWK jwk.Key
}

func startJWKSStub(t *testing.T) *jwksStub {
	t.Helper()

	rawKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	privJWK, err := jwk.FromRaw(rawKey)
	if err != nil {
		t.Fatalf("create JWK: %v", err)
	}
	_ = privJWK.Set(jwk.KeyIDKey, "test-key-1")
	_ = privJWK.Set(jwk.AlgorithmKey, jwa.RS256)

	privSet := jwk.NewSet()
	_ = privSet.AddKey(privJWK)

	pubSet, err := jwk.PublicSetOf(privSet)
	if err != nil {
		t.Fatalf("derive public JWKS: %v", err)
	}

	jwksBytes, err := json.Marshal(pubSet)
	if err != nil {
		t.Fatalf("marshal JWKS: %v", err)
	}

	bridgeIdentity := map[string]any{
		"valid": true,
		"key": map[string]any{
			"userId": "00000000-0000-0000-0000-000000000001",
			"metadata": map[string]any{
				"username":     "bridge",
				"name":         "MCP Bridge",
				"display_name": "Bridge",
				"type":         "service",
			},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwksBytes)
	})
	mux.HandleFunc("POST /v1/verify-api-key", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(bridgeIdentity)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("jwks listen: %v", err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)

	stub := &jwksStub{addr: ln.Addr().String(), privJWK: privJWK}
	stub.stop = func() { srv.Close() }
	t.Cleanup(stub.stop)
	return stub
}

func (s *jwksStub) signJWT(id, username, displayName, userType string) string {
	now := time.Now()
	tok, err := jwt.NewBuilder().
		Subject(id).
		Issuer("passport-stub").
		Audience([]string{"sharkfin"}).
		IssuedAt(now).
		Expiration(now.Add(1*time.Hour)).
		Claim("username", username).
		Claim("name", displayName).
		Claim("display_name", displayName).
		Claim("type", userType).
		Build()
	if err != nil {
		panic(fmt.Sprintf("build JWT: %v", err))
	}
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256, s.privJWK))
	if err != nil {
		panic(fmt.Sprintf("sign JWT: %v", err))
	}
	return string(signed)
}

func (s *jwksStub) passportURL() string {
	return "http://" + s.addr
}

// testEnv holds a running server and helpers for integration tests.
type testEnv struct {
	t    *testing.T
	srv  *pkgdaemon.Server
	addr string
	jwks *jwksStub
}

func startTestServer(t *testing.T) *testEnv {
	t.Helper()

	jwks := startJWKSStub(t)

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

	ctx := context.Background()
	srv, err := pkgdaemon.NewServer(ctx, addr, store, 20*time.Second, "", nil, "test", jwks.passportURL())
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

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
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutCancel()
		srv.Shutdown(shutCtx)
	})

	return &testEnv{t: t, srv: srv, addr: addr, jwks: jwks}
}

// mcpRequest sends a JSON-RPC request to /mcp with the given auth token.
func (e *testEnv) mcpRequest(sessionID, token, method string, id int, params interface{}) (*http.Response, jsonrpcResponse) {
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
	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
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
func (e *testEnv) toolCall(sessionID, token string, id int, name string, args interface{}) (*http.Response, jsonrpcResponse) {
	e.t.Helper()
	return e.mcpRequest(sessionID, token, "tools/call", id, map[string]interface{}{
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

// userSession holds the MCP session ID and JWT for a test user.
type userSession struct {
	sessionID string
	token     string
}

// initUser initializes an MCP session for a user with a signed JWT.
// The first tool call auto-provisions the identity.
func (e *testEnv) initUser(id, username, displayName string) userSession {
	e.t.Helper()

	token := e.jwks.signJWT(id, username, displayName, "user")

	httpResp, _ := e.mcpRequest("", token, "initialize", 1, map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]string{"name": "test", "version": "0.1"},
	})
	sessionID := httpResp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		e.t.Fatal("no Mcp-Session-Id in initialize response")
	}

	// First tool call triggers auto-provisioning.
	_, ulResp := e.toolCall(sessionID, token, 2, "user_list", map[string]interface{}{})
	if ulResp.Error != nil {
		e.t.Fatalf("auto-provision tool call failed: %s", ulResp.Error.Message)
	}

	return userSession{sessionID: sessionID, token: token}
}

// connectPresence establishes a WebSocket presence connection with auth.
func (e *testEnv) connectPresence(token string) func() {
	e.t.Helper()

	wsURL := fmt.Sprintf("ws://%s/presence", e.addr)
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		e.t.Fatalf("dial presence: %v", err)
	}

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	return func() {
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		conn.Close()
		<-readDone
	}
}

// grantAdmin promotes a user to admin role via the server's Store.
func (e *testEnv) grantAdmin(username string) {
	e.t.Helper()
	if err := e.srv.Store().SetUserRole(username, "admin"); err != nil {
		e.t.Fatalf("grant admin to %s: %v", username, err)
	}
}

// --- Test Scenarios ---

func TestScenario1_IdentityAndPresence(t *testing.T) {
	env := startTestServer(t)

	alice := env.initUser("alice-uuid", "alice", "Alice")
	cancelPresence := env.connectPresence(alice.token)

	// Verify alice is online.
	_, ulResp := env.toolCall(alice.sessionID, alice.token, 10, "user_list", map[string]interface{}{})
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

	// Disconnect presence.
	cancelPresence()
	time.Sleep(100 * time.Millisecond)

	// Alice should appear offline (MCP session survives).
	_, ulResp2 := env.toolCall(alice.sessionID, alice.token, 11, "user_list", map[string]interface{}{})
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

func TestScenario2_MessagingBetweenTwoUsers(t *testing.T) {
	env := startTestServer(t)

	alice := env.initUser("alice-uuid", "alice", "Alice")
	env.grantAdmin("alice")
	bob := env.initUser("bob-uuid", "bob", "Bob")

	// Alice creates a private channel with Bob.
	_, chResp := env.toolCall(alice.sessionID, alice.token, 20, "channel_create", map[string]interface{}{
		"name": "alice-bob", "public": false, "members": []string{"bob"},
	})
	if chResp.Error != nil {
		t.Fatalf("channel_create: %s", chResp.Error.Message)
	}

	// Alice sends a message.
	_, sendResp := env.toolCall(alice.sessionID, alice.token, 21, "send_message", map[string]interface{}{
		"channel": "alice-bob", "message": "hello bob!",
	})
	if sendResp.Error != nil {
		t.Fatalf("send_message: %s", sendResp.Error.Message)
	}

	// Bob reads unread messages.
	_, unreadResp := env.toolCall(bob.sessionID, bob.token, 22, "unread_messages", map[string]interface{}{})
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

	// Bob reads again — no new messages.
	_, unreadResp2 := env.toolCall(bob.sessionID, bob.token, 23, "unread_messages", map[string]interface{}{})
	unreadText2 := toolResultText(t, unreadResp2)
	if unreadText2 != "null" && unreadText2 != "[]" {
		var msgs2 []interface{}
		json.Unmarshal([]byte(unreadText2), &msgs2)
		if len(msgs2) != 0 {
			t.Fatalf("expected no new messages, got: %s", unreadText2)
		}
	}

	// Alice sends another message.
	_, _ = env.toolCall(alice.sessionID, alice.token, 24, "send_message", map[string]interface{}{
		"channel": "alice-bob", "message": "are you there?",
	})

	// Bob reads — gets only the new message.
	_, unreadResp3 := env.toolCall(bob.sessionID, bob.token, 25, "unread_messages", map[string]interface{}{})
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

	alice := env.initUser("alice-uuid", "alice", "Alice")
	env.grantAdmin("alice")
	bob := env.initUser("bob-uuid", "bob", "Bob")
	charlie := env.initUser("charlie-uuid", "charlie", "Charlie")

	// Alice creates public channel, adds Bob.
	_, _ = env.toolCall(alice.sessionID, alice.token, 30, "channel_create", map[string]interface{}{
		"name": "general", "public": true, "members": []string{"bob"},
	})

	// Charlie sees the public channel.
	_, clResp := env.toolCall(charlie.sessionID, charlie.token, 31, "channel_list", map[string]interface{}{})
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

	// Alice creates private channel with Bob.
	_, _ = env.toolCall(alice.sessionID, alice.token, 32, "channel_create", map[string]interface{}{
		"name": "secret", "public": false, "members": []string{"bob"},
	})

	// Charlie should NOT see the private channel.
	_, clResp2 := env.toolCall(charlie.sessionID, charlie.token, 33, "channel_list", map[string]interface{}{})
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

	// Bob sees both channels.
	_, clResp3 := env.toolCall(bob.sessionID, bob.token, 34, "channel_list", map[string]interface{}{})
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

	alice := env.initUser("alice-uuid", "alice", "Alice")
	// alice has "user" role which lacks create_channel permission.

	_, chResp := env.toolCall(alice.sessionID, alice.token, 10, "channel_create", map[string]interface{}{
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

func TestScenario5_UnauthenticatedRequest(t *testing.T) {
	env := startTestServer(t)

	// Request without auth token should fail.
	httpReq, _ := http.NewRequest("POST", fmt.Sprintf("http://%s/mcp", env.addr), bytes.NewReader([]byte(`{"jsonrpc":"2.0","method":"initialize","id":1}`)))
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestScenario6_ChannelInvite(t *testing.T) {
	env := startTestServer(t)

	alice := env.initUser("alice-uuid", "alice", "Alice")
	env.grantAdmin("alice")
	bob := env.initUser("bob-uuid", "bob", "Bob")
	charlie := env.initUser("charlie-uuid", "charlie", "Charlie")

	// Alice creates private channel with Bob.
	_, _ = env.toolCall(alice.sessionID, alice.token, 30, "channel_create", map[string]interface{}{
		"name": "project-x", "public": false, "members": []string{"bob"},
	})

	// Bob invites Charlie.
	_, invResp := env.toolCall(bob.sessionID, bob.token, 31, "channel_invite", map[string]interface{}{
		"channel": "project-x", "username": "charlie",
	})
	if invResp.Error != nil {
		t.Fatalf("invite failed: %s", invResp.Error.Message)
	}

	// Charlie can now see the channel.
	_, clResp := env.toolCall(charlie.sessionID, charlie.token, 32, "channel_list", map[string]interface{}{})
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

	// Charlie can send a message.
	_, sendResp := env.toolCall(charlie.sessionID, charlie.token, 33, "send_message", map[string]interface{}{
		"channel": "project-x", "message": "hey everyone!",
	})
	if sendResp.Error != nil {
		t.Fatalf("charlie send_message failed: %s", sendResp.Error.Message)
	}
}

func TestScenario7_NonParticipantRejection(t *testing.T) {
	env := startTestServer(t)

	alice := env.initUser("alice-uuid", "alice", "Alice")
	env.grantAdmin("alice")
	env.initUser("bob-uuid", "bob", "Bob")

	// Alice creates a private channel (alone).
	_, _ = env.toolCall(alice.sessionID, alice.token, 20, "channel_create", map[string]interface{}{
		"name": "private-notes", "public": false,
	})

	// Charlie (outsider) tries to send a message.
	charlie := env.initUser("charlie-uuid", "charlie", "Charlie")
	_, sendResp := env.toolCall(charlie.sessionID, charlie.token, 40, "send_message", map[string]interface{}{
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
