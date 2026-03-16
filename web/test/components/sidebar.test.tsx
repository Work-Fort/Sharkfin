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
