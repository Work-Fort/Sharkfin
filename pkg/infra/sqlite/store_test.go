// SPDX-License-Identifier: AGPL-3.0-or-later
package sqlite

import (
	"fmt"
	"testing"
	"time"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// --- Users ---

func TestCreateUser(t *testing.T) {
	s := newTestStore(t)
	id, err := s.CreateUser("alice", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive id, got %d", id)
	}
}

func TestCreateDuplicateUser(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateUser("alice", ""); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := s.CreateUser("alice", ""); err == nil {
		t.Error("expected error for duplicate username")
	}
}

func TestGetUserByUsername(t *testing.T) {
	s := newTestStore(t)
	s.CreateUser("alice", "")
	u, err := s.GetUserByUsername("alice")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if u.Username != "alice" {
		t.Errorf("username = %q, want alice", u.Username)
	}
}

func TestGetUserByUsernameNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetUserByUsername("nobody")
	if err == nil {
		t.Error("expected error for missing user")
	}
}

func TestListUsers(t *testing.T) {
	s := newTestStore(t)
	s.CreateUser("alice", "")
	s.CreateUser("bob", "")
	users, err := s.ListUsers()
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("len = %d, want 2", len(users))
	}
}

// --- Channels ---

func TestCreateChannel(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")

	chID, err := s.CreateChannel("general", true, []int64{aliceID, bobID}, "channel")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if chID <= 0 {
		t.Errorf("expected positive id, got %d", chID)
	}
}

func TestGetChannelByID(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	chID, _ := s.CreateChannel("general", true, []int64{aliceID}, "channel")

	ch, err := s.GetChannelByID(chID)
	if err != nil {
		t.Fatalf("get channel: %v", err)
	}
	if ch.Name != "general" {
		t.Errorf("name = %q, want general", ch.Name)
	}
	if !ch.Public {
		t.Error("expected public=true")
	}
	if ch.Type != "channel" {
		t.Errorf("type = %q, want channel", ch.Type)
	}
}

func TestGetChannelByName(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	s.CreateChannel("general", true, []int64{aliceID}, "channel")

	ch, err := s.GetChannelByName("general")
	if err != nil {
		t.Fatalf("get channel: %v", err)
	}
	if ch.Name != "general" {
		t.Errorf("name = %q, want general", ch.Name)
	}
}

func TestGetChannelByNameNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetChannelByName("nonexistent")
	if err == nil {
		t.Error("expected error for missing channel")
	}
}

func TestListChannelsPublicVisibility(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")
	charlieID, _ := s.CreateUser("charlie", "")

	s.CreateChannel("public-ch", true, []int64{aliceID, bobID}, "channel")

	// Charlie is not a member but should see public channels
	channels, err := s.ListChannelsForUser(charlieID)
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("len = %d, want 1", len(channels))
	}
	if channels[0].Name != "public-ch" {
		t.Errorf("name = %q, want public-ch", channels[0].Name)
	}
	if channels[0].Member {
		t.Error("charlie should not be a member")
	}
}

func TestListChannelsPrivateNotVisible(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")
	charlieID, _ := s.CreateUser("charlie", "")

	s.CreateChannel("secret", false, []int64{aliceID, bobID}, "channel")

	// Charlie should not see private channel
	channels, err := s.ListChannelsForUser(charlieID)
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	if len(channels) != 0 {
		t.Errorf("len = %d, want 0 (private channel should be hidden)", len(channels))
	}

	// Alice should see it
	channels, err = s.ListChannelsForUser(aliceID)
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	if len(channels) != 1 {
		t.Errorf("len = %d, want 1", len(channels))
	}
	if !channels[0].Member {
		t.Error("alice should be a member")
	}
}

func TestListAllChannelsWithMembership(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")

	s.CreateChannel("public-ch", true, []int64{aliceID}, "channel")
	s.CreateChannel("secret", false, []int64{aliceID}, "channel")

	channels, err := s.ListAllChannelsWithMembership(bobID)
	if err != nil {
		t.Fatalf("list all channels: %v", err)
	}
	if len(channels) != 2 {
		t.Fatalf("len = %d, want 2", len(channels))
	}
}

func TestAddChannelMember(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")
	charlieID, _ := s.CreateUser("charlie", "")

	chID, _ := s.CreateChannel("private", false, []int64{aliceID, bobID}, "channel")

	if err := s.AddChannelMember(chID, charlieID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	ok, err := s.IsChannelMember(chID, charlieID)
	if err != nil {
		t.Fatalf("is member: %v", err)
	}
	if !ok {
		t.Error("charlie should be a member")
	}
}

func TestIsChannelMember(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")

	chID, _ := s.CreateChannel("ch", false, []int64{aliceID}, "channel")

	ok, _ := s.IsChannelMember(chID, aliceID)
	if !ok {
		t.Error("alice should be a member")
	}

	ok, _ = s.IsChannelMember(chID, bobID)
	if ok {
		t.Error("bob should not be a member")
	}
}

func TestChannelMemberUsernames(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")

	chID, _ := s.CreateChannel("general", true, []int64{aliceID, bobID}, "channel")

	names, err := s.ChannelMemberUsernames(chID)
	if err != nil {
		t.Fatalf("channel member usernames: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("len = %d, want 2", len(names))
	}
	if names[0] != "alice" || names[1] != "bob" {
		t.Errorf("names = %v, want [alice bob]", names)
	}
}

// --- Messages ---

func TestSendMessage(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	chID, _ := s.CreateChannel("general", true, []int64{aliceID}, "channel")

	msgID, err := s.SendMessage(chID, aliceID, "hello world", nil, nil)
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	if msgID <= 0 {
		t.Errorf("expected positive id, got %d", msgID)
	}
}

func TestGetMessages(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	chID, _ := s.CreateChannel("general", true, []int64{aliceID}, "channel")

	for i := 0; i < 5; i++ {
		s.SendMessage(chID, aliceID, fmt.Sprintf("msg%d", i), nil, nil)
	}

	msgs, err := s.GetMessages(chID, nil, nil, 50, nil)
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
	// Verify From field is populated
	if msgs[0].From != "alice" {
		t.Errorf("from = %q, want alice", msgs[0].From)
	}
}

func TestGetMessagesBefore(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	chID, _ := s.CreateChannel("general", true, []int64{aliceID}, "channel")

	var ids []int64
	for i := 0; i < 5; i++ {
		id, _ := s.SendMessage(chID, aliceID, fmt.Sprintf("msg%d", i), nil, nil)
		ids = append(ids, id)
	}

	msgs, err := s.GetMessages(chID, &ids[3], nil, 50, nil)
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
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	chID, _ := s.CreateChannel("general", true, []int64{aliceID}, "channel")

	var ids []int64
	for i := 0; i < 5; i++ {
		id, _ := s.SendMessage(chID, aliceID, fmt.Sprintf("msg%d", i), nil, nil)
		ids = append(ids, id)
	}

	msgs, err := s.GetMessages(chID, nil, &ids[1], 50, nil)
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
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	chID, _ := s.CreateChannel("general", true, []int64{aliceID}, "channel")

	for i := 0; i < 10; i++ {
		s.SendMessage(chID, aliceID, fmt.Sprintf("msg%d", i), nil, nil)
	}

	msgs, err := s.GetMessages(chID, nil, nil, 3, nil)
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

func TestUnreadMessagesFirstRead(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")
	chID, _ := s.CreateChannel("dm", false, []int64{aliceID, bobID}, "channel")

	s.SendMessage(chID, aliceID, "msg1", nil, nil)
	s.SendMessage(chID, aliceID, "msg2", nil, nil)

	msgs, err := s.GetUnreadMessages(bobID, nil, false, nil)
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
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")
	chID, _ := s.CreateChannel("dm", false, []int64{aliceID, bobID}, "channel")

	s.SendMessage(chID, aliceID, "msg1", nil, nil)
	s.GetUnreadMessages(bobID, nil, false, nil)

	// Second call should return nothing
	msgs, err := s.GetUnreadMessages(bobID, nil, false, nil)
	if err != nil {
		t.Fatalf("get unread: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("len = %d, want 0 (cursor should have advanced)", len(msgs))
	}

	// New message should appear
	s.SendMessage(chID, aliceID, "msg2", nil, nil)
	msgs, err = s.GetUnreadMessages(bobID, nil, false, nil)
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
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")
	ch1, _ := s.CreateChannel("ch1", false, []int64{aliceID, bobID}, "channel")
	ch2, _ := s.CreateChannel("ch2", false, []int64{aliceID, bobID}, "channel")

	s.SendMessage(ch1, aliceID, "in ch1", nil, nil)
	s.SendMessage(ch2, aliceID, "in ch2", nil, nil)

	msgs, err := s.GetUnreadMessages(bobID, &ch1, false, nil)
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
	msgs, err = s.GetUnreadMessages(bobID, &ch2, false, nil)
	if err != nil {
		t.Fatalf("get unread: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len = %d, want 1", len(msgs))
	}
}

func TestUnreadExcludesOwnMessages(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")
	chID, _ := s.CreateChannel("dm", false, []int64{aliceID, bobID}, "channel")

	s.SendMessage(chID, aliceID, "from alice", nil, nil)
	s.SendMessage(chID, bobID, "from bob", nil, nil)
	s.SendMessage(chID, aliceID, "from alice again", nil, nil)

	msgs, err := s.GetUnreadMessages(bobID, nil, false, nil)
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
	s.SendMessage(chID, aliceID, "new from alice", nil, nil)
	s.SendMessage(chID, bobID, "new from bob", nil, nil)

	msgs, err = s.GetUnreadMessages(bobID, nil, false, nil)
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
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	chID, _ := s.CreateChannel("general", true, []int64{aliceID}, "channel")

	parentID, _ := s.SendMessage(chID, aliceID, "parent message", nil, nil)
	replyID, err := s.SendMessage(chID, aliceID, "reply message", &parentID, nil)
	if err != nil {
		t.Fatalf("send reply: %v", err)
	}
	if replyID <= 0 {
		t.Errorf("expected positive reply id, got %d", replyID)
	}
}

func TestSendMessageRejectNestedReply(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	chID, _ := s.CreateChannel("general", true, []int64{aliceID}, "channel")

	parentID, _ := s.SendMessage(chID, aliceID, "parent", nil, nil)
	replyID, _ := s.SendMessage(chID, aliceID, "reply", &parentID, nil)
	_, err := s.SendMessage(chID, aliceID, "nested reply", &replyID, nil)
	if err == nil {
		t.Error("expected error for nested reply")
	}
}

func TestSendMessageRejectCrossChannelThread(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	ch1, _ := s.CreateChannel("ch1", true, []int64{aliceID}, "channel")
	ch2, _ := s.CreateChannel("ch2", true, []int64{aliceID}, "channel")

	parentID, _ := s.SendMessage(ch1, aliceID, "parent in ch1", nil, nil)
	_, err := s.SendMessage(ch2, aliceID, "reply in ch2", &parentID, nil)
	if err == nil {
		t.Error("expected error for cross-channel thread")
	}
}

func TestGetMessagesThreadFilter(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	chID, _ := s.CreateChannel("general", true, []int64{aliceID}, "channel")

	parentID, _ := s.SendMessage(chID, aliceID, "parent", nil, nil)
	s.SendMessage(chID, aliceID, "reply1", &parentID, nil)
	s.SendMessage(chID, aliceID, "reply2", &parentID, nil)
	s.SendMessage(chID, aliceID, "unrelated", nil, nil)

	msgs, err := s.GetMessages(chID, nil, nil, 50, &parentID)
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
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")
	chID, _ := s.CreateChannel("general", true, []int64{aliceID, bobID}, "channel")

	_, err := s.SendMessage(chID, aliceID, "hey @bob", nil, []int64{bobID})
	if err != nil {
		t.Fatalf("send message with mention: %v", err)
	}

	msgs, _ := s.GetMessages(chID, nil, nil, 50, nil)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if len(msgs[0].Mentions) != 1 || msgs[0].Mentions[0] != "bob" {
		t.Errorf("mentions = %v, want [bob]", msgs[0].Mentions)
	}
}

func TestUnreadMessagesMentionsOnly(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")
	chID, _ := s.CreateChannel("dm", false, []int64{aliceID, bobID}, "channel")

	s.SendMessage(chID, aliceID, "no mention", nil, nil)
	s.SendMessage(chID, aliceID, "hey @bob", nil, []int64{bobID})

	msgs, err := s.GetUnreadMessages(bobID, nil, true, nil)
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
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")
	chID, _ := s.CreateChannel("dm", false, []int64{aliceID, bobID}, "channel")

	parentID, _ := s.SendMessage(chID, aliceID, "parent", nil, nil)
	s.SendMessage(chID, aliceID, "reply", &parentID, nil)
	s.SendMessage(chID, aliceID, "top-level", nil, nil)

	// Read all first to advance cursor
	s.GetUnreadMessages(bobID, nil, false, nil)

	// Now send new messages
	s.SendMessage(chID, aliceID, "new reply", &parentID, nil)
	s.SendMessage(chID, aliceID, "new top-level", nil, nil)

	// Filter by thread -- should not advance cursor
	msgs, err := s.GetUnreadMessages(bobID, nil, false, &parentID)
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

// --- Unread Counts ---

func TestUnreadCountsIncludesType(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")

	s.CreateChannel("general", true, []int64{aliceID, bobID}, "channel")
	s.OpenDM(aliceID, bobID, "bob")

	ch, _ := s.GetChannelByName("general")
	dm, _ := s.GetChannelByName("dm-alice-bob")

	s.SendMessage(ch.ID, aliceID, "hello channel", nil, nil)
	s.SendMessage(dm.ID, aliceID, "hello dm", nil, nil)

	counts, err := s.GetUnreadCounts(bobID)
	if err != nil {
		t.Fatalf("get unread counts: %v", err)
	}
	if len(counts) != 2 {
		t.Fatalf("expected 2 counts, got %d", len(counts))
	}

	typeMap := make(map[string]string)
	channelIDMap := make(map[string]int64)
	for _, c := range counts {
		typeMap[c.Channel] = c.Type
		channelIDMap[c.Channel] = c.ChannelID
	}
	if typeMap["general"] != "channel" {
		t.Errorf("general type = %q, want channel", typeMap["general"])
	}
	if typeMap["dm-alice-bob"] != "dm" {
		t.Errorf("dm type = %q, want dm", typeMap["dm-alice-bob"])
	}
	// Verify ChannelID is populated
	if channelIDMap["general"] != ch.ID {
		t.Errorf("general channel id = %d, want %d", channelIDMap["general"], ch.ID)
	}
	if channelIDMap["dm-alice-bob"] != dm.ID {
		t.Errorf("dm channel id = %d, want %d", channelIDMap["dm-alice-bob"], dm.ID)
	}
}

// --- Mark Read ---

func TestMarkRead(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")
	chID, _ := s.CreateChannel("general", true, []int64{aliceID, bobID}, "channel")

	msg1, _ := s.SendMessage(chID, aliceID, "msg1", nil, nil)
	s.SendMessage(chID, aliceID, "msg2", nil, nil)

	// Mark read up to msg1
	if err := s.MarkRead(bobID, chID, &msg1); err != nil {
		t.Fatalf("mark read: %v", err)
	}

	// Should only see msg2 as unread
	msgs, err := s.GetUnreadMessages(bobID, &chID, false, nil)
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

func TestMarkReadLatest(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")
	chID, _ := s.CreateChannel("general", true, []int64{aliceID, bobID}, "channel")

	s.SendMessage(chID, aliceID, "msg1", nil, nil)
	s.SendMessage(chID, aliceID, "msg2", nil, nil)

	// Mark read to latest (nil messageID)
	if err := s.MarkRead(bobID, chID, nil); err != nil {
		t.Fatalf("mark read: %v", err)
	}

	// Should have no unreads
	msgs, err := s.GetUnreadMessages(bobID, &chID, false, nil)
	if err != nil {
		t.Fatalf("get unread: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("len = %d, want 0", len(msgs))
	}
}

func TestMarkReadForwardOnly(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")
	chID, _ := s.CreateChannel("general", true, []int64{aliceID, bobID}, "channel")

	msg1, _ := s.SendMessage(chID, aliceID, "msg1", nil, nil)
	msg2, _ := s.SendMessage(chID, aliceID, "msg2", nil, nil)
	s.SendMessage(chID, aliceID, "msg3", nil, nil)

	// Mark read to msg2
	s.MarkRead(bobID, chID, &msg2)

	// Try to move cursor backwards to msg1 -- should be a no-op
	s.MarkRead(bobID, chID, &msg1)

	// Should still only see msg3 as unread (cursor at msg2, not msg1)
	msgs, err := s.GetUnreadMessages(bobID, &chID, false, nil)
	if err != nil {
		t.Fatalf("get unread: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len = %d, want 1", len(msgs))
	}
	if msgs[0].Body != "msg3" {
		t.Errorf("body = %q, want msg3", msgs[0].Body)
	}
}

// --- Settings ---

func TestSetAndGetSetting(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetSetting("test_key", "true"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	val, err := s.GetSetting("test_key")
	if err != nil {
		t.Fatalf("get setting: %v", err)
	}
	if val != "true" {
		t.Errorf("value = %q, want true", val)
	}
}

func TestSetSettingUpsert(t *testing.T) {
	s := newTestStore(t)
	s.SetSetting("key", "v1")
	s.SetSetting("key", "v2")
	val, _ := s.GetSetting("key")
	if val != "v2" {
		t.Errorf("value = %q, want v2", val)
	}
}

func TestGetSettingNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetSetting("nonexistent")
	if err == nil {
		t.Error("expected error for missing setting")
	}
}

func TestListSettings(t *testing.T) {
	s := newTestStore(t)
	s.SetSetting("a", "1")
	s.SetSetting("b", "2")
	settings, err := s.ListSettings()
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

// --- DMs ---

func TestOpenDMCreatesAndFinds(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")

	// First call creates
	name, created, err := s.OpenDM(aliceID, bobID, "bob")
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
	name2, created2, err := s.OpenDM(bobID, aliceID, "alice")
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
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")
	charlieID, _ := s.CreateUser("charlie", "")

	s.OpenDM(aliceID, bobID, "bob")
	s.OpenDM(aliceID, charlieID, "charlie")

	dms, err := s.ListDMsForUser(aliceID)
	if err != nil {
		t.Fatalf("list dms: %v", err)
	}
	if len(dms) != 2 {
		t.Fatalf("expected 2 dms, got %d", len(dms))
	}
	// Ordered by participant username
	if dms[0].OtherUsername != "bob" || dms[1].OtherUsername != "charlie" {
		t.Errorf("participants = [%s, %s], want [bob, charlie]", dms[0].OtherUsername, dms[1].OtherUsername)
	}
	// Verify ChannelID and OtherUserID are populated
	if dms[0].ChannelID <= 0 {
		t.Errorf("expected positive channel id, got %d", dms[0].ChannelID)
	}
	if dms[0].OtherUserID <= 0 {
		t.Errorf("expected positive other user id, got %d", dms[0].OtherUserID)
	}

	// Bob should only see one DM
	bobDMs, err := s.ListDMsForUser(bobID)
	if err != nil {
		t.Fatalf("list bob dms: %v", err)
	}
	if len(bobDMs) != 1 {
		t.Fatalf("expected 1 dm for bob, got %d", len(bobDMs))
	}
	if bobDMs[0].OtherUsername != "alice" {
		t.Errorf("participant = %q, want alice", bobDMs[0].OtherUsername)
	}
}

func TestListAllDMs(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")
	charlieID, _ := s.CreateUser("charlie", "")

	s.OpenDM(aliceID, bobID, "bob")
	s.OpenDM(aliceID, charlieID, "charlie")

	dms, err := s.ListAllDMs()
	if err != nil {
		t.Fatalf("list all dms: %v", err)
	}
	if len(dms) != 2 {
		t.Fatalf("expected 2 dms, got %d", len(dms))
	}

	// Verify User1/User2 fields
	for _, dm := range dms {
		if dm.User1Username == "" || dm.User2Username == "" {
			t.Errorf("expected both usernames filled, got %q and %q", dm.User1Username, dm.User2Username)
		}
		if dm.ChannelID <= 0 {
			t.Errorf("expected positive channel id, got %d", dm.ChannelID)
		}
		if dm.User1ID <= 0 || dm.User2ID <= 0 {
			t.Errorf("expected positive user ids, got %d and %d", dm.User1ID, dm.User2ID)
		}
	}
}

func TestChannelListExcludesDMs(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")

	s.CreateChannel("general", true, []int64{aliceID, bobID}, "channel")
	s.OpenDM(aliceID, bobID, "bob")

	channels, err := s.ListChannelsForUser(aliceID)
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

// --- Roles ---

func TestCreateAndListRoles(t *testing.T) {
	s := newTestStore(t)
	// Built-in roles are seeded by migrations
	roles, err := s.ListRoles()
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	builtInCount := len(roles)

	if err := s.CreateRole("custom"); err != nil {
		t.Fatalf("create role: %v", err)
	}

	roles, err = s.ListRoles()
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	if len(roles) != builtInCount+1 {
		t.Errorf("len = %d, want %d", len(roles), builtInCount+1)
	}
}

func TestDeleteRole(t *testing.T) {
	s := newTestStore(t)
	s.CreateRole("custom")

	if err := s.DeleteRole("custom"); err != nil {
		t.Fatalf("delete role: %v", err)
	}

	// Deleting built-in role should fail
	if err := s.DeleteRole("admin"); err == nil {
		t.Error("expected error for deleting built-in role")
	}
}

func TestGrantAndRevokePermission(t *testing.T) {
	s := newTestStore(t)
	s.CreateRole("custom")

	if err := s.GrantPermission("custom", "send_message"); err != nil {
		t.Fatalf("grant: %v", err)
	}

	perms, err := s.GetRolePermissions("custom")
	if err != nil {
		t.Fatalf("get perms: %v", err)
	}
	if len(perms) != 1 || perms[0] != "send_message" {
		t.Errorf("perms = %v, want [send_message]", perms)
	}

	if err := s.RevokePermission("custom", "send_message"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	perms, err = s.GetRolePermissions("custom")
	if err != nil {
		t.Fatalf("get perms: %v", err)
	}
	if len(perms) != 0 {
		t.Errorf("perms = %v, want empty", perms)
	}
}

func TestGetUserPermissions(t *testing.T) {
	s := newTestStore(t)
	s.CreateUser("alice", "")

	perms, err := s.GetUserPermissions("alice")
	if err != nil {
		t.Fatalf("get user perms: %v", err)
	}
	// Default role is "user" which has seeded permissions
	if len(perms) == 0 {
		t.Error("expected user to have seeded permissions")
	}
}

func TestHasPermission(t *testing.T) {
	s := newTestStore(t)
	s.CreateUser("alice", "")

	has, err := s.HasPermission("alice", "send_message")
	if err != nil {
		t.Fatalf("has permission: %v", err)
	}
	if !has {
		t.Error("expected alice to have send_message")
	}

	has, err = s.HasPermission("alice", "manage_roles")
	if err != nil {
		t.Fatalf("has permission: %v", err)
	}
	if has {
		t.Error("expected alice (user role) not to have manage_roles")
	}
}

func TestSetUserRole(t *testing.T) {
	s := newTestStore(t)
	s.CreateUser("alice", "")

	if err := s.SetUserRole("alice", "admin"); err != nil {
		t.Fatalf("set role: %v", err)
	}

	has, _ := s.HasPermission("alice", "manage_roles")
	if !has {
		t.Error("expected alice (admin) to have manage_roles")
	}

	// Non-existent user
	if err := s.SetUserRole("nobody", "admin"); err == nil {
		t.Error("expected error for non-existent user")
	}
}

func TestSetUserType(t *testing.T) {
	s := newTestStore(t)
	s.CreateUser("bot1", "")

	if err := s.SetUserType("bot1", "agent"); err != nil {
		t.Fatalf("set type: %v", err)
	}

	u, _ := s.GetUserByUsername("bot1")
	if u.Type != "agent" {
		t.Errorf("type = %q, want agent", u.Type)
	}

	// Non-existent user
	if err := s.SetUserType("nobody", "agent"); err == nil {
		t.Error("expected error for non-existent user")
	}
}

// --- Import/Backup helpers ---

func TestImportMessage(t *testing.T) {
	s := newTestStore(t)
	uid, _ := s.CreateUser("alice", "")
	chID, _ := s.CreateChannel("general", true, []int64{uid}, "channel")
	ts := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	msgID, err := s.ImportMessage(chID, uid, "old message", nil, nil, ts)
	if err != nil {
		t.Fatalf("import message: %v", err)
	}
	if msgID == 0 {
		t.Fatal("expected non-zero message ID")
	}
	msgs, _ := s.GetMessages(chID, nil, nil, 50, nil)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !msgs[0].CreatedAt.Equal(ts) {
		t.Errorf("created_at = %v, want %v", msgs[0].CreatedAt, ts)
	}
}

func TestImportMessageWithMentions(t *testing.T) {
	s := newTestStore(t)
	aliceID, _ := s.CreateUser("alice", "")
	bobID, _ := s.CreateUser("bob", "")
	chID, _ := s.CreateChannel("general", true, []int64{aliceID, bobID}, "channel")
	ts := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	msgID, err := s.ImportMessage(chID, aliceID, "hey @bob", nil, []int64{bobID}, ts)
	if err != nil {
		t.Fatalf("import message: %v", err)
	}
	if msgID == 0 {
		t.Fatal("expected non-zero message ID")
	}
	msgs, _ := s.GetMessages(chID, nil, nil, 50, nil)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if len(msgs[0].Mentions) != 1 || msgs[0].Mentions[0] != "bob" {
		t.Errorf("mentions = %v, want [bob]", msgs[0].Mentions)
	}
}

func TestIsEmpty(t *testing.T) {
	s := newTestStore(t)
	empty, err := s.IsEmpty()
	if err != nil {
		t.Fatalf("is empty: %v", err)
	}
	if !empty {
		t.Error("expected empty store")
	}
	s.CreateUser("alice", "")
	empty, _ = s.IsEmpty()
	if empty {
		t.Error("expected non-empty store")
	}
}

// --- Compile-time check ---

func TestStoreImplementsDomainStore(t *testing.T) {
	// This test validates the compile-time check at the top of store.go.
	var _ domain.Store = (*Store)(nil)
}
