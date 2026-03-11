// SPDX-License-Identifier: AGPL-3.0-or-later
package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func sendHello(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	hello := map[string]any{
		"type": "hello",
		"d": map[string]any{
			"heartbeat_interval": 10,
			"version":            "v0.1.0",
		},
	}
	data, _ := json.Marshal(hello)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("send hello: %v", err)
	}
}

func TestDialReadsHello(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		sendHello(t, conn)
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

	if c.ServerVersion() != "v0.1.0" {
		t.Errorf("ServerVersion = %q, want %q", c.ServerVersion(), "v0.1.0")
	}
	if c.HeartbeatInterval() != 10 {
		t.Errorf("HeartbeatInterval = %d, want 10", c.HeartbeatInterval())
	}
}

func TestClose(t *testing.T) {
	srv, wsURL := mockServer(t, func(conn *websocket.Conn) {
		sendHello(t, conn)
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
		sendHello(t, conn)
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
