import { describe, it, expect, vi } from 'vitest';
import { createRoot } from 'solid-js';
import { createPermissionStore } from '../../src/stores/permissions';
import { flushPromises } from '../helpers';

describe('createPermissionStore', () => {
  it('starts with no permissions', () => {
    let store!: ReturnType<typeof createPermissionStore>;
    createRoot(() => {
      store = createPermissionStore();
    });
    expect(store.can('send_message')).toBe(false);
  });

  it('updates permissions via update()', async () => {
    let store!: ReturnType<typeof createPermissionStore>;
    createRoot(() => {
      store = createPermissionStore();
    });

    store.update(['send_message', 'channel_list', 'create_channel']);
    expect(store.can('send_message')).toBe(true);
    expect(store.can('create_channel')).toBe(true);
    expect(store.can('invite_channel')).toBe(false);
  });

  it('returns permissions list', () => {
    let store!: ReturnType<typeof createPermissionStore>;
    createRoot(() => {
      store = createPermissionStore();
    });

    store.update(['a', 'b']);
    expect(store.permissions()).toEqual(['a', 'b']);
  });
});
