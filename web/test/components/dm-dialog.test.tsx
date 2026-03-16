import { describe, it, expect, vi } from 'vitest';
import { render } from 'solid-js/web';
import { DMDialog } from '../../src/components/dm-dialog';
import type { User } from '@workfort/sharkfin-client';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('DMDialog', () => {
  const users: User[] = [
    { username: 'alice', online: true, type: 'user' },
    { username: 'bob', online: false, type: 'agent' },
  ];

  it('renders dialog', () => {
    const el = renderInto(() => (
      <DMDialog users={users} currentUsername="me" open={true} onSelect={() => {}} onClose={() => {}} />
    ));
    expect(el.querySelector('wf-dialog')).toBeTruthy();
  });

  it('filters out current user from list', () => {
    const el = renderInto(() => (
      <DMDialog
        users={[...users, { username: 'me', online: true, type: 'user' }]}
        currentUsername="me" open={true} onSelect={() => {}} onClose={() => {}}
      />
    ));
    const items = el.querySelectorAll('wf-list-item');
    expect(items.length).toBe(2);
  });

  it('calls onSelect when user is clicked', () => {
    const onSelect = vi.fn();
    const el = renderInto(() => (
      <DMDialog users={users} currentUsername="me" open={true} onSelect={onSelect} onClose={() => {}} />
    ));
    const firstItem = el.querySelector('wf-list-item') as HTMLElement;
    firstItem?.dispatchEvent(new CustomEvent('wf-select', { bubbles: true }));
    expect(onSelect).toHaveBeenCalledWith('alice');
  });
});
