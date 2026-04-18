// SPDX-License-Identifier: Apache-2.0
package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewRESTClient_NoDial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewRESTClient(srv.URL, WithToken("tok"))
	if c == nil {
		t.Fatal("NewRESTClient returned nil")
	}
	// Constructor must not open a WS connection — nothing to close
	// beyond its own http.Client transport. Calling Close is a no-op
	// but must not panic.
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestRESTClientRegister(t *testing.T) {
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/register", func(w http.ResponseWriter, r *http.Request) {
		called = true
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q, want Bearer tok", got)
		}
		w.WriteHeader(http.StatusCreated)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewRESTClient(srv.URL, WithToken("tok"))
	if err := c.Register(context.Background()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !called {
		t.Error("expected POST /api/v1/auth/register to be called")
	}
}

func TestRESTClientChannels(t *testing.T) {
	// REST endpoint returns a bare JSON array (server includes an extra
	// {id} field per channel that the client ignores). Verify decode
	// into the shared []Channel type works.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/channels", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "name": "general", "public": true, "member": true},
			{"id": 2, "name": "alpha", "public": false, "member": false},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewRESTClient(srv.URL, WithToken("tok"))
	channels, err := c.Channels(context.Background())
	if err != nil {
		t.Fatalf("Channels: %v", err)
	}
	if len(channels) != 2 {
		t.Fatalf("len = %d, want 2", len(channels))
	}
	if channels[0].Name != "general" || !channels[0].Public || !channels[0].Member {
		t.Errorf("channels[0] = %+v", channels[0])
	}
	if channels[1].Name != "alpha" || channels[1].Public || channels[1].Member {
		t.Errorf("channels[1] = %+v", channels[1])
	}
}

func TestRESTClientCreateChannel(t *testing.T) {
	var gotName string
	var gotPublic bool
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/channels", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name   string `json:"name"`
			Public bool   `json:"public"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		gotName = body.Name
		gotPublic = body.Public
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": 11, "name": body.Name, "public": body.Public})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewRESTClient(srv.URL, WithToken("tok"))
	if err := c.CreateChannel(context.Background(), "general", true); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if gotName != "general" || !gotPublic {
		t.Errorf("server saw name=%q public=%v", gotName, gotPublic)
	}
}

func TestRESTClientJoinChannel(t *testing.T) {
	var gotChannel string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/channels/{channel}/join", func(w http.ResponseWriter, r *http.Request) {
		gotChannel = r.PathValue("channel")
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewRESTClient(srv.URL, WithToken("tok"))
	if err := c.JoinChannel(context.Background(), "general"); err != nil {
		t.Fatalf("JoinChannel: %v", err)
	}
	if gotChannel != "general" {
		t.Errorf("path channel = %q, want general", gotChannel)
	}
}

func TestRESTClientSendMessage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/channels/{channel}/messages", func(w http.ResponseWriter, r *http.Request) {
		if got := r.PathValue("channel"); got != "general" {
			t.Errorf("channel = %q, want general", got)
		}
		var body struct {
			Body     string `json:"body"`
			ThreadID *int64 `json:"thread_id,omitempty"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Body != "hello" {
			t.Errorf("body = %q, want hello", body.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": 42, "body": body.Body, "sent_at": "2026-04-18T00:00:00Z"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewRESTClient(srv.URL, WithToken("tok"))
	id, err := c.SendMessage(context.Background(), "general", "hello", nil)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if id != 42 {
		t.Errorf("id = %d, want 42", id)
	}
}

func TestRESTClientSendMessageMetadata(t *testing.T) {
	// The server expects metadata as a JSON object (map[string]any),
	// not a JSON string. Verify the client parses the JSON-string
	// SendOpts.Metadata into an object before transmitting.
	var gotMetadata any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/channels/{channel}/messages", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Body     string `json:"body"`
			Metadata any    `json:"metadata"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("server decode: %v", err)
		}
		gotMetadata = body.Metadata
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": 99, "body": body.Body, "sent_at": "2026-04-18T00:00:00Z"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewRESTClient(srv.URL, WithToken("tok"))
	meta := `{"event_type":"task_assignment","event_payload":{"task_id":"42"}}`
	id, err := c.SendMessage(context.Background(), "general", "body", &SendOpts{Metadata: &meta})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if id != 99 {
		t.Errorf("id = %d, want 99", id)
	}
	m, ok := gotMetadata.(map[string]any)
	if !ok {
		t.Fatalf("metadata wire type = %T, want map[string]any (was sent as a JSON string?)", gotMetadata)
	}
	if got := m["event_type"]; got != "task_assignment" {
		t.Errorf("metadata.event_type = %v, want task_assignment", got)
	}
	payload, ok := m["event_payload"].(map[string]any)
	if !ok {
		t.Fatalf("metadata.event_payload type = %T, want map[string]any", m["event_payload"])
	}
	if payload["task_id"] != "42" {
		t.Errorf("metadata.event_payload.task_id = %v, want 42", payload["task_id"])
	}
}

func TestRESTClientSendMessageInvalidMetadata(t *testing.T) {
	// Invalid JSON in SendOpts.Metadata must be rejected before any
	// network call so the caller gets a clear error.
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/channels/{channel}/messages", func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewRESTClient(srv.URL, WithToken("tok"))
	bad := `not-json`
	if _, err := c.SendMessage(context.Background(), "general", "body", &SendOpts{Metadata: &bad}); err == nil {
		t.Fatal("expected error for invalid metadata JSON")
	}
	if called {
		t.Error("server should not have been contacted with invalid metadata")
	}
}

func TestRESTClientListMessages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/channels/{channel}/messages", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("limit"); got != "10" {
			t.Errorf("limit = %q, want 10", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "from": "alice", "body": "hi", "sent_at": "2026-04-18T00:00:00Z"},
			{"id": 2, "from": "bob", "body": "yo", "sent_at": "2026-04-18T00:00:01Z"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewRESTClient(srv.URL, WithToken("tok"))
	limit := 10
	msgs, err := c.ListMessages(context.Background(), "general", &HistoryOpts{Limit: &limit})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len = %d, want 2", len(msgs))
	}
	if msgs[0].From != "alice" || msgs[1].From != "bob" {
		t.Errorf("msgs = %+v", msgs)
	}
}

func TestRESTClientRegisterWebhook(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/webhooks", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			URL string `json:"url"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": "hook-xyz", "url": req.URL, "active": true})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewRESTClient(srv.URL, WithToken("tok"))
	id, err := c.RegisterWebhook(context.Background(), "http://example.com/hook")
	if err != nil {
		t.Fatalf("RegisterWebhook: %v", err)
	}
	if id != "hook-xyz" {
		t.Errorf("id = %q, want hook-xyz", id)
	}
}

func TestRESTClientListWebhooks(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/webhooks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": "h1", "url": "http://a/", "active": true},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewRESTClient(srv.URL, WithToken("tok"))
	hooks, err := c.ListWebhooks(context.Background())
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if len(hooks) != 1 || hooks[0].ID != "h1" {
		t.Errorf("hooks = %+v", hooks)
	}
}

func TestRESTClientUnregisterWebhook(t *testing.T) {
	var gotID string
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/v1/webhooks/{id}", func(w http.ResponseWriter, r *http.Request) {
		gotID = r.PathValue("id")
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewRESTClient(srv.URL, WithToken("tok"))
	if err := c.UnregisterWebhook(context.Background(), "hook-abc"); err != nil {
		t.Fatalf("UnregisterWebhook: %v", err)
	}
	if gotID != "hook-abc" {
		t.Errorf("gotID = %q, want hook-abc", gotID)
	}
}

func TestRESTClientErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		wantIs error
	}{
		{"not_found", http.StatusNotFound, "channel not found\n", ErrNotFound},
		{"conflict", http.StatusConflict, "channel exists\n", ErrConflict},
		{"bad_request", http.StatusBadRequest, "name is required\n", ErrBadRequest},
		{"unauthorized", http.StatusUnauthorized, "unauthorized\n", ErrUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("POST /api/v1/channels/{channel}/join", func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, tc.body, tc.status)
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			c := NewRESTClient(srv.URL, WithToken("tok"))
			err := c.JoinChannel(context.Background(), "general")
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, tc.wantIs) {
				t.Errorf("errors.Is(err, %v) = false; err = %v", tc.wantIs, err)
			}
			var se *ServerError
			if !errors.As(err, &se) {
				t.Fatalf("expected *ServerError, got %T", err)
			}
			if se.Status != tc.status {
				t.Errorf("Status = %d, want %d", se.Status, tc.status)
			}
		})
	}
}

func TestRESTClientRoundTrip(t *testing.T) {
	// Minimal in-memory state — enough to prove the four calls
	// interlock correctly over REST only, without any WS connection.
	type chRecord struct {
		name     string
		public   bool
		members  map[string]bool
		messages []map[string]any
		nextID   int64
	}
	channels := map[string]*chRecord{}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/channels", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name   string `json:"name"`
			Public bool   `json:"public"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if _, ok := channels[body.Name]; ok {
			http.Error(w, "channel exists", http.StatusConflict)
			return
		}
		channels[body.Name] = &chRecord{name: body.Name, public: body.Public, members: map[string]bool{"creator": true}}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": 1, "name": body.Name, "public": body.Public})
	})
	mux.HandleFunc("POST /api/v1/channels/{channel}/join", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("channel")
		ch, ok := channels[name]
		if !ok {
			http.Error(w, "channel not found", http.StatusNotFound)
			return
		}
		ch.members["caller"] = true
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /api/v1/channels/{channel}/messages", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("channel")
		ch, ok := channels[name]
		if !ok {
			http.Error(w, "channel not found", http.StatusNotFound)
			return
		}
		var body struct {
			Body string `json:"body"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		ch.nextID++
		msg := map[string]any{"id": ch.nextID, "from": "caller", "body": body.Body, "sent_at": "2026-04-18T00:00:00Z"}
		ch.messages = append(ch.messages, msg)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": ch.nextID, "body": body.Body, "sent_at": msg["sent_at"]})
	})
	mux.HandleFunc("GET /api/v1/channels/{channel}/messages", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("channel")
		ch, ok := channels[name]
		if !ok {
			http.Error(w, "channel not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ch.messages)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewRESTClient(srv.URL, WithToken("tok"))
	ctx := context.Background()

	if err := c.CreateChannel(ctx, "demo", true); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if err := c.JoinChannel(ctx, "demo"); err != nil {
		t.Fatalf("JoinChannel: %v", err)
	}
	id, err := c.SendMessage(ctx, "demo", "hello", nil)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if id != 1 {
		t.Errorf("id = %d, want 1", id)
	}
	msgs, err := c.ListMessages(ctx, "demo", nil)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Body != "hello" {
		t.Errorf("msgs = %+v", msgs)
	}
}
