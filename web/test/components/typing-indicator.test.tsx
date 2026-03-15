import { describe, it, expect } from 'vitest';
import { render } from 'solid-js/web';
import { TypingIndicator } from '../../src/components/typing-indicator';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('TypingIndicator', () => {
  it('renders empty when no one is typing', () => {
    const el = renderInto(() => <TypingIndicator typingUsers={[]} />);
    const indicator = el.querySelector('.sf-typing');
    expect(indicator?.textContent?.trim()).toBe('');
  });

  it('shows single user typing', () => {
    const el = renderInto(() => <TypingIndicator typingUsers={['alice']} />);
    expect(el.querySelector('.sf-typing')?.textContent).toContain('alice is typing');
  });

  it('shows multiple users typing', () => {
    const el = renderInto(() => <TypingIndicator typingUsers={['alice', 'bob']} />);
    expect(el.querySelector('.sf-typing')?.textContent).toContain('alice and bob are typing');
  });
});
