// SPDX-License-Identifier: AGPL-3.0-or-later
package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

func TestPublishSubscribe(t *testing.T) {
	bus := domain.NewEventBus()
	sub := bus.Subscribe(domain.EventMessageNew)
	defer sub.Close()

	bus.Publish(domain.Event{
		Type:    domain.EventMessageNew,
		Payload: domain.MessageEvent{From: "alice", ChannelName: "general"},
	})

	select {
	case evt := <-sub.Events():
		assert.Equal(t, domain.EventMessageNew, evt.Type)
		msg := evt.Payload.(domain.MessageEvent)
		assert.Equal(t, "alice", msg.From)
		assert.Equal(t, "general", msg.ChannelName)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestSubscribeFiltersByType(t *testing.T) {
	bus := domain.NewEventBus()
	sub := bus.Subscribe(domain.EventPresenceUpdate)
	defer sub.Close()

	bus.Publish(domain.Event{
		Type:    domain.EventMessageNew,
		Payload: domain.MessageEvent{From: "alice"},
	})

	select {
	case <-sub.Events():
		t.Fatal("should not receive non-matching event type")
	case <-time.After(50 * time.Millisecond):
		// expected: no event received
	}
}

func TestSubscribeAllTypes(t *testing.T) {
	bus := domain.NewEventBus()
	sub := bus.Subscribe() // no type args = all events
	defer sub.Close()

	bus.Publish(domain.Event{Type: domain.EventMessageNew, Payload: "msg"})
	bus.Publish(domain.Event{Type: domain.EventPresenceUpdate, Payload: "pres"})

	for i := 0; i < 2; i++ {
		select {
		case <-sub.Events():
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for event %d", i)
		}
	}
}

func TestPublishDropsWhenBufferFull(t *testing.T) {
	bus := domain.NewEventBus()
	sub := bus.Subscribe(domain.EventMessageNew)
	defer sub.Close()

	// Publish more events than buffer size (64) without reading
	for i := 0; i < 100; i++ {
		bus.Publish(domain.Event{
			Type:    domain.EventMessageNew,
			Payload: domain.MessageEvent{From: "alice"},
		})
	}
	// Should not panic or block — excess events are dropped
}

func TestSubscriptionClose(t *testing.T) {
	bus := domain.NewEventBus()
	sub := bus.Subscribe(domain.EventMessageNew)

	sub.Close()

	// Channel should be closed
	_, ok := <-sub.Events()
	assert.False(t, ok, "channel should be closed after Close()")

	// Publishing after close should not panic
	require.NotPanics(t, func() {
		bus.Publish(domain.Event{
			Type:    domain.EventMessageNew,
			Payload: domain.MessageEvent{From: "alice"},
		})
	})
}

func TestMultipleSubscribers(t *testing.T) {
	bus := domain.NewEventBus()
	sub1 := bus.Subscribe(domain.EventMessageNew)
	sub2 := bus.Subscribe(domain.EventMessageNew)
	defer sub1.Close()
	defer sub2.Close()

	bus.Publish(domain.Event{
		Type:    domain.EventMessageNew,
		Payload: domain.MessageEvent{From: "alice"},
	})

	for _, sub := range []domain.Subscription{sub1, sub2} {
		select {
		case evt := <-sub.Events():
			msg := evt.Payload.(domain.MessageEvent)
			assert.Equal(t, "alice", msg.From)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}
	}
}
