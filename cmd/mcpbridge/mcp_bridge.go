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

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// NewMCPBridgeCmd creates the mcp-bridge subcommand.
func NewMCPBridgeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp-bridge",
		Short: "MCP stdio to HTTP bridge",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			addr := viper.GetString("daemon")

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
			}

			if err := b.startPresence(ctx); err != nil {
				return fmt.Errorf("start presence: %w", err)
			}

			return b.processStdin()
		},
	}
}

type bridge struct {
	client    *http.Client
	mcpURL    string
	wsURL     string
	sessionID string
	token     string
}

func (b *bridge) startPresence(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.Dial(b.wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial presence: %w", err)
	}

	// Read token (first message from server)
	_, msg, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("read token: %w", err)
	}
	b.token = string(msg)

	// Close WebSocket when context is cancelled
	go func() {
		<-ctx.Done()
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		conn.Close()
	}()

	// Read loop: processes server pings (gorilla auto-responds with pong)
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	return nil
}

func (b *bridge) processStdin() error {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if b.interceptGetIdentityToken(line) {
			continue
		}

		req, err := http.NewRequest("POST", b.mcpURL, strings.NewReader(line))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
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

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("read response: %w", err)
		}

		os.Stdout.Write(bytes.TrimRight(body, "\n"))
		os.Stdout.Write([]byte("\n"))
	}
	return scanner.Err()
}

func (b *bridge) interceptGetIdentityToken(line string) bool {
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
		Name string `json:"name"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return false
	}
	if params.Name != "get_identity_token" {
		return false
	}

	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      req.ID,
		"result": map[string]interface{}{
			"content": []map[string]string{
				{"type": "text", "text": b.token},
			},
		},
	}
	data, _ := json.Marshal(resp)
	os.Stdout.Write(data)
	os.Stdout.Write([]byte("\n"))
	return true
}
