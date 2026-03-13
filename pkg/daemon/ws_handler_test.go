// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	auth "github.com/Work-Fort/Passport/go/service-auth"
	"github.com/Work-Fort/sharkfin/pkg/domain"
	"github.com/Work-Fort/sharkfin/pkg/infra/sqlite"
)

// wsTestEnv bundles a test server, store, hub, and presence handler.
// It supports per-user identity injection via a mux keyed on URL path.
type wsTestEnv struct {
	store    domain.Store
	hub      *Hub
	presence *PresenceHandler
	// Per-user servers, keyed by username
	mu      sync.Mutex
	servers map[string]*httptest.Server
	wh      *WSHandler
}

func newWSTestEnv(t *testing.T) *wsTestEnv {
	t.Helper()
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	hub := NewHub(nil)
	presence := NewPresenceHandler(20 * time.Second)
	wh := NewWSHandler(store, hub, presence, 20*time.Second, "test")

	return &wsTestEnv{
		store:    store,
		hub:      hub,
		presence: presence,
		servers:  make(map[string]*httptest.Server),
		wh:       wh,
	}
}

// serverForUser returns a per-user httptest.Server that injects the given identity.
func (env *wsTestEnv) serverForUser(t *testing.T, username string) *httptest.Server {
	t.Helper()
	env.mu.Lock()
	defer env.mu.Unlock()
	if s, ok := env.servers[username]; ok {
		return s
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := auth.Identity{ID: "uuid-" + username, Username: username, DisplayName: username, Type: "user"}
		ctx := auth.ContextWithIdentity(r.Context(), identity)
		env.wh.ServeHTTP(w, r.WithContext(ctx))
	})
	server := httptest.NewServer(handler)
	t.Cleanup(func() { server.Close() })
	env.servers[username] = server
	return server
}

// connectUser opens a WS connection as the given user (upserts identity + connects).
func connectUser(t *testing.T, env *wsTestEnv, username string) *websocket.Conn {
	t.Helper()
	server := env.serverForUser(t, username)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatalf("dial as %s: %v", username, err)
	}
	t.Cleanup(func() { conn.Close() })
	// Give the server a moment to register the client
	time.Sleep(30 * time.Millisecond)
	return conn
}

// wsReq sends a request and reads the response envelope matching the ref.
// Skips interleaved broadcast messages that may arrive between request and response.
func wsReq(t *testing.T, conn *websocket.Conn, typ string, d interface{}, ref string) wsEnvelope {
	t.Helper()
	raw, _ := json.Marshal(d)
	req := wsRequest{Type: typ, D: raw, Ref: ref}
	data, _ := json.Marshal(req)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write: %v", err)
	}
	for {
		env := readWSEnvelope(t, conn)
		if env.Ref == ref {
			return env
		}
		// Discard broadcast messages that arrived between request and response
	}
}

func readWSEnvelope(t *testing.T, conn *websocket.Conn) wsEnvelope {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var env wsEnvelope
	if err := json.Unmarshal(msg, &env); err != nil {
		t.Fatalf("unmarshal: %v (body: %s)", err, string(msg))
	}
	return env
}

// grantAdmin promotes a user to admin role for tests that need elevated permissions.
func grantAdmin(t *testing.T, env *wsTestEnv, username string) {
	t.Helper()
	if err := env.store.SetUserRole(username, "admin"); err != nil {
		t.Fatalf("grant admin to %s: %v", username, err)
	}
}

// --- Tests ---

func TestWSPing(t *testing.T) {
	env := newWSTestEnv(t)
	conn := connectUser(t, env, "alice")

	resp := wsReq(t, conn, "ping", nil, "p1")
	if resp.Type != "pong" {
		t.Errorf("type = %q, want pong", resp.Type)
	}
}

func TestWSUserList(t *testing.T) {
	env := newWSTestEnv(t)
	conn := connectUser(t, env, "alice")

	resp := wsReq(t, conn, "user_list", map[string]interface{}{}, "u1")
	if resp.OK == nil || !*resp.OK {
		t.Fatal("expected ok")
	}

	d, _ := json.Marshal(resp.D)
	var result struct {
		Users []struct {
			Username string `json:"username"`
		} `json:"users"`
	}
	json.Unmarshal(d, &result)
	if len(result.Users) == 0 {
		t.Fatal("expected users")
	}
	found := false
	for _, u := range result.Users {
		if u.Username == "alice" {
			found = true
		}
	}
	if !found {
		t.Error("expected alice in user list")
	}
}

func TestWSChannelCreate(t *testing.T) {
	env := newWSTestEnv(t)
	conn := connectUser(t, env, "alice")
	grantAdmin(t, env, "alice")

	resp := wsReq(t, conn, "channel_create", map[string]interface{}{
		"name":   "general",
		"public": true,
	}, "c1")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("expected ok, got %+v", resp)
	}
}

func TestWSChannelCreatePermissionDenied(t *testing.T) {
	env := newWSTestEnv(t)
	conn := connectUser(t, env, "alice") // "user" role lacks create_channel

	resp := wsReq(t, conn, "channel_create", map[string]interface{}{
		"name":   "secret",
		"public": false,
	}, "c1")
	if resp.OK != nil && *resp.OK {
		t.Error("expected error: permission denied")
	}
	// Verify correct error message
	d, _ := json.Marshal(resp.D)
	var result struct {
		Message string `json:"message"`
	}
	json.Unmarshal(d, &result)
	if result.Message != "permission denied: create_channel" {
		t.Errorf("error = %q, want 'permission denied: create_channel'", result.Message)
	}
}

func TestWSChannelList(t *testing.T) {
	env := newWSTestEnv(t)
	conn := connectUser(t, env, "alice")
	grantAdmin(t, env, "alice")

	// Create a channel first
	wsReq(t, conn, "channel_create", map[string]interface{}{
		"name": "general", "public": true,
	}, "c1")

	resp := wsReq(t, conn, "channel_list", map[string]interface{}{}, "l1")
	if resp.OK == nil || !*resp.OK {
		t.Fatal("expected ok")
	}
	d, _ := json.Marshal(resp.D)
	var result struct {
		Channels []struct {
			Name   string `json:"name"`
			Public bool   `json:"public"`
		} `json:"channels"`
	}
	json.Unmarshal(d, &result)
	if len(result.Channels) == 0 {
		t.Fatal("expected channels")
	}
	if result.Channels[0].Name != "general" {
		t.Errorf("channel name = %q, want general", result.Channels[0].Name)
	}
}

func TestWSChannelInvite(t *testing.T) {
	env := newWSTestEnv(t)
	aliceConn := connectUser(t, env, "alice")
	grantAdmin(t, env, "alice")
	connectUser(t, env, "bob")

	wsReq(t, aliceConn, "channel_create", map[string]interface{}{
		"name": "project", "public": false,
	}, "c1")

	resp := wsReq(t, aliceConn, "channel_invite", map[string]interface{}{
		"channel": "project", "username": "bob",
	}, "inv1")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("expected ok, got %+v", resp)
	}
}

func TestWSChannelInviteNonParticipant(t *testing.T) {
	env := newWSTestEnv(t)
	aliceConn := connectUser(t, env, "alice")
	grantAdmin(t, env, "alice")
	bobConn := connectUser(t, env, "bob")
	connectUser(t, env, "charlie")

	wsReq(t, aliceConn, "channel_create", map[string]interface{}{
		"name": "secret", "public": false,
	}, "c1")

	// Bob tries to invite charlie — should fail
	resp := wsReq(t, bobConn, "channel_invite", map[string]interface{}{
		"channel": "secret", "username": "charlie",
	}, "inv2")
	if resp.OK != nil && *resp.OK {
		t.Error("expected error: bob is not a participant")
	}
}

func TestWSSendMessage(t *testing.T) {
	env := newWSTestEnv(t)
	conn := connectUser(t, env, "alice")
	grantAdmin(t, env, "alice")

	wsReq(t, conn, "channel_create", map[string]interface{}{
		"name": "general", "public": true,
	}, "c1")

	resp := wsReq(t, conn, "send_message", map[string]interface{}{
		"channel": "general", "body": "hello world",
	}, "m1")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("expected ok, got %+v", resp)
	}

	// Check id is returned
	d, _ := json.Marshal(resp.D)
	var result struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(d, &result)
	if result.ID == 0 {
		t.Error("expected non-zero message ID")
	}
}

func TestWSSendMessageNonParticipant(t *testing.T) {
	env := newWSTestEnv(t)
	aliceConn := connectUser(t, env, "alice")
	grantAdmin(t, env, "alice")
	bobConn := connectUser(t, env, "bob")

	wsReq(t, aliceConn, "channel_create", map[string]interface{}{
		"name": "secret", "public": false,
	}, "c1")

	resp := wsReq(t, bobConn, "send_message", map[string]interface{}{
		"channel": "secret", "body": "sneaky",
	}, "m2")
	if resp.OK != nil && *resp.OK {
		t.Error("expected error: bob is not a participant")
	}
}

func TestWSHistory(t *testing.T) {
	env := newWSTestEnv(t)
	conn := connectUser(t, env, "alice")
	grantAdmin(t, env, "alice")

	wsReq(t, conn, "channel_create", map[string]interface{}{
		"name": "general", "public": true,
	}, "c1")

	// Send a few messages
	for i := 0; i < 3; i++ {
		wsReq(t, conn, "send_message", map[string]interface{}{
			"channel": "general", "body": "msg",
		}, "m")
	}

	resp := wsReq(t, conn, "history", map[string]interface{}{
		"channel": "general",
	}, "h1")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("expected ok, got %+v", resp)
	}
	d, _ := json.Marshal(resp.D)
	var result struct {
		Channel  string `json:"channel"`
		Messages []struct {
			ID   int64  `json:"id"`
			From string `json:"from"`
			Body string `json:"body"`
		} `json:"messages"`
	}
	json.Unmarshal(d, &result)
	if result.Channel != "general" {
		t.Errorf("channel = %q, want general", result.Channel)
	}
	if len(result.Messages) != 3 {
		t.Errorf("got %d messages, want 3", len(result.Messages))
	}
}

func TestWSHistoryNonParticipant(t *testing.T) {
	env := newWSTestEnv(t)
	aliceConn := connectUser(t, env, "alice")
	grantAdmin(t, env, "alice")
	bobConn := connectUser(t, env, "bob")

	wsReq(t, aliceConn, "channel_create", map[string]interface{}{
		"name": "secret", "public": false,
	}, "c1")

	// WS has admin-like access: non-members can read history
	resp := wsReq(t, bobConn, "history", map[string]interface{}{
		"channel": "secret",
	}, "h2")
	if resp.OK == nil || !*resp.OK {
		t.Error("expected ok: WS should allow history for non-members")
	}
}

func TestWSSetAndGetSettings(t *testing.T) {
	env := newWSTestEnv(t)
	conn := connectUser(t, env, "alice")
	grantAdmin(t, env, "alice")

	// Set a setting
	resp := wsReq(t, conn, "set_setting", map[string]interface{}{
		"key": "theme", "value": "dark",
	}, "s1")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("set_setting expected ok, got %+v", resp)
	}

	// Get settings
	resp = wsReq(t, conn, "get_settings", map[string]interface{}{}, "s2")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("get_settings expected ok, got %+v", resp)
	}
	d, _ := json.Marshal(resp.D)
	var result struct {
		Settings map[string]string `json:"settings"`
	}
	json.Unmarshal(d, &result)
	if result.Settings["theme"] != "dark" {
		t.Errorf("setting theme = %q, want dark", result.Settings["theme"])
	}
}

func TestWSUnknownType(t *testing.T) {
	env := newWSTestEnv(t)
	conn := connectUser(t, env, "alice")

	resp := wsReq(t, conn, "nonexistent", map[string]interface{}{}, "x1")
	if resp.OK != nil && *resp.OK {
		t.Error("expected error for unknown type")
	}
}

func TestWSSendMessageWithMentions(t *testing.T) {
	env := newWSTestEnv(t)
	aliceConn := connectUser(t, env, "alice")
	grantAdmin(t, env, "alice")
	bobConn := connectUser(t, env, "bob")

	wsReq(t, aliceConn, "channel_create", map[string]interface{}{
		"name": "general", "public": true,
	}, "c1")
	wsReq(t, aliceConn, "channel_invite", map[string]interface{}{
		"channel": "general", "username": "bob",
	}, "inv1")

	resp := wsReq(t, aliceConn, "send_message", map[string]interface{}{
		"channel": "general",
		"body":    "hey @bob check this",
	}, "m1")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("expected ok, got %+v", resp)
	}

	// Bob should receive broadcast with mentions
	bcast := readWSEnvelope(t, bobConn)
	if bcast.Type != "message.new" {
		t.Fatalf("type = %q, want message.new", bcast.Type)
	}
	d, _ := json.Marshal(bcast.D)
	var msg struct {
		Mentions []string `json:"mentions"`
	}
	json.Unmarshal(d, &msg)
	if len(msg.Mentions) != 1 || msg.Mentions[0] != "bob" {
		t.Errorf("mentions = %v, want [bob]", msg.Mentions)
	}
}

func TestWSSendMessageAutoMention(t *testing.T) {
	env := newWSTestEnv(t)
	aliceConn := connectUser(t, env, "alice")
	grantAdmin(t, env, "alice")
	bobConn := connectUser(t, env, "bob")

	wsReq(t, aliceConn, "channel_create", map[string]interface{}{
		"name": "general", "public": true,
	}, "c1")
	wsReq(t, aliceConn, "channel_invite", map[string]interface{}{
		"channel": "general", "username": "bob",
	}, "inv1")

	// No explicit mentions — server should extract @bob from body
	resp := wsReq(t, aliceConn, "send_message", map[string]interface{}{
		"channel": "general",
		"body":    "hey @bob check this",
	}, "m1")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("expected ok, got %+v", resp)
	}

	// Bob should receive broadcast with auto-extracted mention
	bcast := readWSEnvelope(t, bobConn)
	if bcast.Type != "message.new" {
		t.Fatalf("type = %q, want message.new", bcast.Type)
	}
	d, _ := json.Marshal(bcast.D)
	var msg struct {
		Mentions []string `json:"mentions"`
	}
	json.Unmarshal(d, &msg)
	if len(msg.Mentions) != 1 || msg.Mentions[0] != "bob" {
		t.Errorf("mentions = %v, want [bob]", msg.Mentions)
	}
}

func TestWSSendMessageWithThread(t *testing.T) {
	env := newWSTestEnv(t)
	aliceConn := connectUser(t, env, "alice")
	grantAdmin(t, env, "alice")
	bobConn := connectUser(t, env, "bob")

	wsReq(t, aliceConn, "channel_create", map[string]interface{}{
		"name": "general", "public": true,
	}, "c1")
	wsReq(t, aliceConn, "channel_invite", map[string]interface{}{
		"channel": "general", "username": "bob",
	}, "inv1")

	// Send parent message
	parentResp := wsReq(t, aliceConn, "send_message", map[string]interface{}{
		"channel": "general", "body": "parent msg",
	}, "m1")
	// Read bob's broadcast for parent
	readWSEnvelope(t, bobConn)

	d, _ := json.Marshal(parentResp.D)
	var pr struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(d, &pr)

	// Reply in thread
	resp := wsReq(t, aliceConn, "send_message", map[string]interface{}{
		"channel":   "general",
		"body":      "thread reply",
		"thread_id": pr.ID,
	}, "m2")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("expected ok, got %+v", resp)
	}

	// Bob should receive broadcast with thread_id
	bcast := readWSEnvelope(t, bobConn)
	d, _ = json.Marshal(bcast.D)
	var msg struct {
		ThreadID int64 `json:"thread_id"`
	}
	json.Unmarshal(d, &msg)
	if msg.ThreadID != pr.ID {
		t.Errorf("thread_id = %d, want %d", msg.ThreadID, pr.ID)
	}
}

func TestWSSendMessageRejectNestedReply(t *testing.T) {
	env := newWSTestEnv(t)
	conn := connectUser(t, env, "alice")
	grantAdmin(t, env, "alice")

	wsReq(t, conn, "channel_create", map[string]interface{}{
		"name": "general", "public": true,
	}, "c1")

	parentResp := wsReq(t, conn, "send_message", map[string]interface{}{
		"channel": "general", "body": "parent",
	}, "m1")
	d, _ := json.Marshal(parentResp.D)
	var pr struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(d, &pr)

	replyResp := wsReq(t, conn, "send_message", map[string]interface{}{
		"channel": "general", "body": "reply", "thread_id": pr.ID,
	}, "m2")
	d, _ = json.Marshal(replyResp.D)
	var rr struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(d, &rr)

	// Try nested reply — should fail
	resp := wsReq(t, conn, "send_message", map[string]interface{}{
		"channel": "general", "body": "nested", "thread_id": rr.ID,
	}, "m3")
	if resp.OK != nil && *resp.OK {
		t.Error("expected error for nested reply")
	}
}

func TestWSHistoryWithThreadFilter(t *testing.T) {
	env := newWSTestEnv(t)
	conn := connectUser(t, env, "alice")
	grantAdmin(t, env, "alice")

	wsReq(t, conn, "channel_create", map[string]interface{}{
		"name": "general", "public": true,
	}, "c1")

	parentResp := wsReq(t, conn, "send_message", map[string]interface{}{
		"channel": "general", "body": "parent",
	}, "m1")
	d, _ := json.Marshal(parentResp.D)
	var pr struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(d, &pr)

	wsReq(t, conn, "send_message", map[string]interface{}{
		"channel": "general", "body": "reply1", "thread_id": pr.ID,
	}, "m2")
	wsReq(t, conn, "send_message", map[string]interface{}{
		"channel": "general", "body": "other top-level",
	}, "m3")

	resp := wsReq(t, conn, "history", map[string]interface{}{
		"channel": "general", "thread_id": pr.ID,
	}, "h1")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("expected ok, got %+v", resp)
	}
	d, _ = json.Marshal(resp.D)
	var result struct {
		Messages []struct {
			Body     string `json:"body"`
			ThreadID int64  `json:"thread_id"`
		} `json:"messages"`
	}
	json.Unmarshal(d, &result)
	if len(result.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(result.Messages))
	}
	if result.Messages[0].Body != "reply1" {
		t.Errorf("body = %q, want reply1", result.Messages[0].Body)
	}
}

func TestWSSendMessageMentionInvalidUser(t *testing.T) {
	env := newWSTestEnv(t)
	conn := connectUser(t, env, "alice")
	grantAdmin(t, env, "alice")

	wsReq(t, conn, "channel_create", map[string]interface{}{
		"name": "general", "public": true,
	}, "c1")

	// Invalid mentions are silently ignored
	resp := wsReq(t, conn, "send_message", map[string]interface{}{
		"channel": "general",
		"body":    "hey @nobody",
	}, "m1")
	if resp.OK == nil || !*resp.OK {
		t.Error("expected ok: invalid mentions should be silently ignored")
	}
}

func TestWSUnreadMessages(t *testing.T) {
	env := newWSTestEnv(t)
	aliceConn := connectUser(t, env, "alice")
	grantAdmin(t, env, "alice")
	bobConn := connectUser(t, env, "bob")

	wsReq(t, aliceConn, "channel_create", map[string]interface{}{
		"name": "general", "public": true,
	}, "c1")
	wsReq(t, aliceConn, "channel_invite", map[string]interface{}{
		"channel": "general", "username": "bob",
	}, "inv1")

	wsReq(t, aliceConn, "send_message", map[string]interface{}{
		"channel": "general", "body": "hello bob",
	}, "m1")
	// Drain bob's broadcast
	readWSEnvelope(t, bobConn)

	resp := wsReq(t, bobConn, "unread_messages", map[string]interface{}{}, "u1")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("expected ok, got %+v", resp)
	}
	d, _ := json.Marshal(resp.D)
	var result struct {
		Messages []struct {
			Body    string `json:"body"`
			Channel string `json:"channel"`
		} `json:"messages"`
	}
	json.Unmarshal(d, &result)
	if len(result.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(result.Messages))
	}
	if result.Messages[0].Body != "hello bob" {
		t.Errorf("body = %q, want 'hello bob'", result.Messages[0].Body)
	}
	if result.Messages[0].Channel != "general" {
		t.Errorf("channel = %q, want general", result.Messages[0].Channel)
	}
}

func TestWSSendMessageWithMentionGroup(t *testing.T) {
	env := newWSTestEnv(t)
	aliceConn := connectUser(t, env, "alice")
	grantAdmin(t, env, "alice")
	bobConn := connectUser(t, env, "bob")

	// Create a mention group.
	resp := wsReq(t, aliceConn, "mention_group_create", map[string]interface{}{
		"slug": "backend",
	}, "mg1")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("create group: %+v", resp)
	}
	wsReq(t, aliceConn, "mention_group_add_member", map[string]interface{}{
		"slug": "backend", "username": "bob",
	}, "mg2")

	// Create channel and invite bob.
	wsReq(t, aliceConn, "channel_create", map[string]interface{}{
		"name": "general", "public": true,
	}, "c1")
	wsReq(t, aliceConn, "channel_invite", map[string]interface{}{
		"channel": "general", "username": "bob",
	}, "inv1")

	// Send with group mention.
	resp = wsReq(t, aliceConn, "send_message", map[string]interface{}{
		"channel": "general",
		"body":    "hey @backend check this",
	}, "m1")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("send: %+v", resp)
	}

	// Bob should receive broadcast with mentions (expanded from group).
	bcast := readWSEnvelope(t, bobConn)
	if bcast.Type != "message.new" {
		t.Fatalf("type = %q, want message.new", bcast.Type)
	}
	d, _ := json.Marshal(bcast.D)
	var msg struct {
		Mentions []string `json:"mentions"`
	}
	json.Unmarshal(d, &msg)
	if len(msg.Mentions) == 0 {
		t.Error("expected mentions from group expansion")
	}
}
