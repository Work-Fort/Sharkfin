import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render } from 'solid-js/web';
import { createSignal } from 'solid-js';

// Use vi.hoisted so these are available inside the hoisted vi.mock factory.
const mocks = vi.hoisted(() => {
  // We can't import solid-js inside vi.hoisted, so we'll create simple getter fns.
  let _activeChannel = '';
  let _canFn: (p: string) => boolean = () => true;
  return {
    setActiveChannel: (v: string) => { _activeChannel = v; },
    getActiveChannel: () => _activeChannel,
    setCanFn: (fn: (p: string) => boolean) => { _canFn = fn; },
    can: (p: string) => _canFn(p),
  };
});

vi.mock('../../src/stores', async () => {
  const { createSignal } = await import('solid-js');
  const [channels] = createSignal([{ name: 'general', public: true, member: true }]);
  const [activeChannel, setActiveChannel] = createSignal('');
  const [dms] = createSignal([]);
  const [messages] = createSignal([]);
  const [unreads] = createSignal([]);
  const [users] = createSignal([]);
  const [connectionState] = createSignal('connected');
  const [loading] = createSignal(false);

  return {
    initApp: async () => {},
    getStores: () => ({
      channels: { channels, activeChannel, setActiveChannel, dms, setChannels: () => {}, setDms: () => {} },
      messages: { messages, sendMessage: vi.fn().mockResolvedValue(undefined) },
      users: { users, setUsers: () => {} },
      unread: { unreads, totalUnread: () => 0, setUnreads: () => {} },
      permissions: { can: mocks.can, permissions: () => new Set<string>() },
    }),
    connectionState,
    loading,
    resetStores: () => {},
  };
});

import { SharkfinChat } from '../../src/components/chat';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('SharkfinChat', () => {
  beforeEach(() => {
    mocks.setCanFn(() => true);
  });

  it('renders main layout structure when connected', async () => {
    const el = renderInto(() => <SharkfinChat connected={true} />);
    // Allow async store initialization (onMount + initApp)
    await new Promise(r => setTimeout(r, 100));
    expect(el.querySelector('.sf-main')).toBeTruthy();
    expect(el.querySelector('.sf-main__header')).toBeTruthy();
    expect(el.querySelector('.sf-messages')).toBeTruthy();
    expect(el.querySelector('.sf-input')).toBeTruthy();
  });

  it('shows sign-in banner when initApp fails', async () => {
    // Override initApp to reject, simulating auth failure.
    const stores = await import('../../src/stores');
    const original = stores.initApp;
    (stores as any).initApp = async () => { throw new Error('auth'); };
    const el = renderInto(() => <SharkfinChat connected={false} />);
    await new Promise(r => setTimeout(r, 100));
    const banner = el.querySelector('wf-banner');
    expect(banner).toBeTruthy();
    expect(banner?.getAttribute('headline')).toBe('Sign in to use Chat');
    (stores as any).initApp = original;
  });

  it('shows no-permission message when history permission is denied', async () => {
    mocks.setCanFn((p: string) => p !== 'history');
    const el = renderInto(() => <SharkfinChat connected={true} />);
    await new Promise(r => setTimeout(r, 100));
    expect(el.textContent).toContain("You don't have permission to view message history.");
  });
});
