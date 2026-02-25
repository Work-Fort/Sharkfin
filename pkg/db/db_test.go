// SPDX-License-Identifier: GPL-2.0-only
package db

import (
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
