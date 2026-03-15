import { Show, createEffect } from 'solid-js';
import '../styles/chat.css';
import { getStores } from '../stores';
import { ChannelHeader } from './channel-header';
import { MessageArea } from './message-area';
import { TypingIndicator } from './typing-indicator';
import { InputBar } from './input-bar';

interface SharkfinChatProps {
  connected: boolean;
}

export function SharkfinChat(props: SharkfinChatProps) {
  const { channels, messages } = getStores();

  // Auto-select first channel once loaded.
  createEffect(() => {
    const chs = channels.channels();
    if (chs.length > 0 && !channels.activeChannel()) {
      channels.setActiveChannel(chs[0].name);
    }
  });

  return (
    <div class="sf-main">
      <Show when={props.connected} fallback={
        <wf-banner variant="warning" headline="Chat is reconnecting\u2026" />
      }>
        <ChannelHeader name={channels.activeChannel()} />
        <MessageArea messages={messages.messages()} />
        <TypingIndicator typingUsers={[]} />
        <InputBar
          channel={channels.activeChannel()}
          onSend={(body) => messages.sendMessage(body)}
        />
      </Show>
    </div>
  );
}
