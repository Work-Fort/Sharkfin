// SPDX-License-Identifier: GPL-2.0-only
package mcpbridge

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// NewMCPBridgeCmd creates the mcp-bridge subcommand.
func NewMCPBridgeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp-bridge",
		Short: "MCP stdio to HTTP bridge",
		RunE: func(cmd *cobra.Command, args []string) error {
			addr := viper.GetString("daemon")
			mcpURL := fmt.Sprintf("http://%s/mcp", addr)

			client := &http.Client{}
			scanner := bufio.NewScanner(os.Stdin)
			var sessionID string

			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}

				req, err := http.NewRequest("POST", mcpURL, strings.NewReader(line))
				if err != nil {
					fmt.Fprintf(os.Stderr, "error creating request: %s\n", err)
					return err
				}
				req.Header.Set("Content-Type", "application/json")
				if sessionID != "" {
					req.Header.Set("Mcp-Session-Id", sessionID)
				}

				resp, err := client.Do(req)
				if err != nil {
					fmt.Fprintf(os.Stderr, "error connecting to sharkfind: %s\n", err)
					return err
				}

				// Capture session ID from first response that sets it
				if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
					sessionID = sid
				}

				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					fmt.Fprintf(os.Stderr, "error reading response: %s\n", err)
					return err
				}

				os.Stdout.Write(body)
				os.Stdout.Write([]byte("\n"))
			}

			return scanner.Err()
		},
	}
}
