// SPDX-License-Identifier: Apache-2.0
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	eventBufSize = 64
)

// Client is a WebSocket client for the Sharkfin messaging server.
type Client struct {
	conn       *websocket.Conn
	url        string
	baseURL    string // e.g. "http://localhost:16000"
	httpClient *http.Client

	events chan Event
	done   chan struct{}
	closed atomic.Bool

	// pending tracks in-flight requests by ref.
	mu      sync.Mutex
	pending map[string]chan envelope
	refSeq  atomic.Int64

	opts clientOpts
}

type clientOpts struct {
	dialer    *websocket.Dialer
	reconnect BackoffFunc
	logger    *slog.Logger
	token     string
	apiKey    string
}

// BackoffFunc returns the delay before reconnection attempt N (0-indexed).
// Return a negative duration to stop reconnecting.
type BackoffFunc func(attempt int) time.Duration

// Option configures a Client.
type Option func(*clientOpts)

// WithDialer sets a custom websocket.Dialer.
func WithDialer(d *websocket.Dialer) Option {
	return func(o *clientOpts) { o.dialer = d }
}

// WithReconnect enables automatic reconnection with the given backoff function.
func WithReconnect(backoff BackoffFunc) Option {
	return func(o *clientOpts) { o.reconnect = backoff }
}

// WithLogger sets a structured logger for the client.
func WithLogger(l *slog.Logger) Option {
	return func(o *clientOpts) { o.logger = l }
}

// WithToken sets a Passport JWT token for authentication.
func WithToken(t string) Option {
	return func(o *clientOpts) { o.token = t }
}

// WithAPIKey sets an API key for authentication.
func WithAPIKey(k string) Option {
	return func(o *clientOpts) { o.apiKey = k }
}

type envelope struct {
	Type string          `json:"type"`
	D    json.RawMessage `json:"d,omitempty"`
	Ref  string          `json:"ref,omitempty"`
	OK   *bool           `json:"ok,omitempty"`
}

// Dial connects to a Sharkfin server at the given WebSocket URL (e.g.
// "ws://localhost:16000/ws") and starts the background read pump.
// Authentication is provided via WithToken or WithAPIKey options.
func Dial(ctx context.Context, url string, opts ...Option) (*Client, error) {
	o := clientOpts{
		dialer: websocket.DefaultDialer,
	}
	for _, opt := range opts {
		opt(&o)
	}

	header := http.Header{}
	if o.token != "" {
		header.Set("Authorization", "Bearer "+o.token)
	} else if o.apiKey != "" {
		header.Set("Authorization", "Bearer "+o.apiKey)
	}

	conn, _, err := o.dialer.DialContext(ctx, url, header)
	if err != nil {
		return nil, fmt.Errorf("client: dial: %w", err)
	}

	// Derive HTTP base URL from WS URL.
	baseURL := deriveBaseURL(url)

	c := &Client{
		conn:       conn,
		url:        url,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		events:     make(chan Event, eventBufSize),
		done:       make(chan struct{}),
		pending:    make(map[string]chan envelope),
		opts:       o,
	}

	go c.readPump()

	return c, nil
}

// Events returns a channel that receives server-pushed broadcasts
// (message.new, presence, etc.). The channel is closed when the
// client disconnects.
func (c *Client) Events() <-chan Event { return c.events }

// Close cleanly shuts down the client connection.
func (c *Client) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return ErrClosed
	}
	// Send close frame.
	deadline := time.Now().Add(time.Second)
	msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	c.conn.WriteControl(websocket.CloseMessage, msg, deadline)
	err := c.conn.Close()
	// Wait for readPump to finish.
	<-c.done
	return err
}

// readPump reads messages from the WebSocket and routes them:
// - replies/errors -> matching pending request channel
// - broadcasts -> events channel
func (c *Client) readPump() {
	defer func() {
		if c.opts.reconnect != nil && !c.closed.Load() {
			c.emitEvent(Event{Type: "disconnect"})
			c.cleanupPending()
			go c.reconnectLoop()
			return
		}
		close(c.events)
		c.cleanupPending()
		close(c.done)
	}()

	for {
		var env envelope
		if err := c.conn.ReadJSON(&env); err != nil {
			return
		}

		switch {
		case env.Ref != "":
			// Reply to a pending request.
			c.mu.Lock()
			ch, ok := c.pending[env.Ref]
			if ok {
				delete(c.pending, env.Ref)
			}
			c.mu.Unlock()
			if ok {
				ch <- env
			}
		default:
			// Server push (broadcast).
			c.emitEvent(Event{Type: env.Type, Data: env.D})
		}
	}
}

func (c *Client) emitEvent(ev Event) {
	select {
	case c.events <- ev:
	default:
		// Drop if buffer full — consumer too slow.
	}
}

// cleanupPending fails all in-flight requests on disconnect.
func (c *Client) cleanupPending() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for ref, ch := range c.pending {
		close(ch)
		delete(c.pending, ref)
	}
}

func (c *Client) reconnectLoop() {
	header := http.Header{}
	if c.opts.token != "" {
		header.Set("Authorization", "Bearer "+c.opts.token)
	} else if c.opts.apiKey != "" {
		header.Set("Authorization", "Bearer "+c.opts.apiKey)
	}

	for attempt := 0; ; attempt++ {
		if c.closed.Load() {
			close(c.events)
			close(c.done)
			return
		}

		delay := c.opts.reconnect(attempt)
		if delay < 0 {
			close(c.events)
			close(c.done)
			return
		}
		time.Sleep(delay)

		if c.closed.Load() {
			close(c.events)
			close(c.done)
			return
		}

		conn, _, err := c.opts.dialer.Dial(c.url, header)
		if err != nil {
			continue
		}

		// Swap connection.
		c.conn = conn
		c.done = make(chan struct{})

		// Emit reconnect event.
		c.emitEvent(Event{Type: "reconnect"})

		// Restart read pump.
		go c.readPump()
		return
	}
}

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

// nextRef generates a unique reference string for request tracking.
func (c *Client) nextRef() string {
	return fmt.Sprintf("ref_%d", c.refSeq.Add(1))
}

// request sends a typed envelope and waits for the matching reply.
// Returns the reply envelope or an error on timeout/disconnect.
func (c *Client) request(ctx context.Context, typ string, d any) (json.RawMessage, error) {
	if c.closed.Load() {
		return nil, ErrClosed
	}

	ref := c.nextRef()

	// Register pending before sending to avoid race.
	ch := make(chan envelope, 1)
	c.mu.Lock()
	c.pending[ref] = ch
	c.mu.Unlock()

	// Marshal d to raw JSON.
	var rawD json.RawMessage
	if d != nil {
		var err error
		rawD, err = json.Marshal(d)
		if err != nil {
			c.mu.Lock()
			delete(c.pending, ref)
			c.mu.Unlock()
			return nil, fmt.Errorf("client: marshal request: %w", err)
		}
	}

	req := envelope{Type: typ, D: rawD, Ref: ref}
	if err := c.conn.WriteJSON(req); err != nil {
		c.mu.Lock()
		delete(c.pending, ref)
		c.mu.Unlock()
		return nil, fmt.Errorf("client: write: %w", err)
	}

	select {
	case env, ok := <-ch:
		if !ok {
			return nil, ErrNotConnected
		}
		if env.OK != nil && !*env.OK {
			// Server error.
			var errData struct {
				Message string `json:"message"`
			}
			json.Unmarshal(env.D, &errData)
			return nil, &ServerError{Message: errData.Message}
		}
		return env.D, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, ref)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}
