// SPDX-License-Identifier: AGPL-3.0-or-later
package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

func mockServer(t *testing.T, handler func(*websocket.Conn)) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		handler(conn)
	}))
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	return srv, wsURL
}

// mockServerWithREST creates a test server that handles both WS (on /ws) and REST routes.
func mockServerWithREST(t *testing.T, wsHandler func(*websocket.Conn), mux *http.ServeMux) (*httptest.Server, string) {
	t.Helper()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		wsHandler(conn)
	})
	srv := httptest.NewServer(mux)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	return srv, wsURL
}

func readReqAndReply(t *testing.T, conn *websocket.Conn, response any) {
	t.Helper()
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read request: %v", err)
	}
	var req struct {
		Type string          `json:"type"`
		Ref  string          `json:"ref"`
		D    json.RawMessage `json:"d,omitempty"`
	}
	json.Unmarshal(msg, &req)
	ok := true
	var rawD json.RawMessage
	if response != nil {
		rawD, _ = json.Marshal(response)
	}
	reply := map[string]any{
		"type": "reply",
		"ref":  req.Ref,
		"ok":   ok,
	}
	if rawD != nil {
		reply["d"] = json.RawMessage(rawD)
	}
	data, _ := json.Marshal(reply)
	conn.WriteMessage(websocket.TextMessage, data)
}

func readReqAndError(t *testing.T, conn *websocket.Conn, errMsg string) {
	t.Helper()
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read request: %v", err)
	}
	var req struct {
		Ref string `json:"ref"`
	}
	json.Unmarshal(msg, &req)
	ok := false
	d, _ := json.Marshal(map[string]string{"message": errMsg})
	reply := map[string]any{
		"type": "reply",
		"ref":  req.Ref,
		"ok":   ok,
		"d":    json.RawMessage(d),
	}
	data, _ := json.Marshal(reply)
	conn.WriteMessage(websocket.TextMessage, data)
}

func TestDialConnects(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c, err := Dial(ctx, wsURL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
}

func TestClose(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()

	c, err := Dial(context.Background(), wsURL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	if _, ok := <-c.Events(); ok {
		t.Error("Events channel should be closed after Close")
	}

	if err := c.Close(); err != ErrClosed {
		t.Errorf("second Close = %v, want ErrClosed", err)
	}
}

func TestEventDelivery(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		time.Sleep(50 * time.Millisecond)
		broadcast := map[string]any{
			"type": "message.new",
			"d": map[string]any{
				"id":           1,
				"channel":      "general",
				"channel_type": "channel",
				"from":         "alice",
				"body":         "hello",
				"sent_at":      "2026-03-11T00:00:00Z",
			},
		}
		data, _ := json.Marshal(broadcast)
		conn.WriteMessage(websocket.TextMessage, data)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()

	c, err := Dial(context.Background(), wsURL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	select {
	case ev := <-c.Events():
		if ev.Type != "message.new" {
			t.Errorf("event type = %q, want message.new", ev.Type)
		}
		msg, err := ev.AsMessage()
		if err != nil {
			t.Fatalf("AsMessage: %v", err)
		}
		if msg.From != "alice" {
			t.Errorf("From = %q, want alice", msg.From)
		}
		if msg.Channel != "general" {
			t.Errorf("Channel = %q, want general", msg.Channel)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestServerError(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		readReqAndError(t, conn, "not authorized")
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var se *ServerError
	if !errors.As(err, &se) {
		t.Fatalf("expected ServerError, got %T: %v", err, err)
	}
	if se.Message != "not authorized" {
		t.Errorf("Message = %q, want %q", se.Message, "not authorized")
	}
}

func TestUsers(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		readReqAndReply(t, conn, map[string]any{
			"users": []map[string]any{
				{"username": "alice", "online": true, "type": "human", "state": "active"},
				{"username": "bob", "online": false, "type": "human"},
			},
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	users, err := c.Users(context.Background())
	if err != nil {
		t.Fatalf("Users: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len(users) = %d, want 2", len(users))
	}
	if users[0].Username != "alice" || !users[0].Online {
		t.Errorf("users[0] = %+v", users[0])
	}
}

func TestChannels(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		readReqAndReply(t, conn, map[string]any{
			"channels": []map[string]any{
				{"name": "general", "public": true, "member": true},
			},
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	channels, err := c.Channels(context.Background())
	if err != nil {
		t.Fatalf("Channels: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("len = %d, want 1", len(channels))
	}
	if !channels[0].Public || channels[0].Name != "general" {
		t.Errorf("channels[0] = %+v", channels[0])
	}
}

func TestCreateChannel(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		readReqAndReply(t, conn, map[string]any{"name": "test"})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	if err := c.CreateChannel(context.Background(), "test", true); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
}

func TestJoinChannel(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		readReqAndReply(t, conn, nil)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	if err := c.JoinChannel(context.Background(), "general"); err != nil {
		t.Fatalf("JoinChannel: %v", err)
	}
}

func TestSendMessage(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		readReqAndReply(t, conn, map[string]any{"id": 42})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	id, err := c.SendMessage(context.Background(), "general", "hello", nil)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if id != 42 {
		t.Errorf("id = %d, want 42", id)
	}
}

func TestHistory(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		readReqAndReply(t, conn, map[string]any{
			"channel": "general",
			"messages": []map[string]any{
				{"id": 1, "from": "alice", "body": "hi", "sent_at": "2026-03-11T00:00:00Z"},
				{"id": 2, "from": "bob", "body": "hello", "sent_at": "2026-03-11T00:01:00Z"},
			},
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	msgs, err := c.History(context.Background(), "general", nil)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len = %d, want 2", len(msgs))
	}
	if msgs[0].From != "alice" {
		t.Errorf("msgs[0].From = %q, want alice", msgs[0].From)
	}
}

func TestDMOpen(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		readReqAndReply(t, conn, map[string]any{
			"channel": "dm_alice_bob", "participant": "bob", "created": true,
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	result, err := c.DMOpen(context.Background(), "bob")
	if err != nil {
		t.Fatalf("DMOpen: %v", err)
	}
	if result.Channel != "dm_alice_bob" || !result.Created {
		t.Errorf("result = %+v", result)
	}
}

func TestDMList(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		readReqAndReply(t, conn, map[string]any{
			"dms": []map[string]any{
				{"channel": "dm_alice_bob", "participants": []string{"alice", "bob"}},
			},
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	dms, err := c.DMList(context.Background())
	if err != nil {
		t.Fatalf("DMList: %v", err)
	}
	if len(dms) != 1 || dms[0].Channel != "dm_alice_bob" {
		t.Errorf("dms = %+v", dms)
	}
}

func TestUnreadCounts(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		readReqAndReply(t, conn, map[string]any{
			"counts": []map[string]any{
				{"channel": "general", "type": "channel", "unread_count": 5, "mention_count": 1},
			},
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	counts, err := c.UnreadCounts(context.Background())
	if err != nil {
		t.Fatalf("UnreadCounts: %v", err)
	}
	if len(counts) != 1 || counts[0].UnreadCount != 5 {
		t.Errorf("counts = %+v", counts)
	}
}

func TestVersion(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		readReqAndReply(t, conn, map[string]any{"version": "v1.2.3"})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	v, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != "v1.2.3" {
		t.Errorf("version = %q, want v1.2.3", v)
	}
}

func TestCapabilities(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		readReqAndReply(t, conn, map[string]any{
			"permissions": []string{"send_message", "history"},
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	perms, err := c.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if len(perms) != 2 {
		t.Errorf("len(perms) = %d, want 2", len(perms))
	}
}

func TestSetState(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		readReqAndReply(t, conn, nil)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	if err := c.SetState(context.Background(), "active"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
}

func TestGetSettings(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		readReqAndReply(t, conn, map[string]any{
			"settings": map[string]string{"theme": "dark"},
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	settings, err := c.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if settings["theme"] != "dark" {
		t.Errorf("settings = %v", settings)
	}
}

func TestCreateMentionGroup(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		readReqAndReply(t, conn, map[string]any{"id": 7, "slug": "backend"})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	id, err := c.CreateMentionGroup(context.Background(), "backend")
	if err != nil {
		t.Fatalf("CreateMentionGroup: %v", err)
	}
	if id != 7 {
		t.Errorf("id = %d, want 7", id)
	}
}

func TestPing(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		// Ping gets a pong reply (type=pong, ok=true, with matching ref).
		_, msg, _ := conn.ReadMessage()
		var req struct {
			Ref string `json:"ref"`
		}
		json.Unmarshal(msg, &req)
		ok := true
		reply, _ := json.Marshal(map[string]any{
			"type": "pong", "ref": req.Ref, "ok": ok,
		})
		conn.WriteMessage(websocket.TextMessage, reply)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestRequestInterleaveWithBroadcasts(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		_, msg, _ := conn.ReadMessage()
		var req struct {
			Ref string `json:"ref"`
		}
		json.Unmarshal(msg, &req)

		// Send a broadcast BEFORE the reply.
		broadcast, _ := json.Marshal(map[string]any{
			"type": "presence",
			"d":    map[string]any{"username": "bob", "status": "online", "state": "active"},
		})
		conn.WriteMessage(websocket.TextMessage, broadcast)

		// Now send the actual reply.
		ok := true
		reply, _ := json.Marshal(map[string]any{
			"type": "reply", "ref": req.Ref, "ok": ok,
		})
		conn.WriteMessage(websocket.TextMessage, reply)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()

	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	select {
	case ev := <-c.Events():
		if ev.Type != "presence" {
			t.Errorf("event type = %q, want presence", ev.Type)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for presence event")
	}
}

func TestContextCancellation(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.Ping(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestDisconnectMidRequest(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		conn.ReadMessage()
		conn.Close()
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	_, err := c.Users(context.Background())
	if err == nil {
		t.Fatal("expected error on disconnect mid-request")
	}
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

func TestPresenceEvent(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		time.Sleep(50 * time.Millisecond)
		broadcast, _ := json.Marshal(map[string]any{
			"type": "presence",
			"d":    map[string]any{"username": "bob", "status": "offline"},
		})
		conn.WriteMessage(websocket.TextMessage, broadcast)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer srv.Close()
	c, _ := Dial(context.Background(), wsURL)
	defer c.Close()
	select {
	case ev := <-c.Events():
		p, err := ev.AsPresence()
		if err != nil {
			t.Fatalf("AsPresence: %v", err)
		}
		if p.Username != "bob" || p.Status != "offline" {
			t.Errorf("presence = %+v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for presence event")
	}
}

func TestReconnect(t *testing.T) {
	var connCount atomic.Int32
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		n := connCount.Add(1)
		if n == 1 {
			// First connection: close after reading one message.
			conn.ReadMessage()
			conn.Close()
			return
		}
		// Second connection: stay open, reply to requests.
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var req struct {
				Ref string `json:"ref"`
			}
			json.Unmarshal(msg, &req)
			ok := true
			reply, _ := json.Marshal(map[string]any{
				"type": "reply", "ref": req.Ref, "ok": ok,
			})
			conn.WriteMessage(websocket.TextMessage, reply)
		}
	})
	defer srv.Close()

	c, _ := Dial(context.Background(), wsURL,
		WithReconnect(func(attempt int) time.Duration {
			return 50 * time.Millisecond // fast backoff for test
		}),
	)
	defer c.Close()

	// Trigger disconnect by sending a message (server closes after first read).
	c.Ping(context.Background()) // will fail

	// Wait for reconnect event.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-c.Events():
			if ev.Type == "reconnect" {
				// After reconnect, requests should work.
				if err := c.Ping(context.Background()); err != nil {
					t.Fatalf("Ping after reconnect: %v", err)
				}
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting for reconnect")
		}
	}
}

func TestAuthHeaders(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	c, err := Dial(context.Background(), wsURL, WithToken("my-jwt-token"))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if gotAuth != "Bearer my-jwt-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer my-jwt-token")
	}
}

func TestAPIKeyHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	c, err := Dial(context.Background(), wsURL, WithAPIKey("sk-test-key"))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if gotAuth != "Bearer sk-test-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer sk-test-key")
	}
}

func TestRESTRegisterWebhook(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/webhooks", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			URL string `json:"url"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": "hook-123", "url": req.URL, "active": true})
	})

	srv, wsURL := mockServerWithREST(t, func(conn *websocket.Conn) {
		// keep WS open for the client lifecycle
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}, mux)
	defer srv.Close()

	c, err := Dial(context.Background(), wsURL, WithToken("tok"))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	id, err := c.RegisterWebhook(context.Background(), "http://example.com/hook")
	if err != nil {
		t.Fatalf("RegisterWebhook: %v", err)
	}
	if id != "hook-123" {
		t.Errorf("id = %q, want hook-123", id)
	}
}

func TestRESTUnregisterWebhook(t *testing.T) {
	var deletedID string
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/v1/webhooks/{id}", func(w http.ResponseWriter, r *http.Request) {
		deletedID = r.PathValue("id")
		w.WriteHeader(http.StatusNoContent)
	})

	srv, wsURL := mockServerWithREST(t, func(conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}, mux)
	defer srv.Close()

	c, err := Dial(context.Background(), wsURL, WithToken("tok"))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if err := c.UnregisterWebhook(context.Background(), "hook-abc"); err != nil {
		t.Fatalf("UnregisterWebhook: %v", err)
	}
	if deletedID != "hook-abc" {
		t.Errorf("deleted id = %q, want hook-abc", deletedID)
	}
}

func TestRESTListWebhooks(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/webhooks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": "hook-1", "url": "http://a.example/hook", "active": true},
			{"id": "hook-2", "url": "http://b.example/hook", "active": true},
		})
	})

	srv, wsURL := mockServerWithREST(t, func(conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}, mux)
	defer srv.Close()

	c, err := Dial(context.Background(), wsURL, WithToken("tok"))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	hooks, err := c.ListWebhooks(context.Background())
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if len(hooks) != 2 || hooks[0].ID != "hook-1" || hooks[1].ID != "hook-2" {
		t.Errorf("unexpected hooks: %v", hooks)
	}
}

func TestRESTRegisterIdentity(t *testing.T) {
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/register", func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	})

	srv, wsURL := mockServerWithREST(t, func(conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}, mux)
	defer srv.Close()

	c, err := Dial(context.Background(), wsURL, WithToken("tok"))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if err := c.Register(context.Background()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !called {
		t.Error("expected POST /api/v1/auth/register to be called")
	}
}
