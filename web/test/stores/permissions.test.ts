import { describe, it, expect, vi } from 'vitest';
import { createRoot } from 'solid-js';
import { createPermissionStore } from '../../src/stores/permissions';
import { createMockClient, flushPromises } from '../helpers';

describe('createPermissionStore', () => {
  it('fetches capabilities on creation', async () => {
    const client = createMockClient();
    (client as any).capabilities = vi.fn().mockResolvedValue(['send_message', 'channel_list', 'create_channel']);

    let store!: ReturnType<typeof createPermissionStore>;
    createRoot(() => {
      store = createPermissionStore(client);
    });

    await flushPromises();
    expect(store.can('send_message')).toBe(true);
    expect(store.can('create_channel')).toBe(true);
    expect(store.can('invite_channel')).toBe(false);
  });

  it('returns false for all permissions when capabilities fails', async () => {
    const client = createMockClient();
    (client as any).capabilities = vi.fn().mockRejectedValue(new Error('fail'));

    let store!: ReturnType<typeof createPermissionStore>;
    createRoot(() => {
      store = createPermissionStore(client);
    });

    await flushPromises();
    expect(store.can('send_message')).toBe(false);
    expect(store.can('create_channel')).toBe(false);
  });
});
