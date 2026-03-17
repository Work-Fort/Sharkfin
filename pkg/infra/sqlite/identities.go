// SPDX-License-Identifier: AGPL-3.0-or-later
package sqlite

import (
	"database/sql"
	"fmt"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

func (s *Store) UpsertIdentity(id, username, displayName, identityType, role string) error {
	if role == "" {
		role = "user"
	}
	_, err := s.db.Exec(`
		INSERT INTO identities (id, username, display_name, type, role)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			username = excluded.username,
			display_name = excluded.display_name,
			type = excluded.type
	`, id, username, displayName, identityType, role)
	if err != nil {
		return fmt.Errorf("upsert identity: %w", err)
	}
	return nil
}

func (s *Store) GetIdentityByID(id string) (*domain.Identity, error) {
	var i domain.Identity
	err := s.db.QueryRow(
		"SELECT id, username, display_name, type, role, created_at FROM identities WHERE id = ?", id,
	).Scan(&i.ID, &i.Username, &i.DisplayName, &i.Type, &i.Role, &i.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("identity not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
	}
	return &i, nil
}

func (s *Store) GetIdentityByUsername(username string) (*domain.Identity, error) {
	var i domain.Identity
	err := s.db.QueryRow(
		"SELECT id, username, display_name, type, role, created_at FROM identities WHERE username = ?", username,
	).Scan(&i.ID, &i.Username, &i.DisplayName, &i.Type, &i.Role, &i.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("identity not found: %s", username)
	}
	if err != nil {
		return nil, fmt.Errorf("get identity: %w", err)
	}
	return &i, nil
}

func (s *Store) ListIdentities() ([]domain.Identity, error) {
	rows, err := s.db.Query(
		"SELECT id, username, display_name, type, role, created_at FROM identities ORDER BY username",
	)
	if err != nil {
		return nil, fmt.Errorf("list identities: %w", err)
	}
	defer rows.Close()

	var identities []domain.Identity
	for rows.Next() {
		var i domain.Identity
		if err := rows.Scan(&i.ID, &i.Username, &i.DisplayName, &i.Type, &i.Role, &i.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan identity: %w", err)
		}
		identities = append(identities, i)
	}
	return identities, rows.Err()
}

// IsEmpty reports whether the identities table has no rows.
// Used by backup import to prevent importing into a non-empty database.
func (s *Store) IsEmpty() (bool, error) {
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM identities").Scan(&count); err != nil {
		return false, fmt.Errorf("is empty: %w", err)
	}
	return count == 0, nil
}

// validWipeTables is the allowlist of tables that WipeAll may truncate.
var validWipeTables = map[string]bool{
	"mention_group_members": true,
	"mention_groups":        true,
	"message_mentions":      true,
	"read_cursors":          true,
	"messages":              true,
	"channel_members":       true,
	"channels":              true,
	"settings":              true,
	"identities":            true,
}

// WipeAll deletes all user data, preserving schema and built-in RBAC seeds.
func (s *Store) WipeAll() error {
	tables := []string{
		"mention_group_members",
		"mention_groups",
		"message_mentions",
		"read_cursors",
		"messages",
		"channel_members",
		"channels",
		"settings",
		"identities",
	}
	for _, t := range tables {
		if !validWipeTables[t] {
			return fmt.Errorf("wipe rejected: %q is not an allowed table", t)
		}
		if _, err := s.db.Exec("DELETE FROM " + t); err != nil {
			return fmt.Errorf("wipe %s: %w", t, err)
		}
	}
	// Remove custom roles and their permissions; keep built-in RBAC seeds.
	if _, err := s.db.Exec("DELETE FROM role_permissions WHERE role NOT IN (SELECT name FROM roles WHERE built_in = 1)"); err != nil {
		return fmt.Errorf("wipe custom role_permissions: %w", err)
	}
	if _, err := s.db.Exec("DELETE FROM roles WHERE built_in = 0"); err != nil {
		return fmt.Errorf("wipe custom roles: %w", err)
	}
	return nil
}
