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

  it('renders wf-user-picker with header', () => {
    const el = renderInto(() => (
      <InviteDialog channel="private-ch" users={users} open={true} onInvite={() => {}} onClose={() => {}} />
    ));
    const picker = el.querySelector('wf-user-picker') as any;
    expect(picker).toBeTruthy();
    expect(picker.header).toBe('Invite to #private-ch');
  });

  it('calls onInvite with channel and username on wf-select', () => {
    const onInvite = vi.fn();
    const el = renderInto(() => (
      <InviteDialog channel="private-ch" users={users} open={true} onInvite={onInvite} onClose={() => {}} />
    ));
    const picker = el.querySelector('wf-user-picker') as HTMLElement;
    picker.dispatchEvent(new CustomEvent('wf-select', {
      bubbles: true,
      detail: { username: 'alice' },
    }));
    expect(onInvite).toHaveBeenCalledWith('private-ch', 'alice');
  });
});
