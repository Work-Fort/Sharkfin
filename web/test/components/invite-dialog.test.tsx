import { describe, it, expect, vi } from 'vitest';
import { render } from 'solid-js/web';
import { InviteDialog } from '../../src/components/invite-dialog';
import type { User } from '@workfort/sharkfin-client';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('InviteDialog', () => {
  const users: User[] = [
    { username: 'alice', online: true, type: 'user' },
    { username: 'bob', online: false, type: 'agent' },
  ];

  it('renders dialog with user list', () => {
    const el = renderInto(() => (
      <InviteDialog channel="private-ch" users={users} open={true} onInvite={() => {}} onClose={() => {}} />
    ));
    expect(el.querySelector('wf-dialog')).toBeTruthy();
    const items = el.querySelectorAll('wf-list-item');
    expect(items.length).toBe(2);
  });

  it('calls onInvite with channel and username', () => {
    const onInvite = vi.fn();
    const el = renderInto(() => (
      <InviteDialog channel="private-ch" users={users} open={true} onInvite={onInvite} onClose={() => {}} />
    ));
    const firstItem = el.querySelector('wf-list-item') as HTMLElement;
    firstItem?.dispatchEvent(new CustomEvent('wf-select', { bubbles: true }));
    expect(onInvite).toHaveBeenCalledWith('private-ch', 'alice');
  });
});
