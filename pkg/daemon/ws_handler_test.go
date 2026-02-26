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

// wsReq sends a request and reads the response envelope.
func wsReq(t *testing.T, conn *websocket.Conn, typ string, d interface{}, ref string) wsEnvelope {
	t.Helper()
	raw, _ := json.Marshal(d)
	req := wsRequest{Type: typ, D: raw, Ref: ref}
	data, _ := json.Marshal(req)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write: %v", err)
	}
	return readWSEnvelope(t, conn)
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

	tools := []string{"user_list", "channel_list", "channel_create", "channel_invite", "send_message", "history", "set_setting", "get_settings"}
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
		Messages []struct {
			ID   int64  `json:"id"`
			From string `json:"from"`
			Body string `json:"body"`
		} `json:"messages"`
	}
	json.Unmarshal(d, &result)
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
