import { createSignal, createRoot } from 'solid-js';
import { getClient } from '../client';
import { createChannelStore } from './channels';
import { createMessageStore } from './messages';
import { createUserStore } from './users';
import { createUnreadStore } from './unread';
import { createPermissionStore } from './permissions';

const [connectionState, setConnectionState] = createSignal<'connecting' | 'connected' | 'disconnected'>('connecting');
const [loading, setLoading] = createSignal(true);

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

  client.on('disconnect', () => setConnectionState('disconnected'));
  client.on('reconnect', () => {
    setConnectionState('connected');
    const stores = getStores();
    client.channels().then((chs) => stores.channels.setChannels?.(chs)).catch(() => {});
    client.users().then((us) => stores.users.setUsers?.(us)).catch(() => {});
    client.unreadCounts().then((counts) => stores.unread.setUnreads?.(counts)).catch(() => {});
    client.capabilities().then((perms) => stores.permissions.update(perms)).catch(() => {});
  });

  await client.connect();
  setConnectionState('connected');

  // Fetch permissions after connect.
  client.capabilities().then((perms) => _permissions!.update(perms)).catch(() => {});

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

export { connectionState, loading };

export function resetStores(): void {
  _dispose?.();
  _dispose = null;
  _stores = null;
  setConnectionState('connecting');
  setLoading(true);
}
