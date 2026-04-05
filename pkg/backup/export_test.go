// SPDX-License-Identifier: AGPL-3.0-or-later
package backup_test

import (
	"testing"

	"github.com/Work-Fort/sharkfin/pkg/backup"
	"github.com/Work-Fort/sharkfin/pkg/infra/sqlite"
)

func newTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	s, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestExportData(t *testing.T) {
	s := newTestStore(t)

	// --- Seed identities ---
	alice, _ := s.UpsertIdentity("uuid-alice", "alice", "Alice", "user", "user")
	bob, _ := s.UpsertIdentity("uuid-bob", "bob", "Bob", "user", "user")
	s.SetUserRole("alice", "admin")
	s.SetUserType("bob", "agent")

	// --- Seed channels (avoid "general" — already seeded by store) ---
	devID, _ := s.CreateChannel("dev", true, []string{alice.ID, bob.ID}, "channel")
	secretID, _ := s.CreateChannel("secret", false, []string{alice.ID}, "channel")

	// --- Seed messages ---
	parentID, _ := s.SendMessage(devID, alice.ID, "hello @bob", nil, []string{bob.ID}, nil)
	s.SendMessage(devID, bob.ID, "reply", &parentID, nil, nil)
	s.SendMessage(secretID, alice.ID, "secret msg", nil, nil, nil)

	// --- Seed DM ---
	s.OpenDM(alice.ID, bob.ID, "bob")
	dm, _ := s.GetChannelByName("dm-alice-bob")
	s.SendMessage(dm.ID, alice.ID, "dm msg", nil, nil, nil)

	// --- Seed settings ---
	s.SetSetting("motd", "Welcome!")

	// --- Seed custom role ---
	s.CreateRole("moderator")
	s.GrantPermission("moderator", "send_message")

	// --- Export ---
	b, err := backup.ExportData(s)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// --- Validate ---
	if b.Version != 1 {
		t.Errorf("version = %d, want 1", b.Version)
	}

	// Users
	if len(b.Users) != 2 {
		t.Fatalf("users = %d, want 2", len(b.Users))
	}
	userMap := make(map[string]backup.BackupUser)
	for _, u := range b.Users {
		userMap[u.Username] = u
	}
	if userMap["alice"].Role != "admin" {
		t.Errorf("alice role = %q, want admin", userMap["alice"].Role)
	}
	if userMap["bob"].Type != "agent" {
		t.Errorf("bob type = %q, want agent", userMap["bob"].Type)
	}

	// Channels (seeded "general" + "dev" + "secret" = 3)
	if len(b.Channels) != 3 {
		t.Fatalf("channels = %d, want 3", len(b.Channels))
	}
	chMap := make(map[string]backup.BackupChannel)
	for _, ch := range b.Channels {
		chMap[ch.Name] = ch
	}
	if !chMap["dev"].Public {
		t.Error("dev should be public")
	}
	if chMap["secret"].Public {
		t.Error("secret should not be public")
	}

	// Channel members
	if members, ok := b.ChannelMembers["dev"]; !ok || len(members) != 2 {
		t.Errorf("dev members = %v, want 2 members", members)
	}
	if members, ok := b.ChannelMembers["secret"]; !ok || len(members) != 1 {
		t.Errorf("secret members = %v, want 1 member", members)
	}

	// Messages
	if len(b.Messages) != 4 {
		t.Fatalf("messages = %d, want 4", len(b.Messages))
	}

	// Verify sequential IDs
	for i, msg := range b.Messages {
		if msg.ID != i+1 {
			t.Errorf("msg[%d].ID = %d, want %d", i, msg.ID, i+1)
		}
	}

	// Verify thread reference uses export IDs
	if b.Messages[1].ThreadID == nil {
		t.Fatal("reply should have thread_id")
	}
	if *b.Messages[1].ThreadID != b.Messages[0].ID {
		t.Errorf("reply thread_id = %d, want %d", *b.Messages[1].ThreadID, b.Messages[0].ID)
	}

	// Verify mention
	if len(b.Messages[0].Mentions) != 1 || b.Messages[0].Mentions[0] != "bob" {
		t.Errorf("msg0 mentions = %v, want [bob]", b.Messages[0].Mentions)
	}

	// Verify nil mentions are empty slices
	if b.Messages[2].Mentions == nil {
		t.Error("msg2 mentions should be empty slice, not nil")
	}

	// DMs
	if len(b.DMs) != 1 {
		t.Fatalf("dms = %d, want 1", len(b.DMs))
	}
	if b.DMs[0].ChannelName != "dm-alice-bob" {
		t.Errorf("dm channel = %q, want dm-alice-bob", b.DMs[0].ChannelName)
	}

	// Roles (built-in + moderator)
	if len(b.Roles) < 4 {
		t.Errorf("roles = %d, want >= 4 (admin, user, agent, moderator)", len(b.Roles))
	}
	foundModerator := false
	for _, r := range b.Roles {
		if r.Name == "moderator" {
			foundModerator = true
			if r.BuiltIn {
				t.Error("moderator should not be built-in")
			}
		}
	}
	if !foundModerator {
		t.Error("moderator role not found in export")
	}

	// Role permissions
	modPerms, ok := b.RolePermissions["moderator"]
	if !ok {
		t.Fatal("moderator permissions not found")
	}
	if len(modPerms) != 1 || modPerms[0] != "send_message" {
		t.Errorf("moderator perms = %v, want [send_message]", modPerms)
	}

	// Settings
	if b.Settings["motd"] != "Welcome!" {
		t.Errorf("motd = %q, want Welcome!", b.Settings["motd"])
	}
}

func TestExportDataEmptyStore(t *testing.T) {
	s := newTestStore(t)

	b, err := backup.ExportData(s)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(b.Users) != 0 {
		t.Errorf("users = %d, want 0", len(b.Users))
	}
	// Seeded "general" channel is always present
	if len(b.Channels) != 1 {
		t.Errorf("channels = %d, want 1 (seeded general)", len(b.Channels))
	}
	if len(b.Messages) != 0 {
		t.Errorf("messages = %d, want 0", len(b.Messages))
	}
}
