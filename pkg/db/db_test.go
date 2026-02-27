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

	chID, err := d.CreateChannel("general", true, []int64{aliceID, bobID}, "channel")
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

	d.CreateChannel("public-ch", true, []int64{aliceID, bobID}, "channel")

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

	d.CreateChannel("secret", false, []int64{aliceID, bobID}, "channel")

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

	chID, _ := d.CreateChannel("private", false, []int64{aliceID, bobID}, "channel")

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

	chID, _ := d.CreateChannel("dm", false, []int64{aliceID}, "channel")

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
	chID, _ := d.CreateChannel("general", true, []int64{aliceID}, "channel")

	msgID, err := d.SendMessage(chID, aliceID, "hello world", nil, nil)
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
	chID, _ := d.CreateChannel("dm", false, []int64{aliceID, bobID}, "channel")

	d.SendMessage(chID, aliceID, "msg1", nil, nil)
	d.SendMessage(chID, aliceID, "msg2", nil, nil)

	msgs, err := d.GetUnreadMessages(bobID, nil, false, nil)
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
	chID, _ := d.CreateChannel("dm", false, []int64{aliceID, bobID}, "channel")

	d.SendMessage(chID, aliceID, "msg1", nil, nil)
	d.GetUnreadMessages(bobID, nil, false, nil)

	// Second call should return nothing
	msgs, err := d.GetUnreadMessages(bobID, nil, false, nil)
	if err != nil {
		t.Fatalf("get unread: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("len = %d, want 0 (cursor should have advanced)", len(msgs))
	}

	// New message should appear
	d.SendMessage(chID, aliceID, "msg2", nil, nil)
	msgs, err = d.GetUnreadMessages(bobID, nil, false, nil)
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
	ch1, _ := d.CreateChannel("ch1", false, []int64{aliceID, bobID}, "channel")
	ch2, _ := d.CreateChannel("ch2", false, []int64{aliceID, bobID}, "channel")

	d.SendMessage(ch1, aliceID, "in ch1", nil, nil)
	d.SendMessage(ch2, aliceID, "in ch2", nil, nil)

	msgs, err := d.GetUnreadMessages(bobID, &ch1, false, nil)
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
	msgs, err = d.GetUnreadMessages(bobID, &ch2, false, nil)
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
	chID, _ := d.CreateChannel("general", true, []int64{aliceID}, "channel")

	for i := 0; i < 5; i++ {
		d.SendMessage(chID, aliceID, fmt.Sprintf("msg%d", i), nil, nil)
	}

	// Get all (no cursor)
	msgs, err := d.GetMessages(chID, nil, nil, 50, nil)
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
	chID, _ := d.CreateChannel("general", true, []int64{aliceID}, "channel")

	var ids []int64
	for i := 0; i < 5; i++ {
		id, _ := d.SendMessage(chID, aliceID, fmt.Sprintf("msg%d", i), nil, nil)
		ids = append(ids, id)
	}

	// Get messages before msg3 (should return msg0, msg1, msg2)
	msgs, err := d.GetMessages(chID, &ids[3], nil, 50, nil)
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
	chID, _ := d.CreateChannel("general", true, []int64{aliceID}, "channel")

	var ids []int64
	for i := 0; i < 5; i++ {
		id, _ := d.SendMessage(chID, aliceID, fmt.Sprintf("msg%d", i), nil, nil)
		ids = append(ids, id)
	}

	// Get messages after msg1 (should return msg2, msg3, msg4)
	msgs, err := d.GetMessages(chID, nil, &ids[1], 50, nil)
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
	chID, _ := d.CreateChannel("general", true, []int64{aliceID}, "channel")

	for i := 0; i < 10; i++ {
		d.SendMessage(chID, aliceID, fmt.Sprintf("msg%d", i), nil, nil)
	}

	msgs, err := d.GetMessages(chID, nil, nil, 3, nil)
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
	chID, _ := d.CreateChannel("general", true, []int64{aliceID}, "channel")
	d.SendMessage(chID, aliceID, "hello", nil, nil)

	msgs, _ := d.GetMessages(chID, nil, nil, 50, nil)
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
	chID, _ := d.CreateChannel("dm", false, []int64{aliceID, bobID}, "channel")

	d.SendMessage(chID, aliceID, "from alice", nil, nil)
	d.SendMessage(chID, bobID, "from bob", nil, nil)
	d.SendMessage(chID, aliceID, "from alice again", nil, nil)

	// Bob should only see Alice's messages, not his own
	msgs, err := d.GetUnreadMessages(bobID, nil, false, nil)
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
	d.SendMessage(chID, aliceID, "new from alice", nil, nil)
	d.SendMessage(chID, bobID, "new from bob", nil, nil)

	msgs, err = d.GetUnreadMessages(bobID, nil, false, nil)
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

// --- Threads ---

func TestSendMessageWithThread(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	chID, _ := d.CreateChannel("general", true, []int64{aliceID}, "channel")

	parentID, _ := d.SendMessage(chID, aliceID, "parent message", nil, nil)
	replyID, err := d.SendMessage(chID, aliceID, "reply message", &parentID, nil)
	if err != nil {
		t.Fatalf("send reply: %v", err)
	}
	if replyID <= 0 {
		t.Errorf("expected positive reply id, got %d", replyID)
	}
}

func TestSendMessageRejectNestedReply(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	chID, _ := d.CreateChannel("general", true, []int64{aliceID}, "channel")

	parentID, _ := d.SendMessage(chID, aliceID, "parent", nil, nil)
	replyID, _ := d.SendMessage(chID, aliceID, "reply", &parentID, nil)
	_, err := d.SendMessage(chID, aliceID, "nested reply", &replyID, nil)
	if err == nil {
		t.Error("expected error for nested reply")
	}
}

func TestSendMessageRejectCrossChannelThread(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	ch1, _ := d.CreateChannel("ch1", true, []int64{aliceID}, "channel")
	ch2, _ := d.CreateChannel("ch2", true, []int64{aliceID}, "channel")

	parentID, _ := d.SendMessage(ch1, aliceID, "parent in ch1", nil, nil)
	_, err := d.SendMessage(ch2, aliceID, "reply in ch2", &parentID, nil)
	if err == nil {
		t.Error("expected error for cross-channel thread")
	}
}

func TestGetMessagesThreadFilter(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	chID, _ := d.CreateChannel("general", true, []int64{aliceID}, "channel")

	parentID, _ := d.SendMessage(chID, aliceID, "parent", nil, nil)
	d.SendMessage(chID, aliceID, "reply1", &parentID, nil)
	d.SendMessage(chID, aliceID, "reply2", &parentID, nil)
	d.SendMessage(chID, aliceID, "unrelated", nil, nil)

	msgs, err := d.GetMessages(chID, nil, nil, 50, &parentID)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 thread replies, got %d", len(msgs))
	}
	if msgs[0].Body != "reply1" || msgs[1].Body != "reply2" {
		t.Errorf("got %q and %q, want reply1 and reply2", msgs[0].Body, msgs[1].Body)
	}
}

// --- Mentions ---

func TestSendMessageWithMentions(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	bobID, _ := d.CreateUser("bob", "")
	chID, _ := d.CreateChannel("general", true, []int64{aliceID, bobID}, "channel")

	_, err := d.SendMessage(chID, aliceID, "hey @bob", nil, []int64{bobID})
	if err != nil {
		t.Fatalf("send message with mention: %v", err)
	}

	msgs, _ := d.GetMessages(chID, nil, nil, 50, nil)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if len(msgs[0].Mentions) != 1 || msgs[0].Mentions[0] != "bob" {
		t.Errorf("mentions = %v, want [bob]", msgs[0].Mentions)
	}
}

func TestUnreadMessagesMentionsOnly(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	bobID, _ := d.CreateUser("bob", "")
	chID, _ := d.CreateChannel("dm", false, []int64{aliceID, bobID}, "channel")

	d.SendMessage(chID, aliceID, "no mention", nil, nil)
	d.SendMessage(chID, aliceID, "hey @bob", nil, []int64{bobID})

	msgs, err := d.GetUnreadMessages(bobID, nil, true, nil)
	if err != nil {
		t.Fatalf("get unread: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 mention, got %d", len(msgs))
	}
	if msgs[0].Body != "hey @bob" {
		t.Errorf("body = %q, want 'hey @bob'", msgs[0].Body)
	}
}

func TestUnreadMessagesThreadFilter(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	bobID, _ := d.CreateUser("bob", "")
	chID, _ := d.CreateChannel("dm", false, []int64{aliceID, bobID}, "channel")

	parentID, _ := d.SendMessage(chID, aliceID, "parent", nil, nil)
	d.SendMessage(chID, aliceID, "reply", &parentID, nil)
	d.SendMessage(chID, aliceID, "top-level", nil, nil)

	// Read all first to advance cursor
	d.GetUnreadMessages(bobID, nil, false, nil)

	// Now send new messages
	d.SendMessage(chID, aliceID, "new reply", &parentID, nil)
	d.SendMessage(chID, aliceID, "new top-level", nil, nil)

	// Filter by thread — should not advance cursor
	msgs, err := d.GetUnreadMessages(bobID, nil, false, &parentID)
	if err != nil {
		t.Fatalf("get unread: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 thread reply, got %d", len(msgs))
	}
	if msgs[0].Body != "new reply" {
		t.Errorf("body = %q, want 'new reply'", msgs[0].Body)
	}
}

// --- DMs ---

func TestOpenDMCreatesAndFinds(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	bobID, _ := d.CreateUser("bob", "")

	// First call creates
	name, created, err := d.OpenDM(aliceID, bobID, "bob")
	if err != nil {
		t.Fatalf("open dm: %v", err)
	}
	if !created {
		t.Error("expected created=true on first call")
	}
	if name != "dm-alice-bob" {
		t.Errorf("name = %q, want dm-alice-bob", name)
	}

	// Second call finds existing
	name2, created2, err := d.OpenDM(bobID, aliceID, "alice")
	if err != nil {
		t.Fatalf("open dm again: %v", err)
	}
	if created2 {
		t.Error("expected created=false on second call")
	}
	if name2 != name {
		t.Errorf("name = %q, want %q", name2, name)
	}
}

func TestListDMsForUser(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	bobID, _ := d.CreateUser("bob", "")
	charlieID, _ := d.CreateUser("charlie", "")

	d.OpenDM(aliceID, bobID, "bob")
	d.OpenDM(aliceID, charlieID, "charlie")

	dms, err := d.ListDMsForUser(aliceID)
	if err != nil {
		t.Fatalf("list dms: %v", err)
	}
	if len(dms) != 2 {
		t.Fatalf("expected 2 dms, got %d", len(dms))
	}
	// Ordered by participant username
	if dms[0].Participant != "bob" || dms[1].Participant != "charlie" {
		t.Errorf("participants = [%s, %s], want [bob, charlie]", dms[0].Participant, dms[1].Participant)
	}

	// Bob should only see one DM
	bobDMs, err := d.ListDMsForUser(bobID)
	if err != nil {
		t.Fatalf("list bob dms: %v", err)
	}
	if len(bobDMs) != 1 {
		t.Fatalf("expected 1 dm for bob, got %d", len(bobDMs))
	}
	if bobDMs[0].Participant != "alice" {
		t.Errorf("participant = %q, want alice", bobDMs[0].Participant)
	}
}

func TestChannelListExcludesDMs(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	bobID, _ := d.CreateUser("bob", "")

	d.CreateChannel("general", true, []int64{aliceID, bobID}, "channel")
	d.OpenDM(aliceID, bobID, "bob")

	channels, err := d.ListChannelsForUser(aliceID)
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	for _, ch := range channels {
		if ch.Type == "dm" {
			t.Errorf("channel_list should not include DMs, got %q", ch.Name)
		}
	}
	if len(channels) != 1 {
		t.Errorf("expected 1 channel, got %d", len(channels))
	}
}

func TestUnreadCountsIncludesType(t *testing.T) {
	d := newTestDB(t)
	aliceID, _ := d.CreateUser("alice", "")
	bobID, _ := d.CreateUser("bob", "")

	d.CreateChannel("general", true, []int64{aliceID, bobID}, "channel")
	d.OpenDM(aliceID, bobID, "bob")

	ch, _ := d.GetChannelByName("general")
	dm, _ := d.GetChannelByName("dm-alice-bob")

	d.SendMessage(ch.ID, aliceID, "hello channel", nil, nil)
	d.SendMessage(dm.ID, aliceID, "hello dm", nil, nil)

	counts, err := d.GetUnreadCounts(bobID)
	if err != nil {
		t.Fatalf("get unread counts: %v", err)
	}
	if len(counts) != 2 {
		t.Fatalf("expected 2 counts, got %d", len(counts))
	}

	typeMap := make(map[string]string)
	for _, c := range counts {
		typeMap[c.ChannelName] = c.ChannelType
	}
	if typeMap["general"] != "channel" {
		t.Errorf("general type = %q, want channel", typeMap["general"])
	}
	if typeMap["dm-alice-bob"] != "dm" {
		t.Errorf("dm type = %q, want dm", typeMap["dm-alice-bob"])
	}
}
