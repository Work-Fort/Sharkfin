import { describe, it, expect, vi } from 'vitest';
import { createRoot } from 'solid-js';
import { createChannelStore } from '../../src/stores/channels';
import { createMockClient, flushPromises } from '../helpers';

describe('createChannelStore', () => {
  it('fetches channels on creation', async () => {
    const client = createMockClient();
    client.channels.mockResolvedValue([
      { name: 'general', public: true, member: true },
      { name: 'random', public: true, member: true },
    ]);

    let store!: ReturnType<typeof createChannelStore>;
    createRoot(() => {
      store = createChannelStore(client);
    });

    await flushPromises();
    expect(store.channels()).toEqual([
      { name: 'general', public: true, member: true },
      { name: 'random', public: true, member: true },
    ]);
  });

  it('defaults active channel to empty string', () => {
    const client = createMockClient();
    let store!: ReturnType<typeof createChannelStore>;
    createRoot(() => {
      store = createChannelStore(client);
    });
    expect(store.activeChannel()).toBe('');
  });

  it('updates active channel', () => {
    const client = createMockClient();
    let store!: ReturnType<typeof createChannelStore>;
    createRoot(() => {
      store = createChannelStore(client);
      store.setActiveChannel('random');
    });
    expect(store.activeChannel()).toBe('random');
  });

  it('fetches DMs on creation', async () => {
    const client = createMockClient();
    client.dmList.mockResolvedValue([
      { channel: 'dm-abc', participants: ['alice', 'bob'] },
    ]);

    let store!: ReturnType<typeof createChannelStore>;
    createRoot(() => {
      store = createChannelStore(client);
    });

    await flushPromises();
    expect(store.dms()).toEqual([
      { channel: 'dm-abc', participants: ['alice', 'bob'] },
    ]);
  });
});
