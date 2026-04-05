// SPDX-License-Identifier: Apache-2.0
import { render } from 'solid-js/web';
import { SharkfinChat } from './components/chat';
import { ChannelSidebar } from './components/sidebar';
import { CreateChannelDialog } from './components/create-channel-dialog';
import { DMDialog } from './components/dm-dialog';
import { getStores, loading } from './stores';
import { getClient } from './client';
import { Show, createSignal } from 'solid-js';
import { useAuth } from '@workfort/ui-solid';

function SharkfinApp(props: { connected: boolean }) {
  return <SharkfinChat connected={props.connected} />;
}

const roots = new WeakMap<HTMLElement, () => void>();

export function mount(el: HTMLElement, props: { connected: boolean }) {
  const dispose = render(() => <SharkfinApp connected={props.connected} />, el);
  roots.set(el, dispose);
}

export function unmount(el: HTMLElement) {
  const dispose = roots.get(el);
  if (dispose) {
    dispose();
    roots.delete(el);
  }
}

export const manifest = {
  name: 'sharkfin',
  label: 'Chat',
  route: '/chat',
  display: 'nav' as const,
};

function SidebarContent() {
  return (
    <Show when={!loading()} fallback={
      <div style="padding: var(--wf-space-md);">
        <wf-skeleton width="100%" height="1.5rem" />
        <wf-skeleton width="100%" height="1.5rem" style="margin-top: var(--wf-space-sm);" />
        <wf-skeleton width="80%" height="1.5rem" style="margin-top: var(--wf-space-sm);" />
      </div>
    }>
      <SidebarLoaded />
    </Show>
  );
}

export function mountSidebar(el: HTMLElement) {
  const dispose = render(() => <SidebarContent />, el);
  roots.set(el, dispose);
}

export function unmountSidebar(el: HTMLElement) {
  const dispose = roots.get(el);
  if (dispose) {
    dispose();
    roots.delete(el);
  }
}

function SidebarLoaded() {
  const { channels, users, unread, permissions } = getStores();
  const { user } = useAuth();
  const [dmDialogOpen, setDmDialogOpen] = createSignal(false);
  const [createChannelOpen, setCreateChannelOpen] = createSignal(false);

  async function handleDMSelect(username: string) {
    setDmDialogOpen(false);
    const result = await getClient().dmOpen(username);
    channels.setActiveChannel(result.channel);
    getClient().dmList().then((dms) => channels.setDms?.(dms)).catch(() => {});
  }

  async function handleCreateChannel(name: string, isPublic: boolean) {
    setCreateChannelOpen(false);
    await getClient().createChannel(name, isPublic);
    getClient().channels().then((chs) => channels.setChannels?.(chs)).catch(() => {});
    channels.setActiveChannel(name);
  }

  async function handleJoinChannel(channel: string) {
    await getClient().joinChannel(channel);
    getClient().channels().then((chs) => channels.setChannels?.(chs)).catch(() => {});
    channels.setActiveChannel(channel);
  }

  return (
    <>
      <ChannelSidebar
        channels={channels.channels()}
        dms={channels.dms()}
        unreads={unread.unreads()}
        users={users.users()}
        activeChannel={channels.activeChannel()}
        onSelectChannel={channels.setActiveChannel}
        currentUsername={user()?.name ?? ''}
        onNewDM={() => setDmDialogOpen(true)}
        onNewChannel={() => setCreateChannelOpen(true)}
        onJoinChannel={handleJoinChannel}
        can={permissions.can}
      />
      <CreateChannelDialog
        open={createChannelOpen()}
        onCreate={handleCreateChannel}
        onClose={() => setCreateChannelOpen(false)}
      />
      <DMDialog
        users={users.users()}
        currentUsername={user()?.name ?? ''}
        open={dmDialogOpen()}
        onSelect={handleDMSelect}
        onClose={() => setDmDialogOpen(false)}
      />
    </>
  );
}
