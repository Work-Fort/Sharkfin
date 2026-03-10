// SPDX-License-Identifier: AGPL-3.0-or-later
package domain

import "sync"

type eventBus struct {
	mu   sync.RWMutex
	subs []*subscription
}

type subscription struct {
	bus    *eventBus
	ch     chan Event
	types  map[string]bool
	closed bool
}

// NewEventBus creates a new in-process event bus.
func NewEventBus() EventBus {
	return &eventBus{}
}

func (b *eventBus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.subs {
		if s.closed {
			continue
		}
		if len(s.types) > 0 && !s.types[event.Type] {
			continue
		}
		select {
		case s.ch <- event:
		default: // buffer full, drop
		}
	}
}

func (b *eventBus) Subscribe(eventTypes ...string) Subscription {
	s := &subscription{
		bus:   b,
		ch:    make(chan Event, 64),
		types: make(map[string]bool),
	}
	for _, t := range eventTypes {
		s.types[t] = true
	}
	b.mu.Lock()
	b.subs = append(b.subs, s)
	b.mu.Unlock()
	return s
}

func (s *subscription) Events() <-chan Event { return s.ch }

func (s *subscription) Close() {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
	// Remove from bus
	for i, sub := range s.bus.subs {
		if sub == s {
			s.bus.subs = append(s.bus.subs[:i], s.bus.subs[i+1:]...)
			break
		}
	}
}
