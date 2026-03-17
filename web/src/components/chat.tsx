import { Show, createEffect, createSignal, onMount, onCleanup } from 'solid-js';
import '../styles/chat.css';
import { initApp, getStores, connectionState, loading } from '../stores';
import { useIdleDetection } from '@workfort/ui-solid';
import { getClient } from '../client';
import { ChannelHeader } from './channel-header';
import { MessageArea } from './message-area';
import { TypingIndicator } from './typing-indicator';
import { InputBar } from './input-bar';
import { InviteDialog } from './invite-dialog';

interface SharkfinChatProps {
  connected: boolean;
}

export function SharkfinChat(props: SharkfinChatProps) {
  const [initFailed, setInitFailed] = createSignal(false);

  onMount(async () => {
    try {
      await initApp();
      const client = getClient();
      useIdleDetection({
        onActive: () => client.setState('active').catch(() => {}),
        onIdle: () => client.setState('idle').catch(() => {}),
      });
    } catch {
      setInitFailed(true);
    }
  });

  // Retry when connected flips to true after auth failure.
  createEffect(() => {
    if (props.connected && initFailed()) {
      setInitFailed(false);
      initApp()
        .then(() => {
          const client = getClient();
          useIdleDetection({
            onActive: () => client.setState('active').catch(() => {}),
            onIdle: () => client.setState('idle').catch(() => {}),
          });
        })
        .catch(() => setInitFailed(true));
    }
  });

  return (
    <div class="sf-main">
      <Show when={initFailed()}>
        <wf-banner variant="info" headline="Sign in to use Chat" />
      </Show>
      <Show when={!initFailed() && loading()}>
        <div style="padding: var(--wf-space-lg);">
          <wf-skeleton width="100%" height="2rem" />
          <wf-skeleton width="100%" height="200px" style="margin-top: var(--wf-space-md);" />
          <wf-skeleton width="60%" height="1rem" style="margin-top: var(--wf-space-md);" />
        </div>
      </Show>
      <Show when={!initFailed() && !loading() && connectionState() !== 'connecting'}>
        <Show when={connectionState() === 'disconnected'}>
          <wf-banner variant="warning" headline="Connection lost. Reconnecting\u2026" />
        </Show>
        <ChatContent />
      </Show>
    </div>
  );
}

function ChatContent() {
  const { channels, messages, users, permissions } = getStores();
  const [inviteOpen, setInviteOpen] = createSignal(false);

  createEffect(() => {
    const chs = channels.channels();
    if (chs.length > 0 && !channels.activeChannel()) {
      channels.setActiveChannel(chs[0].name);
    }
  });

  const activeChannelObj = () => channels.channels().find(c => c.name === channels.activeChannel());

  async function handleInvite(channel: string, username: string) {
    setInviteOpen(false);
    await getClient().inviteToChannel(channel, username);
  }

  return (
    <>
      <ChannelHeader
        name={channels.activeChannel()}
        isPublic={activeChannelObj()?.public ?? true}
        onInvite={() => setInviteOpen(true)}
        can={permissions.can}
      />
      <Show when={() => permissions.can('history')} fallback={
        <div class="sf-messages" style="display: flex; align-items: center; justify-content: center; color: var(--wf-color-text-muted); font-size: var(--wf-text-sm);">
          You don't have permission to view message history.
        </div>
      }>
        <MessageArea messages={messages.messages()} />
      </Show>
      <TypingIndicator typingUsers={[]} />
      <Show when={() => permissions.can('send_message')} fallback={
        <div class="sf-typing" style="color: var(--wf-color-text-muted); font-size: var(--wf-text-xs);">
          You don't have permission to send messages.
        </div>
      }>
        <InputBar
          channel={channels.activeChannel()}
          onSend={(body) => messages.sendMessage(body)}
        />
      </Show>
      <InviteDialog
        channel={channels.activeChannel()}
        users={users.users()}
        open={inviteOpen()}
        onInvite={handleInvite}
        onClose={() => setInviteOpen(false)}
      />
    </>
  );
}
