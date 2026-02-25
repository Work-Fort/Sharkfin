// SPDX-License-Identifier: GPL-2.0-only
package db

import (
	"fmt"
	"time"
)

// Channel represents a messaging channel.
type Channel struct {
	ID        int64
	Name      string
	Public    bool
	CreatedAt time.Time
}

// CreateChannel creates a channel with the given members in a transaction.
func (d *DB) CreateChannel(name string, public bool, memberIDs []int64) (int64, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec("INSERT INTO channels (name, public) VALUES (?, ?)", name, public)
	if err != nil {
		return 0, fmt.Errorf("insert channel: %w", err)
	}

	chID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}

	for _, uid := range memberIDs {
		if _, err := tx.Exec("INSERT INTO channel_members (channel_id, user_id) VALUES (?, ?)", chID, uid); err != nil {
			return 0, fmt.Errorf("insert member %d: %w", uid, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return chID, nil
}

// ListChannelsForUser returns channels visible to a user:
// all public channels plus private channels where the user is a member.
func (d *DB) ListChannelsForUser(userID int64) ([]Channel, error) {
	rows, err := d.db.Query(`
		SELECT DISTINCT c.id, c.name, c.public, c.created_at
		FROM channels c
		LEFT JOIN channel_members cm ON c.id = cm.channel_id AND cm.user_id = ?
		WHERE c.public = 1 OR cm.user_id IS NOT NULL
		ORDER BY c.name
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		var ch Channel
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Public, &ch.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan channel: %w", err)
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

// AddChannelMember adds a user to a channel.
func (d *DB) AddChannelMember(channelID, userID int64) error {
	_, err := d.db.Exec(
		"INSERT INTO channel_members (channel_id, user_id) VALUES (?, ?)",
		channelID, userID,
	)
	if err != nil {
		return fmt.Errorf("add member: %w", err)
	}
	return nil
}

// IsChannelMember returns true if the user is a member of the channel.
func (d *DB) IsChannelMember(channelID, userID int64) (bool, error) {
	var count int
	err := d.db.QueryRow(
		"SELECT COUNT(*) FROM channel_members WHERE channel_id = ? AND user_id = ?",
		channelID, userID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check membership: %w", err)
	}
	return count > 0, nil
}
