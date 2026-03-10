// SPDX-License-Identifier: AGPL-3.0-or-later
package domain

type UserStore interface {
	CreateUser(username, password string) (int64, error)
	GetUserByUsername(username string) (*User, error)
	ListUsers() ([]User, error)
}

type ChannelStore interface {
	CreateChannel(name string, public bool, memberIDs []int64, channelType string) (int64, error)
	GetChannelByID(id int64) (*Channel, error)
	GetChannelByName(name string) (*Channel, error)
	ListChannelsForUser(userID int64) ([]ChannelWithMembership, error)
	ListAllChannelsWithMembership(userID int64) ([]ChannelWithMembership, error)
	AddChannelMember(channelID, userID int64) error
	ChannelMemberUsernames(channelID int64) ([]string, error)
	IsChannelMember(channelID, userID int64) (bool, error)
	ListDMsForUser(userID int64) ([]DMInfo, error)
	ListAllDMs() ([]AllDMInfo, error)
	OpenDM(userID, otherUserID int64, otherUsername string) (string, bool, error)
}

type MessageStore interface {
	SendMessage(channelID, userID int64, body string, threadID *int64, mentionUserIDs []int64) (int64, error)
	GetMessages(channelID int64, before *int64, after *int64, limit int, threadID *int64) ([]Message, error)
	GetUnreadMessages(userID int64, channelID *int64, mentionsOnly bool, threadID *int64) ([]Message, error)
	GetUnreadCounts(userID int64) ([]UnreadCount, error)
	MarkRead(userID, channelID int64, messageID *int64) error
}

type RoleStore interface {
	CreateRole(name string) error
	DeleteRole(name string) error
	ListRoles() ([]Role, error)
	GrantPermission(role, permission string) error
	RevokePermission(role, permission string) error
	GetRolePermissions(role string) ([]string, error)
	GetUserPermissions(username string) ([]string, error)
	HasPermission(username, permission string) (bool, error)
	SetUserRole(username, role string) error
	SetUserType(username, userType string) error
}

type SettingsStore interface {
	GetSetting(key string) (string, error)
	SetSetting(key, value string) error
	ListSettings() (map[string]string, error)
}

// Store is the composite interface for convenient wiring.
type Store interface {
	UserStore
	ChannelStore
	MessageStore
	RoleStore
	SettingsStore
	Close() error
}

// Event is a typed message published to the event bus.
type Event struct {
	Type    string
	Payload any
}

// Subscription receives events from the bus.
type Subscription interface {
	Events() <-chan Event
	Close()
}

// EventBus is an in-process pub/sub system for domain events.
type EventBus interface {
	Publish(event Event)
	Subscribe(eventTypes ...string) Subscription
}
