// SPDX-License-Identifier: AGPL-3.0-or-later
package harness

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// --- Daemon ---

type daemonConfig struct {
	presenceTimeout time.Duration
	webhookURL      string
	dbDSN           string // explicit --db flag (overrides SHARKFIN_DB env)
}

type DaemonOption func(*daemonConfig)

func WithPresenceTimeout(d time.Duration) DaemonOption {
	return func(c *daemonConfig) { c.presenceTimeout = d }
}

func WithWebhookURL(url string) DaemonOption {
	return func(c *daemonConfig) { c.webhookURL = url }
}

func WithDB(dsn string) DaemonOption {
	return func(c *daemonConfig) { c.dbDSN = dsn }
}

type Daemon struct {
	cmd      *exec.Cmd
	addr     string
	xdgDir   string
	stderr   *bytes.Buffer
	stubStop func()
	signJWT  func(id, username, displayName, userType string) string
}

func StartDaemon(binary, addr string, opts ...DaemonOption) (*Daemon, error) {
	cfg := &daemonConfig{
		presenceTimeout: 20 * time.Second,
	}
	for _, o := range opts {
		o(cfg)
	}

	// Start JWKS stub server before the daemon so the initial JWKS fetch succeeds.
	stubAddr, stubStop, signJWT := StartJWKSStub()

	xdgDir, err := os.MkdirTemp("", "sharkfin-e2e-*")
	if err != nil {
		stubStop()
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	args := []string{
		"daemon",
		"--daemon", addr,
		"--log-level", "disabled",
		"--passport-url", "http://" + stubAddr,
	}
	if cfg.webhookURL != "" {
		args = append(args, "--webhook-url", cfg.webhookURL)
	}
	// Explicit --db from WithDB takes priority over SHARKFIN_DB env.
	if cfg.dbDSN != "" {
		args = append(args, "--db", cfg.dbDSN)
	} else if dbDSN := os.Getenv("SHARKFIN_DB"); dbDSN != "" {
		args = append(args, "--db", dbDSN)
		if strings.HasPrefix(dbDSN, "postgres://") || strings.HasPrefix(dbDSN, "postgresql://") {
			if err := resetPostgres(dbDSN); err != nil {
				os.RemoveAll(xdgDir)
				stubStop()
				return nil, fmt.Errorf("reset postgres: %w", err)
			}
		}
	}

	var stderrBuf bytes.Buffer

	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+xdgDir+"/config",
		"XDG_STATE_HOME="+xdgDir+"/state",
		fmt.Sprintf("SHARKFIN_PRESENCE_TIMEOUT=%s", cfg.presenceTimeout),
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	if err := cmd.Start(); err != nil {
		os.RemoveAll(xdgDir)
		stubStop()
		return nil, fmt.Errorf("start daemon: %w", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return &Daemon{
				cmd:      cmd,
				addr:     addr,
				xdgDir:   xdgDir,
				stderr:   &stderrBuf,
				stubStop: stubStop,
				signJWT:  signJWT,
			}, nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	cmd.Process.Kill()
	cmd.Wait()
	os.RemoveAll(xdgDir)
	stubStop()
	return nil, fmt.Errorf("daemon did not become ready on %s", addr)
}

func (d *Daemon) Addr() string   { return d.addr }
func (d *Daemon) XDGDir() string { return d.xdgDir }

// SignJWT creates a signed JWT with the given identity claims.
// The token is valid for 1 hour and signed with the JWKS stub's private key.
func (d *Daemon) SignJWT(id, username, displayName, userType string) string {
	return d.signJWT(id, username, displayName, userType)
}

// DBPath returns the path to the daemon's SQLite database file.
// Only valid when SHARKFIN_DB is not set (i.e., using default SQLite).
func (d *Daemon) DBPath() string {
	return filepath.Join(d.xdgDir, "state", "sharkfin", "sharkfin.db")
}

// StopNoClean stops the daemon and JWKS stub without removing the xdg directory.
// Use Cleanup() later to remove it.
func (d *Daemon) StopNoClean(t testing.TB) {
	t.Helper()
	if d.stubStop != nil {
		d.stubStop()
	}
	if d.cmd.Process == nil {
		return
	}
	d.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- d.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		d.cmd.Process.Kill()
		<-done
		t.Log("daemon did not exit after SIGTERM, killed")
	}
	if d.stderr != nil && strings.Contains(d.stderr.String(), "DATA RACE") {
		t.Fatal("data race detected in daemon (see stderr output above)")
	}
}

// Cleanup removes the daemon's xdg directory.
func (d *Daemon) Cleanup() {
	os.RemoveAll(d.xdgDir)
}

// GrantAdmin promotes a user to admin role using the admin CLI.
func (d *Daemon) GrantAdmin(binary, username string) error {
	args := []string{"admin", "set-role", username, "admin"}
	// Forward SHARKFIN_DB if set (e.g., for Postgres e2e).
	if dbDSN := os.Getenv("SHARKFIN_DB"); dbDSN != "" {
		args = append(args, "--db", dbDSN)
	}
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+d.xdgDir+"/config",
		"XDG_STATE_HOME="+d.xdgDir+"/state",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("grant admin to %s: %w (output: %s)", username, err, out)
	}
	return nil
}

// StopFatal stops the daemon and fails the test if a data race was detected.
func (d *Daemon) StopFatal(t testing.TB) {
	t.Helper()
	if err := d.Stop(); err != nil {
		t.Logf("daemon stop: %v", err)
	}
	if d.stderr != nil && strings.Contains(d.stderr.String(), "DATA RACE") {
		t.Fatal("data race detected in daemon (see stderr output above)")
	}
}

func (d *Daemon) Stop() error {
	if d.stubStop != nil {
		d.stubStop()
	}
	if d.cmd.Process == nil {
		return nil
	}
	d.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- d.cmd.Wait() }()
	select {
	case err := <-done:
		os.RemoveAll(d.xdgDir)
		return err
	case <-time.After(5 * time.Second):
		d.cmd.Process.Kill()
		<-done
		os.RemoveAll(d.xdgDir)
		return fmt.Errorf("daemon did not exit after SIGTERM")
	}
}

// --- Client ---

type ToolResult struct {
	Text  string
	Error *RPCError
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Client struct {
	addr      string
	sessionID string
	authToken string // JWT for this client
	nextID    int
	mu        sync.Mutex
}

func NewClient(daemonAddr string, authToken string) *Client {
	return &Client{addr: daemonAddr, authToken: authToken, nextID: 1}
}

func (c *Client) SessionID() string { return c.sessionID }

func (c *Client) allocID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	return id
}

func (c *Client) RawMCPRequest(method string, id int, params any) (json.RawMessage, *RPCError, http.Header, error) {
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"id":      id,
	}
	if params != nil {
		reqBody["params"] = params
	}

	body, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("http://%s/mcp", c.addr)
	httpReq, err := http.NewRequest("POST", url, strings.NewReader(string(body)))
	if err != nil {
		return nil, nil, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.authToken)
	if c.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", c.sessionID)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.sessionID = sid
	}

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, nil, resp.Header, fmt.Errorf("decode response: %w", err)
	}

	return rpcResp.Result, rpcResp.Error, resp.Header, nil
}

func (c *Client) RawPost(path, body string) (int, []byte, error) {
	url := fmt.Sprintf("http://%s%s", c.addr, path)
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody, nil
}

func (c *Client) RawGet(path string) (int, error) {
	url := fmt.Sprintf("http://%s%s", c.addr, path)
	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

func (c *Client) Initialize() error {
	id := c.allocID()
	_, rpcErr, _, err := c.RawMCPRequest("initialize", id, map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "e2e-test", "version": "0.1"},
	})
	if err != nil {
		return err
	}
	if rpcErr != nil {
		return fmt.Errorf("initialize error: %s", rpcErr.Message)
	}
	return nil
}

func (c *Client) ToolCall(name string, args any) (ToolResult, error) {
	id := c.allocID()
	result, rpcErr, _, err := c.RawMCPRequest("tools/call", id, map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return ToolResult{}, err
	}
	if rpcErr != nil {
		return ToolResult{Error: rpcErr}, nil
	}

	var parsed struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return ToolResult{}, fmt.Errorf("unmarshal tool result: %w", err)
	}
	if len(parsed.Content) == 0 {
		return ToolResult{}, nil
	}
	// Map mcp-go tool errors (isError: true) to the Error field so callers
	// can check r.Error != nil uniformly.
	if parsed.IsError {
		return ToolResult{Error: &RPCError{Code: -1, Message: parsed.Content[0].Text}}, nil
	}
	return ToolResult{Text: parsed.Content[0].Text}, nil
}

// Capabilities calls the capabilities MCP tool and returns the result.
func (c *Client) Capabilities() (ToolResult, error) {
	return c.ToolCall("capabilities", map[string]any{})
}

// SetState calls the set_state MCP tool and returns the result.
func (c *Client) SetState(state string) (ToolResult, error) {
	return c.ToolCall("set_state", map[string]any{"state": state})
}

// SetRole calls the set_role MCP tool and returns the result.
func (c *Client) SetRole(username, role string) (ToolResult, error) {
	return c.ToolCall("set_role", map[string]any{"username": username, "role": role})
}

// --- Bridge ---

type Bridge struct {
	cmd    *exec.Cmd
	stdin  *json.Encoder
	stdout *bufio.Scanner
}

func StartBridge(binary, daemonAddr, xdgDir, apiKey string) (*Bridge, error) {
	cmd := exec.Command(binary,
		"mcp-bridge",
		"--daemon", daemonAddr,
		"--api-key", apiKey,
		"--log-level", "disabled",
	)
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+xdgDir+"/config",
		"XDG_STATE_HOME="+xdgDir+"/state",
	)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start bridge: %w", err)
	}

	time.Sleep(500 * time.Millisecond)

	return &Bridge{
		cmd:    cmd,
		stdin:  json.NewEncoder(stdinPipe),
		stdout: bufio.NewScanner(stdoutPipe),
	}, nil
}

// SendNotification sends a JSON-RPC notification (no id) to the bridge.
// Notifications produce no stdout output (server returns 202).
func (b *Bridge) SendNotification(request any) error {
	return b.stdin.Encode(request)
}

func (b *Bridge) Send(request any) (json.RawMessage, error) {
	if err := b.stdin.Encode(request); err != nil {
		return nil, fmt.Errorf("write to bridge: %w", err)
	}
	if !b.stdout.Scan() {
		if err := b.stdout.Err(); err != nil {
			return nil, fmt.Errorf("read from bridge: %w", err)
		}
		return nil, fmt.Errorf("bridge closed stdout")
	}
	return json.RawMessage(b.stdout.Bytes()), nil
}

func (b *Bridge) Stop() error {
	if b.cmd.Process == nil {
		return nil
	}
	b.cmd.Process.Signal(syscall.SIGTERM)
	return b.cmd.Wait()
}

func (b *Bridge) Kill() error {
	if b.cmd.Process == nil {
		return nil
	}
	return b.cmd.Process.Kill()
}

// --- WSClient ---

// WSEnvelope is the JSON envelope for WebSocket messages.
type WSEnvelope struct {
	Type string          `json:"type"`
	D    json.RawMessage `json:"d,omitempty"`
	Ref  string          `json:"ref,omitempty"`
	OK   *bool           `json:"ok,omitempty"`
}

// WSClient connects to the /ws endpoint for WebSocket-based chat.
type WSClient struct {
	conn *websocket.Conn
}

// NewWSClient dials the daemon's /ws endpoint with JWT auth.
// The connection is authenticated at upgrade time — no hello handshake.
func NewWSClient(daemonAddr string, authToken string) (*WSClient, error) {
	url := fmt.Sprintf("ws://%s/ws", daemonAddr)
	header := http.Header{}
	header.Set("Authorization", "Bearer "+authToken)
	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		return nil, fmt.Errorf("dial ws: %w", err)
	}

	return &WSClient{conn: conn}, nil
}

// Close cleanly closes the WebSocket connection.
func (w *WSClient) Close() {
	w.conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	w.conn.Close()
}

// Send writes a request envelope to the WebSocket.
func (w *WSClient) Send(typ string, d any, ref string) error {
	raw, _ := json.Marshal(d)
	env := map[string]any{"type": typ, "d": json.RawMessage(raw), "ref": ref}
	data, _ := json.Marshal(env)
	return w.conn.WriteMessage(websocket.TextMessage, data)
}

// Read reads a single envelope from the WebSocket with a 2s deadline.
func (w *WSClient) Read() (WSEnvelope, error) {
	w.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := w.conn.ReadMessage()
	if err != nil {
		return WSEnvelope{}, err
	}
	var env WSEnvelope
	if err := json.Unmarshal(msg, &env); err != nil {
		return WSEnvelope{}, fmt.Errorf("unmarshal: %w (body: %s)", err, string(msg))
	}
	return env, nil
}

// Req sends a request and reads the response matching the given ref.
// Broadcasts (messages with no ref) are discarded.
func (w *WSClient) Req(typ string, d any, ref string) (WSEnvelope, error) {
	if err := w.Send(typ, d, ref); err != nil {
		return WSEnvelope{}, err
	}
	for {
		env, err := w.Read()
		if err != nil {
			return WSEnvelope{}, err
		}
		if env.Ref == ref {
			return env, nil
		}
	}
}

// SetState sends a set_state message via WS and returns the response.
func (w *WSClient) SetState(state string) (WSEnvelope, error) {
	return w.Req("set_state", map[string]string{"state": state}, "ss")
}

// Capabilities sends a capabilities request via WS and returns the response.
func (w *WSClient) Capabilities() (WSEnvelope, error) {
	return w.Req("capabilities", nil, "cap")
}

// ReadWithTimeout reads a single envelope with a custom deadline.
func (w *WSClient) ReadWithTimeout(d time.Duration) (WSEnvelope, error) {
	w.conn.SetReadDeadline(time.Now().Add(d))
	_, msg, err := w.conn.ReadMessage()
	if err != nil {
		return WSEnvelope{}, err
	}
	var env WSEnvelope
	if err := json.Unmarshal(msg, &env); err != nil {
		return WSEnvelope{}, fmt.Errorf("unmarshal: %w (body: %s)", err, string(msg))
	}
	return env, nil
}

// --- PresenceClient ---

// PresenceNotification is the JSON envelope sent over the presence WebSocket.
type PresenceNotification struct {
	Type string          `json:"type"`
	D    json.RawMessage `json:"d"`
}

// PresenceClient connects to /presence and buffers incoming notifications.
// Notifications accumulate so that tests can read and assert on them.
type PresenceClient struct {
	conn    *websocket.Conn
	notifCh chan PresenceNotification
	done    chan struct{}
}

// NewPresenceClient dials the /presence WebSocket with JWT auth and starts a
// goroutine that buffers incoming notifications. The connection is authenticated
// at upgrade time — no token to read.
func NewPresenceClient(daemonAddr string, authToken string) (*PresenceClient, error) {
	wsURL := fmt.Sprintf("ws://%s/presence", daemonAddr)
	header := http.Header{}
	header.Set("Authorization", "Bearer "+authToken)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		return nil, fmt.Errorf("dial presence: %w", err)
	}

	pc := &PresenceClient{
		conn:    conn,
		notifCh: make(chan PresenceNotification, 64),
		done:    make(chan struct{}),
	}

	go pc.readLoop()
	return pc, nil
}

func (pc *PresenceClient) readLoop() {
	defer close(pc.done)
	for {
		pc.conn.SetReadDeadline(time.Time{}) // no deadline — block until message or close
		_, msg, err := pc.conn.ReadMessage()
		if err != nil {
			return
		}
		var notif PresenceNotification
		if err := json.Unmarshal(msg, &notif); err != nil {
			continue
		}
		pc.notifCh <- notif
	}
}

// ReadNotification reads a single notification with a timeout.
func (pc *PresenceClient) ReadNotification(timeout time.Duration) (PresenceNotification, error) {
	select {
	case n := <-pc.notifCh:
		return n, nil
	case <-time.After(timeout):
		return PresenceNotification{}, fmt.Errorf("timeout waiting for presence notification")
	}
}

// NoNotification asserts that no notification arrives within the given duration.
// Returns nil if no notification was received (the expected case), or an error
// containing the unexpected notification.
func (pc *PresenceClient) NoNotification(wait time.Duration) error {
	select {
	case n := <-pc.notifCh:
		return fmt.Errorf("unexpected notification: type=%s d=%s", n.Type, string(n.D))
	case <-time.After(wait):
		return nil
	}
}

// Close cleanly closes the presence WebSocket connection.
func (pc *PresenceClient) Close() {
	pc.conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	pc.conn.Close()
	<-pc.done
}

// --- Postgres ---

// resetPostgres drops and recreates the public schema so each test
// gets a fresh database. Goose migrations re-run on daemon startup.
func resetPostgres(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer db.Close()
	if _, err := db.Exec("DROP SCHEMA public CASCADE"); err != nil {
		return fmt.Errorf("drop schema: %w", err)
	}
	if _, err := db.Exec("CREATE SCHEMA public"); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	return nil
}

// --- Helpers ---

func FreePort() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr, nil
}
