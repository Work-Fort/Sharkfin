// SPDX-License-Identifier: GPL-2.0-only
package db

import "fmt"

// Role represents an RBAC role.
type Role struct {
	Name    string
	BuiltIn bool
}

// CreateRole inserts a new custom role.
func (d *DB) CreateRole(name string) error {
	_, err := d.db.Exec("INSERT INTO roles (name) VALUES (?)", name)
	if err != nil {
		return fmt.Errorf("create role: %w", err)
	}
	return nil
}

// DeleteRole removes a custom role. Built-in roles cannot be deleted.
func (d *DB) DeleteRole(name string) error {
	res, err := d.db.Exec("DELETE FROM roles WHERE name = ? AND built_in = FALSE", name)
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
func (d *DB) ListRoles() ([]Role, error) {
	rows, err := d.db.Query("SELECT name, built_in FROM roles ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()

	var roles []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.Name, &r.BuiltIn); err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

// GrantPermission grants a permission to a role. If already granted, this is a no-op.
func (d *DB) GrantPermission(role, permission string) error {
	_, err := d.db.Exec(
		"INSERT OR IGNORE INTO role_permissions (role, permission) VALUES (?, ?)",
		role, permission,
	)
	if err != nil {
		return fmt.Errorf("grant permission: %w", err)
	}
	return nil
}

// RevokePermission removes a permission from a role.
func (d *DB) RevokePermission(role, permission string) error {
	_, err := d.db.Exec(
		"DELETE FROM role_permissions WHERE role = ? AND permission = ?",
		role, permission,
	)
	if err != nil {
		return fmt.Errorf("revoke permission: %w", err)
	}
	return nil
}

// GetRolePermissions returns all permissions granted to a role.
func (d *DB) GetRolePermissions(role string) ([]string, error) {
	rows, err := d.db.Query(
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
func (d *DB) GetUserPermissions(username string) ([]string, error) {
	rows, err := d.db.Query(`
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
func (d *DB) HasPermission(username, permission string) (bool, error) {
	var count int
	err := d.db.QueryRow(`
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
func (d *DB) SetUserRole(username, role string) error {
	res, err := d.db.Exec("UPDATE users SET role = ? WHERE username = ?", role, username)
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
func (d *DB) SetUserType(username, userType string) error {
	res, err := d.db.Exec("UPDATE users SET type = ? WHERE username = ?", userType, username)
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
