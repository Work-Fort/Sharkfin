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

  it('renders wf-user-picker', () => {
    const el = renderInto(() => (
      <DMDialog users={users} currentUsername="me" open={true} onSelect={() => {}} onClose={() => {}} />
    ));
    const picker = el.querySelector('wf-user-picker') as any;
    expect(picker).toBeTruthy();
    expect(picker.getAttribute('header')).toBe('New Direct Message');
  });

  it('passes exclude as currentUsername', () => {
    const el = renderInto(() => (
      <DMDialog users={users} currentUsername="me" open={true} onSelect={() => {}} onClose={() => {}} />
    ));
    const picker = el.querySelector('wf-user-picker') as any;
    expect(picker.exclude).toBe('me');
  });

  it('calls onSelect when wf-select event fires', () => {
    const onSelect = vi.fn();
    const el = renderInto(() => (
      <DMDialog users={users} currentUsername="me" open={true} onSelect={onSelect} onClose={() => {}} />
    ));
    const picker = el.querySelector('wf-user-picker') as HTMLElement;
    picker.dispatchEvent(new CustomEvent('wf-select', {
      bubbles: true,
      detail: { username: 'alice' },
    }));
    expect(onSelect).toHaveBeenCalledWith('alice');
  });
});
