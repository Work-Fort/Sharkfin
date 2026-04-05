// SPDX-License-Identifier: Apache-2.0
import { createSignal, createRoot } from 'solid-js';
import { getClient } from '../client';
import { createChannelStore } from './channels';
import { createMessageStore } from './messages';
import { createUserStore } from './users';
import { createUnreadStore } from './unread';
import { createPermissionStore } from './permissions';

const [connectionState, setConnectionState] = createSignal<'connecting' | 'connected' | 'disconnected'>('connecting');
const [loading, setLoading] = createSignal(true);

// Debounced disconnected state: goes true immediately on disconnect,
// only goes false after 'connected' has been stable for 2 seconds.
// Prevents banner flashing during rapid reconnect cycles.
const [disconnected, setDisconnected] = createSignal(false);
let _reconnectDebounce: ReturnType<typeof setTimeout> | null = null;

// Permissions are created at module level (outside createRoot) so that
// SolidJS components in the shell's render tree can track the signal.
// Signals created inside createRoot are isolated and not tracked by
// computations outside that root.
let _permissions: ReturnType<typeof createPermissionStore> | null = null;

let _stores: ReturnType<typeof createStores> | null = null;
let _dispose: (() => void) | null = null;

function createStores() {
  const client = getClient();
  const channels = createChannelStore(client);
  const messages = createMessageStore(client, channels.activeChannel);
  const users = createUserStore(client);
  const unread = createUnreadStore(client, channels.activeChannel);
  return { channels, messages, users, unread, permissions: _permissions! };
}

export async function initApp(): Promise<void> {
  const client = getClient();

  // Create permissions outside createRoot for cross-tree reactivity.
  // Don't fetch capabilities here — client isn't connected yet.
  _permissions = createPermissionStore();

  client.on('disconnect', () => {
    setConnectionState('disconnected');
    if (_reconnectDebounce) { clearTimeout(_reconnectDebounce); _reconnectDebounce = null; }
    setDisconnected(true);
  });
  client.on('reconnect', () => {
    setConnectionState('connected');
    // Delay clearing the banner so it doesn't flash during rapid reconnect cycles.
    if (_reconnectDebounce) clearTimeout(_reconnectDebounce);
    _reconnectDebounce = setTimeout(() => { setDisconnected(false); _reconnectDebounce = null; }, 2000);
    const stores = getStores();
    client.channels().then((chs) => stores.channels.setChannels?.(chs)).catch(() => {});
    client.users().then((us) => stores.users.setUsers?.(us)).catch(() => {});
    client.unreadCounts().then((counts) => stores.unread.setUnreads?.(counts)).catch(() => {});
    client.capabilities().then((perms) => stores.permissions.update(perms)).catch(() => {});
  });

  await client.connect();
  setConnectionState('connected');

  // Fetch permissions after connect, BEFORE creating stores/rendering.
  try {
    const perms = await client.capabilities();
    _permissions!.update(perms);
  } catch {
    // Non-fatal — UI will show no-permission state.
  }

  createRoot((dispose) => {
    _stores = createStores();
    _dispose = dispose;
  });
  setLoading(false);
}

export function getStores() {
  if (!_stores) throw new Error('initApp() must be called before getStores()');
  return _stores;
}

export { connectionState, disconnected, loading };

export function resetStores(): void {
  _dispose?.();
  _dispose = null;
  _stores = null;
  setConnectionState('connecting');
  setLoading(true);
}
