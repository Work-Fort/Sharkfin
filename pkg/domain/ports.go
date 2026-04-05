// SPDX-License-Identifier: AGPL-3.0-or-later
package domain

type IdentityStore interface {
	UpsertIdentity(authID, username, displayName, identityType, role string) (*Identity, error)
	GetIdentityByID(id string) (*Identity, error)
	GetIdentityByUsername(username string) (*Identity, error)
	ListIdentities() ([]Identity, error)
}

type ChannelStore interface {
	CreateChannel(name string, public bool, memberIDs []string, channelType string) (int64, error)
	GetChannelByID(id int64) (*Channel, error)
	GetChannelByName(name string) (*Channel, error)
	ListChannelsForUser(identityID string) ([]ChannelWithMembership, error)
	ListAllChannelsWithMembership(identityID string) ([]ChannelWithMembership, error)
	AddChannelMember(channelID int64, identityID string) error
	ChannelMemberUsernames(channelID int64) ([]string, error)
	IsChannelMember(channelID int64, identityID string) (bool, error)
	GetServiceMemberUsernames(channelID int64) ([]string, error)
	ListDMsForUser(identityID string) ([]DMInfo, error)
	ListAllDMs() ([]AllDMInfo, error)
	OpenDM(identityID string, otherIdentityID string, otherUsername string) (string, bool, error)
}

type MessageStore interface {
	SendMessage(channelID int64, identityID string, body string, threadID *int64, mentionIdentityIDs []string, metadata *string) (int64, error)
	GetMessages(channelID int64, before *int64, after *int64, limit int, threadID *int64) ([]Message, error)
	GetUnreadMessages(identityID string, channelID *int64, mentionsOnly bool, threadID *int64) ([]Message, error)
	GetUnreadCounts(identityID string) ([]UnreadCount, error)
	MarkRead(identityID string, channelID int64, messageID *int64) error
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

type MentionGroupStore interface {
	CreateMentionGroup(slug string, createdByID string) (int64, error)
	DeleteMentionGroup(id int64) error
	GetMentionGroup(slug string) (*MentionGroup, error)
	ListMentionGroups() ([]MentionGroup, error)
	AddMentionGroupMember(groupID int64, identityID string) error
	RemoveMentionGroupMember(groupID int64, identityID string) error
	GetMentionGroupMembers(groupID int64) ([]string, error)
	ExpandMentionGroups(slugs []string) (map[string][]string, error)
}

type SettingsStore interface {
	GetSetting(key string) (string, error)
	SetSetting(key, value string) error
	ListSettings() (map[string]string, error)
}

type WebhookStore interface {
	RegisterWebhook(identityID, url string) (string, error)
	UnregisterWebhook(identityID, webhookID string) error
	GetActiveWebhooksForIdentity(identityID string) ([]IdentityWebhook, error)
	// GetWebhooksForChannel returns active webhooks for all service members of a channel.
	GetWebhooksForChannel(channelID int64) ([]IdentityWebhook, error)
}

// Store is the composite interface for convenient wiring.
type Store interface {
	IdentityStore
	ChannelStore
	MessageStore
	RoleStore
	MentionGroupStore
	SettingsStore
	WebhookStore
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
