import { describe, it, expect, vi } from 'vitest';
import { render } from 'solid-js/web';
import { ChannelSidebar } from '../../src/components/sidebar';
import type { Channel, DM, UnreadCount, User } from '@workfort/sharkfin-client';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('ChannelSidebar', () => {
  const channels: Channel[] = [
    { name: 'general', public: true, member: true },
    { name: 'random', public: true, member: true },
  ];
  const dms: DM[] = [
    { channel: 'dm-1', participants: ['alice-chen', 'me'] },
  ];
  const unreads: UnreadCount[] = [
    { channel: 'general', type: 'public', unreadCount: 3, mentionCount: 1 },
    { channel: 'random', type: 'public', unreadCount: 0, mentionCount: 0 },
  ];
  const users: User[] = [
    { username: 'alice-chen', online: true, type: 'user' },
  ];

  it('renders channel names', () => {
    const el = renderInto(() => (
      <ChannelSidebar
        channels={channels} dms={dms} unreads={unreads} users={users}
        activeChannel="general" onSelectChannel={() => {}}
        currentUsername="me"
      />
    ));
    const names = el.querySelectorAll('.sf-channel__name');
    expect(names.length).toBe(2);
    expect(names[0].textContent).toBe('general');
    expect(names[1].textContent).toBe('random');
  });

  it('marks active channel', () => {
    const el = renderInto(() => (
      <ChannelSidebar
        channels={channels} dms={dms} unreads={unreads} users={users}
        activeChannel="general" onSelectChannel={() => {}}
        currentUsername="me"
      />
    ));
    const active = el.querySelector('.sf-channel--active .sf-channel__name');
    expect(active?.textContent).toBe('general');
  });

  it('shows unread badge on channel', () => {
    const el = renderInto(() => (
      <ChannelSidebar
        channels={channels} dms={dms} unreads={unreads} users={users}
        activeChannel="random" onSelectChannel={() => {}}
        currentUsername="me"
      />
    ));
    const badges = el.querySelectorAll('wf-badge');
    expect(badges.length).toBeGreaterThan(0);
  });

  it('calls onSelectChannel when clicked', () => {
    const onSelect = vi.fn();
    const el = renderInto(() => (
      <ChannelSidebar
        channels={channels} dms={dms} unreads={unreads} users={users}
        activeChannel="general" onSelectChannel={onSelect}
        currentUsername="me"
      />
    ));
    const randomCh = el.querySelectorAll('.sf-channel')[1] as HTMLElement;
    randomCh.click();
    expect(onSelect).toHaveBeenCalledWith('random');
  });

  it('renders DM participants', () => {
    const el = renderInto(() => (
      <ChannelSidebar
        channels={channels} dms={dms} unreads={unreads} users={users}
        activeChannel="general" onSelectChannel={() => {}}
        currentUsername="me"
      />
    ));
    const dmEls = el.querySelectorAll('.sf-dm');
    expect(dmEls.length).toBe(1);
  });

  it('shows away status for online+idle user', () => {
    const usersWithIdle: User[] = [
      { username: 'alice-chen', online: true, type: 'user', state: 'idle' },
    ];
    const dms: DM[] = [
      { channel: 'dm-1', participants: ['alice-chen', 'testuser'] },
    ];
    const el = renderInto(() => (
      <ChannelSidebar
        channels={[]} dms={dms} unreads={[]} users={usersWithIdle}
        activeChannel="" onSelectChannel={() => {}}
        currentUsername="testuser"
      />
    ));
    const dot = el.querySelector('wf-status-dot') as any;
    expect(dot?.status).toBe('away');
  });

  it('calls onJoinChannel for non-member channels', () => {
    const onSelect = vi.fn();
    const onJoin = vi.fn();
    const mixedChannels: Channel[] = [
      { name: 'general', public: true, member: true },
      { name: 'unjoined', public: true, member: false },
    ];
    const el = renderInto(() => (
      <ChannelSidebar
        channels={mixedChannels} dms={dms} unreads={unreads} users={users}
        activeChannel="general" onSelectChannel={onSelect} onJoinChannel={onJoin}
        currentUsername="me"
      />
    ));
    const unjoinedCh = el.querySelectorAll('.sf-channel')[1] as HTMLElement;
    unjoinedCh.click();
    expect(onJoin).toHaveBeenCalledWith('unjoined');
    expect(onSelect).not.toHaveBeenCalled();
  });

  it('styles non-member channels with italic and reduced opacity', () => {
    const mixedChannels: Channel[] = [
      { name: 'general', public: true, member: true },
      { name: 'unjoined', public: true, member: false },
    ];
    const el = renderInto(() => (
      <ChannelSidebar
        channels={mixedChannels} dms={[]} unreads={[]} users={[]}
        activeChannel="" onSelectChannel={() => {}}
        currentUsername="me"
      />
    ));
    const names = el.querySelectorAll('.sf-channel__name');
    const unjoinedName = names[1] as HTMLElement;
    expect(unjoinedName.style.fontStyle).toBe('italic');
    expect(unjoinedName.style.opacity).toBe('0.7');
  });

  it('filters channels by search term', () => {
    const el = renderInto(() => (
      <ChannelSidebar
        channels={channels} dms={dms} unreads={unreads} users={users}
        activeChannel="general" onSelectChannel={() => {}}
        currentUsername="me"
      />
    ));
    const searchInput = el.querySelector('input[type="text"]') as HTMLInputElement;
    searchInput.value = 'ran';
    searchInput.dispatchEvent(new Event('input', { bubbles: true }));

    const names = el.querySelectorAll('.sf-channel__name');
    expect(names.length).toBe(1);
    expect(names[0].textContent).toBe('random');
  });

  it('filters DMs by participant name', () => {
    const testDms: DM[] = [
      { channel: 'dm-1', participants: ['alice-chen', 'me'] },
      { channel: 'dm-2', participants: ['bob-kim', 'me'] },
    ];
    const testUsers: User[] = [
      { username: 'alice-chen', online: true, type: 'user' },
      { username: 'bob-kim', online: true, type: 'user' },
    ];
    const el = renderInto(() => (
      <ChannelSidebar
        channels={[]} dms={testDms} unreads={[]} users={testUsers}
        activeChannel="" onSelectChannel={() => {}}
        currentUsername="me"
      />
    ));
    const searchInput = el.querySelector('input[type="text"]') as HTMLInputElement;
    searchInput.value = 'bob';
    searchInput.dispatchEvent(new Event('input', { bubbles: true }));

    const dmEls = el.querySelectorAll('.sf-dm');
    expect(dmEls.length).toBe(1);
  });

  it('shows other participant name in DMs (not current user)', () => {
    const testDms: DM[] = [
      { channel: 'dm-1', participants: ['alice-chen', 'bob-kim'] },
    ];
    const el = renderInto(() => (
      <ChannelSidebar
        channels={[]} dms={testDms} unreads={[]} users={users}
        activeChannel="" onSelectChannel={() => {}}
        currentUsername="bob-kim"
      />
    ));
    const dmEl = el.querySelector('.sf-dm span');
    expect(dmEl?.textContent).toBe('alice-chen');
  });
});
