import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render } from 'solid-js/web';
import { createSignal } from 'solid-js';

// Mock the stores module so we don't need a real WebSocket.
const [channels, setChannels] = createSignal([{ name: 'general', public: true, member: true }]);
const [activeChannel, setActiveChannel] = createSignal('');
const [dms] = createSignal([]);
const [messages] = createSignal([]);
const [unreads] = createSignal([]);
const [users] = createSignal([]);

vi.mock('../../src/stores', () => ({
  getStores: () => ({
    channels: { channels, activeChannel, setActiveChannel, dms },
    messages: { messages, sendMessage: vi.fn().mockResolvedValue(undefined) },
    users: { users },
    unread: { unreads, totalUnread: () => 0 },
  }),
  resetStores: vi.fn(),
}));

import { SharkfinChat } from '../../src/components/chat';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('SharkfinChat', () => {
  beforeEach(() => {
    setActiveChannel('');
  });

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
