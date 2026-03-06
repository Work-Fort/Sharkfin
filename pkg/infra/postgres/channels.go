// SPDX-License-Identifier: GPL-2.0-only
package postgres

import (
	"database/sql"
	"fmt"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

// CreateChannel creates a channel with the given members in a transaction.
// channelType should be "channel" or "dm".
func (s *Store) CreateChannel(name string, public bool, memberIDs []int64, channelType string) (int64, error) {
	if channelType == "" {
		channelType = "channel"
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var chID int64
	err = tx.QueryRow(
		"INSERT INTO channels (name, public, type) VALUES ($1, $2, $3) RETURNING id",
		name, public, channelType,
	).Scan(&chID)
	if err != nil {
		return 0, fmt.Errorf("insert channel: %w", err)
	}

	for _, uid := range memberIDs {
		if _, err := tx.Exec(
			"INSERT INTO channel_members (channel_id, user_id) VALUES ($1, $2)",
			chID, uid,
		); err != nil {
			return 0, fmt.Errorf("insert member %d: %w", uid, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return chID, nil
}

// GetChannelByID returns a channel by its ID.
func (s *Store) GetChannelByID(id int64) (*domain.Channel, error) {
	var ch domain.Channel
	err := s.db.QueryRow(
		"SELECT id, name, public, type, created_at FROM channels WHERE id = $1",
		id,
	).Scan(&ch.ID, &ch.Name, &ch.Public, &ch.Type, &ch.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("channel not found: %d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}
	return &ch, nil
}

// GetChannelByName returns a channel by its name.
func (s *Store) GetChannelByName(name string) (*domain.Channel, error) {
	var ch domain.Channel
	err := s.db.QueryRow(
		"SELECT id, name, public, type, created_at FROM channels WHERE name = $1",
		name,
	).Scan(&ch.ID, &ch.Name, &ch.Public, &ch.Type, &ch.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("channel not found: %s", name)
	}
	if err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}
	return &ch, nil
}

// ListChannelsForUser returns non-DM channels visible to a user:
// all public channels plus private channels where the user is a member.
// Each result includes whether the user is a member.
func (s *Store) ListChannelsForUser(userID int64) ([]domain.ChannelWithMembership, error) {
	rows, err := s.db.Query(`
		SELECT DISTINCT c.id, c.name, c.public, c.type, c.created_at,
			CASE WHEN cm.user_id IS NOT NULL THEN TRUE ELSE FALSE END AS member
		FROM channels c
		LEFT JOIN channel_members cm ON c.id = cm.channel_id AND cm.user_id = $1
		WHERE c.type = 'channel' AND (c.public = TRUE OR cm.user_id IS NOT NULL)
		ORDER BY c.name
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer rows.Close()

	var channels []domain.ChannelWithMembership
	for rows.Next() {
		var ch domain.ChannelWithMembership
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Public, &ch.Type, &ch.CreatedAt, &ch.Member); err != nil {
			return nil, fmt.Errorf("scan channel: %w", err)
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

// ListAllChannelsWithMembership returns all non-DM channels with membership status for the given user.
func (s *Store) ListAllChannelsWithMembership(userID int64) ([]domain.ChannelWithMembership, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.name, c.public, c.type, c.created_at,
			CASE WHEN cm.user_id IS NOT NULL THEN TRUE ELSE FALSE END AS member
		FROM channels c
		LEFT JOIN channel_members cm ON c.id = cm.channel_id AND cm.user_id = $1
		WHERE c.type = 'channel'
		ORDER BY c.name
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list all channels: %w", err)
	}
	defer rows.Close()

	var channels []domain.ChannelWithMembership
	for rows.Next() {
		var ch domain.ChannelWithMembership
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Public, &ch.Type, &ch.CreatedAt, &ch.Member); err != nil {
			return nil, fmt.Errorf("scan channel: %w", err)
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

// AddChannelMember adds a user to a channel.
func (s *Store) AddChannelMember(channelID, userID int64) error {
	_, err := s.db.Exec(
		"INSERT INTO channel_members (channel_id, user_id) VALUES ($1, $2)",
		channelID, userID,
	)
	if err != nil {
		return fmt.Errorf("add member: %w", err)
	}
	return nil
}

// ChannelMemberUsernames returns the usernames of all members of a channel.
func (s *Store) ChannelMemberUsernames(channelID int64) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT u.username FROM channel_members cm
		JOIN users u ON cm.user_id = u.id
		WHERE cm.channel_id = $1
		ORDER BY u.username
	`, channelID)
	if err != nil {
		return nil, fmt.Errorf("channel member usernames: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan username: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// IsChannelMember returns true if the user is a member of the channel.
func (s *Store) IsChannelMember(channelID, userID int64) (bool, error) {
	var count int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM channel_members WHERE channel_id = $1 AND user_id = $2",
		channelID, userID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check membership: %w", err)
	}
	return count > 0, nil
}

// ListDMsForUser returns all DM channels the user is a member of,
// with the other participant's info.
func (s *Store) ListDMsForUser(userID int64) ([]domain.DMInfo, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.name, u.id, u.username
		FROM channels c
		JOIN channel_members cm1 ON c.id = cm1.channel_id AND cm1.user_id = $1
		JOIN channel_members cm2 ON c.id = cm2.channel_id AND cm2.user_id != $2
		JOIN users u ON cm2.user_id = u.id
		WHERE c.type = 'dm'
		ORDER BY u.username
	`, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("list dms: %w", err)
	}
	defer rows.Close()

	var dms []domain.DMInfo
	for rows.Next() {
		var dm domain.DMInfo
		if err := rows.Scan(&dm.ChannelID, &dm.ChannelName, &dm.OtherUserID, &dm.OtherUsername); err != nil {
			return nil, fmt.Errorf("scan dm: %w", err)
		}
		dms = append(dms, dm)
	}
	return dms, rows.Err()
}

// ListAllDMs returns all DM channels with both participants (admin view).
func (s *Store) ListAllDMs() ([]domain.AllDMInfo, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.name, u.id, u.username
		FROM channels c
		JOIN channel_members cm ON c.id = cm.channel_id
		JOIN users u ON cm.user_id = u.id
		WHERE c.type = 'dm'
		ORDER BY c.name, u.username
	`)
	if err != nil {
		return nil, fmt.Errorf("list all dms: %w", err)
	}
	defer rows.Close()

	// Build map preserving insertion order.
	type entry struct {
		info  domain.AllDMInfo
		index int
	}
	dmMap := make(map[string]*entry)
	var order []string
	for rows.Next() {
		var chID int64
		var chName string
		var userID int64
		var username string
		if err := rows.Scan(&chID, &chName, &userID, &username); err != nil {
			return nil, fmt.Errorf("scan dm: %w", err)
		}
		e, ok := dmMap[chName]
		if !ok {
			e = &entry{
				info: domain.AllDMInfo{
					ChannelID:   chID,
					ChannelName: chName,
				},
				index: len(order),
			}
			dmMap[chName] = e
			order = append(order, chName)
		}
		// Fill User1 then User2 in order of appearance (sorted by username).
		if e.info.User1Username == "" {
			e.info.User1ID = userID
			e.info.User1Username = username
		} else {
			e.info.User2ID = userID
			e.info.User2Username = username
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	dms := make([]domain.AllDMInfo, 0, len(order))
	for _, name := range order {
		dms = append(dms, dmMap[name].info)
	}
	return dms, nil
}

// OpenDM finds or creates a DM channel between two users.
// Returns the channel name and whether it was newly created.
//
// Concurrency: wrapped in a transaction. The unique index on channels(name)
// with deterministic dm-<lower>-<higher> naming prevents duplicates.
func (s *Store) OpenDM(userID, otherUserID int64, otherUsername string) (string, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Look for existing DM between the two users.
	var name string
	err = tx.QueryRow(`
		SELECT c.name FROM channels c
		JOIN channel_members cm1 ON c.id = cm1.channel_id AND cm1.user_id = $1
		JOIN channel_members cm2 ON c.id = cm2.channel_id AND cm2.user_id = $2
		WHERE c.type = 'dm'
		LIMIT 1
	`, userID, otherUserID).Scan(&name)
	if err == nil {
		// Found existing DM — commit read-only tx and return.
		tx.Commit()
		return name, false, nil
	}
	if err != sql.ErrNoRows {
		return "", false, fmt.Errorf("find dm: %w", err)
	}

	// Create a new DM channel.
	// Use a deterministic name: dm-<lower-username>-<higher-username>
	var callerUsername string
	if err := tx.QueryRow("SELECT username FROM users WHERE id = $1", userID).Scan(&callerUsername); err != nil {
		return "", false, fmt.Errorf("get caller username: %w", err)
	}

	first, second := callerUsername, otherUsername
	if first > second {
		first, second = second, first
	}
	dmName := fmt.Sprintf("dm-%s-%s", first, second)

	var chID int64
	err = tx.QueryRow(
		"INSERT INTO channels (name, public, type) VALUES ($1, FALSE, 'dm') RETURNING id",
		dmName,
	).Scan(&chID)
	if err != nil {
		return "", false, fmt.Errorf("insert dm channel: %w", err)
	}

	for _, uid := range []int64{userID, otherUserID} {
		if _, err := tx.Exec(
			"INSERT INTO channel_members (channel_id, user_id) VALUES ($1, $2)",
			chID, uid,
		); err != nil {
			return "", false, fmt.Errorf("insert dm member %d: %w", uid, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", false, fmt.Errorf("commit: %w", err)
	}
	return dmName, true, nil
}
