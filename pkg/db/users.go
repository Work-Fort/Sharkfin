// SPDX-License-Identifier: GPL-2.0-only
package db

import (
	"database/sql"
	"fmt"
	"time"
)

// User represents a registered user.
type User struct {
	ID        int64
	Username  string
	Password  string
	Role      string
	Type      string
	CreatedAt time.Time
}

// CreateUser inserts a new user and returns its ID.
func (d *DB) CreateUser(username, password string) (int64, error) {
	res, err := d.db.Exec("INSERT INTO users (username, password) VALUES (?, ?)", username, password)
	if err != nil {
		return 0, fmt.Errorf("create user: %w", err)
	}
	return res.LastInsertId()
}

// GetUserByUsername returns a user by username.
func (d *DB) GetUserByUsername(username string) (*User, error) {
	var u User
	err := d.db.QueryRow(
		"SELECT id, username, password, role, type, created_at FROM users WHERE username = ?",
		username,
	).Scan(&u.ID, &u.Username, &u.Password, &u.Role, &u.Type, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found: %s", username)
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return &u, nil
}

// ListUsers returns all registered users.
func (d *DB) ListUsers() ([]User, error) {
	rows, err := d.db.Query("SELECT id, username, password, role, type, created_at FROM users ORDER BY username")
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Password, &u.Role, &u.Type, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
