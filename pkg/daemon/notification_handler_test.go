// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"testing"
	"time"

	"github.com/Work-Fort/sharkfin/pkg/domain"
	"github.com/Work-Fort/sharkfin/pkg/infra/sqlite"
)

func TestClassifyNotification_Mention(t *testing.T) {
	store, hub, wh := notifTestSetup(t)

	store.UpsertIdentity("uuid-alice", "alice", "Alice", "user", "user")
	store.UpsertIdentity("uuid-bob", "bob", "Bob", "user", "user")
	chID, _ := store.CreateChannel("testchan", true, []string{"uuid-alice", "uuid-bob"}, "channel")
	_ = chID

	msg := domain.MessageEvent{
		ChannelName: "testchan",
		ChannelType: "channel",
		From:        "bob",
		Body:        "hey @alice check this out",
		MessageID:   1,
		SentAt:      time.Now(),
		Mentions:    []string{"alice"},
	}

	_ = hub
	n := wh.classifyNotification(msg, "alice", "uuid-alice")
	if n == nil {
		t.Fatal("expected notification, got nil")
	}
	if n.Urgency != "active" {
		t.Fatalf("expected urgency=active, got %s", n.Urgency)
	}
	if n.Title != "Mention in #testchan" {
		t.Fatalf("unexpected title: %s", n.Title)
	}
	if n.Route != "/chat" {
		t.Fatalf("unexpected route: %s", n.Route)
	}
}

func TestClassifyNotification_DM(t *testing.T) {
	store, _, wh := notifTestSetup(t)

	alice, _ := store.UpsertIdentity("uuid-alice", "alice", "Alice", "user", "user")
	bob, _ := store.UpsertIdentity("uuid-bob", "bob", "Bob", "user", "user")
	store.CreateChannel("dm-alice-bob", false, []string{alice.ID, bob.ID}, "dm")

	msg := domain.MessageEvent{
		ChannelName: "dm-alice-bob",
		ChannelType: "dm",
		From:        "bob",
		Body:        "private message",
		MessageID:   1,
		SentAt:      time.Now(),
	}

	n := wh.classifyNotification(msg, "alice", alice.ID)
	if n == nil {
		t.Fatal("expected notification, got nil")
	}
	if n.Urgency != "active" {
		t.Fatalf("expected urgency=active, got %s", n.Urgency)
	}
	if n.Title != "DM from bob" {
		t.Fatalf("unexpected title: %s", n.Title)
	}
}

func TestClassifyNotification_RegularMessage(t *testing.T) {
	store, _, wh := notifTestSetup(t)

	alice, _ := store.UpsertIdentity("uuid-alice", "alice", "Alice", "user", "user")
	bob, _ := store.UpsertIdentity("uuid-bob", "bob", "Bob", "user", "user")
	store.CreateChannel("testchan", true, []string{alice.ID, bob.ID}, "channel")

	msg := domain.MessageEvent{
		ChannelName: "testchan",
		ChannelType: "channel",
		From:        "bob",
		Body:        "hello everyone",
		MessageID:   1,
		SentAt:      time.Now(),
	}

	n := wh.classifyNotification(msg, "alice", alice.ID)
	if n == nil {
		t.Fatal("expected notification, got nil")
	}
	if n.Urgency != "passive" {
		t.Fatalf("expected urgency=passive, got %s", n.Urgency)
	}
	if n.Title != "New message in #testchan" {
		t.Fatalf("unexpected title: %s", n.Title)
	}
	if n.Body != "" {
		t.Fatalf("expected empty body for passive notification, got %s", n.Body)
	}
}

func TestClassifyNotification_NotMember(t *testing.T) {
	store, _, wh := notifTestSetup(t)

	store.UpsertIdentity("uuid-alice", "alice", "Alice", "user", "user")
	store.UpsertIdentity("uuid-bob", "bob", "Bob", "user", "user")
	// Alice is NOT a member of this channel.
	store.CreateChannel("secret", false, []string{"uuid-bob"}, "channel")

	msg := domain.MessageEvent{
		ChannelName: "secret",
		ChannelType: "channel",
		From:        "bob",
		Body:        "secret stuff",
		MessageID:   1,
		SentAt:      time.Now(),
	}

	n := wh.classifyNotification(msg, "alice", "uuid-alice")
	if n != nil {
		t.Fatalf("expected nil for non-member, got %+v", n)
	}
}

func TestTruncate(t *testing.T) {
	short := "hello"
	if got := truncate(short, 100); got != short {
		t.Fatalf("expected %q, got %q", short, got)
	}

	long := ""
	for i := 0; i < 120; i++ {
		long += "x"
	}
	if got := truncate(long, 100); len([]rune(got)) != 100 {
		t.Fatalf("expected 100 runes, got %d", len([]rune(got)))
	}
}

func notifTestSetup(t *testing.T) (domain.Store, *Hub, *WSHandler) {
	t.Helper()
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	bus := domain.NewEventBus()
	hub := NewHub(bus)
	presence := NewPresenceHandler(20 * time.Second)
	wh := NewWSHandler(store, hub, presence, 20*time.Second, "test")

	return store, hub, wh
}
