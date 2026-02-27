// SPDX-License-Identifier: GPL-2.0-only
package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Channel represents a messaging channel.
type Channel struct {
	ID        int64
	Name      string
	Public    bool
	Type      string // "channel" or "dm"
	CreatedAt time.Time
}

// CreateChannel creates a channel with the given members in a transaction.
// channelType should be "channel" or "dm".
func (d *DB) CreateChannel(name string, public bool, memberIDs []int64, channelType string) (int64, error) {
	if channelType == "" {
		channelType = "channel"
	}
	tx, err := d.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec("INSERT INTO channels (name, public, type) VALUES (?, ?, ?)", name, public, channelType)
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

// ChannelWithMembership extends Channel with membership status.
type ChannelWithMembership struct {
	Channel
	Member bool
}

// ListChannelsForUser returns non-DM channels visible to a user:
// all public channels plus private channels where the user is a member.
// Each result includes whether the user is a member.
func (d *DB) ListChannelsForUser(userID int64) ([]ChannelWithMembership, error) {
	rows, err := d.db.Query(`
		SELECT DISTINCT c.id, c.name, c.public, c.type, c.created_at,
			CASE WHEN cm.user_id IS NOT NULL THEN 1 ELSE 0 END AS member
		FROM channels c
		LEFT JOIN channel_members cm ON c.id = cm.channel_id AND cm.user_id = ?
		WHERE c.type = 'channel' AND (c.public = 1 OR cm.user_id IS NOT NULL)
		ORDER BY c.name
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer rows.Close()

	var channels []ChannelWithMembership
	for rows.Next() {
		var ch ChannelWithMembership
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Public, &ch.Type, &ch.CreatedAt, &ch.Member); err != nil {
			return nil, fmt.Errorf("scan channel: %w", err)
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

// ListAllChannelsWithMembership returns all non-DM channels with membership status for the given user.
func (d *DB) ListAllChannelsWithMembership(userID int64) ([]ChannelWithMembership, error) {
	rows, err := d.db.Query(`
		SELECT c.id, c.name, c.public, c.type, c.created_at,
			CASE WHEN cm.user_id IS NOT NULL THEN 1 ELSE 0 END AS member
		FROM channels c
		LEFT JOIN channel_members cm ON c.id = cm.channel_id AND cm.user_id = ?
		WHERE c.type = 'channel'
		ORDER BY c.name
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list all channels: %w", err)
	}
	defer rows.Close()

	var channels []ChannelWithMembership
	for rows.Next() {
		var ch ChannelWithMembership
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Public, &ch.Type, &ch.CreatedAt, &ch.Member); err != nil {
			return nil, fmt.Errorf("scan channel: %w", err)
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

// GetChannelByID returns a channel by its ID.
func (d *DB) GetChannelByID(id int64) (*Channel, error) {
	var ch Channel
	err := d.db.QueryRow(
		"SELECT id, name, public, type, created_at FROM channels WHERE id = ?",
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
func (d *DB) GetChannelByName(name string) (*Channel, error) {
	var ch Channel
	err := d.db.QueryRow(
		"SELECT id, name, public, type, created_at FROM channels WHERE name = ?",
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

// ChannelMemberUsernames returns the usernames of all members of a channel.
func (d *DB) ChannelMemberUsernames(channelID int64) ([]string, error) {
	rows, err := d.db.Query(`
		SELECT u.username FROM channel_members cm
		JOIN users u ON cm.user_id = u.id
		WHERE cm.channel_id = ?
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

// DMInfo holds a DM channel with the other participant's username.
type DMInfo struct {
	Channel     string
	Participant string
}

// ListDMsForUser returns all DM channels the user is a member of,
// with the other participant's username.
func (d *DB) ListDMsForUser(userID int64) ([]DMInfo, error) {
	rows, err := d.db.Query(`
		SELECT c.name, u.username
		FROM channels c
		JOIN channel_members cm1 ON c.id = cm1.channel_id AND cm1.user_id = ?
		JOIN channel_members cm2 ON c.id = cm2.channel_id AND cm2.user_id != ?
		JOIN users u ON cm2.user_id = u.id
		WHERE c.type = 'dm'
		ORDER BY u.username
	`, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("list dms: %w", err)
	}
	defer rows.Close()

	var dms []DMInfo
	for rows.Next() {
		var dm DMInfo
		if err := rows.Scan(&dm.Channel, &dm.Participant); err != nil {
			return nil, fmt.Errorf("scan dm: %w", err)
		}
		dms = append(dms, dm)
	}
	return dms, rows.Err()
}

// AllDMInfo holds a DM channel with both participants' usernames.
type AllDMInfo struct {
	Channel      string
	Participants []string
}

// ListAllDMs returns all DM channels with their participants (admin view).
func (d *DB) ListAllDMs() ([]AllDMInfo, error) {
	rows, err := d.db.Query(`
		SELECT c.name, u.username
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

	dmMap := make(map[string]*AllDMInfo)
	var order []string
	for rows.Next() {
		var chName, username string
		if err := rows.Scan(&chName, &username); err != nil {
			return nil, fmt.Errorf("scan dm: %w", err)
		}
		if _, ok := dmMap[chName]; !ok {
			dmMap[chName] = &AllDMInfo{Channel: chName}
			order = append(order, chName)
		}
		dmMap[chName].Participants = append(dmMap[chName].Participants, username)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	dms := make([]AllDMInfo, 0, len(order))
	for _, name := range order {
		dms = append(dms, *dmMap[name])
	}
	return dms, nil
}

// OpenDM finds or creates a DM channel between two users.
// Returns the channel name and whether it was newly created.
func (d *DB) OpenDM(userID, otherUserID int64, otherUsername string) (string, bool, error) {
	// Look for existing DM between the two users.
	var name string
	err := d.db.QueryRow(`
		SELECT c.name FROM channels c
		JOIN channel_members cm1 ON c.id = cm1.channel_id AND cm1.user_id = ?
		JOIN channel_members cm2 ON c.id = cm2.channel_id AND cm2.user_id = ?
		WHERE c.type = 'dm'
		LIMIT 1
	`, userID, otherUserID).Scan(&name)
	if err == nil {
		return name, false, nil
	}
	if err != sql.ErrNoRows {
		return "", false, fmt.Errorf("find dm: %w", err)
	}

	// Create a new DM channel.
	// Use a deterministic name: dm-<lower-username>-<higher-username>
	callerUsername := ""
	if err := d.db.QueryRow("SELECT username FROM users WHERE id = ?", userID).Scan(&callerUsername); err != nil {
		return "", false, fmt.Errorf("get caller username: %w", err)
	}

	first, second := callerUsername, otherUsername
	if first > second {
		first, second = second, first
	}
	dmName := fmt.Sprintf("dm-%s-%s", first, second)

	chID, err := d.CreateChannel(dmName, false, []int64{userID, otherUserID}, "dm")
	if err != nil {
		return "", false, fmt.Errorf("create dm: %w", err)
	}
	_ = chID
	return dmName, true, nil
}
