import { describe, it, expect } from 'vitest';
import { createRoot } from 'solid-js';
import { createUserStore } from '../../src/stores/users';
import { createMockClient, flushPromises } from '../helpers';
import type { PresenceUpdate, User } from '@workfort/sharkfin-client';

describe('createUserStore', () => {
  it('fetches users on creation', async () => {
    const client = createMockClient();
    const userList: User[] = [
      { username: 'alice', online: true, type: 'user' },
      { username: 'bob', online: false, type: 'agent' },
    ];
    client.users.mockResolvedValue(userList);

    let store!: ReturnType<typeof createUserStore>;
    createRoot(() => {
      store = createUserStore(client);
    });

    await flushPromises();
    expect(store.users()).toEqual(userList);
  });

  it('updates user online status on presence event', async () => {
    const client = createMockClient();
    client.users.mockResolvedValue([
      { username: 'alice', online: true, type: 'user' },
      { username: 'bob', online: false, type: 'agent' },
    ]);

    let store!: ReturnType<typeof createUserStore>;
    createRoot(() => {
      store = createUserStore(client);
    });

    await flushPromises();

    const update: PresenceUpdate = { username: 'bob', status: 'online' };
    client._emit('presence', update);

    const bob = store.users().find((u) => u.username === 'bob');
    expect(bob?.online).toBe(true);
  });

  it('updates user state on presence event', async () => {
    const client = createMockClient();
    client.users.mockResolvedValue([
      { username: 'alice', online: true, type: 'user', state: 'active' },
    ]);

    let store!: ReturnType<typeof createUserStore>;
    createRoot(() => {
      store = createUserStore(client);
    });

    await flushPromises();

    client._emit('presence', { username: 'alice', status: 'online', state: 'idle' } as PresenceUpdate);

    const alice = store.users().find((u) => u.username === 'alice');
    expect(alice?.state).toBe('idle');
  });
});
