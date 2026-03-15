import { describe, it, expect } from 'vitest';
import { render } from 'solid-js/web';
import { Message } from '../../src/components/message';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('Message', () => {
  it('renders author, time, and body', () => {
    const el = renderInto(() => (
      <Message from="alice" body="hello world" sentAt="2026-03-15T09:14:00Z" />
    ));
    expect(el.querySelector('.sf-msg__author')?.textContent).toBe('alice');
    expect(el.querySelector('.sf-msg__text')?.textContent).toBe('hello world');
    expect(el.querySelector('.sf-msg__time')?.textContent).toBeTruthy();
  });

  it('renders avatar initials from username', () => {
    const el = renderInto(() => (
      <Message from="alice-chen" body="hi" sentAt="2026-03-15T09:14:00Z" />
    ));
    expect(el.querySelector('.sf-msg__avatar')?.textContent?.trim()).toBe('AC');
  });

  it('hides avatar and header for continuation messages', () => {
    const el = renderInto(() => (
      <Message from="alice" body="continued" sentAt="2026-03-15T09:14:00Z" continuation />
    ));
    expect(el.querySelector('.sf-msg--cont')).toBeTruthy();
    expect(el.querySelector('.sf-msg__avatar--hidden')).toBeTruthy();
    expect(el.querySelector('.sf-msg__header')).toBeFalsy();
  });
});
