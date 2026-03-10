// SPDX-License-Identifier: AGPL-3.0-or-later
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
	"github.com/Work-Fort/sharkfin/pkg/domain"
	"github.com/Work-Fort/sharkfin/pkg/infra"
)

// NewDaemonCmd creates the daemon subcommand.
func NewDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Start the sharkfind daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			addr := viper.GetString("daemon")

			dsn := viper.GetString("db")
			if dsn == "" {
				dsn = filepath.Join(config.GlobalPaths.StateDir, "sharkfin.db")
			}
			store, err := infra.Open(dsn)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer store.Close()

			timeoutStr := viper.GetString("presence-timeout")
			pongTimeout, err := time.ParseDuration(timeoutStr)
			if err != nil {
				return fmt.Errorf("invalid presence-timeout %q: %w", timeoutStr, err)
			}

			webhookURL := viper.GetString("webhook-url")
			bus := domain.NewEventBus()
			srv, err := pkgdaemon.NewServer(addr, store, pongTimeout, webhookURL, bus)
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

	cmd.Flags().String("db", "", "Database DSN (postgres://... or path to SQLite file)")
	_ = viper.BindPFlag("db", cmd.Flags().Lookup("db"))

	cmd.Flags().String("webhook-url", "", "URL to POST webhook notifications to on mentions and DMs")
	_ = viper.BindPFlag("webhook-url", cmd.Flags().Lookup("webhook-url"))

	return cmd
}
