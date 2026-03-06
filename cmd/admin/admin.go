// SPDX-License-Identifier: GPL-2.0-only
package admin

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Work-Fort/sharkfin/pkg/config"
	"github.com/Work-Fort/sharkfin/pkg/domain"
	"github.com/Work-Fort/sharkfin/pkg/infra"
)

func openStore() (domain.Store, error) {
	dsn := viper.GetString("db")
	if dsn == "" {
		dsn = filepath.Join(config.GlobalPaths.StateDir, "sharkfin.db")
	}
	return infra.Open(dsn)
}

// NewAdminCmd creates the admin subcommand for direct DB role management.
func NewAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Manage roles and permissions directly via the database",
	}

	cmd.PersistentFlags().String("db", "", "Database DSN (postgres://... or path to SQLite file)")
	_ = viper.BindPFlag("db", cmd.PersistentFlags().Lookup("db"))

	cmd.AddCommand(
		newSetRoleCmd(),
		newCreateRoleCmd(),
		newDeleteRoleCmd(),
		newGrantCmd(),
		newRevokeCmd(),
		newListRolesCmd(),
	)

	return cmd
}

func newSetRoleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-role <username> <role>",
		Short: "Set a user's role",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()

			if err := s.SetUserRole(args[0], args[1]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Set role %q on user %q\n", args[1], args[0])
			return nil
		},
	}
}

func newCreateRoleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create-role <name>",
		Short: "Create a custom role",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()

			if err := s.CreateRole(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created role %q\n", args[0])
			return nil
		},
	}
}

func newDeleteRoleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete-role <name>",
		Short: "Delete a custom role",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()

			if err := s.DeleteRole(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted role %q\n", args[0])
			return nil
		},
	}
}

func newGrantCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "grant <role> <permission>",
		Short: "Grant a permission to a role",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()

			if err := s.GrantPermission(args[0], args[1]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Granted %q to role %q\n", args[1], args[0])
			return nil
		},
	}
}

func newRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <role> <permission>",
		Short: "Revoke a permission from a role",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()

			if err := s.RevokePermission(args[0], args[1]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Revoked %q from role %q\n", args[1], args[0])
			return nil
		},
	}
}

func newListRolesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-roles",
		Short: "List all roles and their permissions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()

			roles, err := s.ListRoles()
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			for _, r := range roles {
				label := r.Name
				if r.BuiltIn {
					label += " (built-in)"
				}

				perms, err := s.GetRolePermissions(r.Name)
				if err != nil {
					return err
				}

				if len(perms) == 0 {
					fmt.Fprintf(out, "%s: (no permissions)\n", label)
				} else {
					fmt.Fprintf(out, "%s: %s\n", label, strings.Join(perms, ", "))
				}
			}
			return nil
		},
	}
}
