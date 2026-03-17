import { vi } from 'vitest';
import type { SharkfinClient } from '@workfort/sharkfin-client';

type Listener = (...args: unknown[]) => void;

export function createMockClient() {
  const listeners = new Map<string, Set<Listener>>();

  const mock = {
    on: vi.fn((event: string, fn: Listener) => {
      if (!listeners.has(event)) listeners.set(event, new Set());
      listeners.get(event)!.add(fn);
      return mock;
    }),
    off: vi.fn((event: string, fn: Listener) => {
      listeners.get(event)?.delete(fn);
      return mock;
    }),
    channels: vi.fn().mockResolvedValue([]),
    users: vi.fn().mockResolvedValue([]),
    history: vi.fn().mockResolvedValue([]),
    unreadCounts: vi.fn().mockResolvedValue([]),
    sendMessage: vi.fn().mockResolvedValue(1),
    markRead: vi.fn().mockResolvedValue(undefined),
    dmList: vi.fn().mockResolvedValue([]),
    dmOpen: vi.fn().mockResolvedValue({ channel: 'dm-1', participant: 'user', created: false }),
    capabilities: vi.fn().mockResolvedValue([]),
    connect: vi.fn().mockResolvedValue(undefined),
    close: vi.fn(),
    /** Emit an event to all registered listeners (test-only). */
    _emit(event: string, ...args: unknown[]) {
      listeners.get(event)?.forEach((fn) => fn(...args));
    },
  };

  return mock as typeof mock & SharkfinClient;
}

/** Flush microtask queue so resolved promises run. */
export function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}
