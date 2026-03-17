import { describe, it, expect, vi } from 'vitest';
import { render } from 'solid-js/web';
import { CreateChannelDialog } from '../../src/components/create-channel-dialog';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('CreateChannelDialog', () => {
  it('renders dialog with channel name input', () => {
    const el = renderInto(() => (
      <CreateChannelDialog open={true} onCreate={() => {}} onClose={() => {}} />
    ));
    expect(el.querySelector('wf-dialog')).toBeTruthy();
    expect(el.querySelector('input[placeholder*="hannel"]')).toBeTruthy();
  });

  it('renders public/private toggle', () => {
    const el = renderInto(() => (
      <CreateChannelDialog open={true} onCreate={() => {}} onClose={() => {}} />
    ));
    const checkbox = el.querySelector('input[type="checkbox"]');
    expect(checkbox).toBeTruthy();
  });

  it('calls onCreate with name and visibility', () => {
    const onCreate = vi.fn();
    const el = renderInto(() => (
      <CreateChannelDialog open={true} onCreate={onCreate} onClose={() => {}} />
    ));
    const input = el.querySelector('input[type="text"]') as HTMLInputElement;
    input.value = 'new-channel';
    input.dispatchEvent(new Event('input', { bubbles: true }));

    const buttons = el.querySelectorAll('wf-button');
    const createBtn = Array.from(buttons).find(b => b.textContent?.includes('Create'));
    createBtn?.dispatchEvent(new MouseEvent('click', { bubbles: true }));

    expect(onCreate).toHaveBeenCalledWith('new-channel', true);
  });
});
