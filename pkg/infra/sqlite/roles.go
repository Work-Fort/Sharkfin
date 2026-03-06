// SPDX-License-Identifier: GPL-2.0-only
package sqlite

import (
	"fmt"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

// CreateRole inserts a new custom role.
func (s *Store) CreateRole(name string) error {
	_, err := s.db.Exec("INSERT INTO roles (name) VALUES (?)", name)
	if err != nil {
		return fmt.Errorf("create role: %w", err)
	}
	return nil
}

// DeleteRole removes a custom role. Built-in roles cannot be deleted.
func (s *Store) DeleteRole(name string) error {
	res, err := s.db.Exec("DELETE FROM roles WHERE name = ? AND built_in = FALSE", name)
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete role rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("role not found or is built-in: %s", name)
	}
	return nil
}

// ListRoles returns all roles ordered by name.
func (s *Store) ListRoles() ([]domain.Role, error) {
	rows, err := s.db.Query("SELECT name, built_in FROM roles ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()

	var roles []domain.Role
	for rows.Next() {
		var r domain.Role
		if err := rows.Scan(&r.Name, &r.BuiltIn); err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

// GrantPermission grants a permission to a role. If already granted, this is a no-op.
func (s *Store) GrantPermission(role, permission string) error {
	_, err := s.db.Exec(
		"INSERT OR IGNORE INTO role_permissions (role, permission) VALUES (?, ?)",
		role, permission,
	)
	if err != nil {
		return fmt.Errorf("grant permission: %w", err)
	}
	return nil
}

// RevokePermission removes a permission from a role.
func (s *Store) RevokePermission(role, permission string) error {
	_, err := s.db.Exec(
		"DELETE FROM role_permissions WHERE role = ? AND permission = ?",
		role, permission,
	)
	if err != nil {
		return fmt.Errorf("revoke permission: %w", err)
	}
	return nil
}

// GetRolePermissions returns all permissions granted to a role.
func (s *Store) GetRolePermissions(role string) ([]string, error) {
	rows, err := s.db.Query(
		"SELECT permission FROM role_permissions WHERE role = ? ORDER BY permission",
		role,
	)
	if err != nil {
		return nil, fmt.Errorf("get role permissions: %w", err)
	}
	defer rows.Close()

	var perms []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

// GetUserPermissions returns all permissions for a user based on their role.
func (s *Store) GetUserPermissions(username string) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT rp.permission
		FROM users u
		JOIN role_permissions rp ON u.role = rp.role
		WHERE u.username = ?
		ORDER BY rp.permission
	`, username)
	if err != nil {
		return nil, fmt.Errorf("get user permissions: %w", err)
	}
	defer rows.Close()

	var perms []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

// HasPermission reports whether a user has the given permission via their role.
func (s *Store) HasPermission(username, permission string) (bool, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM users u
		JOIN role_permissions rp ON u.role = rp.role
		WHERE u.username = ? AND rp.permission = ?
	`, username, permission).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("has permission: %w", err)
	}
	return count > 0, nil
}

// SetUserRole updates a user's role. Returns an error if the user does not exist.
func (s *Store) SetUserRole(username, role string) error {
	res, err := s.db.Exec("UPDATE users SET role = ? WHERE username = ?", role, username)
	if err != nil {
		return fmt.Errorf("set user role: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set user role rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("user not found: %s", username)
	}
	return nil
}

// SetUserType updates a user's type. Returns an error if the user does not exist.
func (s *Store) SetUserType(username, userType string) error {
	res, err := s.db.Exec("UPDATE users SET type = ? WHERE username = ?", userType, username)
	if err != nil {
		return fmt.Errorf("set user type: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set user type rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("user not found: %s", username)
	}
	return nil
}
