// SPDX-License-Identifier: AGPL-3.0-or-later
package client

import (
	"context"
	"encoding/json"
)

// --- Identity ---

// Register creates a new user and authenticates this connection.
func (c *Client) Register(ctx context.Context, username string, opts *RegisterOpts) error {
	d := map[string]any{"username": username}
	if opts != nil && opts.NotificationsOnly {
		d["notifications_only"] = true
	}
	_, err := c.request(ctx, "register", d)
	return err
}

// Identify authenticates as an existing user.
func (c *Client) Identify(ctx context.Context, username string, opts *IdentifyOpts) error {
	d := map[string]any{"username": username}
	if opts != nil && opts.NotificationsOnly {
		d["notifications_only"] = true
	}
	_, err := c.request(ctx, "identify", d)
	return err
}

// Users returns all registered users.
func (c *Client) Users(ctx context.Context) ([]User, error) {
	raw, err := c.request(ctx, "user_list", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Users []User `json:"users"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return resp.Users, nil
}

// --- Channels ---

// Channels returns all channels visible to the current user.
func (c *Client) Channels(ctx context.Context) ([]Channel, error) {
	raw, err := c.request(ctx, "channel_list", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Channels []Channel `json:"channels"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return resp.Channels, nil
}

// CreateChannel creates a new channel.
func (c *Client) CreateChannel(ctx context.Context, name string, public bool) error {
	_, err := c.request(ctx, "channel_create", map[string]any{
		"name":   name,
		"public": public,
	})
	return err
}

// InviteToChannel invites a user to a channel.
func (c *Client) InviteToChannel(ctx context.Context, channel, username string) error {
	_, err := c.request(ctx, "channel_invite", map[string]any{
		"channel":  channel,
		"username": username,
	})
	return err
}

// JoinChannel joins a public channel.
func (c *Client) JoinChannel(ctx context.Context, channel string) error {
	_, err := c.request(ctx, "channel_join", map[string]any{
		"channel": channel,
	})
	return err
}

// --- Messages ---

// SendMessage sends a message to a channel. Returns the message ID.
func (c *Client) SendMessage(ctx context.Context, channel, body string, opts *SendOpts) (int64, error) {
	d := map[string]any{
		"channel": channel,
		"body":    body,
	}
	if opts != nil && opts.ThreadID != nil {
		d["thread_id"] = *opts.ThreadID
	}
	raw, err := c.request(ctx, "send_message", d)
	if err != nil {
		return 0, err
	}
	var resp struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0, err
	}
	return resp.ID, nil
}

// History retrieves message history for a channel.
func (c *Client) History(ctx context.Context, channel string, opts *HistoryOpts) ([]Message, error) {
	d := map[string]any{"channel": channel}
	if opts != nil {
		if opts.Before != nil {
			d["before"] = *opts.Before
		}
		if opts.After != nil {
			d["after"] = *opts.After
		}
		if opts.Limit != nil {
			d["limit"] = *opts.Limit
		}
		if opts.ThreadID != nil {
			d["thread_id"] = *opts.ThreadID
		}
	}
	raw, err := c.request(ctx, "history", d)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Messages []Message `json:"messages"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return resp.Messages, nil
}

// UnreadMessages retrieves unread messages, optionally filtered.
func (c *Client) UnreadMessages(ctx context.Context, channel string, opts *UnreadOpts) ([]Message, error) {
	d := map[string]any{}
	if channel != "" {
		d["channel"] = channel
	}
	if opts != nil {
		if opts.MentionsOnly {
			d["mentions_only"] = true
		}
		if opts.ThreadID != nil {
			d["thread_id"] = *opts.ThreadID
		}
	}
	raw, err := c.request(ctx, "unread_messages", d)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Messages []Message `json:"messages"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return resp.Messages, nil
}

// UnreadCounts returns unread and mention counts per channel.
func (c *Client) UnreadCounts(ctx context.Context) ([]UnreadCount, error) {
	raw, err := c.request(ctx, "unread_counts", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Counts []UnreadCount `json:"counts"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return resp.Counts, nil
}

// MarkRead marks messages as read in a channel. If messageID is 0,
// all messages in the channel are marked read.
func (c *Client) MarkRead(ctx context.Context, channel string, messageID int64) error {
	d := map[string]any{"channel": channel}
	if messageID > 0 {
		d["message_id"] = messageID
	}
	_, err := c.request(ctx, "mark_read", d)
	return err
}

// --- DMs ---

// DMOpen opens or retrieves a DM channel with the given user.
func (c *Client) DMOpen(ctx context.Context, username string) (*DMOpenResult, error) {
	raw, err := c.request(ctx, "dm_open", map[string]any{
		"username": username,
	})
	if err != nil {
		return nil, err
	}
	var result DMOpenResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DMList returns all DM channels for the current user.
func (c *Client) DMList(ctx context.Context) ([]DM, error) {
	raw, err := c.request(ctx, "dm_list", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		DMs []DM `json:"dms"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return resp.DMs, nil
}

// --- Presence ---

// SetState sets the user's presence state ("active" or "idle").
func (c *Client) SetState(ctx context.Context, state string) error {
	_, err := c.request(ctx, "set_state", map[string]any{"state": state})
	return err
}

// --- Info ---

// Ping sends an application-level ping and waits for pong.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.request(ctx, "ping", nil)
	return err
}

// Version returns the server version string.
func (c *Client) Version(ctx context.Context) (string, error) {
	raw, err := c.request(ctx, "version", nil)
	if err != nil {
		return "", err
	}
	var resp struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", err
	}
	return resp.Version, nil
}

// Capabilities returns the current user's permissions.
func (c *Client) Capabilities(ctx context.Context) ([]string, error) {
	raw, err := c.request(ctx, "capabilities", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return resp.Permissions, nil
}

// --- Settings ---

// SetSetting sets a server setting key-value pair.
func (c *Client) SetSetting(ctx context.Context, key, value string) error {
	_, err := c.request(ctx, "set_setting", map[string]any{
		"key":   key,
		"value": value,
	})
	return err
}

// GetSettings returns all server settings.
func (c *Client) GetSettings(ctx context.Context) (map[string]string, error) {
	raw, err := c.request(ctx, "get_settings", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Settings map[string]string `json:"settings"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return resp.Settings, nil
}

// --- Mention Groups ---

// CreateMentionGroup creates a new mention group. Returns the group ID.
func (c *Client) CreateMentionGroup(ctx context.Context, slug string) (int64, error) {
	raw, err := c.request(ctx, "mention_group_create", map[string]any{
		"slug": slug,
	})
	if err != nil {
		return 0, err
	}
	var resp struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0, err
	}
	return resp.ID, nil
}

// DeleteMentionGroup deletes a mention group by slug.
func (c *Client) DeleteMentionGroup(ctx context.Context, slug string) error {
	_, err := c.request(ctx, "mention_group_delete", map[string]any{"slug": slug})
	return err
}

// GetMentionGroup returns a mention group by slug.
func (c *Client) GetMentionGroup(ctx context.Context, slug string) (*MentionGroup, error) {
	raw, err := c.request(ctx, "mention_group_get", map[string]any{"slug": slug})
	if err != nil {
		return nil, err
	}
	var group MentionGroup
	if err := json.Unmarshal(raw, &group); err != nil {
		return nil, err
	}
	return &group, nil
}

// ListMentionGroups returns all mention groups.
func (c *Client) ListMentionGroups(ctx context.Context) ([]MentionGroup, error) {
	raw, err := c.request(ctx, "mention_group_list", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Groups []MentionGroup `json:"groups"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return resp.Groups, nil
}

// AddMentionGroupMember adds a user to a mention group.
func (c *Client) AddMentionGroupMember(ctx context.Context, slug, username string) error {
	_, err := c.request(ctx, "mention_group_add_member", map[string]any{
		"slug":     slug,
		"username": username,
	})
	return err
}

// RemoveMentionGroupMember removes a user from a mention group.
func (c *Client) RemoveMentionGroupMember(ctx context.Context, slug, username string) error {
	_, err := c.request(ctx, "mention_group_remove_member", map[string]any{
		"slug":     slug,
		"username": username,
	})
	return err
}
