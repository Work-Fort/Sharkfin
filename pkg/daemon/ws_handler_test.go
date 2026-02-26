// SPDX-License-Identifier: GPL-2.0-only
package daemon

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Work-Fort/sharkfin/pkg/db"
)

// wsTestEnv bundles a test server, session manager, db, and hub.
type wsTestEnv struct {
	server *httptest.Server
	sm     *SessionManager
	db     *db.DB
	hub    *Hub
}

func newWSTestEnv(t *testing.T) *wsTestEnv {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	sm := NewSessionManager(d)
	hub := NewHub()
	wh := NewWSHandler(sm, d, hub, 20*time.Second)
	server := httptest.NewServer(wh)
	t.Cleanup(func() { server.Close() })

	return &wsTestEnv{server: server, sm: sm, db: d, hub: hub}
}

// dialWS opens a WebSocket connection to the test server and reads the hello message.
func dialWS(t *testing.T, env *wsTestEnv) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(env.server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	// Read and discard hello
	readWSEnvelope(t, conn)
	return conn
}

// wsReq sends a request and reads the response envelope matching the ref.
// Skips interleaved broadcast messages (presence, message.new) that may arrive
// between request and response.
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

// registerWSUser registers a user on a WS connection and returns the conn.
func registerWSUser(t *testing.T, env *wsTestEnv, username string) *websocket.Conn {
	t.Helper()
	conn := dialWS(t, env)
	resp := wsReq(t, conn, "register", map[string]string{"username": username}, "r1")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("register %s failed: %+v", username, resp)
	}
	return conn
}

// --- Tests ---

func TestWSHello(t *testing.T) {
	env := newWSTestEnv(t)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(env.server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	env_ := readWSEnvelope(t, conn)
	if env_.Type != "hello" {
		t.Errorf("type = %q, want hello", env_.Type)
	}
}

func TestWSRegister(t *testing.T) {
	env := newWSTestEnv(t)
	conn := dialWS(t, env)

	resp := wsReq(t, conn, "register", map[string]string{"username": "alice"}, "r1")
	if resp.OK == nil || !*resp.OK {
		t.Error("expected ok: true")
	}
	if resp.Ref != "r1" {
		t.Errorf("ref = %q, want r1", resp.Ref)
	}
}

func TestWSIdentify(t *testing.T) {
	env := newWSTestEnv(t)

	// First register a user and disconnect
	conn1 := registerWSUser(t, env, "bob")
	conn1.Close()
	time.Sleep(50 * time.Millisecond) // let disconnect propagate

	// Now identify as bob from a new connection
	conn2 := dialWS(t, env)
	resp := wsReq(t, conn2, "identify", map[string]string{"username": "bob"}, "i1")
	if resp.OK == nil || !*resp.OK {
		t.Errorf("expected ok: true, got %+v", resp)
	}
}

func TestWSRegisterEmptyUsername(t *testing.T) {
	env := newWSTestEnv(t)
	conn := dialWS(t, env)

	resp := wsReq(t, conn, "register", map[string]string{"username": ""}, "r1")
	if resp.OK != nil && *resp.OK {
		t.Error("expected ok: false for empty username")
	}
}

func TestWSPingBeforeAuth(t *testing.T) {
	env := newWSTestEnv(t)
	conn := dialWS(t, env)

	resp := wsReq(t, conn, "ping", nil, "p1")
	if resp.Type != "pong" {
		t.Errorf("type = %q, want pong", resp.Type)
	}
}

func TestWSPingAfterAuth(t *testing.T) {
	env := newWSTestEnv(t)
	conn := registerWSUser(t, env, "alice")

	resp := wsReq(t, conn, "ping", nil, "p2")
	if resp.Type != "pong" {
		t.Errorf("type = %q, want pong", resp.Type)
	}
}

func TestWSProtectedToolBeforeAuth(t *testing.T) {
	env := newWSTestEnv(t)
	conn := dialWS(t, env)

	tools := []string{"user_list", "channel_list", "channel_create", "channel_invite", "send_message", "history", "unread_messages", "set_setting", "get_settings"}
	for _, tool := range tools {
		resp := wsReq(t, conn, tool, map[string]interface{}{}, tool)
		if resp.OK != nil && *resp.OK {
			t.Errorf("%s: expected error before auth", tool)
		}
	}
}

func TestWSDoubleRegister(t *testing.T) {
	env := newWSTestEnv(t)
	conn := registerWSUser(t, env, "alice")

	resp := wsReq(t, conn, "register", map[string]string{"username": "bob"}, "r2")
	if resp.OK != nil && *resp.OK {
		t.Error("expected error: already identified")
	}
}

func TestWSIdentifyAfterRegister(t *testing.T) {
	env := newWSTestEnv(t)
	conn := registerWSUser(t, env, "alice")

	resp := wsReq(t, conn, "identify", map[string]string{"username": "alice"}, "i2")
	if resp.OK != nil && *resp.OK {
		t.Error("expected error: already identified")
	}
}

func TestWSUserList(t *testing.T) {
	env := newWSTestEnv(t)
	conn := registerWSUser(t, env, "alice")

	resp := wsReq(t, conn, "user_list", map[string]interface{}{}, "u1")
	if resp.OK == nil || !*resp.OK {
		t.Fatal("expected ok")
	}

	d, _ := json.Marshal(resp.D)
	var result struct {
		Users []struct {
			Username string `json:"username"`
			Online   bool   `json:"online"`
		} `json:"users"`
	}
	json.Unmarshal(d, &result)
	if len(result.Users) == 0 {
		t.Fatal("expected users")
	}
	found := false
	for _, u := range result.Users {
		if u.Username == "alice" && u.Online {
			found = true
		}
	}
	if !found {
		t.Error("expected alice to be online in user list")
	}
}

func TestWSChannelCreate(t *testing.T) {
	env := newWSTestEnv(t)
	conn := registerWSUser(t, env, "alice")

	resp := wsReq(t, conn, "channel_create", map[string]interface{}{
		"name":   "general",
		"public": true,
	}, "c1")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("expected ok, got %+v", resp)
	}
}

func TestWSChannelCreateDisabled(t *testing.T) {
	env := newWSTestEnv(t)
	env.db.SetSetting("allow_channel_creation", "false")
	conn := registerWSUser(t, env, "alice")

	resp := wsReq(t, conn, "channel_create", map[string]interface{}{
		"name":   "secret",
		"public": false,
	}, "c1")
	if resp.OK != nil && *resp.OK {
		t.Error("expected error: channel creation disabled")
	}
}

func TestWSChannelList(t *testing.T) {
	env := newWSTestEnv(t)
	conn := registerWSUser(t, env, "alice")

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
	aliceConn := registerWSUser(t, env, "alice")
	registerWSUser(t, env, "bob")

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
	aliceConn := registerWSUser(t, env, "alice")
	bobConn := registerWSUser(t, env, "bob")
	registerWSUser(t, env, "charlie")

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
	conn := registerWSUser(t, env, "alice")

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
	aliceConn := registerWSUser(t, env, "alice")
	bobConn := registerWSUser(t, env, "bob")

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
	conn := registerWSUser(t, env, "alice")

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
	aliceConn := registerWSUser(t, env, "alice")
	bobConn := registerWSUser(t, env, "bob")

	wsReq(t, aliceConn, "channel_create", map[string]interface{}{
		"name": "secret", "public": false,
	}, "c1")

	resp := wsReq(t, bobConn, "history", map[string]interface{}{
		"channel": "secret",
	}, "h2")
	if resp.OK != nil && *resp.OK {
		t.Error("expected error: bob is not a participant")
	}
}

func TestWSSetAndGetSettings(t *testing.T) {
	env := newWSTestEnv(t)
	conn := registerWSUser(t, env, "alice")

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
	conn := registerWSUser(t, env, "alice")

	resp := wsReq(t, conn, "nonexistent", map[string]interface{}{}, "x1")
	if resp.OK != nil && *resp.OK {
		t.Error("expected error for unknown type")
	}
}

func TestWSPresenceOnDisconnect(t *testing.T) {
	env := newWSTestEnv(t)
	conn := registerWSUser(t, env, "alice")
	_ = conn // keep reference

	if !env.sm.IsUserOnline("alice") {
		t.Error("alice should be online")
	}

	conn.Close()
	time.Sleep(100 * time.Millisecond) // let disconnect propagate

	if env.sm.IsUserOnline("alice") {
		t.Error("alice should be offline after disconnect")
	}
}

func TestWSIdentifyAlreadyOnline(t *testing.T) {
	env := newWSTestEnv(t)
	registerWSUser(t, env, "alice")

	// Try to identify as alice from another connection — should fail
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL(env.server), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn2.Close()
	readWSEnvelope(t, conn2) // hello

	resp := wsReq(t, conn2, "identify", map[string]string{"username": "alice"}, "i1")
	if resp.OK != nil && *resp.OK {
		t.Error("expected error: user already online")
	}
}

func TestWSSendMessageWithMentions(t *testing.T) {
	env := newWSTestEnv(t)
	aliceConn := registerWSUser(t, env, "alice")
	bobConn := registerWSUser(t, env, "bob")

	wsReq(t, aliceConn, "channel_create", map[string]interface{}{
		"name": "general", "public": true,
	}, "c1")
	wsReq(t, aliceConn, "channel_invite", map[string]interface{}{
		"channel": "general", "username": "bob",
	}, "inv1")

	resp := wsReq(t, aliceConn, "send_message", map[string]interface{}{
		"channel":  "general",
		"body":     "hey @bob check this",
		"mentions": []string{"bob"},
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

func TestWSSendMessageWithThread(t *testing.T) {
	env := newWSTestEnv(t)
	aliceConn := registerWSUser(t, env, "alice")
	bobConn := registerWSUser(t, env, "bob")

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
	var pr struct{ ID int64 `json:"id"` }
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
	conn := registerWSUser(t, env, "alice")

	wsReq(t, conn, "channel_create", map[string]interface{}{
		"name": "general", "public": true,
	}, "c1")

	parentResp := wsReq(t, conn, "send_message", map[string]interface{}{
		"channel": "general", "body": "parent",
	}, "m1")
	d, _ := json.Marshal(parentResp.D)
	var pr struct{ ID int64 `json:"id"` }
	json.Unmarshal(d, &pr)

	replyResp := wsReq(t, conn, "send_message", map[string]interface{}{
		"channel": "general", "body": "reply", "thread_id": pr.ID,
	}, "m2")
	d, _ = json.Marshal(replyResp.D)
	var rr struct{ ID int64 `json:"id"` }
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
	conn := registerWSUser(t, env, "alice")

	wsReq(t, conn, "channel_create", map[string]interface{}{
		"name": "general", "public": true,
	}, "c1")

	parentResp := wsReq(t, conn, "send_message", map[string]interface{}{
		"channel": "general", "body": "parent",
	}, "m1")
	d, _ := json.Marshal(parentResp.D)
	var pr struct{ ID int64 `json:"id"` }
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
	conn := registerWSUser(t, env, "alice")

	wsReq(t, conn, "channel_create", map[string]interface{}{
		"name": "general", "public": true,
	}, "c1")

	resp := wsReq(t, conn, "send_message", map[string]interface{}{
		"channel":  "general",
		"body":     "hey @nobody",
		"mentions": []string{"nobody"},
	}, "m1")
	if resp.OK != nil && *resp.OK {
		t.Error("expected error for invalid mention username")
	}
}

func TestWSUnreadMessages(t *testing.T) {
	env := newWSTestEnv(t)
	aliceConn := registerWSUser(t, env, "alice")
	bobConn := registerWSUser(t, env, "bob")

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
