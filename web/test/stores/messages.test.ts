import { describe, it, expect, vi } from 'vitest';
import { createRoot, createSignal } from 'solid-js';
import { createMessageStore } from '../../src/stores/messages';
import { createMockClient, flushPromises } from '../helpers';
import type { BroadcastMessage, Message } from '@workfort/sharkfin-client';

describe('createMessageStore', () => {
  it('fetches history when active channel is set', async () => {
    const client = createMockClient();
    const msgs: Message[] = [
      { id: 1, from: 'alice', body: 'hello', sentAt: '2026-03-15T09:00:00Z' },
    ];
    client.history.mockResolvedValue(msgs);

    let store!: ReturnType<typeof createMessageStore>;
    const [active, setActive] = createSignal('general');

    createRoot(() => {
      store = createMessageStore(client, active);
    });

    await flushPromises();
    expect(client.history).toHaveBeenCalledWith('general', { limit: 50 });
    expect(store.messages()).toEqual(msgs);
  });

  it('refetches history on channel switch', async () => {
    const client = createMockClient();
    client.history.mockResolvedValue([]);

    let store!: ReturnType<typeof createMessageStore>;
    const [active, setActive] = createSignal('general');

    createRoot(() => {
      store = createMessageStore(client, active);
      setActive('random');
    });

    await flushPromises();
    expect(client.history).toHaveBeenCalledWith('random', { limit: 50 });
  });

  it('appends broadcast messages for active channel', async () => {
    const client = createMockClient();
    client.history.mockResolvedValue([]);

    let store!: ReturnType<typeof createMessageStore>;
    const [active] = createSignal('general');

    createRoot(() => {
      store = createMessageStore(client, active);
    });

    await flushPromises();

    const broadcast: BroadcastMessage = {
      id: 2,
      channel: 'general',
      channelType: 'public',
      from: 'bob',
      body: 'hey',
      sentAt: '2026-03-15T09:01:00Z',
    };
    client._emit('message', broadcast);

    expect(store.messages()).toHaveLength(1);
    expect(store.messages()[0].body).toBe('hey');
  });

  it('does not append messages for other channels', async () => {
    const client = createMockClient();
    client.history.mockResolvedValue([]);

    let store!: ReturnType<typeof createMessageStore>;
    const [active] = createSignal('general');

    createRoot(() => {
      store = createMessageStore(client, active);
    });

    await flushPromises();

    client._emit('message', {
      id: 3, channel: 'random', channelType: 'public',
      from: 'bob', body: 'hey', sentAt: '2026-03-15T09:01:00Z',
    } as BroadcastMessage);

    expect(store.messages()).toHaveLength(0);
  });
});
