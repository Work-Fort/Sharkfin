import { For, Show } from 'solid-js';
import type { Channel, DM, UnreadCount, User } from '@workfort/sharkfin-client';

/** Extract initials from a username. */
function initials(username: string): string {
  const parts = username.split(/[-_.\s]+/);
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
  return username.slice(0, 2).toUpperCase();
}

interface SidebarProps {
  channels: Channel[];
  dms: DM[];
  unreads: UnreadCount[];
  users: User[];
  activeChannel: string;
  onSelectChannel: (channel: string) => void;
}

export function ChannelSidebar(props: SidebarProps) {
  const unreadFor = (channel: string) =>
    props.unreads.find((u) => u.channel === channel);

  const userStatus = (username: string) =>
    props.users.find((u) => u.username === username);

  return (
    <div class="sf-sidebar">
      <div class="sf-sidebar__header">
        <span class="sf-sidebar__title">Sharkfin</span>
        <wf-button style="padding: 2px 6px; font-size: 14px;" title="New channel">
          +
        </wf-button>
      </div>
      <div class="sf-sidebar__search">
        <input type="text" placeholder="Search conversations\u2026" />
      </div>
      <div class="sf-channels">
        <div class="sf-section-label">Channels</div>
        <For each={props.channels}>
          {(ch) => {
            const count = () => unreadFor(ch.name)?.unreadCount ?? 0;
            return (
              <div
                class={`sf-channel${ch.name === props.activeChannel ? ' sf-channel--active' : ''}`}
                on:click={() => props.onSelectChannel(ch.name)}
              >
                <span class="sf-channel__hash">#</span>
                <span class="sf-channel__name">{ch.name}</span>
                <Show when={count() > 0}>
                  <wf-badge count={count()} size="sm" />
                </Show>
              </div>
            );
          }}
        </For>

        <div class="sf-section-label">Direct Messages</div>
        <For each={props.dms}>
          {(dm) => {
            const other = () => dm.participants.find((p) => p !== 'me') ?? dm.participants[0];
            const status = () => userStatus(other());
            const presenceStatus = () => (status()?.online ? 'online' : 'offline');
            return (
              <div class="sf-dm" on:click={() => props.onSelectChannel(dm.channel)}>
                <div class="sf-dm__avatar">
                  {initials(other())}
                  <wf-status-dot
                    status={presenceStatus()}
                    style="position:absolute;bottom:-1px;right:-1px;"
                  />
                </div>
                <span>{other()}</span>
              </div>
            );
          }}
        </For>
      </div>
    </div>
  );
}
