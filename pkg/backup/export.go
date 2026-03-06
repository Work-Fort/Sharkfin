// SPDX-License-Identifier: AGPL-3.0-or-later
package backup

import (
	"fmt"
	"time"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

// ExportData reads all data from the store and returns a Backup struct.
func ExportData(s domain.Store) (*Backup, error) {
	b := &Backup{
		Version:         1,
		ExportedAt:      time.Now().UTC(),
		ChannelMembers:  make(map[string][]string),
		RolePermissions: make(map[string][]string),
		Settings:        make(map[string]string),
	}

	// --- Users ---
	users, err := s.ListUsers()
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	for _, u := range users {
		b.Users = append(b.Users, BackupUser{
			Username: u.Username,
			Password: u.Password,
			Role:     u.Role,
			Type:     u.Type,
		})
	}

	// --- Channels ---
	channels, err := s.ListAllChannelsWithMembership(0)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	for _, ch := range channels {
		b.Channels = append(b.Channels, BackupChannel{
			Name:   ch.Name,
			Public: ch.Public,
			Type:   ch.Type,
		})

		members, err := s.ChannelMemberUsernames(ch.ID)
		if err != nil {
			return nil, fmt.Errorf("channel members %q: %w", ch.Name, err)
		}
		if members == nil {
			members = []string{}
		}
		b.ChannelMembers[ch.Name] = members
	}

	// --- DMs ---
	dms, err := s.ListAllDMs()
	if err != nil {
		return nil, fmt.Errorf("list dms: %w", err)
	}
	for _, dm := range dms {
		b.DMs = append(b.DMs, BackupDM{
			User1:       dm.User1Username,
			User2:       dm.User2Username,
			ChannelName: dm.ChannelName,
		})
	}

	// --- Messages ---
	// Build a username map for looking up user info by ID.
	usernameByID := make(map[int64]string, len(users))
	for _, u := range users {
		usernameByID[u.ID] = u.Username
	}

	// Build a channel name map for all channels (including DMs).
	channelNameByID := make(map[int64]string)
	for _, ch := range channels {
		channelNameByID[ch.ID] = ch.Name
	}
	for _, dm := range dms {
		channelNameByID[dm.ChannelID] = dm.ChannelName
	}

	// Collect all channel IDs to export messages from.
	var allChannelIDs []int64
	for _, ch := range channels {
		allChannelIDs = append(allChannelIDs, ch.ID)
	}
	for _, dm := range dms {
		allChannelIDs = append(allChannelIDs, dm.ChannelID)
	}

	// Map from DB message ID to sequential export ID.
	dbIDToExportID := make(map[int64]int)
	exportSeq := 1

	for _, chID := range allChannelIDs {
		var afterID int64
		for {
			msgs, err := s.GetMessages(chID, nil, &afterID, 100, nil)
			if err != nil {
				return nil, fmt.Errorf("get messages ch=%d: %w", chID, err)
			}
			if len(msgs) == 0 {
				break
			}
			for _, m := range msgs {
				exportID := exportSeq
				exportSeq++
				dbIDToExportID[m.ID] = exportID

				var threadExportID *int
				if m.ThreadID != nil {
					if tid, ok := dbIDToExportID[*m.ThreadID]; ok {
						threadExportID = &tid
					}
				}

				mentions := m.Mentions
				if mentions == nil {
					mentions = []string{}
				}

				b.Messages = append(b.Messages, BackupMessage{
					ID:        exportID,
					Channel:   channelNameByID[m.ChannelID],
					From:      m.From,
					Body:      m.Body,
					ThreadID:  threadExportID,
					Mentions:  mentions,
					CreatedAt: m.CreatedAt,
				})
				afterID = m.ID
			}
		}
	}

	// --- Roles ---
	roles, err := s.ListRoles()
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	for _, r := range roles {
		b.Roles = append(b.Roles, BackupRole{
			Name:    r.Name,
			BuiltIn: r.BuiltIn,
		})
		perms, err := s.GetRolePermissions(r.Name)
		if err != nil {
			return nil, fmt.Errorf("role permissions %q: %w", r.Name, err)
		}
		if perms == nil {
			perms = []string{}
		}
		b.RolePermissions[r.Name] = perms
	}

	// --- Settings ---
	settings, err := s.ListSettings()
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	b.Settings = settings

	return b, nil
}
