// SPDX-License-Identifier: AGPL-3.0-or-later
package postgres

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

// SendMessage inserts a message into a channel with optional thread, mentions, and metadata.
func (s *Store) SendMessage(channelID int64, identityID string, body string, threadID *int64, mentionIdentityIDs []string, metadata *string) (int64, error) {
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
		"INSERT INTO messages (channel_id, identity_id, body, thread_id, metadata) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		channelID, identityID, body, threadID, metadata,
	).Scan(&msgID)
	if err != nil {
		return 0, fmt.Errorf("send message: %w", err)
	}

	for _, id := range mentionIdentityIDs {
		if _, err := tx.Exec(
			"INSERT INTO message_mentions (message_id, identity_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			msgID, id,
		); err != nil {
			return 0, fmt.Errorf("insert mention: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return msgID, nil
}

// ImportMessage inserts a message with an explicit created_at timestamp.
// Used by the backup import to preserve original message timestamps.
func (s *Store) ImportMessage(channelID int64, identityID string, body string, threadID *int64, mentionIdentityIDs []string, createdAt time.Time) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var msgID int64
	err = tx.QueryRow(
		"INSERT INTO messages (channel_id, identity_id, body, thread_id, created_at) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		channelID, identityID, body, threadID, createdAt,
	).Scan(&msgID)
	if err != nil {
		return 0, fmt.Errorf("import message: %w", err)
	}

	for _, id := range mentionIdentityIDs {
		if _, err := tx.Exec("INSERT INTO message_mentions (message_id, identity_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", msgID, id); err != nil {
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
				SELECT m.id, m.channel_id, m.identity_id, m.body, m.created_at, i.username, m.thread_id, m.metadata
				FROM messages m
				JOIN identities i ON m.identity_id = i.id
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
			SELECT m.id, m.channel_id, m.identity_id, m.body, m.created_at, i.username, m.thread_id, m.metadata
			FROM messages m
			JOIN identities i ON m.identity_id = i.id
			WHERE m.channel_id = %s AND m.id > %s%s
			ORDER BY m.id ASC
			LIMIT %s`, chPh, aPh, threadFilter, lPh)
		args = append(args, channelID, *after, limit)
	default:
		chPh := ph()
		lPh := ph()
		query = fmt.Sprintf(`
			SELECT * FROM (
				SELECT m.id, m.channel_id, m.identity_id, m.body, m.created_at, i.username, m.thread_id, m.metadata
				FROM messages m
				JOIN identities i ON m.identity_id = i.id
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
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.IdentityID, &m.Body, &m.CreatedAt, &m.From, &m.ThreadID, &m.Metadata); err != nil {
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

// GetUnreadMessages returns unread messages for an identity, optionally filtered
// by channel, mentions, or thread. Advances the read cursor only when no
// filters are active (a filtered read is partial and shouldn't mark
// everything as read).
//
// Concurrency fix: loadMentions runs inside the transaction so that the
// mention set is consistent with the message snapshot.
func (s *Store) GetUnreadMessages(identityID string, channelID *int64, mentionsOnly bool, threadID *int64) ([]domain.Message, error) {
	filtered := mentionsOnly || threadID != nil
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var messages []domain.Message
	if channelID != nil {
		messages, err = fetchUnreadForChannel(tx, identityID, *channelID, mentionsOnly, threadID, filtered)
	} else {
		messages, err = fetchUnreadAllChannels(tx, identityID, mentionsOnly, threadID, filtered)
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

func fetchUnreadForChannel(tx *sql.Tx, identityID string, channelID int64, mentionsOnly bool, threadID *int64, skipCursorAdvance bool) ([]domain.Message, error) {
	var args []interface{}
	argN := 1

	ph := func() string {
		p := fmt.Sprintf("$%d", argN)
		argN++
		return p
	}

	mentionJoin := ""
	if mentionsOnly {
		mentionJoin = fmt.Sprintf(" JOIN message_mentions mm ON m.id = mm.message_id AND mm.identity_id = %s", ph())
		args = append(args, identityID)
	}

	chPh := ph()
	args = append(args, channelID)
	chPh2 := ph()
	args = append(args, channelID)
	uPh := ph()
	args = append(args, identityID)

	threadFilter := ""
	if threadID != nil {
		threadFilter = fmt.Sprintf(" AND m.thread_id = %s", ph())
		args = append(args, *threadID)
	}

	query := fmt.Sprintf(`
		SELECT m.id, m.channel_id, m.identity_id, m.body, m.created_at, i.username, m.thread_id, m.metadata
		FROM messages m
		JOIN identities i ON m.identity_id = i.id%s
		WHERE m.channel_id = %s
		  AND m.id > COALESCE(
			(SELECT last_read_message_id FROM read_cursors
			 WHERE channel_id = %s AND identity_id = %s), 0)%s
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
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.IdentityID, &m.Body, &m.CreatedAt, &m.From, &m.ThreadID, &m.Metadata); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if m.ID > maxID {
			maxID = m.ID
		}
		if m.IdentityID != identityID {
			messages = append(messages, m)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Concurrency fix: use GREATEST for forward-only guarantee in advanceCursor.
	if !skipCursorAdvance && maxID > 0 {
		if err := advanceCursor(tx, channelID, identityID, maxID); err != nil {
			return nil, err
		}
	}

	return messages, nil
}

func fetchUnreadAllChannels(tx *sql.Tx, identityID string, mentionsOnly bool, threadID *int64, skipCursorAdvance bool) ([]domain.Message, error) {
	chRows, err := tx.Query(
		"SELECT channel_id FROM channel_members WHERE identity_id = $1", identityID,
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
		msgs, err := fetchUnreadForChannel(tx, identityID, chID, mentionsOnly, threadID, skipCursorAdvance)
		if err != nil {
			return nil, err
		}
		allMessages = append(allMessages, msgs...)
	}
	return allMessages, nil
}

// advanceCursor upserts the read cursor, using GREATEST to ensure forward-only movement.
func advanceCursor(tx *sql.Tx, channelID int64, identityID string, messageID int64) error {
	_, err := tx.Exec(`
		INSERT INTO read_cursors (channel_id, identity_id, last_read_message_id)
		VALUES ($1, $2, $3)
		ON CONFLICT(channel_id, identity_id)
		DO UPDATE SET last_read_message_id = GREATEST(read_cursors.last_read_message_id, EXCLUDED.last_read_message_id)
	`, channelID, identityID, messageID)
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
		SELECT mm.message_id, i.username
		FROM message_mentions mm
		JOIN identities i ON mm.identity_id = i.id
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
		SELECT mm.message_id, i.username
		FROM message_mentions mm
		JOIN identities i ON mm.identity_id = i.id
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

// GetUnreadCounts returns per-channel unread message and mention counts for an identity.
// Only returns channels with >0 unreads. Excludes the identity's own messages.
func (s *Store) GetUnreadCounts(identityID string) ([]domain.UnreadCount, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.name, c.type,
		       COUNT(m.id) AS unread_count,
		       COUNT(mm.message_id) AS mention_count
		FROM channel_members cm
		JOIN channels c ON cm.channel_id = c.id
		JOIN messages m ON m.channel_id = c.id
		  AND m.identity_id != $1
		  AND m.id > COALESCE(
			(SELECT last_read_message_id FROM read_cursors
			 WHERE channel_id = c.id AND identity_id = $2), 0)
		LEFT JOIN message_mentions mm ON mm.message_id = m.id AND mm.identity_id = $3
		WHERE cm.identity_id = $4
		GROUP BY c.id, c.name, c.type
		HAVING COUNT(m.id) > 0
	`, identityID, identityID, identityID, identityID)
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

// MarkRead advances the read cursor for an identity in a channel.
// If messageID is nil, advances to the latest message.
// Forward-only: never moves the cursor backwards.
func (s *Store) MarkRead(identityID string, channelID int64, messageID *int64) error {
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
		INSERT INTO read_cursors (channel_id, identity_id, last_read_message_id)
		VALUES ($1, $2, $3)
		ON CONFLICT(channel_id, identity_id)
		DO UPDATE SET last_read_message_id = GREATEST(read_cursors.last_read_message_id, EXCLUDED.last_read_message_id)
	`, channelID, identityID, targetID)
	if err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	return nil
}
