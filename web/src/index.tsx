import { SharkfinChat } from './components/chat';
import { ChannelSidebar } from './components/sidebar';
import { getStores, loading } from './stores';
import { Show } from 'solid-js';

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

  return (
    <ChannelSidebar
      channels={channels.channels()}
      dms={channels.dms()}
      unreads={unread.unreads()}
      users={users.users()}
      activeChannel={channels.activeChannel()}
      onSelectChannel={channels.setActiveChannel}
    />
  );
}
