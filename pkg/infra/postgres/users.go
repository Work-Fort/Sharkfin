// SPDX-License-Identifier: GPL-2.0-only
package postgres

import (
	"database/sql"
	"fmt"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

// CreateUser inserts a new user and returns its ID.
func (s *Store) CreateUser(username, password string) (int64, error) {
	var id int64
	err := s.db.QueryRow(
		"INSERT INTO users (username, password) VALUES ($1, $2) RETURNING id",
		username, password,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create user: %w", err)
	}
	return id, nil
}

// GetUserByUsername returns a user by username.
func (s *Store) GetUserByUsername(username string) (*domain.User, error) {
	var u domain.User
	err := s.db.QueryRow(
		"SELECT id, username, password, role, type, created_at FROM users WHERE username = $1",
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
func (s *Store) ListUsers() ([]domain.User, error) {
	rows, err := s.db.Query("SELECT id, username, password, role, type, created_at FROM users ORDER BY username")
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []domain.User
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Password, &u.Role, &u.Type, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
