import { createSignal, createEffect, type Accessor } from 'solid-js';
import type { SharkfinClient, UnreadCount, BroadcastMessage } from '@workfort/sharkfin-client';

export function createUnreadStore(client: SharkfinClient, activeChannel: Accessor<string>) {
  const [unreads, setUnreads] = createSignal<UnreadCount[]>([]);

  client.unreadCounts().then((counts) => {
    const ch = activeChannel();
    setUnreads(
      ch
        ? counts.map((u) =>
            u.channel === ch ? { ...u, unreadCount: 0, mentionCount: 0 } : u,
          )
        : counts,
    );
  });

  // Increment count for messages arriving in non-active channels.
  client.on('message', (msg: BroadcastMessage) => {
    if (msg.channel !== activeChannel()) {
      setUnreads((prev) =>
        prev.map((u) =>
          u.channel === msg.channel
            ? { ...u, unreadCount: u.unreadCount + 1 }
            : u,
        ),
      );
    }
  });

  // Mark read and reset counts when switching channels.
  createEffect(() => {
    const ch = activeChannel();
    if (!ch) return;
    client.markRead(ch);
    setUnreads((prev) =>
      prev.map((u) =>
        u.channel === ch ? { ...u, unreadCount: 0, mentionCount: 0 } : u,
      ),
    );
  });

  const totalUnread = () => unreads().reduce((sum, u) => sum + u.unreadCount, 0);

  return { unreads, totalUnread };
}
