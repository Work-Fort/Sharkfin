import { getClient } from '../client';
import { createChannelStore } from './channels';
import { createMessageStore } from './messages';
import { createUserStore } from './users';
import { createUnreadStore } from './unread';

let _stores: ReturnType<typeof initStores> | null = null;

function initStores() {
  const client = getClient();
  const channels = createChannelStore(client);
  const messages = createMessageStore(client, channels.activeChannel);
  const users = createUserStore(client);
  const unread = createUnreadStore(client, channels.activeChannel);
  return { channels, messages, users, unread };
}

export function getStores() {
  if (!_stores) _stores = initStores();
  return _stores;
}

/** Reset the singleton (for tests). */
export function resetStores(): void {
  _stores = null;
}
