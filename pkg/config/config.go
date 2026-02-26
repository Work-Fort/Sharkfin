// SPDX-License-Identifier: GPL-2.0-only
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	EnvPrefix         = "SHARKFIN"
	ConfigFileName    = "config"
	ConfigType        = "yaml"
	DefaultDaemonAddr = "127.0.0.1:16000"
)

// Paths holds all XDG-compliant directory paths.
type Paths struct {
	ConfigDir string
	StateDir  string
}

var GlobalPaths *Paths

func init() {
	GlobalPaths = GetPaths()
}

// GetPaths returns XDG-compliant directory paths.
func GetPaths() *Paths {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to get home directory: %v\n", err)
			os.Exit(1)
		}
		configHome = filepath.Join(home, ".config")
	}

	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to get home directory: %v\n", err)
			os.Exit(1)
		}
		stateHome = filepath.Join(home, ".local", "state")
	}

	return &Paths{
		ConfigDir: filepath.Join(configHome, "sharkfin"),
		StateDir:  filepath.Join(stateHome, "sharkfin"),
	}
}

// InitDirs creates all necessary directories.
func InitDirs() error {
	dirs := []string{
		GlobalPaths.ConfigDir,
		GlobalPaths.StateDir,
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	return nil
}

// InitViper sets up viper defaults and config file search paths.
func InitViper() {
	viper.SetDefault("daemon", DefaultDaemonAddr)
	viper.SetDefault("log-level", "debug")
	viper.SetDefault("presence-timeout", "20s")

	viper.SetConfigName(ConfigFileName)
	viper.SetConfigType(ConfigType)
	viper.AddConfigPath(GlobalPaths.ConfigDir)

	viper.SetEnvPrefix(EnvPrefix)
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
}

// LoadConfig reads the config file if present.
func LoadConfig() error {
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return nil
		}
		return fmt.Errorf("failed to read config: %w", err)
	}
	return nil
}

// BindFlags binds cobra flags to viper.
func BindFlags(flags *pflag.FlagSet) error {
	flagsToBind := []string{"daemon", "log-level"}
	for _, name := range flagsToBind {
		if err := viper.BindPFlag(name, flags.Lookup(name)); err != nil {
			return fmt.Errorf("bind flag %s: %w", name, err)
		}
	}
	return nil
}
