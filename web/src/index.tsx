import { SharkfinChat } from './components/chat';
import { ChannelSidebar } from './components/sidebar';
import { getStores } from './stores';

export default function SharkfinApp(props: { connected: boolean }) {
  return <SharkfinChat connected={props.connected} />;
}

export const manifest = {
  name: 'sharkfin',
  label: 'Chat',
  route: '/chat',
};

export function SidebarContent() {
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
