// SPDX-License-Identifier: AGPL-3.0-or-later
package client

// User represents a user returned by the user_list operation.
type User struct {
	Username string `json:"username"`
	Online   bool   `json:"online"`
	Type     string `json:"type"`
	State    string `json:"state,omitempty"`
}

// Channel represents a channel returned by channel_list.
type Channel struct {
	Name   string `json:"name"`
	Public bool   `json:"public"`
	Member bool   `json:"member"`
}

// Message represents a message in history or unread_messages responses.
type Message struct {
	ID       int64    `json:"id,omitempty"`
	Channel  string   `json:"channel,omitempty"`
	From     string   `json:"from"`
	Body     string   `json:"body"`
	SentAt   string   `json:"sent_at"`
	ThreadID *int64   `json:"thread_id,omitempty"`
	Mentions []string `json:"mentions,omitempty"`
}

// BroadcastMessage is the payload of a message.new server push.
type BroadcastMessage struct {
	ID          int64    `json:"id"`
	Channel     string   `json:"channel"`
	ChannelType string   `json:"channel_type"`
	From        string   `json:"from"`
	Body        string   `json:"body"`
	SentAt      string   `json:"sent_at"`
	ThreadID    *int64   `json:"thread_id,omitempty"`
	Mentions    []string `json:"mentions,omitempty"`
}

// PresenceUpdate is the payload of a presence server push.
type PresenceUpdate struct {
	Username string `json:"username"`
	Status   string `json:"status"`
	State    string `json:"state,omitempty"`
}

// UnreadCount is one entry in the unread_counts response.
type UnreadCount struct {
	Channel      string `json:"channel"`
	Type         string `json:"type"`
	UnreadCount  int    `json:"unread_count"`
	MentionCount int    `json:"mention_count"`
}

// DM is one entry in the dm_list response.
type DM struct {
	Channel      string   `json:"channel"`
	Participants []string `json:"participants"`
}

// DMOpenResult is the response from dm_open.
type DMOpenResult struct {
	Channel     string `json:"channel"`
	Participant string `json:"participant"`
	Created     bool   `json:"created"`
}

// MentionGroup represents a mention group.
type MentionGroup struct {
	ID        int64    `json:"id"`
	Slug      string   `json:"slug"`
	CreatedBy string   `json:"created_by,omitempty"`
	Members   []string `json:"members,omitempty"`
}

// Option structs for methods with optional parameters.

// RegisterOpts are optional parameters for Register.
type RegisterOpts struct {
	NotificationsOnly bool
}

// IdentifyOpts are optional parameters for Identify.
type IdentifyOpts struct {
	NotificationsOnly bool
}

// SendOpts are optional parameters for SendMessage.
type SendOpts struct {
	ThreadID *int64
}

// HistoryOpts are optional parameters for History.
type HistoryOpts struct {
	Before   *int64
	After    *int64
	Limit    *int
	ThreadID *int64
}

// UnreadOpts are optional parameters for UnreadMessages.
type UnreadOpts struct {
	MentionsOnly bool
	ThreadID     *int64
}
