import { createSignal, createEffect, type Accessor } from 'solid-js';
import type { SharkfinClient, Message, BroadcastMessage } from '@workfort/sharkfin-client';

export function createMessageStore(client: SharkfinClient, activeChannel: Accessor<string>) {
  const [messages, setMessages] = createSignal<Message[]>([]);

  // Refetch history whenever active channel changes.
  createEffect(() => {
    const ch = activeChannel();
    if (!ch) return;
    client.history(ch, { limit: 50 }).then(setMessages);
  });

  // Append incoming messages for the active channel.
  client.on('message', (msg: BroadcastMessage) => {
    if (msg.channel === activeChannel()) {
      setMessages((prev) => [...prev, {
        id: msg.id,
        channel: msg.channel,
        from: msg.from,
        body: msg.body,
        sentAt: msg.sentAt,
        threadId: msg.threadId,
        mentions: msg.mentions,
      }]);
    }
  });

  async function sendMessage(body: string): Promise<void> {
    const ch = activeChannel();
    if (ch) await client.sendMessage(ch, body);
  }

  return { messages, sendMessage };
}
