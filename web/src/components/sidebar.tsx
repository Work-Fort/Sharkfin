import { For, Show } from 'solid-js';
import type { Channel, DM, UnreadCount, User } from '@workfort/sharkfin-client';
import { initials } from '../utils';

interface SidebarProps {
  channels: Channel[];
  dms: DM[];
  unreads: UnreadCount[];
  users: User[];
  activeChannel: string;
  onSelectChannel: (channel: string) => void;
  currentUsername: string;
  onNewDM?: () => void;
  onNewChannel?: () => void;
  onJoinChannel?: (channel: string) => void;
  can?: (permission: string) => boolean;
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
        <Show when={!props.can || props.can('create_channel')}>
          <wf-button style="padding: 2px 6px; font-size: 14px;" title="New channel" on:click={() => props.onNewChannel?.()}>
            +
          </wf-button>
        </Show>
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
                on:click={() => {
                  if (ch.member) {
                    props.onSelectChannel(ch.name);
                  } else {
                    props.onJoinChannel?.(ch.name);
                  }
                }}
              >
                <span class="sf-channel__hash">#</span>
                <span class="sf-channel__name" style={ch.member ? undefined : 'font-style: italic; opacity: 0.7;'}>{ch.name}</span>
                <Show when={count() > 0}>
                  <wf-badge count={count()} size="sm" />
                </Show>
              </div>
            );
          }}
        </For>

        <Show when={!props.can || props.can('dm_list')}>
          <div class="sf-section-label" style="display: flex; justify-content: space-between; align-items: center;">
            Direct Messages
            <Show when={props.onNewDM && (!props.can || props.can('dm_open'))}>
              <wf-button style="padding: 1px 5px; font-size: 12px;" title="New DM" on:click={() => props.onNewDM!()}>+</wf-button>
            </Show>
          </div>
          <For each={props.dms}>
            {(dm) => {
              const other = () => dm.participants.find((p) => p !== props.currentUsername) ?? dm.participants[0];
              const status = () => userStatus(other());
              const presenceStatus = () => {
                const s = status();
                if (!s?.online) return 'offline';
                return s.state === 'idle' ? 'away' : 'online';
              };
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
        </Show>
      </div>
    </div>
  );
}
