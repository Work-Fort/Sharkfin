import { createSignal, createMemo, For, Show } from 'solid-js';
import type { Channel, DM, UnreadCount, User } from '@workfort/sharkfin-client';

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
  // Pre-compute permissions as memos for SolidJS reactivity.
  // Direct calls like props.can('x') in JSX aren't tracked by the compiler.
  const canCreateChannel = createMemo(() => !props.can || props.can('create_channel'));
  const canChannelList = createMemo(() => !props.can || props.can('channel_list'));
  const canJoinChannel = createMemo(() => !props.can || props.can('join_channel'));
  const canDmList = createMemo(() => !props.can || props.can('dm_list'));
  const canDmOpen = createMemo(() => !props.can || props.can('dm_open'));
  const [searchTerm, setSearchTerm] = createSignal('');

  const unreadFor = (channel: string) =>
    props.unreads.find((u) => u.channel === channel);

  const userStatus = (username: string) =>
    props.users.find((u) => u.username === username);

  const filteredChannels = () => {
    const term = searchTerm().toLowerCase();
    if (!term) return props.channels;
    return props.channels.filter((ch) => ch.name.toLowerCase().includes(term));
  };

  const filteredDms = () => {
    const term = searchTerm().toLowerCase();
    if (!term) return props.dms;
    return props.dms.filter((dm) => {
      const other = dm.participants.find((p) => p !== props.currentUsername) ?? dm.participants[0];
      return other.toLowerCase().includes(term);
    });
  };

  return (
    <wf-nav-sidebar
      heading="Sharkfin"
      search-placeholder="Search conversations…"
      on:wf-search={(e: CustomEvent) => setSearchTerm(e.detail.term)}
    >
      <Show when={canCreateChannel()}>
        <wf-button slot="actions" style="padding: var(--wf-space-xs) var(--wf-space-sm); font-size: var(--wf-text-sm);" title="New channel" on:click={() => props.onNewChannel?.()}>
          +
        </wf-button>
      </Show>

      <wf-nav-section heading="Channels">
        <Show when={canChannelList()} fallback={
          <div style="padding: var(--wf-space-sm) var(--wf-space-md); font-size: var(--wf-text-xs); color: var(--wf-color-text-muted);">
            No channel access
          </div>
        }>
          <wf-list>
            <For each={filteredChannels()}>
              {(ch) => {
                const count = () => unreadFor(ch.name)?.unreadCount ?? 0;
                return (
                  <wf-list-item
                    active={ch.name === props.activeChannel}
                    on:wf-select={() => {
                      if (ch.member) {
                        props.onSelectChannel(ch.name);
                      } else if (canJoinChannel()) {
                        props.onJoinChannel?.(ch.name);
                      }
                    }}
                  >
                    <span class="sf-channel__hash">#</span>
                    <span class="sf-channel__name" style={ch.member ? undefined : 'font-style: italic; color: var(--wf-color-text-muted);'}>{ch.name}</span>
                    <Show when={count() > 0}>
                      <wf-badge data-wf="trailing" count={count()} size="sm" />
                    </Show>
                  </wf-list-item>
                );
              }}
            </For>
          </wf-list>
        </Show>
      </wf-nav-section>

      <Show when={canDmList()}>
        <wf-nav-section heading="Direct Messages">
          <Show when={props.onNewDM && canDmOpen()}>
            <wf-button slot="section-actions" style="padding: 0 var(--wf-space-xs); font-size: var(--wf-text-xs);" title="New DM" on:click={() => props.onNewDM!()}>+</wf-button>
          </Show>
          <wf-list>
            <For each={filteredDms()}>
              {(dm) => {
                const other = () => dm.participants.find((p) => p !== props.currentUsername) ?? dm.participants[0];
                const status = () => userStatus(other());
                const presenceStatus = () => {
                  const s = status();
                  if (!s?.online) return 'offline';
                  return s.state === 'idle' ? 'away' : 'online';
                };
                return (
                  <wf-list-item on:wf-select={() => props.onSelectChannel(dm.channel)}>
                    <wf-avatar class="sf-dm__avatar" username={other()} size="sm" status={presenceStatus()} />
                    <span>{other()}</span>
                  </wf-list-item>
                );
              }}
            </For>
          </wf-list>
        </wf-nav-section>
      </Show>
    </wf-nav-sidebar>
  );
}
