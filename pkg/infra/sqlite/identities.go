// SPDX-License-Identifier: AGPL-3.0-or-later
package sqlite

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"

	"github.com/Work-Fort/sharkfin/pkg/domain"
	"github.com/charmbracelet/log"
)

func (s *Store) UpsertIdentity(authID, username, displayName, identityType, role string) (*domain.Identity, error) {
	if role == "" {
		role = "user"
	}
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM identities").Scan(&count)
	if count == 0 {
		role = "admin"
	}

	// 1. Look up existing identity by auth_id.
	var existingID string
	err := s.db.QueryRow("SELECT id FROM identities WHERE auth_id = ?", authID).Scan(&existingID)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("upsert identity lookup by auth_id: %w", err)
	}

	// 2. If not found by auth_id, try by username.
	if err == sql.ErrNoRows {
		err = s.db.QueryRow("SELECT id FROM identities WHERE username = ?", username).Scan(&existingID)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("upsert identity lookup by username: %w", err)
		}
	}

	if existingID != "" {
		// Found existing identity — check if auth_id changed.
		var oldAuthID sql.NullString
		s.db.QueryRow("SELECT auth_id FROM identities WHERE id = ?", existingID).Scan(&oldAuthID)
		if oldAuthID.Valid && oldAuthID.String != authID {
			log.Warn("identity auth_id changed", "internal_id", existingID, "old_auth_id", oldAuthID.String, "new_auth_id", authID)
		}

		_, err = s.db.Exec(`
			UPDATE identities
			SET auth_id = ?, username = ?, display_name = ?, type = ?
			WHERE id = ?
		`, authID, username, displayName, identityType, existingID)
		if err != nil {
			return nil, fmt.Errorf("upsert identity update: %w", err)
		}
		return s.GetIdentityByID(existingID)
	}

	// Not found — generate a new internal UUID and INSERT.
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("upsert identity generate id: %w", err)
	}
	newID := hex.EncodeToString(buf)

	_, err = s.db.Exec(`
		INSERT INTO identities (id, auth_id, username, display_name, type, role)
		VALUES (?, ?, ?, ?, ?, ?)
	`, newID, authID, username, displayName, identityType, role)
	if err != nil {
		return nil, fmt.Errorf("upsert identity insert: %w", err)
	}
	return s.GetIdentityByID(newID)
}

func (s *Store) GetIdentityByID(id string) (*domain.Identity, error) {
	var i domain.Identity
	err := s.db.QueryRow(
		"SELECT id, auth_id, username, display_name, type, role, created_at FROM identities WHERE id = ?", id,
	).Scan(&i.ID, &i.AuthID, &i.Username, &i.DisplayName, &i.Type, &i.Role, &i.CreatedAt)
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
		"SELECT id, auth_id, username, display_name, type, role, created_at FROM identities WHERE username = ?", username,
	).Scan(&i.ID, &i.AuthID, &i.Username, &i.DisplayName, &i.Type, &i.Role, &i.CreatedAt)
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
		"SELECT id, auth_id, username, display_name, type, role, created_at FROM identities ORDER BY username",
	)
	if err != nil {
		return nil, fmt.Errorf("list identities: %w", err)
	}
	defer rows.Close()

	var identities []domain.Identity
	for rows.Next() {
		var i domain.Identity
		if err := rows.Scan(&i.ID, &i.AuthID, &i.Username, &i.DisplayName, &i.Type, &i.Role, &i.CreatedAt); err != nil {
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
