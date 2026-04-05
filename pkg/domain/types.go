// SPDX-License-Identifier: AGPL-3.0-or-later
package domain

import "time"

type Identity struct {
	ID          string // Internal stable UUID (never changes after creation)
	AuthID      string // Passport-provided UUID (may change if user is recreated)
	Username    string
	DisplayName string
	Type        string // "user", "agent", "service"
	Role        string
	CreatedAt   time.Time
}

type Channel struct {
	ID        int64
	Name      string
	Public    bool
	Type      string // "channel" or "dm"
	CreatedAt time.Time
}

type ChannelWithMembership struct {
	Channel
	Member bool
}

type Message struct {
	ID         int64
	ChannelID  int64
	IdentityID string // was UserID int64
	From       string
	Body       string
	ThreadID   *int64
	Mentions   []string
	CreatedAt  time.Time
}

type IdentityWebhook struct {
	ID         string
	IdentityID string
	URL        string
	Secret     string
	Active     bool
}

type UnreadCount struct {
	ChannelID    int64
	Channel      string
	UnreadCount  int
	MentionCount int
	Type         string
}

type DMInfo struct {
	ChannelID     int64
	ChannelName   string
	OtherUsername string // removed OtherUserID — not needed
}

type AllDMInfo struct {
	ChannelID     int64
	ChannelName   string
	User1Username string // removed User1ID, User2ID — not needed
	User2Username string
}

type Role struct {
	Name    string
	BuiltIn bool
}

type MentionGroup struct {
	ID        int64     `json:"id"`
	Slug      string    `json:"slug"`
	CreatedBy string    `json:"created_by,omitempty"`
	Members   []string  `json:"members,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// Event type constants.
const (
	EventMessageNew     = "message.new"
	EventPresenceUpdate = "presence.update"
)

// MessageEvent is the payload for EventMessageNew.
type MessageEvent struct {
	ChannelName string
	ChannelType string // "channel" or "dm"
	From        string
	Body        string
	MessageID   int64
	SentAt      time.Time
	Mentions    []string
	ThreadID    *int64
}
