// SPDX-License-Identifier: AGPL-3.0-or-later
package sqlite

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

func (s *Store) CreateMentionGroup(slug string, createdBy int64) (int64, error) {
	res, err := s.db.Exec("INSERT INTO mention_groups (slug, created_by) VALUES (?, ?)", slug, createdBy)
	if err != nil {
		return 0, fmt.Errorf("create mention group: %w", err)
	}
	return res.LastInsertId()
}

func (s *Store) DeleteMentionGroup(id int64) error {
	res, err := s.db.Exec("DELETE FROM mention_groups WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete mention group: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("mention group not found: %d", id)
	}
	return nil
}

func (s *Store) GetMentionGroup(slug string) (*domain.MentionGroup, error) {
	var g domain.MentionGroup
	err := s.db.QueryRow(
		"SELECT mg.id, mg.slug, u.username, mg.created_at FROM mention_groups mg JOIN users u ON mg.created_by = u.id WHERE mg.slug = ?",
		slug,
	).Scan(&g.ID, &g.Slug, &g.CreatedBy, &g.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("mention group not found: %s", slug)
	}
	if err != nil {
		return nil, fmt.Errorf("get mention group: %w", err)
	}

	members, err := s.GetMentionGroupMembers(g.ID)
	if err != nil {
		return nil, err
	}
	g.Members = members
	return &g, nil
}

func (s *Store) ListMentionGroups() ([]domain.MentionGroup, error) {
	rows, err := s.db.Query(
		"SELECT mg.id, mg.slug, u.username, mg.created_at FROM mention_groups mg JOIN users u ON mg.created_by = u.id ORDER BY mg.slug",
	)
	if err != nil {
		return nil, fmt.Errorf("list mention groups: %w", err)
	}
	defer rows.Close()

	// Collect all groups first to avoid nested queries on the single SQLite connection.
	var groups []domain.MentionGroup
	for rows.Next() {
		var g domain.MentionGroup
		if err := rows.Scan(&g.ID, &g.Slug, &g.CreatedBy, &g.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan mention group: %w", err)
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Now fetch members for each group (rows are closed).
	for i := range groups {
		members, err := s.GetMentionGroupMembers(groups[i].ID)
		if err != nil {
			return nil, err
		}
		groups[i].Members = members
	}
	return groups, nil
}

func (s *Store) AddMentionGroupMember(groupID, userID int64) error {
	_, err := s.db.Exec(
		"INSERT OR IGNORE INTO mention_group_members (group_id, user_id) VALUES (?, ?)",
		groupID, userID,
	)
	if err != nil {
		return fmt.Errorf("add mention group member: %w", err)
	}
	return nil
}

func (s *Store) RemoveMentionGroupMember(groupID, userID int64) error {
	_, err := s.db.Exec(
		"DELETE FROM mention_group_members WHERE group_id = ? AND user_id = ?",
		groupID, userID,
	)
	if err != nil {
		return fmt.Errorf("remove mention group member: %w", err)
	}
	return nil
}

func (s *Store) GetMentionGroupMembers(groupID int64) ([]string, error) {
	rows, err := s.db.Query(
		"SELECT u.username FROM mention_group_members mgm JOIN users u ON mgm.user_id = u.id WHERE mgm.group_id = ? ORDER BY u.username",
		groupID,
	)
	if err != nil {
		return nil, fmt.Errorf("get mention group members: %w", err)
	}
	defer rows.Close()

	var members []string
	for rows.Next() {
		var username string
		if err := rows.Scan(&username); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		members = append(members, username)
	}
	return members, rows.Err()
}

func (s *Store) ExpandMentionGroups(slugs []string) (map[string][]int64, error) {
	if len(slugs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(slugs))
	args := make([]interface{}, len(slugs))
	for i, slug := range slugs {
		placeholders[i] = "?"
		args[i] = slug
	}

	rows, err := s.db.Query(
		fmt.Sprintf(
			"SELECT mg.slug, mgm.user_id FROM mention_groups mg JOIN mention_group_members mgm ON mg.id = mgm.group_id WHERE mg.slug IN (%s)",
			strings.Join(placeholders, ","),
		),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("expand mention groups: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]int64)
	for rows.Next() {
		var slug string
		var userID int64
		if err := rows.Scan(&slug, &userID); err != nil {
			return nil, fmt.Errorf("scan expansion: %w", err)
		}
		result[slug] = append(result[slug], userID)
	}
	return result, rows.Err()
}
