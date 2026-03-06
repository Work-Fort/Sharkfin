// SPDX-License-Identifier: AGPL-3.0-or-later
package domain

import "time"

type User struct {
	ID        int64
	Username  string
	Password  string
	Role      string
	Type      string
	CreatedAt time.Time
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
	ID        int64
	ChannelID int64
	UserID    int64
	From      string // username
	Body      string
	ThreadID  *int64
	Mentions  []string
	CreatedAt time.Time
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
	OtherUserID   int64
	OtherUsername string
}

type AllDMInfo struct {
	ChannelID     int64
	ChannelName   string
	User1ID       int64
	User1Username string
	User2ID       int64
	User2Username string
}

type Role struct {
	Name    string
	BuiltIn bool
}
