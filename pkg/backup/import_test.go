// SPDX-License-Identifier: AGPL-3.0-or-later
package backup_test

import (
	"strings"
	"testing"

	"github.com/Work-Fort/sharkfin/pkg/backup"
	"github.com/Work-Fort/sharkfin/pkg/domain"
)

// populateSource seeds a store with representative data for roundtrip testing.
// It accepts domain.Store because it only calls port methods — no adapter-specific
// behaviour is needed here.
func populateSource(t *testing.T, s domain.Store) {
	t.Helper()

	alice, _ := s.UpsertIdentity("uuid-alice", "alice", "Alice", "user", "user")
	bob, _ := s.UpsertIdentity("uuid-bob", "bob", "Bob", "user", "user")
	s.SetUserRole("alice", "admin")
	s.SetUserType("bob", "agent")

	devID, _ := s.CreateChannel("dev", true, []string{alice.ID, bob.ID}, "channel")
	s.CreateChannel("secret", false, []string{alice.ID}, "channel")

	parentID, _ := s.SendMessage(devID, alice.ID, "hello @bob", nil, []string{bob.ID}, nil)
	s.SendMessage(devID, bob.ID, "reply to alice", &parentID, nil, nil)

	s.OpenDM(alice.ID, bob.ID, "bob")
	dm, _ := s.GetChannelByName("dm-alice-bob")
	s.SendMessage(dm.ID, alice.ID, "dm message", nil, nil, nil)

	s.SetSetting("motd", "Welcome!")

	s.CreateRole("moderator")
	s.GrantPermission("moderator", "send_message")
}

func TestImportDataRoundtrip(t *testing.T) {
	// --- Export from source ---
	src := newTestStore(t)
	populateSource(t, src)

	exported, err := backup.ExportData(src)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// --- Import to destination ---
	// Seed a throwaway identity so IsEmpty() returns false, then force=true
	// triggers WipeAll() which clears the auto-seeded "general" channel.
	dst := newTestStore(t)
	dst.UpsertIdentity("throwaway", "throwaway", "Throwaway", "user", "user")
	if err := backup.ImportData(dst, exported, true); err != nil {
		t.Fatalf("import: %v", err)
	}

	// --- Re-export from destination ---
	reimported, err := backup.ExportData(dst)
	if err != nil {
		t.Fatalf("re-export: %v", err)
	}

	// --- Compare ---

	// Users
	if len(reimported.Users) != len(exported.Users) {
		t.Fatalf("users: got %d, want %d", len(reimported.Users), len(exported.Users))
	}
	srcUserMap := make(map[string]backup.BackupUser)
	for _, u := range exported.Users {
		srcUserMap[u.Username] = u
	}
	for _, u := range reimported.Users {
		src := srcUserMap[u.Username]
		if u.Role != src.Role {
			t.Errorf("user %q role: got %q, want %q", u.Username, u.Role, src.Role)
		}
		if u.Type != src.Type {
			t.Errorf("user %q type: got %q, want %q", u.Username, u.Type, src.Type)
		}
	}

	// Channels
	if len(reimported.Channels) != len(exported.Channels) {
		t.Fatalf("channels: got %d, want %d", len(reimported.Channels), len(exported.Channels))
	}
	srcChMap := make(map[string]backup.BackupChannel)
	for _, ch := range exported.Channels {
		srcChMap[ch.Name] = ch
	}
	for _, ch := range reimported.Channels {
		src := srcChMap[ch.Name]
		if ch.Public != src.Public {
			t.Errorf("channel %q public: got %v, want %v", ch.Name, ch.Public, src.Public)
		}
	}

	// Channel members
	for name, srcMembers := range exported.ChannelMembers {
		dstMembers, ok := reimported.ChannelMembers[name]
		if !ok {
			t.Errorf("channel %q members missing in reimport", name)
			continue
		}
		if len(dstMembers) != len(srcMembers) {
			t.Errorf("channel %q members: got %d, want %d", name, len(dstMembers), len(srcMembers))
		}
	}

	// DMs
	if len(reimported.DMs) != len(exported.DMs) {
		t.Fatalf("dms: got %d, want %d", len(reimported.DMs), len(exported.DMs))
	}

	// Messages
	if len(reimported.Messages) != len(exported.Messages) {
		t.Fatalf("messages: got %d, want %d", len(reimported.Messages), len(exported.Messages))
	}
	for i, srcMsg := range exported.Messages {
		dstMsg := reimported.Messages[i]
		if dstMsg.Channel != srcMsg.Channel {
			t.Errorf("msg[%d] channel: got %q, want %q", i, dstMsg.Channel, srcMsg.Channel)
		}
		if dstMsg.From != srcMsg.From {
			t.Errorf("msg[%d] from: got %q, want %q", i, dstMsg.From, srcMsg.From)
		}
		if dstMsg.Body != srcMsg.Body {
			t.Errorf("msg[%d] body: got %q, want %q", i, dstMsg.Body, srcMsg.Body)
		}
		if !dstMsg.CreatedAt.Equal(srcMsg.CreatedAt) {
			t.Errorf("msg[%d] created_at: got %v, want %v", i, dstMsg.CreatedAt, srcMsg.CreatedAt)
		}

		// Thread references
		if (srcMsg.ThreadID == nil) != (dstMsg.ThreadID == nil) {
			t.Errorf("msg[%d] thread_id nil mismatch: src=%v dst=%v", i, srcMsg.ThreadID, dstMsg.ThreadID)
		} else if srcMsg.ThreadID != nil && *srcMsg.ThreadID != *dstMsg.ThreadID {
			t.Errorf("msg[%d] thread_id: got %d, want %d", i, *dstMsg.ThreadID, *srcMsg.ThreadID)
		}

		// Mentions
		if len(dstMsg.Mentions) != len(srcMsg.Mentions) {
			t.Errorf("msg[%d] mentions: got %v, want %v", i, dstMsg.Mentions, srcMsg.Mentions)
		}
	}

	// Roles
	srcRoleMap := make(map[string]backup.BackupRole)
	for _, r := range exported.Roles {
		srcRoleMap[r.Name] = r
	}
	for _, r := range reimported.Roles {
		if src, ok := srcRoleMap[r.Name]; ok {
			if r.BuiltIn != src.BuiltIn {
				t.Errorf("role %q built_in: got %v, want %v", r.Name, r.BuiltIn, src.BuiltIn)
			}
		}
	}

	// Role permissions for moderator
	srcModPerms := exported.RolePermissions["moderator"]
	dstModPerms := reimported.RolePermissions["moderator"]
	if len(dstModPerms) != len(srcModPerms) {
		t.Errorf("moderator perms: got %v, want %v", dstModPerms, srcModPerms)
	}

	// Settings
	if reimported.Settings["motd"] != exported.Settings["motd"] {
		t.Errorf("motd: got %q, want %q", reimported.Settings["motd"], exported.Settings["motd"])
	}
}

func TestImportDataNonEmptyStoreRefused(t *testing.T) {
	s := newTestStore(t)
	s.UpsertIdentity("uuid-existing", "existing", "Existing", "user", "user")

	b := &backup.Backup{
		Version: 1,
		Users: []backup.BackupUser{
			{Username: "new", Password: "", Role: "user", Type: "user"},
		},
	}

	err := backup.ImportData(s, b, false)
	if err == nil {
		t.Fatal("expected error for non-empty store")
	}
	if !strings.Contains(err.Error(), "not empty") {
		t.Errorf("error = %q, want to contain 'not empty'", err.Error())
	}
}

func TestImportDataForceOverride(t *testing.T) {
	s := newTestStore(t)
	// Pre-populate with a user that will CONFLICT with the backup data.
	s.UpsertIdentity("uuid-alice-old", "alice", "Alice Old", "user", "user")

	b := &backup.Backup{
		Version: 1,
		Users: []backup.BackupUser{
			{Username: "alice", Password: "", Role: "admin", Type: "user"},
			{Username: "bob", Password: "", Role: "user", Type: "user"},
		},
	}

	// With force=true, import should wipe existing data and succeed.
	err := backup.ImportData(s, b, true)
	if err != nil {
		t.Fatalf("import with force: %v", err)
	}

	// Verify alice was recreated with backup data (not old data).
	alice, err := s.GetIdentityByUsername("alice")
	if err != nil {
		t.Fatalf("get alice: %v", err)
	}
	if alice.Role != "admin" {
		t.Errorf("alice role = %q, want admin", alice.Role)
	}

	// Verify bob was also created.
	bob, err := s.GetIdentityByUsername("bob")
	if err != nil {
		t.Fatalf("get bob: %v", err)
	}
	if bob.Username != "bob" {
		t.Errorf("username = %q, want bob", bob.Username)
	}
}
