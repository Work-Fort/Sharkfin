import { SharkfinChat } from './components/chat';
import { ChannelSidebar } from './components/sidebar';
import { DMDialog } from './components/dm-dialog';
import { getStores, loading } from './stores';
import { getClient } from './client';
import { Show, createSignal } from 'solid-js';
import { useAuth } from '@workfort/ui-solid';

export default function SharkfinApp(props: { connected: boolean }) {
  return <SharkfinChat connected={props.connected} />;
}

export const manifest = {
  name: 'sharkfin',
  label: 'Chat',
  route: '/chat',
};

export function SidebarContent() {
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

function SidebarLoaded() {
  const { channels, users, unread } = getStores();
  const { user } = useAuth();
  const [dmDialogOpen, setDmDialogOpen] = createSignal(false);

  async function handleDMSelect(username: string) {
    setDmDialogOpen(false);
    const result = await getClient().dmOpen(username);
    channels.setActiveChannel(result.channel);
    getClient().dmList().then((dms) => channels.setDms?.(dms)).catch(() => {});
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
