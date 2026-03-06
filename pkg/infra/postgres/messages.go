// SPDX-License-Identifier: GPL-2.0-only
package postgres

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

// SendMessage inserts a message into a channel with optional thread and mentions.
func (s *Store) SendMessage(channelID, userID int64, body string, threadID *int64, mentionUserIDs []int64) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if threadID != nil {
		var parentChannelID int64
		var parentThreadID *int64
		err := tx.QueryRow(
			"SELECT channel_id, thread_id FROM messages WHERE id = $1", *threadID,
		).Scan(&parentChannelID, &parentThreadID)
		if err != nil {
			return 0, fmt.Errorf("parent message not found: %d", *threadID)
		}
		if parentChannelID != channelID {
			return 0, fmt.Errorf("parent message is in a different channel")
		}
		if parentThreadID != nil {
			return 0, fmt.Errorf("cannot reply to a reply (threads are 1 level deep)")
		}
	}

	var msgID int64
	err = tx.QueryRow(
		"INSERT INTO messages (channel_id, user_id, body, thread_id) VALUES ($1, $2, $3, $4) RETURNING id",
		channelID, userID, body, threadID,
	).Scan(&msgID)
	if err != nil {
		return 0, fmt.Errorf("send message: %w", err)
	}

	for _, uid := range mentionUserIDs {
		if _, err := tx.Exec(
			"INSERT INTO message_mentions (message_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			msgID, uid,
		); err != nil {
			return 0, fmt.Errorf("insert mention: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return msgID, nil
}

// GetMessages returns messages for a channel with cursor-based pagination.
// If before is set, returns messages with id < before (most recent first up to limit, returned in ASC order).
// If after is set, returns messages with id > after in ASC order.
// If neither is set, returns the most recent `limit` messages in ASC order.
// If threadID is set, only returns replies to that parent message.
func (s *Store) GetMessages(channelID int64, before *int64, after *int64, limit int, threadID *int64) ([]domain.Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var query string
	var args []interface{}
	argN := 1 // Postgres placeholder counter

	// Helper to get next placeholder.
	ph := func() string {
		p := fmt.Sprintf("$%d", argN)
		argN++
		return p
	}

	threadFilter := ""
	if threadID != nil {
		threadFilter = fmt.Sprintf(" AND m.thread_id = %s", ph())
		args = append(args, *threadID)
	}

	switch {
	case before != nil:
		chPh := ph()
		bPh := ph()
		lPh := ph()
		query = fmt.Sprintf(`
			SELECT * FROM (
				SELECT m.id, m.channel_id, m.user_id, m.body, m.created_at, u.username, m.thread_id
				FROM messages m
				JOIN users u ON m.user_id = u.id
				WHERE m.channel_id = %s AND m.id < %s%s
				ORDER BY m.id DESC
				LIMIT %s
			) sub ORDER BY sub.id ASC`, chPh, bPh, threadFilter, lPh)
		args = append(args, channelID, *before, limit)
	case after != nil:
		chPh := ph()
		aPh := ph()
		lPh := ph()
		query = fmt.Sprintf(`
			SELECT m.id, m.channel_id, m.user_id, m.body, m.created_at, u.username, m.thread_id
			FROM messages m
			JOIN users u ON m.user_id = u.id
			WHERE m.channel_id = %s AND m.id > %s%s
			ORDER BY m.id ASC
			LIMIT %s`, chPh, aPh, threadFilter, lPh)
		args = append(args, channelID, *after, limit)
	default:
		chPh := ph()
		lPh := ph()
		query = fmt.Sprintf(`
			SELECT * FROM (
				SELECT m.id, m.channel_id, m.user_id, m.body, m.created_at, u.username, m.thread_id
				FROM messages m
				JOIN users u ON m.user_id = u.id
				WHERE m.channel_id = %s%s
				ORDER BY m.id DESC
				LIMIT %s
			) sub ORDER BY sub.id ASC`, chPh, threadFilter, lPh)
		args = append(args, channelID, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	defer rows.Close()

	var messages []domain.Message
	for rows.Next() {
		var m domain.Message
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.Body, &m.CreatedAt, &m.From, &m.ThreadID); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := s.loadMentions(messages); err != nil {
		return nil, err
	}
	return messages, nil
}

// GetUnreadMessages returns unread messages for a user, optionally filtered
// by channel, mentions, or thread. Advances the read cursor only when no
// filters are active (a filtered read is partial and shouldn't mark
// everything as read).
//
// Concurrency fix: loadMentions runs inside the transaction so that the
// mention set is consistent with the message snapshot.
func (s *Store) GetUnreadMessages(userID int64, channelID *int64, mentionsOnly bool, threadID *int64) ([]domain.Message, error) {
	filtered := mentionsOnly || threadID != nil
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var messages []domain.Message
	if channelID != nil {
		messages, err = fetchUnreadForChannel(tx, userID, *channelID, mentionsOnly, threadID, filtered)
	} else {
		messages, err = fetchUnreadAllChannels(tx, userID, mentionsOnly, threadID, filtered)
	}
	if err != nil {
		return nil, err
	}

	// Concurrency fix: load mentions inside the transaction for Postgres
	// to ensure consistency with the message set under concurrent writes.
	if err := loadMentionsTx(tx, messages); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return messages, nil
}

func fetchUnreadForChannel(tx *sql.Tx, userID, channelID int64, mentionsOnly bool, threadID *int64, skipCursorAdvance bool) ([]domain.Message, error) {
	var args []interface{}
	argN := 1

	ph := func() string {
		p := fmt.Sprintf("$%d", argN)
		argN++
		return p
	}

	mentionJoin := ""
	if mentionsOnly {
		mentionJoin = fmt.Sprintf(" JOIN message_mentions mm ON m.id = mm.message_id AND mm.user_id = %s", ph())
		args = append(args, userID)
	}

	chPh := ph()
	args = append(args, channelID)
	chPh2 := ph()
	args = append(args, channelID)
	uPh := ph()
	args = append(args, userID)

	threadFilter := ""
	if threadID != nil {
		threadFilter = fmt.Sprintf(" AND m.thread_id = %s", ph())
		args = append(args, *threadID)
	}

	query := fmt.Sprintf(`
		SELECT m.id, m.channel_id, m.user_id, m.body, m.created_at, u.username, m.thread_id
		FROM messages m
		JOIN users u ON m.user_id = u.id%s
		WHERE m.channel_id = %s
		  AND m.id > COALESCE(
			(SELECT last_read_message_id FROM read_cursors
			 WHERE channel_id = %s AND user_id = %s), 0)%s
		ORDER BY m.id ASC
	`, mentionJoin, chPh, chPh2, uPh, threadFilter)

	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query unread: %w", err)
	}
	defer rows.Close()

	var messages []domain.Message
	var maxID int64
	for rows.Next() {
		var m domain.Message
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.Body, &m.CreatedAt, &m.From, &m.ThreadID); err != nil {
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

	// Concurrency fix: use GREATEST for forward-only guarantee in advanceCursor.
	if !skipCursorAdvance && maxID > 0 {
		if err := advanceCursor(tx, channelID, userID, maxID); err != nil {
			return nil, err
		}
	}

	return messages, nil
}

func fetchUnreadAllChannels(tx *sql.Tx, userID int64, mentionsOnly bool, threadID *int64, skipCursorAdvance bool) ([]domain.Message, error) {
	chRows, err := tx.Query(
		"SELECT channel_id FROM channel_members WHERE user_id = $1", userID,
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

	var allMessages []domain.Message
	for _, chID := range channelIDs {
		msgs, err := fetchUnreadForChannel(tx, userID, chID, mentionsOnly, threadID, skipCursorAdvance)
		if err != nil {
			return nil, err
		}
		allMessages = append(allMessages, msgs...)
	}
	return allMessages, nil
}

// advanceCursor upserts the read cursor, using GREATEST to ensure forward-only movement.
func advanceCursor(tx *sql.Tx, channelID, userID, messageID int64) error {
	_, err := tx.Exec(`
		INSERT INTO read_cursors (channel_id, user_id, last_read_message_id)
		VALUES ($1, $2, $3)
		ON CONFLICT(channel_id, user_id)
		DO UPDATE SET last_read_message_id = GREATEST(read_cursors.last_read_message_id, EXCLUDED.last_read_message_id)
	`, channelID, userID, messageID)
	if err != nil {
		return fmt.Errorf("advance cursor: %w", err)
	}
	return nil
}

// loadMentions populates the Mentions field for a slice of messages.
func (s *Store) loadMentions(messages []domain.Message) error {
	if len(messages) == 0 {
		return nil
	}

	idxMap := make(map[int64]int, len(messages))
	ids := make([]interface{}, len(messages))
	placeholders := make([]string, len(messages))
	for i, m := range messages {
		idxMap[m.ID] = i
		ids[i] = m.ID
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	query := fmt.Sprintf(`
		SELECT mm.message_id, u.username
		FROM message_mentions mm
		JOIN users u ON mm.user_id = u.id
		WHERE mm.message_id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := s.db.Query(query, ids...)
	if err != nil {
		return fmt.Errorf("load mentions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var msgID int64
		var username string
		if err := rows.Scan(&msgID, &username); err != nil {
			return fmt.Errorf("scan mention: %w", err)
		}
		if idx, ok := idxMap[msgID]; ok {
			messages[idx].Mentions = append(messages[idx].Mentions, username)
		}
	}
	return rows.Err()
}

// loadMentionsTx populates the Mentions field inside a transaction.
// Used by GetUnreadMessages to keep mentions consistent with the message snapshot.
func loadMentionsTx(tx *sql.Tx, messages []domain.Message) error {
	if len(messages) == 0 {
		return nil
	}

	idxMap := make(map[int64]int, len(messages))
	ids := make([]interface{}, len(messages))
	placeholders := make([]string, len(messages))
	for i, m := range messages {
		idxMap[m.ID] = i
		ids[i] = m.ID
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	query := fmt.Sprintf(`
		SELECT mm.message_id, u.username
		FROM message_mentions mm
		JOIN users u ON mm.user_id = u.id
		WHERE mm.message_id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := tx.Query(query, ids...)
	if err != nil {
		return fmt.Errorf("load mentions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var msgID int64
		var username string
		if err := rows.Scan(&msgID, &username); err != nil {
			return fmt.Errorf("scan mention: %w", err)
		}
		if idx, ok := idxMap[msgID]; ok {
			messages[idx].Mentions = append(messages[idx].Mentions, username)
		}
	}
	return rows.Err()
}

// GetUnreadCounts returns per-channel unread message and mention counts for a user.
// Only returns channels with >0 unreads. Excludes the user's own messages.
func (s *Store) GetUnreadCounts(userID int64) ([]domain.UnreadCount, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.name, c.type,
		       COUNT(m.id) AS unread_count,
		       COUNT(mm.message_id) AS mention_count
		FROM channel_members cm
		JOIN channels c ON cm.channel_id = c.id
		JOIN messages m ON m.channel_id = c.id
		  AND m.user_id != $1
		  AND m.id > COALESCE(
			(SELECT last_read_message_id FROM read_cursors
			 WHERE channel_id = c.id AND user_id = $2), 0)
		LEFT JOIN message_mentions mm ON mm.message_id = m.id AND mm.user_id = $3
		WHERE cm.user_id = $4
		GROUP BY c.id, c.name, c.type
		HAVING COUNT(m.id) > 0
	`, userID, userID, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("get unread counts: %w", err)
	}
	defer rows.Close()

	var counts []domain.UnreadCount
	for rows.Next() {
		var c domain.UnreadCount
		if err := rows.Scan(&c.ChannelID, &c.Channel, &c.Type, &c.UnreadCount, &c.MentionCount); err != nil {
			return nil, fmt.Errorf("scan unread count: %w", err)
		}
		counts = append(counts, c)
	}
	return counts, rows.Err()
}

// MarkRead advances the read cursor for a user in a channel.
// If messageID is nil, advances to the latest message.
// Forward-only: never moves the cursor backwards.
func (s *Store) MarkRead(userID, channelID int64, messageID *int64) error {
	var targetID int64
	if messageID != nil {
		targetID = *messageID
	} else {
		err := s.db.QueryRow(
			"SELECT COALESCE(MAX(id), 0) FROM messages WHERE channel_id = $1",
			channelID,
		).Scan(&targetID)
		if err != nil {
			return fmt.Errorf("get max message id: %w", err)
		}
	}

	if targetID == 0 {
		return nil // no messages in channel
	}

	_, err := s.db.Exec(`
		INSERT INTO read_cursors (channel_id, user_id, last_read_message_id)
		VALUES ($1, $2, $3)
		ON CONFLICT(channel_id, user_id)
		DO UPDATE SET last_read_message_id = GREATEST(read_cursors.last_read_message_id, EXCLUDED.last_read_message_id)
	`, channelID, userID, targetID)
	if err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	return nil
}
