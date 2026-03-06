// SPDX-License-Identifier: AGPL-3.0-or-later
package backup_test

import (
	"strings"
	"testing"

	"github.com/Work-Fort/sharkfin/pkg/backup"
	"github.com/Work-Fort/sharkfin/pkg/infra/sqlite"
)

// populateSource sets up a source store with representative data for roundtrip testing.
func populateSource(t *testing.T, s *sqlite.Store) {
	t.Helper()

	aliceID, _ := s.CreateUser("alice", "pass1")
	bobID, _ := s.CreateUser("bob", "pass2")
	s.SetUserRole("alice", "admin")
	s.SetUserType("bob", "agent")

	genID, _ := s.CreateChannel("general", true, []int64{aliceID, bobID}, "channel")
	s.CreateChannel("secret", false, []int64{aliceID}, "channel")

	parentID, _ := s.SendMessage(genID, aliceID, "hello @bob", nil, []int64{bobID})
	s.SendMessage(genID, bobID, "reply to alice", &parentID, nil)

	s.OpenDM(aliceID, bobID, "bob")
	dm, _ := s.GetChannelByName("dm-alice-bob")
	s.SendMessage(dm.ID, aliceID, "dm message", nil, nil)

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
	dst := newTestStore(t)
	if err := backup.ImportData(dst, exported, false); err != nil {
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
		if u.Password != src.Password {
			t.Errorf("user %q password: got %q, want %q", u.Username, u.Password, src.Password)
		}
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
	s.CreateUser("existing", "")

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
	s.CreateUser("existing", "")

	b := &backup.Backup{
		Version: 1,
		Users: []backup.BackupUser{
			{Username: "new", Password: "", Role: "user", Type: "user"},
		},
	}

	// With force=true, import should succeed despite non-empty store.
	err := backup.ImportData(s, b, true)
	if err != nil {
		t.Fatalf("import with force: %v", err)
	}

	// Verify the new user was created
	u, err := s.GetUserByUsername("new")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if u.Username != "new" {
		t.Errorf("username = %q, want new", u.Username)
	}
}
