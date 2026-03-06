// SPDX-License-Identifier: AGPL-3.0-or-later
package backup

import (
	"fmt"
)

// ImportData inserts backup data into the store in dependency order.
// If force is false, it refuses to import into a non-empty database.
func ImportData(s BackupStore, b *Backup, force bool) error {
	if !force {
		empty, err := s.IsEmpty()
		if err != nil {
			return fmt.Errorf("check empty: %w", err)
		}
		if !empty {
			return fmt.Errorf("database is not empty; use force=true to override")
		}
	}

	// 1. Users
	userIDByName := make(map[string]int64, len(b.Users))
	for _, u := range b.Users {
		uid, err := s.CreateUser(u.Username, u.Password)
		if err != nil {
			return fmt.Errorf("create user %q: %w", u.Username, err)
		}
		userIDByName[u.Username] = uid

		if u.Role != "" && u.Role != "user" {
			if err := s.SetUserRole(u.Username, u.Role); err != nil {
				return fmt.Errorf("set role for %q: %w", u.Username, err)
			}
		}
		if u.Type != "" && u.Type != "user" {
			if err := s.SetUserType(u.Username, u.Type); err != nil {
				return fmt.Errorf("set type for %q: %w", u.Username, err)
			}
		}
	}

	// 2. Custom roles (skip built-in, they are seeded by migrations)
	for _, r := range b.Roles {
		if r.BuiltIn {
			continue
		}
		if err := s.CreateRole(r.Name); err != nil {
			return fmt.Errorf("create role %q: %w", r.Name, err)
		}
	}

	// 3. Role permissions (grant non-default permissions)
	// First, collect the current default permissions for each role.
	defaultPerms := make(map[string]map[string]bool)
	for _, r := range b.Roles {
		existing, err := s.GetRolePermissions(r.Name)
		if err != nil {
			// Role might not exist yet if it failed; skip gracefully.
			continue
		}
		permSet := make(map[string]bool, len(existing))
		for _, p := range existing {
			permSet[p] = true
		}
		defaultPerms[r.Name] = permSet
	}

	for roleName, perms := range b.RolePermissions {
		defaults := defaultPerms[roleName]
		for _, perm := range perms {
			if defaults != nil && defaults[perm] {
				continue // already granted by migration seed
			}
			if err := s.GrantPermission(roleName, perm); err != nil {
				return fmt.Errorf("grant %q to %q: %w", perm, roleName, err)
			}
		}
	}

	// 4. Channels (non-DM)
	channelIDByName := make(map[string]int64)
	for _, ch := range b.Channels {
		memberNames := b.ChannelMembers[ch.Name]
		memberIDs := make([]int64, 0, len(memberNames))
		for _, name := range memberNames {
			if uid, ok := userIDByName[name]; ok {
				memberIDs = append(memberIDs, uid)
			}
		}
		chID, err := s.CreateChannel(ch.Name, ch.Public, memberIDs, ch.Type)
		if err != nil {
			return fmt.Errorf("create channel %q: %w", ch.Name, err)
		}
		channelIDByName[ch.Name] = chID
	}

	// 5. DMs
	for _, dm := range b.DMs {
		user1ID, ok1 := userIDByName[dm.User1]
		user2ID, ok2 := userIDByName[dm.User2]
		if !ok1 || !ok2 {
			return fmt.Errorf("dm users not found: %q, %q", dm.User1, dm.User2)
		}
		chName, _, err := s.OpenDM(user1ID, user2ID, dm.User2)
		if err != nil {
			return fmt.Errorf("open dm %q<->%q: %w", dm.User1, dm.User2, err)
		}
		// Look up the actual channel ID.
		ch, err := s.GetChannelByName(chName)
		if err != nil {
			return fmt.Errorf("get dm channel %q: %w", chName, err)
		}
		channelIDByName[dm.ChannelName] = ch.ID
	}

	// 6. Messages (in order, mapping export ID -> real ID for threads)
	exportIDToRealID := make(map[int]int64, len(b.Messages))
	for _, msg := range b.Messages {
		chID, ok := channelIDByName[msg.Channel]
		if !ok {
			return fmt.Errorf("channel not found for message: %q", msg.Channel)
		}
		userID, ok := userIDByName[msg.From]
		if !ok {
			return fmt.Errorf("user not found for message: %q", msg.From)
		}

		var threadID *int64
		if msg.ThreadID != nil {
			if realID, ok := exportIDToRealID[*msg.ThreadID]; ok {
				threadID = &realID
			}
		}

		// Resolve mention usernames to IDs.
		var mentionIDs []int64
		for _, mention := range msg.Mentions {
			if uid, ok := userIDByName[mention]; ok {
				mentionIDs = append(mentionIDs, uid)
			}
		}

		realID, err := s.ImportMessage(chID, userID, msg.Body, threadID, mentionIDs, msg.CreatedAt)
		if err != nil {
			return fmt.Errorf("import message %d: %w", msg.ID, err)
		}
		exportIDToRealID[msg.ID] = realID
	}

	// 7. Settings
	for key, value := range b.Settings {
		if err := s.SetSetting(key, value); err != nil {
			return fmt.Errorf("set setting %q: %w", key, err)
		}
	}

	return nil
}
