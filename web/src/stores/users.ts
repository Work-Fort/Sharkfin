import { createSignal } from 'solid-js';
import type { SharkfinClient, User, PresenceUpdate } from '@workfort/sharkfin-client';

export function createUserStore(client: SharkfinClient) {
  const [users, setUsers] = createSignal<User[]>([]);

  client.users().then(setUsers).catch(() => {});

  client.on('presence', (update: PresenceUpdate) => {
    setUsers((prev) =>
      prev.map((u) =>
        u.username === update.username
          ? { ...u, online: update.status === 'online', state: update.state ?? u.state }
          : u,
      ),
    );
  });

  return { users };
}
