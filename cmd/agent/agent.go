// SPDX-License-Identifier: AGPL-3.0-or-later
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"

	"github.com/charmbracelet/log"
	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// wsEnvelope mirrors the daemon's wire format.
type wsEnvelope struct {
	Type string          `json:"type"`
	D    json.RawMessage `json:"d,omitempty"`
	Ref  string          `json:"ref,omitempty"`
	OK   *bool           `json:"ok,omitempty"`
}

// NewAgentCmd creates the agent subcommand.
func NewAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Agent sidecar that executes a command on new messages",
		Args:  cobra.NoArgs,
		RunE:  runAgent,
	}

	cmd.Flags().String("username", "", "Agent username for identification")
	cmd.Flags().String("exec", "", "Command to execute on new messages (run via sh -c)")

	_ = viper.BindPFlag("agent.username", cmd.Flags().Lookup("username"))
	_ = viper.BindPFlag("agent.exec", cmd.Flags().Lookup("exec"))

	return cmd
}

func runAgent(cmd *cobra.Command, args []string) error {
	username := viper.GetString("agent.username")
	execCmd := viper.GetString("agent.exec")

	if username == "" {
		return fmt.Errorf("--username is required (or set agent.username in config)")
	}
	if execCmd == "" {
		return fmt.Errorf("--exec is required (or set agent.exec in config)")
	}

	addr := viper.GetString("daemon")
	wsURL := fmt.Sprintf("ws://%s/ws", addr)

	log.Info("agent: connecting", "addr", addr, "username", username)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	// Step 1: Read and discard the hello message.
	if _, _, err := conn.ReadMessage(); err != nil {
		return fmt.Errorf("read hello: %w", err)
	}
	log.Debug("agent: received hello")

	// Step 2: Send identify with notifications_only.
	if err := sendWS(conn, "identify", map[string]interface{}{
		"username":           username,
		"notifications_only": true,
	}, "identify"); err != nil {
		return fmt.Errorf("send identify: %w", err)
	}

	// Step 3: Read identify response.
	identReply, err := readReply(conn)
	if err != nil {
		return fmt.Errorf("read identify reply: %w", err)
	}
	if identReply.OK == nil || !*identReply.OK {
		return fmt.Errorf("identify failed: %s", string(identReply.D))
	}
	log.Info("agent: identified", "username", username)

	// Step 4: Set initial state to idle.
	if err := sendWS(conn, "set_state", map[string]interface{}{
		"state": "idle",
	}, "init-state"); err != nil {
		return fmt.Errorf("send set_state: %w", err)
	}
	if reply, err := readReply(conn); err != nil {
		return fmt.Errorf("read set_state reply: %w", err)
	} else if reply.OK == nil || !*reply.OK {
		return fmt.Errorf("set_state failed: %s", string(reply.D))
	}
	log.Info("agent: state set to idle, waiting for notifications")

	// Step 5: Start read goroutine that feeds messages into a channel.
	msgCh := make(chan wsEnvelope, 64)
	readErr := make(chan error, 1)
	var stopping atomic.Bool

	go func() {
		defer close(msgCh)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				if !stopping.Load() {
					readErr <- fmt.Errorf("read: %w", err)
				}
				return
			}
			var env wsEnvelope
			if err := json.Unmarshal(raw, &env); err != nil {
				log.Warn("agent: invalid JSON from server", "err", err)
				continue
			}
			msgCh <- env
		}
	}()

	// Step 6: Signal handling.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Step 7: Main loop.
	for {
		select {
		case <-sigCh:
			log.Info("agent: shutting down")
			stopping.Store(true)
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return nil

		case err := <-readErr:
			return err

		case env, ok := <-msgCh:
			if !ok {
				return fmt.Errorf("connection closed")
			}
			if env.Type != "message.new" {
				log.Debug("agent: ignoring message", "type", env.Type)
				continue
			}

			log.Info("agent: notification received, executing command")
			if err := executeLoop(conn, execCmd, msgCh); err != nil {
				log.Error("agent: execute loop error", "err", err)
			}
		}
	}
}

// executeLoop runs the configured command and re-runs if unreads remain.
// It sets state to active before execution and back to idle when done.
func executeLoop(conn *websocket.Conn, command string, msgCh <-chan wsEnvelope) error {
	// Set state to active.
	if err := sendWS(conn, "set_state", map[string]interface{}{
		"state": "active",
	}, "exec-active"); err != nil {
		return fmt.Errorf("send set_state active: %w", err)
	}
	// Drain the set_state reply from msgCh.
	drainReply(msgCh, "exec-active")

	for {
		log.Info("agent: running command", "cmd", command)
		c := exec.Command("sh", "-c", command)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			log.Warn("agent: command exited with error", "err", err)
		}

		// Check for remaining unreads.
		if err := sendWS(conn, "unread_counts", nil, "check-unreads"); err != nil {
			return fmt.Errorf("send unread_counts: %w", err)
		}

		reply := drainReply(msgCh, "check-unreads")
		if reply == nil || reply.OK == nil || !*reply.OK {
			log.Debug("agent: unread_counts failed, returning to idle")
			break
		}

		if hasUnreads(reply.D) {
			log.Info("agent: unreads remain, re-executing command")
			continue
		}
		break
	}

	// Set state back to idle.
	if err := sendWS(conn, "set_state", map[string]interface{}{
		"state": "idle",
	}, "exec-idle"); err != nil {
		return fmt.Errorf("send set_state idle: %w", err)
	}
	drainReply(msgCh, "exec-idle")

	log.Info("agent: returning to idle")
	return nil
}

// sendWS marshals and sends a typed WS message.
func sendWS(conn *websocket.Conn, msgType string, d interface{}, ref string) error {
	env := struct {
		Type string      `json:"type"`
		D    interface{} `json:"d,omitempty"`
		Ref  string      `json:"ref,omitempty"`
	}{
		Type: msgType,
		D:    d,
		Ref:  ref,
	}
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

// readReply reads the next WS message directly from the connection.
// Used during the handshake phase before the read goroutine starts.
func readReply(conn *websocket.Conn) (*wsEnvelope, error) {
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	var env wsEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("unmarshal reply: %w", err)
	}
	return &env, nil
}

// drainReply reads from msgCh until it finds a reply matching the given ref.
// Non-matching messages are logged and discarded.
func drainReply(msgCh <-chan wsEnvelope, ref string) *wsEnvelope {
	for env := range msgCh {
		if (env.Type == "reply" || env.Type == "error") && env.Ref == ref {
			return &env
		}
		log.Debug("agent: draining non-matching message", "type", env.Type, "ref", env.Ref)
	}
	return nil
}

// hasUnreads checks whether the unread_counts response contains any channels with unreads.
func hasUnreads(raw json.RawMessage) bool {
	var d struct {
		Counts []struct {
			UnreadCount int `json:"unread_count"`
		} `json:"counts"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return false
	}
	for _, c := range d.Counts {
		if c.UnreadCount > 0 {
			return true
		}
	}
	return false
}
