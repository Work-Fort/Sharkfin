// SPDX-License-Identifier: GPL-2.0-only
package presence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// NewPresenceCmd creates the presence subcommand.
func NewPresenceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "presence <token>",
		Short: "Establish presence connection to the daemon",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			addr := viper.GetString("daemon")
			presenceURL := fmt.Sprintf("http://%s/presence", addr)
			token := args[0]

			body, _ := json.Marshal(map[string]string{"token": token})

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Cancel the request on SIGINT/SIGTERM
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				cancel()
			}()

			req, err := http.NewRequestWithContext(ctx, "POST", presenceURL, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				// Context cancellation from signal is expected
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("presence connection failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("presence rejected: %s", resp.Status)
			}

			return nil
		},
	}
}
