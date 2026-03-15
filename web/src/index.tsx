import { SharkfinChat } from './components/chat';
import { ChannelSidebar } from './components/sidebar';
import { getClient } from './client';
import { createChannelStore } from './stores/channels';
import { createUserStore } from './stores/users';
import { createUnreadStore } from './stores/unread';

export default function SharkfinApp(props: { connected: boolean }) {
  return <SharkfinChat connected={props.connected} />;
}

export const manifest = {
  name: 'sharkfin',
  label: 'Chat',
  route: '/chat',
};

export function SidebarContent() {
  const client = getClient();
  const channelStore = createChannelStore(client);
  const userStore = createUserStore(client);
  const unreadStore = createUnreadStore(client, channelStore.activeChannel);

  return (
    <ChannelSidebar
      channels={channelStore.channels()}
      dms={channelStore.dms()}
      unreads={unreadStore.unreads()}
      users={userStore.users()}
      activeChannel={channelStore.activeChannel()}
      onSelectChannel={channelStore.setActiveChannel}
    />
  );
}
