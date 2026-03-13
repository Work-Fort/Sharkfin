// SPDX-License-Identifier: AGPL-3.0-or-later
package backup

import (
	"time"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

// BackupStore extends domain.Store with backup-specific methods.
type BackupStore interface {
	domain.Store
	ImportMessage(channelID int64, identityID string, body string, threadID *int64, mentionIdentityIDs []string, createdAt time.Time) (int64, error)
	IsEmpty() (bool, error)
	WipeAll() error
}

// Backup is the top-level JSON-serializable backup format.
type Backup struct {
	Version         int                 `json:"version"`
	ExportedAt      time.Time           `json:"exported_at"`
	Users           []BackupUser        `json:"users"`
	Channels        []BackupChannel     `json:"channels"`
	ChannelMembers  map[string][]string `json:"channel_members"`
	Messages        []BackupMessage     `json:"messages"`
	Roles           []BackupRole        `json:"roles"`
	RolePermissions map[string][]string `json:"role_permissions"`
	Settings        map[string]string   `json:"settings"`
	DMs             []BackupDM          `json:"dms"`
}

// BackupUser represents a user in the backup.
type BackupUser struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
	Type     string `json:"type"`
}

// BackupChannel represents a non-DM channel in the backup.
type BackupChannel struct {
	Name   string `json:"name"`
	Public bool   `json:"public"`
	Type   string `json:"type"`
}

// BackupMessage represents a message in the backup.
type BackupMessage struct {
	ID        int       `json:"id"`
	Channel   string    `json:"channel"`
	From      string    `json:"from"`
	Body      string    `json:"body"`
	ThreadID  *int      `json:"thread_id"`
	Mentions  []string  `json:"mentions"`
	CreatedAt time.Time `json:"created_at"`
}

// BackupRole represents a role in the backup.
type BackupRole struct {
	Name    string `json:"name"`
	BuiltIn bool   `json:"built_in"`
}

// BackupDM represents a direct message channel in the backup.
type BackupDM struct {
	User1       string `json:"user1"`
	User2       string `json:"user2"`
	ChannelName string `json:"channel_name"`
}
