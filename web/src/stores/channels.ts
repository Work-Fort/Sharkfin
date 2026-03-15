import { createSignal } from 'solid-js';
import type { SharkfinClient, Channel, DM } from '@workfort/sharkfin-client';

export function createChannelStore(client: SharkfinClient) {
  const [channels, setChannels] = createSignal<Channel[]>([]);
  const [activeChannel, setActiveChannel] = createSignal('');
  const [dms, setDms] = createSignal<DM[]>([]);

  // Fetch initial data.
  client.channels().then(setChannels);
  client.dmList().then(setDms);

  return { channels, activeChannel, setActiveChannel, dms };
}
