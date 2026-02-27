// SPDX-License-Identifier: GPL-2.0-only
package harness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// --- Daemon ---

type daemonConfig struct {
	allowChannelCreation bool
	presenceTimeout      time.Duration
}

type DaemonOption func(*daemonConfig)

func WithAllowChannelCreation(allow bool) DaemonOption {
	return func(c *daemonConfig) { c.allowChannelCreation = allow }
}

func WithPresenceTimeout(d time.Duration) DaemonOption {
	return func(c *daemonConfig) { c.presenceTimeout = d }
}

type Daemon struct {
	cmd    *exec.Cmd
	addr   string
	xdgDir string
	stderr *bytes.Buffer
}

func StartDaemon(binary, addr string, opts ...DaemonOption) (*Daemon, error) {
	cfg := &daemonConfig{
		allowChannelCreation: true,
		presenceTimeout:      20 * time.Second,
	}
	for _, o := range opts {
		o(cfg)
	}

	xdgDir, err := os.MkdirTemp("", "sharkfin-e2e-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	args := []string{
		"daemon",
		"--daemon", addr,
		"--log-level", "disabled",
		fmt.Sprintf("--allow-channel-creation=%t", cfg.allowChannelCreation),
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
		return nil, fmt.Errorf("start daemon: %w", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return &Daemon{cmd: cmd, addr: addr, xdgDir: xdgDir, stderr: &stderrBuf}, nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	cmd.Process.Kill()
	cmd.Wait()
	os.RemoveAll(xdgDir)
	return nil, fmt.Errorf("daemon did not become ready on %s", addr)
}

func (d *Daemon) Addr() string   { return d.addr }
func (d *Daemon) XDGDir() string { return d.xdgDir }

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
	token     string
	wsConn    *websocket.Conn
	wsDone    chan struct{}
	mu        sync.Mutex
	nextID    int
}

func NewClient(daemonAddr string) *Client {
	return &Client{addr: daemonAddr, nextID: 1}
}

func (c *Client) ConnectPresence() error {
	wsURL := fmt.Sprintf("ws://%s/presence", c.addr)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial presence: %w", err)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("read token: %w", err)
	}
	c.token = string(msg)
	c.wsConn = conn
	c.wsDone = make(chan struct{})

	go func() {
		defer close(c.wsDone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	return nil
}

func (c *Client) DisconnectPresence() {
	if c.wsConn == nil {
		return
	}
	c.wsConn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	c.wsConn.Close()
	<-c.wsDone
	c.wsConn = nil
}

func (c *Client) Token() string     { return c.token }
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
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return ToolResult{}, fmt.Errorf("unmarshal tool result: %w", err)
	}
	if len(parsed.Content) == 0 {
		return ToolResult{}, nil
	}
	return ToolResult{Text: parsed.Content[0].Text}, nil
}

func (c *Client) Register(username, password string) error {
	r, err := c.ToolCall("register", map[string]any{
		"token": c.token, "username": username, "password": password,
	})
	if err != nil {
		return err
	}
	if r.Error != nil {
		return fmt.Errorf("register: %s", r.Error.Message)
	}
	return nil
}

func (c *Client) Identify(username, password string) error {
	r, err := c.ToolCall("identify", map[string]any{
		"token": c.token, "username": username, "password": password,
	})
	if err != nil {
		return err
	}
	if r.Error != nil {
		return fmt.Errorf("identify: %s", r.Error.Message)
	}
	return nil
}

func (c *Client) RegisterFlow(username string) error {
	if err := c.ConnectPresence(); err != nil {
		return err
	}
	if err := c.Initialize(); err != nil {
		return err
	}
	return c.Register(username, "")
}

func (c *Client) IdentifyFlow(username string) error {
	if err := c.ConnectPresence(); err != nil {
		return err
	}
	if err := c.Initialize(); err != nil {
		return err
	}
	return c.Identify(username, "")
}

// --- Bridge ---

type Bridge struct {
	cmd    *exec.Cmd
	stdin  *json.Encoder
	stdout *bufio.Scanner
}

func StartBridge(binary, daemonAddr, xdgDir string) (*Bridge, error) {
	cmd := exec.Command(binary,
		"mcp-bridge",
		"--daemon", daemonAddr,
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

// NewWSClient dials the daemon's /ws endpoint and reads the hello message.
func NewWSClient(daemonAddr string) (*WSClient, error) {
	url := fmt.Sprintf("ws://%s/ws", daemonAddr)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, fmt.Errorf("dial ws: %w", err)
	}

	// Read and discard hello
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = conn.ReadMessage()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read hello: %w", err)
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

// WSRegister registers a user on the WS connection.
func (w *WSClient) WSRegister(username string) error {
	env, err := w.Req("register", map[string]string{"username": username}, "reg")
	if err != nil {
		return err
	}
	if env.OK == nil || !*env.OK {
		return fmt.Errorf("ws register failed: %s", string(env.D))
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
