// SPDX-License-Identifier: AGPL-3.0-or-later
package postgres

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

func (s *Store) CreateMentionGroup(slug string, createdByID string) (int64, error) {
	var id int64
	err := s.db.QueryRow(
		"INSERT INTO mention_groups (slug, created_by) VALUES ($1, $2) RETURNING id",
		slug, createdByID,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create mention group: %w", err)
	}
	return id, nil
}

func (s *Store) DeleteMentionGroup(id int64) error {
	res, err := s.db.Exec("DELETE FROM mention_groups WHERE id = $1", id)
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
		"SELECT mg.id, mg.slug, i.username, mg.created_at FROM mention_groups mg JOIN identities i ON mg.created_by = i.id WHERE mg.slug = $1",
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
		"SELECT mg.id, mg.slug, i.username, mg.created_at FROM mention_groups mg JOIN identities i ON mg.created_by = i.id ORDER BY mg.slug",
	)
	if err != nil {
		return nil, fmt.Errorf("list mention groups: %w", err)
	}
	defer rows.Close()

	// Collect all groups first, then close rows before fetching members.
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

func (s *Store) AddMentionGroupMember(groupID int64, identityID string) error {
	_, err := s.db.Exec(
		"INSERT INTO mention_group_members (group_id, identity_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		groupID, identityID,
	)
	if err != nil {
		return fmt.Errorf("add mention group member: %w", err)
	}
	return nil
}

func (s *Store) RemoveMentionGroupMember(groupID int64, identityID string) error {
	_, err := s.db.Exec(
		"DELETE FROM mention_group_members WHERE group_id = $1 AND identity_id = $2",
		groupID, identityID,
	)
	if err != nil {
		return fmt.Errorf("remove mention group member: %w", err)
	}
	return nil
}

func (s *Store) GetMentionGroupMembers(groupID int64) ([]string, error) {
	rows, err := s.db.Query(
		"SELECT i.username FROM mention_group_members mgm JOIN identities i ON mgm.identity_id = i.id WHERE mgm.group_id = $1 ORDER BY i.username",
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

func (s *Store) ExpandMentionGroups(slugs []string) (map[string][]string, error) {
	if len(slugs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(slugs))
	args := make([]interface{}, len(slugs))
	for i, slug := range slugs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = slug
	}

	rows, err := s.db.Query(
		fmt.Sprintf(
			"SELECT mg.slug, mgm.identity_id FROM mention_groups mg JOIN mention_group_members mgm ON mg.id = mgm.group_id WHERE mg.slug IN (%s)",
			strings.Join(placeholders, ","),
		),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("expand mention groups: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]string)
	for rows.Next() {
		var slug string
		var identityID string
		if err := rows.Scan(&slug, &identityID); err != nil {
			return nil, fmt.Errorf("scan expansion: %w", err)
		}
		result[slug] = append(result[slug], identityID)
	}
	return result, rows.Err()
}
