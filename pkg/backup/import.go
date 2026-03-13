// SPDX-License-Identifier: AGPL-3.0-or-later
package backup

import (
	"fmt"

	"github.com/google/uuid"
)

// ImportData inserts backup data into the store in dependency order.
// If force is false, it refuses to import into a non-empty database.
// If force is true and the database is non-empty, it wipes all data first.
func ImportData(s BackupStore, b *Backup, force bool) error {
	if b.Version != 1 {
		return fmt.Errorf("unsupported backup version: %d", b.Version)
	}

	empty, err := s.IsEmpty()
	if err != nil {
		return fmt.Errorf("check empty: %w", err)
	}
	if !empty {
		if !force {
			return fmt.Errorf("database is not empty; use --force to overwrite")
		}
		if err := s.WipeAll(); err != nil {
			return fmt.Errorf("wipe existing data: %w", err)
		}
	}

	// 1. Identities (formerly Users)
	identityIDByName := make(map[string]string, len(b.Users))
	for _, u := range b.Users {
		// Generate a synthetic identity ID for backup imports.
		// In production, Passport assigns these; during restore we mint fresh UUIDs.
		identityID := uuid.New().String()
		identityType := u.Type
		if identityType == "" {
			identityType = "user"
		}
		role := u.Role
		if role == "" {
			role = "user"
		}
		if err := s.UpsertIdentity(identityID, u.Username, u.Username, identityType, role); err != nil {
			return fmt.Errorf("upsert identity %q: %w", u.Username, err)
		}
		identityIDByName[u.Username] = identityID
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
		memberIDs := make([]string, 0, len(memberNames))
		for _, name := range memberNames {
			if id, ok := identityIDByName[name]; ok {
				memberIDs = append(memberIDs, id)
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
		identity1ID, ok1 := identityIDByName[dm.User1]
		identity2ID, ok2 := identityIDByName[dm.User2]
		if !ok1 || !ok2 {
			return fmt.Errorf("dm identities not found: %q, %q", dm.User1, dm.User2)
		}
		chName, _, err := s.OpenDM(identity1ID, identity2ID, dm.User2)
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
		identityID, ok := identityIDByName[msg.From]
		if !ok {
			return fmt.Errorf("identity not found for message: %q", msg.From)
		}

		var threadID *int64
		if msg.ThreadID != nil {
			if realID, ok := exportIDToRealID[*msg.ThreadID]; ok {
				threadID = &realID
			}
		}

		// Resolve mention usernames to identity IDs.
		var mentionIDs []string
		for _, mention := range msg.Mentions {
			if id, ok := identityIDByName[mention]; ok {
				mentionIDs = append(mentionIDs, id)
			}
		}

		realID, err := s.ImportMessage(chID, identityID, msg.Body, threadID, mentionIDs, msg.CreatedAt)
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
