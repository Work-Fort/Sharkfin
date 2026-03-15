import { Show, createEffect } from 'solid-js';
import '../styles/chat.css';
import { getClient } from '../client';
import { createChannelStore } from '../stores/channels';
import { createMessageStore } from '../stores/messages';
import { createUserStore } from '../stores/users';
import { createUnreadStore } from '../stores/unread';
import { ChannelHeader } from './channel-header';
import { MessageArea } from './message-area';
import { TypingIndicator } from './typing-indicator';
import { InputBar } from './input-bar';

interface SharkfinChatProps {
  connected: boolean;
}

export function SharkfinChat(props: SharkfinChatProps) {
  const client = getClient();
  const channelStore = createChannelStore(client);
  const messageStore = createMessageStore(client, channelStore.activeChannel);
  const userStore = createUserStore(client);
  const unreadStore = createUnreadStore(client, channelStore.activeChannel);

  // Auto-select first channel once loaded.
  createEffect(() => {
    const chs = channelStore.channels();
    if (chs.length > 0 && !channelStore.activeChannel()) {
      channelStore.setActiveChannel(chs[0].name);
    }
  });

  return (
    <div class="sf-main">
      <Show when={props.connected} fallback={
        <wf-banner variant="warning" headline="Chat is reconnecting\u2026" />
      }>
        <ChannelHeader name={channelStore.activeChannel()} />
        <MessageArea messages={messageStore.messages()} />
        <TypingIndicator typingUsers={[]} />
        <InputBar
          channel={channelStore.activeChannel()}
          onSend={(body) => messageStore.sendMessage(body)}
        />
      </Show>
    </div>
  );
}
