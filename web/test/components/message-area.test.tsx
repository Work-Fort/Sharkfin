import { describe, it, expect } from 'vitest';
import { render } from 'solid-js/web';
import { MessageArea } from '../../src/components/message-area';
import type { Message as Msg } from '@workfort/sharkfin-client';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('MessageArea', () => {
  it('renders messages', () => {
    const msgs: Msg[] = [
      { id: 1, from: 'alice', body: 'hello', sentAt: '2026-03-15T09:00:00Z' },
      { id: 2, from: 'bob', body: 'hi', sentAt: '2026-03-15T09:01:00Z' },
    ];
    const el = renderInto(() => <MessageArea messages={msgs} />);
    const msgEls = el.querySelectorAll('.sf-msg');
    expect(msgEls.length).toBe(2);
  });

  it('groups consecutive messages from same author as continuations', () => {
    const msgs: Msg[] = [
      { id: 1, from: 'alice', body: 'first', sentAt: '2026-03-15T09:00:00Z' },
      { id: 2, from: 'alice', body: 'second', sentAt: '2026-03-15T09:00:30Z' },
    ];
    const el = renderInto(() => <MessageArea messages={msgs} />);
    const contMsgs = el.querySelectorAll('.sf-msg--cont');
    expect(contMsgs.length).toBe(1);
  });

  it('does not group messages from different authors', () => {
    const msgs: Msg[] = [
      { id: 1, from: 'alice', body: 'hi', sentAt: '2026-03-15T09:00:00Z' },
      { id: 2, from: 'bob', body: 'hey', sentAt: '2026-03-15T09:01:00Z' },
    ];
    const el = renderInto(() => <MessageArea messages={msgs} />);
    const contMsgs = el.querySelectorAll('.sf-msg--cont');
    expect(contMsgs.length).toBe(0);
  });

  it('renders date divider between messages on different days', () => {
    const msgs: Msg[] = [
      { id: 1, from: 'alice', body: 'older', sentAt: '2026-03-13T12:00:00Z' },
      { id: 2, from: 'bob', body: 'newer', sentAt: '2026-03-15T12:00:00Z' },
    ];
    const el = renderInto(() => <MessageArea messages={msgs} />);
    const dividers = el.querySelectorAll('.sf-divider');
    expect(dividers.length).toBeGreaterThanOrEqual(1);
  });

  it('does not render divider between same-day messages', () => {
    const msgs: Msg[] = [
      { id: 1, from: 'alice', body: 'first', sentAt: '2026-03-15T09:00:00Z' },
      { id: 2, from: 'bob', body: 'second', sentAt: '2026-03-15T10:00:00Z' },
    ];
    const el = renderInto(() => <MessageArea messages={msgs} />);
    const dividers = el.querySelectorAll('.sf-divider');
    expect(dividers.length).toBe(0);
  });

  it('does not group as continuation across day boundaries', () => {
    const msgs: Msg[] = [
      { id: 1, from: 'alice', body: 'day1', sentAt: '2026-03-13T23:00:00Z' },
      { id: 2, from: 'alice', body: 'day2', sentAt: '2026-03-15T01:00:00Z' },
    ];
    const el = renderInto(() => <MessageArea messages={msgs} />);
    const continuations = el.querySelectorAll('.sf-msg--cont');
    expect(continuations.length).toBe(0);
  });
});
