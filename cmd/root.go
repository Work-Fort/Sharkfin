// SPDX-License-Identifier: GPL-2.0-only
package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Work-Fort/sharkfin/cmd/daemon"
	"github.com/Work-Fort/sharkfin/cmd/mcpbridge"
	"github.com/Work-Fort/sharkfin/cmd/presence"
	"github.com/Work-Fort/sharkfin/pkg/config"
)

// Version is set at build time via ldflags.
var Version string

var rootCmd = &cobra.Command{
	Use:   "sharkfin",
	Short: "LLM IPC tool for coding agent collaboration",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := config.InitDirs(); err != nil {
			return err
		}
		if err := config.LoadConfig(); err != nil {
			return err
		}

		ll := viper.GetString("log-level")
		if ll == "disabled" {
			log.SetOutput(io.Discard)
			return nil
		}

		var level log.Level
		switch ll {
		case "debug":
			level = log.DebugLevel
		case "info":
			level = log.InfoLevel
		case "warn":
			level = log.WarnLevel
		case "error":
			level = log.ErrorLevel
		default:
			level = log.DebugLevel
		}

		logFile := filepath.Join(config.GlobalPaths.StateDir, "debug.log")
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}

		logger := log.NewWithOptions(f, log.Options{
			ReportTimestamp: true,
			TimeFormat:      "2006-01-02T15:04:05.000Z07:00",
			Level:           level,
			ReportCaller:    true,
			Formatter:       log.JSONFormatter,
		})
		log.SetDefault(logger)

		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func init() {
	config.InitViper()

	rootCmd.PersistentFlags().String("daemon", config.DefaultDaemonAddr, "Daemon address")
	rootCmd.PersistentFlags().StringP("log-level", "l", "debug", "Log level: disabled, debug, info, warn, error")

	if err := config.BindFlags(rootCmd.PersistentFlags()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	rootCmd.AddCommand(daemon.NewDaemonCmd())
	rootCmd.AddCommand(mcpbridge.NewMCPBridgeCmd())
	rootCmd.AddCommand(presence.NewPresenceCmd())

	rootCmd.Version = Version
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
}
