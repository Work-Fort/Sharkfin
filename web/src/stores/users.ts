import { createSignal, onCleanup } from 'solid-js';
import type { SharkfinClient, User, PresenceUpdate } from '@workfort/sharkfin-client';

export function createUserStore(client: SharkfinClient) {
  const [users, setUsers] = createSignal<User[]>([]);

  client.users().then(setUsers).catch(() => {});

  const handler = (update: PresenceUpdate) => {
    setUsers((prev) =>
      prev.map((u) =>
        u.username === update.username
          ? { ...u, online: update.status === 'online', state: update.state ?? u.state }
          : u,
      ),
    );
  };
  client.on('presence', handler);
  onCleanup(() => client.off('presence', handler));

  return { users, setUsers };
}
