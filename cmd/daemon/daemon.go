// SPDX-License-Identifier: GPL-2.0-only
package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Work-Fort/sharkfin/pkg/config"
	pkgdaemon "github.com/Work-Fort/sharkfin/pkg/daemon"
)

// NewDaemonCmd creates the daemon subcommand.
func NewDaemonCmd() *cobra.Command {
	var allowChannelCreation bool

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Start the sharkfind daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			addr := viper.GetString("daemon")
			dbPath := filepath.Join(config.GlobalPaths.StateDir, "sharkfin.db")

			timeoutStr := viper.GetString("presence-timeout")
			pongTimeout, err := time.ParseDuration(timeoutStr)
			if err != nil {
				return fmt.Errorf("invalid presence-timeout %q: %w", timeoutStr, err)
			}

			srv, err := pkgdaemon.NewServer(addr, dbPath, allowChannelCreation, pongTimeout)
			if err != nil {
				return fmt.Errorf("create server: %w", err)
			}

			// Handle shutdown signals
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

			errCh := make(chan error, 1)
			go func() {
				if err := srv.Start(); err != nil && err != http.ErrServerClosed {
					errCh <- err
				}
			}()

			select {
			case sig := <-sigCh:
				fmt.Fprintf(os.Stderr, "\nReceived %s, shutting down...\n", sig)
			case err := <-errCh:
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return srv.Shutdown(ctx)
		},
	}

	cmd.Flags().BoolVar(&allowChannelCreation, "allow-channel-creation", true, "Allow users to create channels")

	return cmd
}
