// SPDX-License-Identifier: GPL-2.0-only
package presence

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// NewPresenceCmd creates the presence subcommand.
func NewPresenceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "presence",
		Short: "Establish presence connection to the daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			addr := viper.GetString("daemon")
			wsURL := fmt.Sprintf("ws://%s/presence", addr)

			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				return fmt.Errorf("connect: %w", err)
			}
			defer conn.Close()

			// Read token (first message from server)
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return fmt.Errorf("read token: %w", err)
			}
			fmt.Println(string(msg))

			// Read loop: processes server pings (gorilla auto-responds with pong).
			// Exits when the server closes the connection.
			connClosed := make(chan struct{})
			go func() {
				defer close(connClosed)
				for {
					if _, _, err := conn.ReadMessage(); err != nil {
						return
					}
				}
			}()

			// Hold connection open until signal or server disconnect
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			select {
			case <-sigCh:
			case <-connClosed:
				return nil
			}

			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return nil
		},
	}
}
