// SPDX-License-Identifier: GPL-2.0-only
package db

import (
	"fmt"
	"testing"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// --- Users ---

func TestCreateUser(t *testing.T) {
	d := newTestDB(t)
	id, err := d.CreateUser("alice", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive id, got %d", id)
	}
}

func TestCreateDuplicateUser(t *testing.T) {
	d := newTestDB(t)
	if _, err := d.CreateUser("alice", ""); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := d.CreateUser("alice", ""); err == nil {
		t.Error("expected error for duplicate username")
	}
}

func TestGetUserByUsername(t *testing.T) {
	d := newTestDB(t)
	d.CreateUser("alice", "")
	u, err := d.GetUserByUsername("alice")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if u.Username != "alice" {
		t.Errorf("username = %q, want alice", u.Username)
	}
}

func TestGetUserByUsernameNotFound(t *testing.T) {
	d := newTestDB(t)
	_, err := d.GetUserByUsername("nobody")
	if err == nil {
		t.Error("expected error for missing user")
	}
}

func TestListUsers(t *testing.T) {
	d := newTestDB(t)
	d.CreateUser("alice", "")
	d.CreateUser("bob", "")
	users, err := d.ListUsers()
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("len = %d, want 2", len(users))
	}
}

// --- Channels ---

func TestCreateChannel(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	bobID, _ := d.CreateUser("bob", "")

	chID, err := d.CreateChannel("general", true, []int64{aliceID, bobID})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if chID <= 0 {
		t.Errorf("expected positive id, got %d", chID)
	}
}

func TestListChannelsPublicVisibility(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	bobID, _ := d.CreateUser("bob", "")
	charlieID, _ := d.CreateUser("charlie", "")

	d.CreateChannel("public-ch", true, []int64{aliceID, bobID})

	// Charlie is not a member but should see public channels
	channels, err := d.ListChannelsForUser(charlieID)
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("len = %d, want 1", len(channels))
	}
	if channels[0].Name != "public-ch" {
		t.Errorf("name = %q, want public-ch", channels[0].Name)
	}
}

func TestListChannelsPrivateNotVisible(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	bobID, _ := d.CreateUser("bob", "")
	charlieID, _ := d.CreateUser("charlie", "")

	d.CreateChannel("secret", false, []int64{aliceID, bobID})

	// Charlie should not see private channel
	channels, err := d.ListChannelsForUser(charlieID)
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	if len(channels) != 0 {
		t.Errorf("len = %d, want 0 (private channel should be hidden)", len(channels))
	}

	// Alice should see it
	channels, err = d.ListChannelsForUser(aliceID)
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	if len(channels) != 1 {
		t.Errorf("len = %d, want 1", len(channels))
	}
}

func TestAddChannelMember(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	bobID, _ := d.CreateUser("bob", "")
	charlieID, _ := d.CreateUser("charlie", "")

	chID, _ := d.CreateChannel("private", false, []int64{aliceID, bobID})

	if err := d.AddChannelMember(chID, charlieID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	ok, err := d.IsChannelMember(chID, charlieID)
	if err != nil {
		t.Fatalf("is member: %v", err)
	}
	if !ok {
		t.Error("charlie should be a member")
	}
}

func TestIsChannelMember(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	bobID, _ := d.CreateUser("bob", "")

	chID, _ := d.CreateChannel("dm", false, []int64{aliceID})

	ok, _ := d.IsChannelMember(chID, aliceID)
	if !ok {
		t.Error("alice should be a member")
	}

	ok, _ = d.IsChannelMember(chID, bobID)
	if ok {
		t.Error("bob should not be a member")
	}
}

// --- Messages ---

func TestSendMessage(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	chID, _ := d.CreateChannel("general", true, []int64{aliceID})

	msgID, err := d.SendMessage(chID, aliceID, "hello world")
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	if msgID <= 0 {
		t.Errorf("expected positive id, got %d", msgID)
	}
}

func TestUnreadMessagesFirstRead(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	bobID, _ := d.CreateUser("bob", "")
	chID, _ := d.CreateChannel("dm", false, []int64{aliceID, bobID})

	d.SendMessage(chID, aliceID, "msg1")
	d.SendMessage(chID, aliceID, "msg2")

	msgs, err := d.GetUnreadMessages(bobID, nil)
	if err != nil {
		t.Fatalf("get unread: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len = %d, want 2", len(msgs))
	}
	if msgs[0].Body != "msg1" {
		t.Errorf("msgs[0] = %q, want msg1", msgs[0].Body)
	}
}

func TestUnreadMessagesAdvancesCursor(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	bobID, _ := d.CreateUser("bob", "")
	chID, _ := d.CreateChannel("dm", false, []int64{aliceID, bobID})

	d.SendMessage(chID, aliceID, "msg1")
	d.GetUnreadMessages(bobID, nil)

	// Second call should return nothing
	msgs, err := d.GetUnreadMessages(bobID, nil)
	if err != nil {
		t.Fatalf("get unread: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("len = %d, want 0 (cursor should have advanced)", len(msgs))
	}

	// New message should appear
	d.SendMessage(chID, aliceID, "msg2")
	msgs, err = d.GetUnreadMessages(bobID, nil)
	if err != nil {
		t.Fatalf("get unread: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len = %d, want 1", len(msgs))
	}
	if msgs[0].Body != "msg2" {
		t.Errorf("body = %q, want msg2", msgs[0].Body)
	}
}

func TestUnreadMessagesFilterByChannel(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	bobID, _ := d.CreateUser("bob", "")
	ch1, _ := d.CreateChannel("ch1", false, []int64{aliceID, bobID})
	ch2, _ := d.CreateChannel("ch2", false, []int64{aliceID, bobID})

	d.SendMessage(ch1, aliceID, "in ch1")
	d.SendMessage(ch2, aliceID, "in ch2")

	msgs, err := d.GetUnreadMessages(bobID, &ch1)
	if err != nil {
		t.Fatalf("get unread: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len = %d, want 1", len(msgs))
	}
	if msgs[0].Body != "in ch1" {
		t.Errorf("body = %q, want 'in ch1'", msgs[0].Body)
	}

	// ch2 should still have unread
	msgs, err = d.GetUnreadMessages(bobID, &ch2)
	if err != nil {
		t.Fatalf("get unread: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len = %d, want 1", len(msgs))
	}
}

// --- Settings ---

func TestSetAndGetSetting(t *testing.T) {
	d := newTestDB(t)
	if err := d.SetSetting("allow_channel_creation", "true"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	val, err := d.GetSetting("allow_channel_creation")
	if err != nil {
		t.Fatalf("get setting: %v", err)
	}
	if val != "true" {
		t.Errorf("value = %q, want true", val)
	}
}

func TestSetSettingUpsert(t *testing.T) {
	d := newTestDB(t)
	d.SetSetting("key", "v1")
	d.SetSetting("key", "v2")
	val, _ := d.GetSetting("key")
	if val != "v2" {
		t.Errorf("value = %q, want v2", val)
	}
}

func TestGetSettingNotFound(t *testing.T) {
	d := newTestDB(t)
	_, err := d.GetSetting("nonexistent")
	if err == nil {
		t.Error("expected error for missing setting")
	}
}

func TestListSettings(t *testing.T) {
	d := newTestDB(t)
	d.SetSetting("a", "1")
	d.SetSetting("b", "2")
	settings, err := d.ListSettings()
	if err != nil {
		t.Fatalf("list settings: %v", err)
	}
	if len(settings) != 2 {
		t.Fatalf("len = %d, want 2", len(settings))
	}
	if settings["a"] != "1" || settings["b"] != "2" {
		t.Errorf("settings = %v, want {a:1, b:2}", settings)
	}
}

// --- Message History ---

func TestGetMessages(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	chID, _ := d.CreateChannel("general", true, []int64{aliceID})

	for i := 0; i < 5; i++ {
		d.SendMessage(chID, aliceID, fmt.Sprintf("msg%d", i))
	}

	// Get all (no cursor)
	msgs, err := d.GetMessages(chID, nil, nil, 50)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 5 {
		t.Fatalf("len = %d, want 5", len(msgs))
	}
	// Should be oldest first
	if msgs[0].Body != "msg0" || msgs[4].Body != "msg4" {
		t.Errorf("order wrong: first=%q last=%q", msgs[0].Body, msgs[4].Body)
	}
}

func TestGetMessagesBefore(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	chID, _ := d.CreateChannel("general", true, []int64{aliceID})

	var ids []int64
	for i := 0; i < 5; i++ {
		id, _ := d.SendMessage(chID, aliceID, fmt.Sprintf("msg%d", i))
		ids = append(ids, id)
	}

	// Get messages before msg3 (should return msg0, msg1, msg2)
	msgs, err := d.GetMessages(chID, &ids[3], nil, 50)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("len = %d, want 3", len(msgs))
	}
	if msgs[2].Body != "msg2" {
		t.Errorf("last = %q, want msg2", msgs[2].Body)
	}
}

func TestGetMessagesAfter(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	chID, _ := d.CreateChannel("general", true, []int64{aliceID})

	var ids []int64
	for i := 0; i < 5; i++ {
		id, _ := d.SendMessage(chID, aliceID, fmt.Sprintf("msg%d", i))
		ids = append(ids, id)
	}

	// Get messages after msg1 (should return msg2, msg3, msg4)
	msgs, err := d.GetMessages(chID, nil, &ids[1], 50)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("len = %d, want 3", len(msgs))
	}
	if msgs[0].Body != "msg2" {
		t.Errorf("first = %q, want msg2", msgs[0].Body)
	}
}

func TestGetMessagesLimit(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	chID, _ := d.CreateChannel("general", true, []int64{aliceID})

	for i := 0; i < 10; i++ {
		d.SendMessage(chID, aliceID, fmt.Sprintf("msg%d", i))
	}

	msgs, err := d.GetMessages(chID, nil, nil, 3)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("len = %d, want 3", len(msgs))
	}
	// With no cursor, limit returns the most recent N messages (oldest first)
	if msgs[0].Body != "msg7" {
		t.Errorf("first = %q, want msg7", msgs[0].Body)
	}
}

func TestGetMessagesIncludesUsername(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	chID, _ := d.CreateChannel("general", true, []int64{aliceID})
	d.SendMessage(chID, aliceID, "hello")

	msgs, _ := d.GetMessages(chID, nil, nil, 50)
	if len(msgs) != 1 {
		t.Fatalf("len = %d, want 1", len(msgs))
	}
	if msgs[0].Username != "alice" {
		t.Errorf("username = %q, want alice", msgs[0].Username)
	}
}

func TestUnreadExcludesOwnMessages(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	bobID, _ := d.CreateUser("bob", "")
	chID, _ := d.CreateChannel("dm", false, []int64{aliceID, bobID})

	d.SendMessage(chID, aliceID, "from alice")
	d.SendMessage(chID, bobID, "from bob")
	d.SendMessage(chID, aliceID, "from alice again")

	// Bob should only see Alice's messages, not his own
	msgs, err := d.GetUnreadMessages(bobID, nil)
	if err != nil {
		t.Fatalf("get unread: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len = %d, want 2", len(msgs))
	}
	if msgs[0].Body != "from alice" || msgs[1].Body != "from alice again" {
		t.Errorf("got %q and %q, want alice's messages only", msgs[0].Body, msgs[1].Body)
	}

	// Cursor should have advanced past all 3 messages (including bob's own).
	// A new message from alice should appear, but bob's own should not reappear.
	d.SendMessage(chID, aliceID, "new from alice")
	d.SendMessage(chID, bobID, "new from bob")

	msgs, err = d.GetUnreadMessages(bobID, nil)
	if err != nil {
		t.Fatalf("get unread: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len = %d, want 1", len(msgs))
	}
	if msgs[0].Body != "new from alice" {
		t.Errorf("body = %q, want 'new from alice'", msgs[0].Body)
	}
}
