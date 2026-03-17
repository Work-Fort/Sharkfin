import { describe, it, expect, vi } from 'vitest';
import { render } from 'solid-js/web';
import { InputBar } from '../../src/components/input-bar';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('InputBar', () => {
  it('renders wf-compose-input with placeholder', () => {
    const el = renderInto(() => <InputBar channel="general" onSend={() => {}} />);
    const compose = el.querySelector('wf-compose-input') as HTMLElement;
    expect(compose).toBeTruthy();
    expect((compose as any)?.placeholder).toBe('Message #general');
  });

  it('wraps in sf-input div', () => {
    const el = renderInto(() => <InputBar channel="general" onSend={() => {}} />);
    expect(el.querySelector('.sf-input')).toBeTruthy();
    expect(el.querySelector('.sf-input wf-compose-input')).toBeTruthy();
  });

  it('calls onSend when wf-send event fires', () => {
    const onSend = vi.fn();
    const el = renderInto(() => <InputBar channel="general" onSend={onSend} />);
    const compose = el.querySelector('wf-compose-input') as HTMLElement;

    compose.dispatchEvent(new CustomEvent('wf-send', {
      bubbles: true,
      detail: { body: 'hello world' },
    }));

    expect(onSend).toHaveBeenCalledWith('hello world');
  });
});
