// SPDX-License-Identifier: GPL-2.0-only
package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Message represents a message in a channel.
type Message struct {
	ID        int64
	ChannelID int64
	UserID    int64
	Body      string
	CreatedAt time.Time
	Username  string
}

// GetMessages returns messages for a channel with cursor-based pagination.
// If before is set, returns messages with id < before (most recent first up to limit, returned in ASC order).
// If after is set, returns messages with id > after in ASC order.
// If neither is set, returns the most recent `limit` messages in ASC order.
func (d *DB) GetMessages(channelID int64, before *int64, after *int64, limit int) ([]Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var query string
	var args []interface{}

	switch {
	case before != nil:
		// Fetch `limit` messages before the cursor, ordered newest-first, then reverse
		query = `
			SELECT * FROM (
				SELECT m.id, m.channel_id, m.user_id, m.body, m.created_at, u.username
				FROM messages m
				JOIN users u ON m.user_id = u.id
				WHERE m.channel_id = ? AND m.id < ?
				ORDER BY m.id DESC
				LIMIT ?
			) sub ORDER BY sub.id ASC`
		args = []interface{}{channelID, *before, limit}
	case after != nil:
		query = `
			SELECT m.id, m.channel_id, m.user_id, m.body, m.created_at, u.username
			FROM messages m
			JOIN users u ON m.user_id = u.id
			WHERE m.channel_id = ? AND m.id > ?
			ORDER BY m.id ASC
			LIMIT ?`
		args = []interface{}{channelID, *after, limit}
	default:
		// No cursor: return most recent `limit` messages
		query = `
			SELECT * FROM (
				SELECT m.id, m.channel_id, m.user_id, m.body, m.created_at, u.username
				FROM messages m
				JOIN users u ON m.user_id = u.id
				WHERE m.channel_id = ?
				ORDER BY m.id DESC
				LIMIT ?
			) sub ORDER BY sub.id ASC`
		args = []interface{}{channelID, limit}
	}

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.Body, &m.CreatedAt, &m.Username); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

// SendMessage inserts a message into a channel.
func (d *DB) SendMessage(channelID, userID int64, body string) (int64, error) {
	res, err := d.db.Exec(
		"INSERT INTO messages (channel_id, user_id, body) VALUES (?, ?, ?)",
		channelID, userID, body,
	)
	if err != nil {
		return 0, fmt.Errorf("send message: %w", err)
	}
	return res.LastInsertId()
}

// GetUnreadMessages returns unread messages for a user, optionally filtered
// by channel. Advances the read cursor after fetching.
func (d *DB) GetUnreadMessages(userID int64, channelID *int64) ([]Message, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var messages []Message

	if channelID != nil {
		messages, err = fetchUnreadForChannel(tx, userID, *channelID)
	} else {
		messages, err = fetchUnreadAllChannels(tx, userID)
	}
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return messages, nil
}

func fetchUnreadForChannel(tx *sql.Tx, userID, channelID int64) ([]Message, error) {
	// Fetch all unread messages (including own) to find the max ID for cursor advancement,
	// but only return messages from other users.
	rows, err := tx.Query(`
		SELECT m.id, m.channel_id, m.user_id, m.body, m.created_at, u.username
		FROM messages m
		JOIN users u ON m.user_id = u.id
		WHERE m.channel_id = ?
		  AND m.id > COALESCE(
			(SELECT last_read_message_id FROM read_cursors
			 WHERE channel_id = ? AND user_id = ?), 0)
		ORDER BY m.id ASC
	`, channelID, channelID, userID)
	if err != nil {
		return nil, fmt.Errorf("query unread: %w", err)
	}
	defer rows.Close()

	var messages []Message
	var maxID int64
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.Body, &m.CreatedAt, &m.Username); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if m.ID > maxID {
			maxID = m.ID
		}
		if m.UserID != userID {
			messages = append(messages, m)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if maxID > 0 {
		if err := advanceCursor(tx, channelID, userID, maxID); err != nil {
			return nil, err
		}
	}

	return messages, nil
}

func fetchUnreadAllChannels(tx *sql.Tx, userID int64) ([]Message, error) {
	// Get all channels the user is a member of
	chRows, err := tx.Query(
		"SELECT channel_id FROM channel_members WHERE user_id = ?", userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query channels: %w", err)
	}
	defer chRows.Close()

	var channelIDs []int64
	for chRows.Next() {
		var chID int64
		if err := chRows.Scan(&chID); err != nil {
			return nil, fmt.Errorf("scan channel id: %w", err)
		}
		channelIDs = append(channelIDs, chID)
	}
	if err := chRows.Err(); err != nil {
		return nil, err
	}

	var allMessages []Message
	for _, chID := range channelIDs {
		msgs, err := fetchUnreadForChannel(tx, userID, chID)
		if err != nil {
			return nil, err
		}
		allMessages = append(allMessages, msgs...)
	}
	return allMessages, nil
}

func advanceCursor(tx *sql.Tx, channelID, userID, messageID int64) error {
	_, err := tx.Exec(`
		INSERT INTO read_cursors (channel_id, user_id, last_read_message_id)
		VALUES (?, ?, ?)
		ON CONFLICT(channel_id, user_id) DO UPDATE SET last_read_message_id = excluded.last_read_message_id
	`, channelID, userID, messageID)
	if err != nil {
		return fmt.Errorf("advance cursor: %w", err)
	}
	return nil
}
