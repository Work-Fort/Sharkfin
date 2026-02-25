// SPDX-License-Identifier: GPL-2.0-only
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	// Version is set at build time via ldflags
	Version    string
	daemonAddr string
	logLevel   string
)

var rootCmd = &cobra.Command{
	Use:   "sharkfin",
	Short: "LLM IPC tool for coding agent collaboration",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

// GetDaemonAddr returns the configured daemon address.
func GetDaemonAddr() string {
	return daemonAddr
}

func init() {
	rootCmd.PersistentFlags().StringVar(&daemonAddr, "daemon", "127.0.0.1:16000", "Daemon address")
	rootCmd.PersistentFlags().StringVarP(&logLevel, "log-level", "l", "debug", "Log level: disabled, debug, info, warn, error")
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
}
