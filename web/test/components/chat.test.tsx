import { describe, it, expect, vi } from 'vitest';
import { render } from 'solid-js/web';

// Mock the client module so we don't need a real WebSocket.
vi.mock('../../src/client', () => {
  // We need to inline the mock since dynamic imports in vi.mock are tricky
  const listeners = new Map<string, Set<Function>>();
  const mock = {
    on: vi.fn((event: string, fn: Function) => {
      if (!listeners.has(event)) listeners.set(event, new Set());
      listeners.get(event)!.add(fn);
      return mock;
    }),
    off: vi.fn(),
    channels: vi.fn().mockResolvedValue([{ name: 'general', public: true, member: true }]),
    users: vi.fn().mockResolvedValue([]),
    dmList: vi.fn().mockResolvedValue([]),
    unreadCounts: vi.fn().mockResolvedValue([]),
    history: vi.fn().mockResolvedValue([]),
    sendMessage: vi.fn().mockResolvedValue(1),
    markRead: vi.fn().mockResolvedValue(undefined),
    connect: vi.fn().mockResolvedValue(undefined),
    close: vi.fn(),
  };
  return { getClient: () => mock, resetClient: vi.fn() };
});

import { SharkfinChat } from '../../src/components/chat';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('SharkfinChat', () => {
  it('renders main layout structure when connected', async () => {
    const el = renderInto(() => <SharkfinChat connected={true} />);
    // Allow async store initialization
    await new Promise(r => setTimeout(r, 100));
    expect(el.querySelector('.sf-main')).toBeTruthy();
    expect(el.querySelector('.sf-main__header')).toBeTruthy();
    expect(el.querySelector('.sf-messages')).toBeTruthy();
    expect(el.querySelector('.sf-input')).toBeTruthy();
  });

  it('shows disconnected banner when not connected', () => {
    const el = renderInto(() => <SharkfinChat connected={false} />);
    expect(el.querySelector('wf-banner')).toBeTruthy();
  });
});
