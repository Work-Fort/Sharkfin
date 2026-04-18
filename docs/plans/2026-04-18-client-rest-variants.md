---
type: plan
step: "1"
title: "Sharkfin Go client — REST variants for command methods"
status: approved
assessment_status: complete
provenance:
  source: roadmap
  issue_id: null
  roadmap_step: null
dates:
  created: "2026-04-18"
  approved: "2026-04-18"
  completed: null
related_plans:
  - 2026-03-11-client-go.md
  - 2026-04-05-flow-adapter-rest-api.md
---

# Sharkfin Go Client — REST Variants for Command Methods

**Goal:** Give REST-only consumers (Flow, other service bots that use webhooks for delivery) a way to call Sharkfin's command-style operations without maintaining a WebSocket connection. Add a new `RESTClient` type whose constructor does not open a WS connection at all. Every existing `*Client` method, option, and error type keeps working unchanged.

**Architecture:** The existing hybrid `*Client` combines WS RPC (`c.request`) with HTTP for a handful of REST-only endpoints (`webhooks`, `auth/register`). Consumers that only need command semantics (and receive events via webhook) still pay for a WS dial plus read-pump goroutine and pending-request machinery.

Today's Sharkfin server (`pkg/daemon/server.go:60-76`) exposes these REST endpoints behind Passport auth:

| Method & Path | Purpose |
|---|---|
| `POST /api/v1/auth/register` | Register identity |
| `GET /api/v1/channels` | List channels with membership |
| `POST /api/v1/channels` | Create channel |
| `POST /api/v1/channels/{channel}/join` | Join channel |
| `POST /api/v1/channels/{channel}/messages` | Send message |
| `GET /api/v1/channels/{channel}/messages` | List messages |
| `POST /api/v1/webhooks` | Register webhook |
| `GET /api/v1/webhooks` | List webhooks |
| `DELETE /api/v1/webhooks/{id}` | Unregister webhook |

We ship a new type `RESTClient` with methods that cover those nine endpoints. The plan introduces:

1. A shared private `restTransport` that holds the base URL, `http.Client`, and auth token/API key, and exposes a single `do(ctx, method, path, reqBody, out)` that returns `(status, error)`. `*Client.httpDo` is re-implemented as a thin wrapper around `restTransport.do` so the WS-backed client keeps the exact HTTP behavior it has today.
2. New `NewRESTClient(baseURL string, opts ...Option) *RESTClient` constructor that only configures `restTransport` — no `Dial`, no WS, no goroutines.
3. Methods on `*RESTClient`: `Register`, `Channels`, `CreateChannel`, `JoinChannel`, `SendMessage`, `ListMessages`, `RegisterWebhook`, `ListWebhooks`, `UnregisterWebhook`.
4. Additional sentinel errors (`ErrNotFound`, `ErrConflict`, `ErrBadRequest`, `ErrUnauthorized`) mapped from HTTP status codes, wrapped inside `*ServerError` so `errors.Is` works alongside existing behavior. The WS-backed `*Client` surfaces the same sentinels via existing `*ServerError` comparisons only where the server's textual reply permits — status-code mapping is REST-specific.
5. Unit tests per method using `httptest.Server`, a round-trip test that exercises create → join → send → list against a single in-memory mock, and error-mapping tests. No changes to existing WS tests.

The methods included are **only** those whose REST endpoint already exists on the server. `DMOpen`, `DMList`, `UnreadMessages`, `UnreadCounts`, `MarkRead`, `SetState`, `Ping`, `Version`, `Capabilities`, `SetSetting`, `GetSettings`, mention-group methods, `InviteToChannel`, `Users` have no REST counterpart today; they stay WS-only. If REST coverage for any of those is wanted later, the server needs an endpoint first — that's a follow-up plan.

**Tech Stack:** Go 1.25, `net/http`, `encoding/json`, `net/http/httptest` for tests. No new third-party dependencies.

**Commands:** The client package has no `mise.toml`. Use `go test` directly (planner.md permits the native test runner for targeted TDD). All paths are relative to `client/go/` unless noted.

---

## Prerequisites

- The sharkfin server already exposes the REST endpoints above (`pkg/daemon/rest_handlers.go`, `pkg/daemon/server.go:60-76`). No server changes.
- The client module (`client/go/go.mod`) stays on `github.com/Work-Fort/sharkfin/client/go`, go 1.25.0. No module changes.
- Existing tests must stay green without modification.

---

## Task Breakdown

### Task 1: Extract `restTransport` out of `*Client.httpDo`

**Files:**
- Create: `client/go/rest_transport.go`
- Modify: `client/go/client.go:264-306`

**Step 1: Write the new file**

```go
// SPDX-License-Identifier: Apache-2.0
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// restTransport is the shared HTTP transport used by both the WS-backed
// *Client and the REST-only *RESTClient. It owns the base URL, HTTP
// client, and auth credentials, and performs authenticated JSON
// request/response round trips.
type restTransport struct {
	baseURL    string
	httpClient *http.Client
	token      string
	apiKey     string
}

// do performs an authenticated JSON request. If reqBody is non-nil it
// is marshaled as JSON. If out is non-nil and the response has a body,
// the body is decoded into out. Returns the HTTP status code and any
// error. On 4xx/5xx, returns a *ServerError that wraps a sentinel
// (ErrNotFound, ErrConflict, ErrBadRequest, ErrUnauthorized) when the
// status maps to one.
func (t *restTransport) do(ctx context.Context, method, path string, reqBody, out any) (int, error) {
	var bodyReader io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return 0, fmt.Errorf("client: marshal: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, t.baseURL+path, bodyReader)
	if err != nil {
		return 0, fmt.Errorf("client: new request: %w", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	} else if t.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("client: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		msg := strings.TrimSpace(string(b))
		serr := &ServerError{Message: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, msg), Status: resp.StatusCode}
		switch resp.StatusCode {
		case http.StatusBadRequest:
			serr.wrapped = ErrBadRequest
		case http.StatusUnauthorized:
			serr.wrapped = ErrUnauthorized
		case http.StatusNotFound:
			serr.wrapped = ErrNotFound
		case http.StatusConflict:
			serr.wrapped = ErrConflict
		}
		return resp.StatusCode, serr
	}

	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("client: decode response: %w", err)
		}
	}
	return resp.StatusCode, nil
}

// deriveBaseURL converts a ws(s):// URL into the corresponding
// http(s):// base, trimming any trailing "/ws". Exported indirectly
// through the constructors.
func deriveBaseURL(wsURL string) string {
	base := strings.TrimSuffix(wsURL, "/ws")
	base = strings.TrimSuffix(base, "/")
	switch {
	case strings.HasPrefix(base, "ws://"):
		return "http://" + base[len("ws://"):]
	case strings.HasPrefix(base, "wss://"):
		return "https://" + base[len("wss://"):]
	}
	return base
}
```

**Step 2: Reduce `client.go`'s `httpDo` to call the transport**

Replace `client.go:264-306` (the `httpDo` method) with:

```go
// httpDo performs an authenticated HTTP request and decodes the JSON
// response into out. Pass out=nil to discard the body (e.g. for 204
// responses). Delegates to the shared restTransport.
func (c *Client) httpDo(ctx context.Context, method, path string, reqBody, out any) (int, error) {
	t := restTransport{
		baseURL:    c.baseURL,
		httpClient: c.httpClient,
		token:      c.opts.token,
		apiKey:     c.opts.apiKey,
	}
	return t.do(ctx, method, path, reqBody, out)
}
```

Also update `Dial` at `client.go:113-120` to use `deriveBaseURL`:

```go
	baseURL := deriveBaseURL(url)
```

**Step 3: Verify compile**

Run: `go build ./...` from `client/go/`
Expected: PASS. No test failures yet because `*ServerError` now has two new unexported/exported fields — the existing `&ServerError{Message: ...}` literal in `client.go:297` is replaced by the transport's construction; the one in `client.go:360` (WS request error path) keeps `Message` only, which remains valid.

**Step 4: Run existing tests to verify no regression**

Run: `go test ./...` from `client/go/`
Expected: PASS. All tests in `client_test.go` still pass because the webhook REST helpers (`RegisterWebhook`, `UnregisterWebhook`, `ListWebhooks`, `Register`) still call `c.httpDo`, which now routes through the transport.

**Step 5: Commit**

`refactor(client): extract restTransport shared helper`

---

### Task 2: Extend `errors.go` with HTTP-mapped sentinels

**Files:**
- Modify: `client/go/errors.go`

**Step 1: Replace the file contents with:**

```go
// SPDX-License-Identifier: Apache-2.0
package client

import (
	"errors"
	"fmt"
)

var (
	// ErrNotConnected is returned when a method is called on a closed or
	// disconnected client.
	ErrNotConnected = errors.New("client: not connected")

	// ErrTimeout is returned when a request does not receive a reply
	// within the context deadline.
	ErrTimeout = errors.New("client: request timeout")

	// ErrClosed is returned when operations are attempted on a closed client.
	ErrClosed = errors.New("client: closed")

	// ErrBadRequest wraps HTTP 400 responses.
	ErrBadRequest = errors.New("client: bad request")

	// ErrUnauthorized wraps HTTP 401 responses.
	ErrUnauthorized = errors.New("client: unauthorized")

	// ErrNotFound wraps HTTP 404 responses.
	ErrNotFound = errors.New("client: not found")

	// ErrConflict wraps HTTP 409 responses.
	ErrConflict = errors.New("client: conflict")
)

// ServerError is returned when the server replies with an error.
// For WS replies this is constructed from an ok:false envelope and
// leaves Status zero. For REST replies Status carries the HTTP code
// and (when the code maps to one) wrapped is set to a sentinel so
// errors.Is(err, ErrNotFound) and friends work.
type ServerError struct {
	Message string
	Status  int

	wrapped error
}

func (e *ServerError) Error() string {
	return fmt.Sprintf("sharkfin: %s", e.Message)
}

// Unwrap returns the wrapped sentinel error if any. Enables
// errors.Is(err, ErrNotFound) etc. for REST callers.
func (e *ServerError) Unwrap() error {
	return e.wrapped
}
```

**Step 2: Verify compile**

Run: `go build ./...`
Expected: PASS.

**Step 3: Run existing tests**

Run: `go test ./...`
Expected: PASS. Existing WS tests construct `&ServerError{Message: "not authorized"}` in the mock server and decode it on the client side; adding `Status` and `wrapped` as extra fields (left as zero values) doesn't change that behavior.

**Step 4: Commit**

`feat(client): add HTTP status-mapped sentinel errors`

---

### Task 3: Write failing test for `RESTClient` constructor

**Files:**
- Create: `client/go/rest_client_test.go`

**Step 1: Write the test**

```go
// SPDX-License-Identifier: Apache-2.0
package client

import (
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
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestNewRESTClient_NoDial ./...`
Expected: FAIL with `undefined: NewRESTClient` (and `c.Close` undefined).

**Step 3: Commit the failing test**

`test(client): add failing TestNewRESTClient_NoDial`

---

### Task 4: Implement `RESTClient` type and constructor

**Depends on:** Task 1 (`restTransport`), Task 2 (`errors.go` update)

**Files:**
- Create: `client/go/rest_client.go`

**Step 1: Write the file**

```go
// SPDX-License-Identifier: Apache-2.0
package client

import (
	"net/http"
	"time"
)

// RESTClient is a stateless HTTP-only client for the Sharkfin server.
// Unlike *Client it does not open a WebSocket connection and does not
// receive server-pushed events. Consumers that receive events via a
// registered webhook instead of the WS event stream should use this
// type — it has no background goroutines, no reconnection state, and
// no Dial step.
type RESTClient struct {
	transport restTransport
}

// NewRESTClient constructs a REST-only Sharkfin client. baseURL must
// point at the server root (e.g. "http://localhost:16000"), not at a
// sub-path — REST method paths like "/api/v1/channels" are appended
// directly. A WebSocket URL such as "ws://localhost:16000/ws" is also
// accepted: the scheme is rewritten to http(s) and a trailing "/ws"
// is trimmed. Authentication is provided via WithToken or WithAPIKey;
// other Options (WithDialer, WithReconnect) are accepted for
// signature compatibility but are ignored because there is no WS
// connection.
func NewRESTClient(baseURL string, opts ...Option) *RESTClient {
	o := clientOpts{}
	for _, opt := range opts {
		opt(&o)
	}
	return &RESTClient{
		transport: restTransport{
			baseURL:    deriveBaseURL(baseURL),
			httpClient: &http.Client{Timeout: 30 * time.Second},
			token:      o.token,
			apiKey:     o.apiKey,
		},
	}
}

// Close releases any resources held by the client. Currently a no-op
// because the underlying http.Client does not require explicit
// cleanup. Provided so callers can write symmetric setup/teardown
// code.
func (c *RESTClient) Close() error {
	return nil
}
```

**Step 2: Run the test**

Run: `go test -run TestNewRESTClient_NoDial ./...`
Expected: PASS.

**Step 3: Commit**

`feat(client): add RESTClient type and constructor`

---

### Task 5: Write failing test for `RESTClient.Register`

**Depends on:** Task 4

**Files:**
- Modify: `client/go/rest_client_test.go`

**Step 1: Append the test**

```go
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
```

Also add the `context` import to `rest_client_test.go`:

```go
import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestRESTClientRegister ./...`
Expected: FAIL with `c.Register undefined (type *RESTClient has no field or method Register)`.

**Step 3: Commit**

`test(client): add failing TestRESTClientRegister`

---

### Task 6: Implement `RESTClient.Register` + `Channels` + `CreateChannel`

**Depends on:** Task 4, Task 5

**Files:**
- Create: `client/go/rest_requests.go`

**Step 1: Write the file**

```go
// SPDX-License-Identifier: Apache-2.0
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// --- Identity ---

// Register registers the calling identity as a service bot.
func (c *RESTClient) Register(ctx context.Context) error {
	_, err := c.transport.do(ctx, http.MethodPost, "/api/v1/auth/register", nil, nil)
	return err
}

// --- Channels ---

// Channels returns all channels visible to the current user.
// The REST endpoint shape matches the shared Channel type's JSON tags
// ({name, public, member}); the server also includes an {id} field
// which the client ignores.
func (c *RESTClient) Channels(ctx context.Context) ([]Channel, error) {
	var out []Channel
	if _, err := c.transport.do(ctx, http.MethodGet, "/api/v1/channels", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateChannel creates a new channel.
func (c *RESTClient) CreateChannel(ctx context.Context, name string, public bool) error {
	body := map[string]any{"name": name, "public": public}
	_, err := c.transport.do(ctx, http.MethodPost, "/api/v1/channels", body, nil)
	return err
}

// JoinChannel joins a public channel by name.
func (c *RESTClient) JoinChannel(ctx context.Context, channel string) error {
	path := "/api/v1/channels/" + url.PathEscape(channel) + "/join"
	_, err := c.transport.do(ctx, http.MethodPost, path, nil, nil)
	return err
}

// --- Messages ---

// SendMessage sends a message to a channel. Returns the message ID
// assigned by the server.
//
// SendOpts.Metadata is a JSON-encoded string matching the WS path's
// type. The REST endpoint expects a JSON object, not a string, so the
// metadata is parsed into a map before sending. Invalid JSON returns
// an error without contacting the server.
func (c *RESTClient) SendMessage(ctx context.Context, channel, body string, opts *SendOpts) (int64, error) {
	reqBody := map[string]any{"body": body}
	if opts != nil {
		if opts.ThreadID != nil {
			reqBody["thread_id"] = *opts.ThreadID
		}
		if opts.Metadata != nil {
			var m map[string]any
			if err := json.Unmarshal([]byte(*opts.Metadata), &m); err != nil {
				return 0, fmt.Errorf("client: metadata is not valid JSON: %w", err)
			}
			reqBody["metadata"] = m
		}
	}
	var out struct {
		ID int64 `json:"id"`
	}
	path := "/api/v1/channels/" + url.PathEscape(channel) + "/messages"
	if _, err := c.transport.do(ctx, http.MethodPost, path, reqBody, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

// ListMessages retrieves messages from a channel, subject to
// optional before/after/limit pagination.
func (c *RESTClient) ListMessages(ctx context.Context, channel string, opts *HistoryOpts) ([]Message, error) {
	q := url.Values{}
	if opts != nil {
		if opts.Before != nil {
			q.Set("before", strconv.FormatInt(*opts.Before, 10))
		}
		if opts.After != nil {
			q.Set("after", strconv.FormatInt(*opts.After, 10))
		}
		if opts.Limit != nil {
			q.Set("limit", strconv.Itoa(*opts.Limit))
		}
	}
	path := "/api/v1/channels/" + url.PathEscape(channel) + "/messages"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var out []Message
	if _, err := c.transport.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// --- Webhooks ---

// RegisterWebhook registers a webhook URL for the calling identity.
// Returns the webhook ID.
func (c *RESTClient) RegisterWebhook(ctx context.Context, hookURL string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if _, err := c.transport.do(ctx, http.MethodPost, "/api/v1/webhooks", map[string]string{"url": hookURL}, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// ListWebhooks returns all active webhooks for the calling identity.
func (c *RESTClient) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	var out []Webhook
	if _, err := c.transport.do(ctx, http.MethodGet, "/api/v1/webhooks", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UnregisterWebhook removes a registered webhook by ID.
func (c *RESTClient) UnregisterWebhook(ctx context.Context, id string) error {
	path := "/api/v1/webhooks/" + url.PathEscape(id)
	_, err := c.transport.do(ctx, http.MethodDelete, path, nil, nil)
	return err
}
```

**Step 2: Run the test**

Run: `go test -run TestRESTClientRegister ./...`
Expected: PASS.

**Step 3: Commit**

`feat(client): add RESTClient command methods`

---

### Task 7: Test `RESTClient.Channels`, `CreateChannel`, and `JoinChannel`

**Depends on:** Task 6

**Files:**
- Modify: `client/go/rest_client_test.go`

**Step 1: Append tests**

```go
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
```

Add `encoding/json` to the imports of `rest_client_test.go`:

```go
import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)
```

**Step 2: Run the tests**

Run: `go test -run 'TestRESTClientChannels|TestRESTClientCreateChannel|TestRESTClientJoinChannel' ./...`
Expected: PASS.

**Step 3: Commit**

`test(client): cover RESTClient Channels, CreateChannel, and JoinChannel`

---

### Task 8: Test `RESTClient.SendMessage` and `ListMessages`

**Depends on:** Task 6

**Files:**
- Modify: `client/go/rest_client_test.go`

**Step 1: Append tests**

```go
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
```

**Step 2: Run**

Run: `go test -run 'TestRESTClientSendMessage|TestRESTClientSendMessageMetadata|TestRESTClientSendMessageInvalidMetadata|TestRESTClientListMessages' ./...`
Expected: PASS.

**Step 3: Commit**

`test(client): cover RESTClient SendMessage and ListMessages`

---

### Task 9: Test `RESTClient` webhook methods

**Depends on:** Task 6

**Files:**
- Modify: `client/go/rest_client_test.go`

**Step 1: Append tests**

```go
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
```

**Step 2: Run**

Run: `go test -run 'TestRESTClient(Register|List|Unregister)Webhook' ./...`
Expected: PASS.

**Step 3: Commit**

`test(client): cover RESTClient webhook methods`

---

### Task 10: Error-mapping test — 404/409/400/401 map to sentinels

**Depends on:** Task 4, Task 6

**Files:**
- Modify: `client/go/rest_client_test.go`

**Step 1: Append test**

```go
func TestRESTClientErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantIs  error
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
```

Add `errors` to imports:

```go
import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)
```

**Step 2: Run**

Run: `go test -run TestRESTClientErrorMapping ./...`
Expected: PASS.

**Step 3: Commit**

`test(client): verify REST error codes map to sentinel errors`

---

### Task 11: Round-trip test — create → join → send → list against a single stateful mock

**Depends on:** Tasks 6–9

**Files:**
- Modify: `client/go/rest_client_test.go`

**Step 1: Append test**

```go
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
```

**Step 2: Run**

Run: `go test -run TestRESTClientRoundTrip ./...`
Expected: PASS. The test proves the REST-only flow works end-to-end without ever opening a WS connection.

**Step 3: Commit**

`test(client): add REST-only round-trip integration test`

---

### Task 12: Update `doc.go` to mention `RESTClient`

**Files:**
- Modify: `client/go/doc.go`

**Step 1: Replace with**

```go
// SPDX-License-Identifier: Apache-2.0

// Package client provides a typed Go client for the Sharkfin messaging
// server.
//
// There are two client types:
//
//   - *Client — connects via WebSocket for real-time events and also
//     offers a handful of REST methods (webhook registration,
//     identity registration). Use this when you need the server's
//     event stream.
//
//   - *RESTClient — HTTP-only, no WebSocket. Use this when your
//     service receives events via a registered webhook and therefore
//     does not need the WS event channel. No goroutines, no Dial
//     step, no reconnection state.
//
// Usage (WebSocket):
//
//	c, err := client.Dial(ctx, "ws://localhost:16000/ws", client.WithToken(tok))
//	if err != nil { log.Fatal(err) }
//	defer c.Close()
//
//	for ev := range c.Events() {
//	    switch ev.Type {
//	    case "message.new":
//	        msg, _ := ev.AsMessage()
//	        fmt.Printf("%s: %s\n", msg.From, msg.Body)
//	    }
//	}
//
// Usage (REST-only):
//
//	c := client.NewRESTClient("http://localhost:16000", client.WithToken(tok))
//	defer c.Close()
//
//	if err := c.Register(ctx); err != nil { log.Fatal(err) }
//	if err := c.CreateChannel(ctx, "general", true); err != nil { log.Fatal(err) }
//	id, err := c.SendMessage(ctx, "general", "hello", nil)
//	if err != nil { log.Fatal(err) }
//	_ = id
package client
```

**Step 2: Verify compile**

Run: `go build ./...`
Expected: PASS.

**Step 3: Commit**

`docs(client): document RESTClient usage in package doc`

---

### Task 13: Full regression run

**Depends on:** All previous tasks

**Step 1: Run the entire client test suite**

Run: `go test -race ./...` from `client/go/`
Expected: PASS. All pre-existing tests (WS tests, reconnection, event delivery, auth headers, existing REST webhook tests) stay green; the new REST tests pass. No data races introduced.

**Step 2: Run `go vet`**

Run: `go vet ./...`
Expected: no output.

**Step 3: Confirm module builds from consumer angle**

Run: `go build ./...` from the repo root of sharkfin (`cd /home/kazw/Work/WorkFort/sharkfin/lead && go build ./...`) to make sure the server side still compiles against unchanged domain types.
Expected: PASS.

---

## Verification Checklist

After all tasks complete:

- [ ] `go test -race ./...` in `client/go/` passes.
- [ ] `go vet ./...` in `client/go/` produces no output.
- [ ] Existing tests in `client_test.go` are unmodified.
- [ ] `NewRESTClient` does not open any WS connection (verified by `TestNewRESTClient_NoDial` and by construction — the constructor only builds an `http.Client`).
- [ ] `RESTClient.Close()` is safe to call multiple times (no-op) and returns nil.
- [ ] `errors.Is(err, ErrNotFound)` returns true for a REST call that receives a 404 (`TestRESTClientErrorMapping`).
- [ ] `*Client` public surface is unchanged: `Dial`, `Close`, `Events`, all existing request/REST methods keep identical signatures.
- [ ] `restTransport` is used by both `*Client.httpDo` and `*RESTClient` methods (single code path for HTTP).
- [ ] `RESTClient.SendMessage` transmits `SendOpts.Metadata` as a JSON object on the wire, not a JSON string (`TestRESTClientSendMessageMetadata`).
- [ ] `RESTClient.Channels` decodes the bare `[]Channel` REST response shape (`TestRESTClientChannels`).
- [ ] Round-trip test (`TestRESTClientRoundTrip`) exercises create → join → send → list against a single `httptest.Server` with no WS upgrade.
- [ ] Package doc (`doc.go`) documents both client types.

## Out of Scope

The following are explicitly not part of this plan:

- **REST endpoints that don't exist on the server yet.** `DMOpen`, `DMList`, `UnreadMessages`, `UnreadCounts`, `MarkRead`, `SetState`, `Ping`, `Version`, `Capabilities`, `SetSetting`, `GetSettings`, mention-group methods, `InviteToChannel`, `Users` stay WS-only. Adding REST coverage for any of these requires a server change first.
- **Changing Flow's adapter to consume `RESTClient`.** Lives in a different repo; separate follow-up plan.
- **Server-side changes.** The Sharkfin daemon already exposes all endpoints this plan depends on.
- **Modifying existing `*Client` method signatures or error surfaces.** Strictly additive.
- **Removing `httpDo` from `*Client`.** It stays; the WS-backed client still offers `RegisterWebhook`, `ListWebhooks`, `UnregisterWebhook`, `Register` to preserve backwards compatibility for existing callers that use `*Client` for a mix of WS and REST work.
