import { describe, it, expect, vi } from 'vitest';
import { createRoot, createSignal } from 'solid-js';
import { createUnreadStore } from '../../src/stores/unread';
import { createMockClient, flushPromises } from '../helpers';
import type { BroadcastMessage, UnreadCount } from '@workfort/sharkfin-client';

describe('createUnreadStore', () => {
  it('fetches unread counts on creation', async () => {
    const client = createMockClient();
    const counts: UnreadCount[] = [
      { channel: 'general', type: 'public', unreadCount: 3, mentionCount: 1 },
    ];
    client.unreadCounts.mockResolvedValue(counts);

    let store!: ReturnType<typeof createUnreadStore>;
    const [active] = createSignal('');

    createRoot(() => {
      store = createUnreadStore(client, active);
    });

    await flushPromises();
    expect(store.unreads()).toEqual(counts);
    expect(store.totalUnread()).toBe(3);
  });

  it('increments unread on message in non-active channel', async () => {
    const client = createMockClient();
    client.unreadCounts.mockResolvedValue([
      { channel: 'general', type: 'public', unreadCount: 0, mentionCount: 0 },
      { channel: 'random', type: 'public', unreadCount: 0, mentionCount: 0 },
    ]);

    let store!: ReturnType<typeof createUnreadStore>;
    const [active] = createSignal('general');

    createRoot(() => {
      store = createUnreadStore(client, active);
    });

    await flushPromises();

    client._emit('message', {
      id: 1, channel: 'random', channelType: 'public',
      from: 'alice', body: 'hey', sentAt: '2026-03-15T09:00:00Z',
    } as BroadcastMessage);

    const random = store.unreads().find((u) => u.channel === 'random');
    expect(random?.unreadCount).toBe(1);
    expect(store.totalUnread()).toBe(1);
  });

  it('does not increment unread for active channel', async () => {
    const client = createMockClient();
    client.unreadCounts.mockResolvedValue([
      { channel: 'general', type: 'public', unreadCount: 0, mentionCount: 0 },
    ]);

    let store!: ReturnType<typeof createUnreadStore>;
    const [active] = createSignal('general');

    createRoot(() => {
      store = createUnreadStore(client, active);
    });

    await flushPromises();

    client._emit('message', {
      id: 1, channel: 'general', channelType: 'public',
      from: 'alice', body: 'hey', sentAt: '2026-03-15T09:00:00Z',
    } as BroadcastMessage);

    expect(store.totalUnread()).toBe(0);
  });

  it('resets unread and marks read on channel switch', async () => {
    const client = createMockClient();
    client.unreadCounts.mockResolvedValue([
      { channel: 'general', type: 'public', unreadCount: 5, mentionCount: 2 },
      { channel: 'random', type: 'public', unreadCount: 3, mentionCount: 0 },
    ]);

    let store!: ReturnType<typeof createUnreadStore>;
    const [active, setActive] = createSignal('');

    createRoot(() => {
      store = createUnreadStore(client, active);
      setActive('general');
    });

    await flushPromises();

    const general = store.unreads().find((u) => u.channel === 'general');
    expect(general?.unreadCount).toBe(0);
    expect(general?.mentionCount).toBe(0);
    expect(client.markRead).toHaveBeenCalledWith('general');
  });
});
