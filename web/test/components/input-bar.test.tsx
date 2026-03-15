import { describe, it, expect, vi } from 'vitest';
import { render } from 'solid-js/web';
import { InputBar } from '../../src/components/input-bar';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('InputBar', () => {
  it('renders textarea with placeholder', () => {
    const el = renderInto(() => <InputBar channel="general" onSend={() => {}} />);
    const textarea = el.querySelector('textarea');
    expect(textarea).toBeTruthy();
    expect(textarea?.placeholder).toBe('Message #general');
  });

  it('calls onSend and clears input on Enter', () => {
    const onSend = vi.fn();
    const el = renderInto(() => <InputBar channel="general" onSend={onSend} />);
    const textarea = el.querySelector('textarea')!;

    textarea.value = 'hello world';
    textarea.dispatchEvent(new Event('input', { bubbles: true }));

    textarea.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));

    expect(onSend).toHaveBeenCalledWith('hello world');
    expect(textarea.value).toBe('');
  });

  it('does not send on Shift+Enter', () => {
    const onSend = vi.fn();
    const el = renderInto(() => <InputBar channel="general" onSend={onSend} />);
    const textarea = el.querySelector('textarea')!;

    textarea.value = 'hello';
    textarea.dispatchEvent(new Event('input', { bubbles: true }));
    textarea.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', shiftKey: true, bubbles: true }));

    expect(onSend).not.toHaveBeenCalled();
  });

  it('does not send empty messages', () => {
    const onSend = vi.fn();
    const el = renderInto(() => <InputBar channel="general" onSend={onSend} />);
    const textarea = el.querySelector('textarea')!;

    textarea.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));

    expect(onSend).not.toHaveBeenCalled();
  });
});
