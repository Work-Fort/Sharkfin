// SPDX-License-Identifier: AGPL-3.0-or-later
package mcpbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// NewMCPBridgeCmd creates the mcp-bridge subcommand.
func NewMCPBridgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp-bridge",
		Short: "MCP stdio to HTTP bridge",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			addr := viper.GetString("daemon")

			apiKey := viper.GetString("api-key")
			if apiKey == "" {
				return fmt.Errorf("--api-key is required")
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				cancel()
			}()

			b := &bridge{
				client: &http.Client{},
				mcpURL: fmt.Sprintf("http://%s/mcp", addr),
				wsURL:  fmt.Sprintf("ws://%s/presence", addr),
				apiKey: apiKey,
			}

			if err := b.startPresence(ctx); err != nil {
				return fmt.Errorf("start presence: %w", err)
			}

			return b.processStdin()
		},
	}

	cmd.Flags().String("api-key", "", "API key for bridge authentication")
	_ = viper.BindPFlag("api-key", cmd.Flags().Lookup("api-key"))

	return cmd
}

type bridge struct {
	client        *http.Client
	mcpURL        string
	wsURL         string
	sessionID     string
	apiKey        string
	notifications chan json.RawMessage
}

func (b *bridge) startPresence(ctx context.Context) error {
	header := http.Header{}
	header.Set("Authorization", "ApiKey-v1 "+b.apiKey)
	conn, _, err := websocket.DefaultDialer.Dial(b.wsURL, header)
	if err != nil {
		return fmt.Errorf("dial presence: %w", err)
	}

	// Close WebSocket when context is cancelled
	go func() {
		<-ctx.Done()
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		conn.Close()
	}()

	b.notifications = make(chan json.RawMessage, 64)

	// Read loop: processes server pings and feeds notifications into the channel
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				close(b.notifications)
				return
			}
			if json.Valid(msg) {
				select {
				case b.notifications <- json.RawMessage(msg):
				default: // buffer full, drop
				}
			}
		}
	}()

	return nil
}

// readResponseBody reads the HTTP response body, handling JSON, SSE, and 202.
// Returns nil for 202 (notification accepted, no body).
// For SSE, returns each data: line as a separate message.
// For JSON, returns the body as a single-element slice.
func readResponseBody(resp *http.Response) ([][]byte, error) {
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusAccepted {
		return nil, nil
	}

	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/event-stream") {
		var messages [][]byte
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				messages = append(messages, []byte(strings.TrimPrefix(line, "data: ")))
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read SSE: %w", err)
		}
		return messages, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return [][]byte{body}, nil
}

func (b *bridge) processStdin() error {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if b.interceptWaitForMessages(line) {
			continue
		}

		req, err := http.NewRequest("POST", b.mcpURL, strings.NewReader(line))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "ApiKey-v1 "+b.apiKey)
		if b.sessionID != "" {
			req.Header.Set("Mcp-Session-Id", b.sessionID)
		}

		resp, err := b.client.Do(req)
		if err != nil {
			return fmt.Errorf("forward request: %w", err)
		}

		if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
			b.sessionID = sid
		}

		messages, err := readResponseBody(resp)
		if err != nil {
			return fmt.Errorf("read response: %w", err)
		}

		for _, msg := range messages {
			os.Stdout.Write(bytes.TrimRight(msg, "\n"))
			os.Stdout.Write([]byte("\n"))
		}
	}
	return scanner.Err()
}

func (b *bridge) interceptWaitForMessages(line string) bool {
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
		ID      json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		return false
	}
	if req.Method != "tools/call" {
		return false
	}

	var params struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return false
	}
	if params.Name != "wait_for_messages" {
		return false
	}

	timeout := 30.0
	if v, ok := params.Arguments["timeout"]; ok {
		if t, ok := v.(float64); ok && t > 0 {
			timeout = t
		}
	}

	// Check for existing unreads first.
	text, err := b.callUnreadMessages()
	if err != nil {
		b.respondToolError(req.ID, fmt.Sprintf("failed to fetch unreads: %v", err))
		return true
	}

	// If there are already unread messages, return immediately.
	if text != "null" && text != "[]" {
		b.respondToolResult(req.ID, text)
		return true
	}

	// Block until notification arrives or timeout.
	timer := time.NewTimer(time.Duration(timeout * float64(time.Second)))
	defer timer.Stop()

	select {
	case _, ok := <-b.notifications:
		if !ok {
			b.respondToolError(req.ID, "presence connection closed")
			return true
		}
		// Got a notification — fetch unreads again.
		text, err := b.callUnreadMessages()
		if err != nil {
			b.respondToolError(req.ID, fmt.Sprintf("failed to fetch unreads: %v", err))
			return true
		}
		b.respondToolResult(req.ID, text)
	case <-timer.C:
		b.respondToolResult(req.ID, `{"status":"timeout","messages":[]}`)
	}

	return true
}

// callUnreadMessages calls the unread_messages tool via HTTP and returns the text content.
func (b *bridge) callUnreadMessages() (string, error) {
	rpcReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "unread_messages",
		},
	}
	body, err := json.Marshal(rpcReq)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", b.mcpURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "ApiKey-v1 "+b.apiKey)
	if b.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", b.sessionID)
	}

	resp, err := b.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("forward request: %w", err)
	}
	messages, err := readResponseBody(resp)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if len(messages) == 0 {
		return "", fmt.Errorf("no response body")
	}

	// Use the last message (the actual response, skipping any intermediate notifications).
	respBody := messages[len(messages)-1]

	// Parse the JSON-RPC response to extract the text content.
	var rpcResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if len(rpcResp.Result.Content) > 0 {
		return rpcResp.Result.Content[0].Text, nil
	}
	return "[]", nil
}

func (b *bridge) respondToolResult(id json.RawMessage, text string) {
	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]interface{}{
			"content": []map[string]string{
				{"type": "text", "text": text},
			},
		},
	}
	data, _ := json.Marshal(resp)
	os.Stdout.Write(data)
	os.Stdout.Write([]byte("\n"))
}

func (b *bridge) respondToolError(id json.RawMessage, msg string) {
	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]interface{}{
			"isError": true,
			"content": []map[string]string{
				{"type": "text", "text": msg},
			},
		},
	}
	data, _ := json.Marshal(resp)
	os.Stdout.Write(data)
	os.Stdout.Write([]byte("\n"))
}
