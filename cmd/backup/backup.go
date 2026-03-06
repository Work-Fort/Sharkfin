// SPDX-License-Identifier: AGPL-3.0-or-later
package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	bk "github.com/Work-Fort/sharkfin/pkg/backup"
	"github.com/Work-Fort/sharkfin/pkg/config"
	"github.com/Work-Fort/sharkfin/pkg/infra"
)

func s3cfg() *bk.S3Config {
	return &bk.S3Config{
		Bucket:    viper.GetString("backup.s3-bucket"),
		Region:    viper.GetString("backup.s3-region"),
		Endpoint:  viper.GetString("backup.s3-endpoint"),
		AccessKey: viper.GetString("backup.s3-access-key"),
		SecretKey: viper.GetString("backup.s3-secret-key"),
	}
}

func getPassphrase(cmd *cobra.Command) (string, error) {
	p, _ := cmd.Flags().GetString("passphrase")
	if p != "" {
		return p, nil
	}
	p = os.Getenv("SHARKFIN_BACKUP_PASSPHRASE")
	if p != "" {
		return p, nil
	}
	return "", fmt.Errorf("passphrase required: use --passphrase flag or SHARKFIN_BACKUP_PASSPHRASE env var")
}

func openDSN() string {
	dsn := viper.GetString("db")
	if dsn == "" {
		dsn = filepath.Join(config.GlobalPaths.StateDir, "sharkfin.db")
	}
	return dsn
}

// NewBackupCmd creates the backup subcommand with export, import, and list.
func NewBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Export, import, and list encrypted S3 backups",
	}

	cmd.PersistentFlags().String("db", "", "Database DSN (postgres://... or path to SQLite file)")
	cmd.PersistentFlags().String("passphrase", "", "Encryption passphrase (or set SHARKFIN_BACKUP_PASSPHRASE)")
	cmd.PersistentFlags().String("s3-bucket", "", "S3 bucket name")
	cmd.PersistentFlags().String("s3-region", "", "S3 region")
	cmd.PersistentFlags().String("s3-endpoint", "", "S3 endpoint (for MinIO/R2/etc.)")
	cmd.PersistentFlags().String("s3-access-key", "", "S3 access key")
	cmd.PersistentFlags().String("s3-secret-key", "", "S3 secret key")

	_ = viper.BindPFlag("db", cmd.PersistentFlags().Lookup("db"))
	_ = viper.BindPFlag("backup.s3-bucket", cmd.PersistentFlags().Lookup("s3-bucket"))
	_ = viper.BindPFlag("backup.s3-region", cmd.PersistentFlags().Lookup("s3-region"))
	_ = viper.BindPFlag("backup.s3-endpoint", cmd.PersistentFlags().Lookup("s3-endpoint"))
	_ = viper.BindPFlag("backup.s3-access-key", cmd.PersistentFlags().Lookup("s3-access-key"))
	_ = viper.BindPFlag("backup.s3-secret-key", cmd.PersistentFlags().Lookup("s3-secret-key"))

	cmd.AddCommand(
		newExportCmd(),
		newImportCmd(),
		newListCmd(),
	)

	return cmd
}

func newExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export",
		Short: "Export config and database to an encrypted S3 backup",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			passphrase, err := getPassphrase(cmd)
			if err != nil {
				return err
			}

			cfg := s3cfg()
			if err := cfg.Validate(); err != nil {
				return err
			}

			dsn := openDSN()
			store, err := infra.Open(dsn)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer store.Close()

			data, err := bk.ExportData(store)
			if err != nil {
				return fmt.Errorf("export data: %w", err)
			}

			dataJSON, err := json.MarshalIndent(data, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal json: %w", err)
			}

			configPath := filepath.Join(config.GlobalPaths.ConfigDir, "config.yaml")
			configData, err := os.ReadFile(configPath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("read config: %w", err)
			}
			if configData == nil {
				configData = []byte{}
			}

			files := map[string][]byte{
				"data.json":   dataJSON,
				"config.yaml": configData,
			}
			packed, err := bk.Pack(files, passphrase)
			if err != nil {
				return fmt.Errorf("pack: %w", err)
			}

			key := fmt.Sprintf("sharkfin-backup-%s.tar.xz.age",
				time.Now().UTC().Format(time.RFC3339))
			ctx := context.Background()
			if err := cfg.Upload(ctx, key, packed); err != nil {
				return fmt.Errorf("upload: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Uploaded: %s (%s)\n", key, humanSize(int64(len(packed))))
			return nil
		},
	}
}

func newImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import <key>",
		Short: "Download and restore an encrypted S3 backup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			passphrase, err := getPassphrase(cmd)
			if err != nil {
				return err
			}
			force, _ := cmd.Flags().GetBool("force")
			restoreConfig, _ := cmd.Flags().GetBool("restore-config")

			cfg := s3cfg()
			if err := cfg.Validate(); err != nil {
				return err
			}

			ctx := context.Background()
			packed, err := cfg.Download(ctx, key)
			if err != nil {
				return fmt.Errorf("download: %w", err)
			}

			files, err := bk.Unpack(packed, passphrase)
			if err != nil {
				return fmt.Errorf("unpack: %w", err)
			}

			dataJSON, ok := files["data.json"]
			if !ok {
				return fmt.Errorf("backup archive missing data.json")
			}
			var data bk.Backup
			if err := json.Unmarshal(dataJSON, &data); err != nil {
				return fmt.Errorf("parse data.json: %w", err)
			}

			dsn := openDSN()
			store, err := infra.Open(dsn)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer store.Close()

			bs, ok := store.(bk.BackupStore)
			if !ok {
				return fmt.Errorf("store does not support backup import")
			}

			if err := bk.ImportData(bs, &data, force); err != nil {
				return fmt.Errorf("import: %w", err)
			}

			if restoreConfig {
				if configData, ok := files["config.yaml"]; ok && len(configData) > 0 {
					configPath := filepath.Join(config.GlobalPaths.ConfigDir, "config.yaml")
					if err := os.WriteFile(configPath, configData, 0644); err != nil {
						return fmt.Errorf("write config: %w", err)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "Restored config.yaml\n")
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Import complete: %d users, %d channels, %d messages\n",
				len(data.Users), len(data.Channels)+len(data.DMs), len(data.Messages))
			return nil
		},
	}

	cmd.Flags().Bool("force", false, "Overwrite non-empty database")
	cmd.Flags().Bool("restore-config", false, "Restore config.yaml from backup")

	return cmd
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List backups in the S3 bucket",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := s3cfg()
			if err := cfg.Validate(); err != nil {
				return err
			}

			ctx := context.Background()
			objects, err := cfg.List(ctx)
			if err != nil {
				return fmt.Errorf("list: %w", err)
			}

			if len(objects) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No backups found.")
				return nil
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%-60s  %10s  %s\n", "KEY", "SIZE", "MODIFIED")
			for _, obj := range objects {
				fmt.Fprintf(out, "%-60s  %10s  %s\n",
					obj.Key, humanSize(obj.Size), obj.LastModified.Format(time.RFC3339))
			}
			return nil
		},
	}
}

func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
