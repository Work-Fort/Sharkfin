# Web UI Fix-Up — Connection Lifecycle, Identity, DMs, Presence (Plan 3 of 4)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the 9 critical and important gaps from the gap analysis: connection lifecycle (`connect()` never called), loading/error states, reconnection UI, current user identity via `useAuth()`, DM participant display, DM creation, full presence (online/away/offline), `setState()`, and permission-gated UI via `capabilities()`.

**Architecture:** The store singleton (`getStores()`) becomes async — it calls `client.connect()` and exposes `connectionState` and `loading` signals. Components gate rendering on connection state. The current user's identity comes from `@workfort/ui-solid`'s `useAuth()` hook, which reads from the shell's shared `@workfort/auth` MF singleton. DM creation uses `wf-dialog`.

**Tech Stack:** SolidJS 1.9, `@workfort/sharkfin-client`, `@workfort/ui-solid` (`useAuth`), `@workfort/ui` (`wf-dialog`, `wf-banner`, `wf-skeleton`)

---

## Coverage Checklist

| Gap # | Description | Task |
|-------|-------------|------|
| 1 | `client.connect()` never called | Task 1 |
| 6 | No loading states (`wf-skeleton`, `wf-spinner`) | Task 2 |
| 7 | No reconnection UI (`disconnect`/`reconnect` events) | Task 3 |
| 15 | `setState()` not called | Task 3 |
| 2 | Hardcoded `'me'` in DM participant filter | Task 4 |
| 10 | Presence doesn't distinguish away | Task 5 |
| 5 | No `dmOpen()` UI | Task 6 |
| 8 | DMs broken overall | Tasks 4 + 6 |
| 16 | `capabilities()` not used — no permission-gated UI | Task 6 (new) |

| Success Criterion | Verified By |
|---|---|
| 3. Messages with real-time updates | Task 1 (connect) |
| 4. User can send messages | Task 1 (connect) |
| 5. Presence online/away/offline | Task 5 |
| 6. Unread badges update | Task 1 (connect) |
| 8. DMs work | Tasks 4, 6 |

---

### Task 1: Connection Lifecycle — `client.connect()` + Loading State

**Files:**
- Modify: `web/src/stores/index.ts`
- Modify: `web/src/components/chat.tsx`
- Modify: `web/src/index.tsx`
- Create: `web/test/stores/connection.test.ts`

**Step 1: Write failing test**

Create `web/test/stores/connection.test.ts`:

```typescript
import { describe, it, expect, vi } from 'vitest';
import { createRoot } from 'solid-js';

vi.mock('../src/client', () => {
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
    setState: vi.fn().mockResolvedValue(undefined),
  };
  return { getClient: () => mock, resetClient: vi.fn() };
});

import { initApp, connectionState, loading } from '../src/stores';
import { flushPromises } from './helpers';

describe('initApp', () => {
  it('calls client.connect()', async () => {
    const { getClient } = await import('../src/client');
    await createRoot(async () => {
      await initApp();
    });
    expect(getClient().connect).toHaveBeenCalled();
  });

  it('sets loading to true initially, false after init', async () => {
    let loadingBefore: boolean;
    await createRoot(async () => {
      loadingBefore = loading();
      await initApp();
    });
    expect(loadingBefore!).toBe(true);
    expect(loading()).toBe(false);
  });

  it('sets connectionState to connected after init', async () => {
    await createRoot(async () => {
      await initApp();
    });
    expect(connectionState()).toBe('connected');
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/stores/connection.test.ts`
Expected: FAIL — `initApp`, `connectionState`, `loading` not exported.

**Step 3: Implement connection lifecycle**

Rewrite `web/src/stores/index.ts`:

```typescript
import { createSignal, createRoot } from 'solid-js';
import { getClient } from '../client';
import { createChannelStore } from './channels';
import { createMessageStore } from './messages';
import { createUserStore } from './users';
import { createUnreadStore } from './unread';

const [connectionState, setConnectionState] = createSignal<'connecting' | 'connected' | 'disconnected'>('connecting');
const [loading, setLoading] = createSignal(true);

let _stores: ReturnType<typeof createStores> | null = null;
let _dispose: (() => void) | null = null;

function createStores() {
  const client = getClient();
  const channels = createChannelStore(client);
  const messages = createMessageStore(client, channels.activeChannel);
  const users = createUserStore(client);
  const unread = createUnreadStore(client, channels.activeChannel);
  return { channels, messages, users, unread };
}

export async function initApp(): Promise<void> {
  const client = getClient();

  client.on('disconnect', () => setConnectionState('disconnected'));
  client.on('reconnect', () => {
    setConnectionState('connected');
    // Refetch data after reconnect.
    const stores = getStores();
    client.channels().then((chs) => stores.channels.setChannels?.(chs)).catch(() => {});
    client.users().then((us) => stores.users.setUsers?.(us)).catch(() => {});
    client.unreadCounts().then((counts) => stores.unread.setUnreads?.(counts)).catch(() => {});
  });

  await client.connect();
  setConnectionState('connected');

  createRoot((dispose) => {
    _stores = createStores();
    _dispose = dispose;
  });
  setLoading(false);
}

export function getStores() {
  if (!_stores) throw new Error('initApp() must be called before getStores()');
  return _stores;
}

export { connectionState, loading };

/** Reset singletons (for tests). */
export function resetStores(): void {
  _dispose?.();
  _dispose = null;
  _stores = null;
  setConnectionState('connecting');
  setLoading(true);
}
```

Note: The channel store, user store, and unread store need to expose their setters for reconnect refetch. Add `setChannels` to the channels store return, `setUsers` to the users store return, and `setUnreads` to the unread store return. This is a one-line change in each store file — add the setter to the return object.

In `web/src/stores/channels.ts`, change return to:
```typescript
return { channels, activeChannel, setActiveChannel, dms, setChannels, setDms };
```

In `web/src/stores/users.ts`, change return to:
```typescript
return { users, setUsers };
```

In `web/src/stores/unread.ts`, change return to:
```typescript
return { unreads, totalUnread, setUnreads };
```

**Step 4: Update chat.tsx to use initApp**

Replace `web/src/components/chat.tsx`:

```tsx
import { Show, createEffect, onMount } from 'solid-js';
import '../styles/chat.css';
import { initApp, getStores, connectionState, loading } from '../stores';
import { ChannelHeader } from './channel-header';
import { MessageArea } from './message-area';
import { TypingIndicator } from './typing-indicator';
import { InputBar } from './input-bar';

interface SharkfinChatProps {
  connected: boolean;
}

export function SharkfinChat(props: SharkfinChatProps) {
  onMount(async () => {
    try {
      await initApp();
    } catch {
      // Connection failed — connectionState stays 'connecting',
      // client will retry via reconnect: true.
    }
  });

  return (
    <div class="sf-main">
      <Show when={!props.connected}>
        <wf-banner variant="warning" headline="Chat service is unavailable." />
      </Show>
      <Show when={loading()}>
        <div style="padding: var(--wf-space-lg);">
          <wf-skeleton width="100%" height="2rem" />
          <wf-skeleton width="100%" height="200px" style="margin-top: var(--wf-space-md);" />
          <wf-skeleton width="60%" height="1rem" style="margin-top: var(--wf-space-md);" />
        </div>
      </Show>
      <Show when={!loading() && connectionState() !== 'connecting'}>
        <Show when={connectionState() === 'disconnected'}>
          <wf-banner variant="warning" headline="Connection lost. Reconnecting\u2026" />
        </Show>
        <ChatContent />
      </Show>
    </div>
  );
}

function ChatContent() {
  const { channels, messages } = getStores();

  createEffect(() => {
    const chs = channels.channels();
    if (chs.length > 0 && !channels.activeChannel()) {
      channels.setActiveChannel(chs[0].name);
    }
  });

  return (
    <>
      <ChannelHeader name={channels.activeChannel()} />
      <MessageArea messages={messages.messages()} />
      <TypingIndicator typingUsers={[]} />
      <InputBar
        channel={channels.activeChannel()}
        onSend={(body) => messages.sendMessage(body)}
      />
    </>
  );
}
```

**Step 5: Update index.tsx**

Replace `web/src/index.tsx`:

```tsx
import { SharkfinChat } from './components/chat';
import { ChannelSidebar } from './components/sidebar';
import { getStores, loading } from './stores';
import { Show } from 'solid-js';

export default function SharkfinApp(props: { connected: boolean }) {
  return <SharkfinChat connected={props.connected} />;
}

export const manifest = {
  name: 'sharkfin',
  label: 'Chat',
  route: '/chat',
};

export function SidebarContent() {
  return (
    <Show when={!loading()} fallback={
      <div style="padding: var(--wf-space-md);">
        <wf-skeleton width="100%" height="1.5rem" />
        <wf-skeleton width="100%" height="1.5rem" style="margin-top: var(--wf-space-sm);" />
        <wf-skeleton width="80%" height="1.5rem" style="margin-top: var(--wf-space-sm);" />
      </div>
    }>
      <SidebarLoaded />
    </Show>
  );
}

function SidebarLoaded() {
  const { channels, users, unread } = getStores();

  return (
    <ChannelSidebar
      channels={channels.channels()}
      dms={channels.dms()}
      unreads={unread.unreads()}
      users={users.users()}
      activeChannel={channels.activeChannel()}
      onSelectChannel={channels.setActiveChannel}
    />
  );
}
```

**Step 6: Update chat.test.tsx to work with new initApp pattern**

The existing chat test mocks `../../src/stores` — update it to also mock `initApp`, `connectionState`, and `loading`:

```tsx
vi.mock('../../src/stores', () => {
  const { createSignal } = require('solid-js');
  const [connectionState] = createSignal('connected');
  const [loading] = createSignal(false);

  const channels = { channels: () => [{ name: 'general', public: true, member: true }], activeChannel: () => 'general', setActiveChannel: () => {}, dms: () => [], setChannels: () => {}, setDms: () => {} };
  const messages = { messages: () => [], sendMessage: async () => {} };
  const users = { users: () => [], setUsers: () => {} };
  const unread = { unreads: () => [], totalUnread: () => 0, setUnreads: () => {} };

  return {
    initApp: async () => {},
    getStores: () => ({ channels, messages, users, unread }),
    connectionState,
    loading,
    resetStores: () => {},
  };
});
```

**Step 7: Run tests**

Run: `cd web && pnpm test`
Expected: All tests pass.

**Step 8: Commit**

```bash
git add web/src/stores/ web/src/components/chat.tsx web/src/index.tsx web/test/
git commit -m "feat: add connection lifecycle — client.connect(), loading states, reconnection UI"
```

---

### Task 2: Current User Identity via `useAuth()`

**Upstream dependency:** `useAuth()` in `@workfort/ui-solid` has a known issue — `getAuthClient()` is async but `use-auth.ts` calls it synchronously. Before implementing this task, verify the fix has been merged in the scope repo, or apply this workaround in `use-auth.ts`:

```typescript
export function useAuth() {
  const [user, setUser] = createSignal<User | null>(null);
  const isAuthenticated = () => user() !== null;
  getAuthClient().then((client) => {
    client.getUser().then(setUser);
    const unsub = client.onAuthChange(setUser);
    onCleanup(unsub);
  });
  return { user, isAuthenticated };
}
```

**Files:**
- Modify: `web/src/components/sidebar.tsx`
- Modify: `web/src/index.tsx`
- Modify: `web/test/components/sidebar.test.tsx`

The shell shares `@workfort/auth` as a Module Federation singleton. The `@workfort/ui-solid` package provides `useAuth()` which returns `{ user, isAuthenticated }` as SolidJS signals.

**Step 1: Write failing test**

Update `web/test/components/sidebar.test.tsx` to test DM participant filtering with a real username instead of `'me'`:

Add a test:
```tsx
it('shows other participant name in DMs (not current user)', () => {
  const dms: DM[] = [
    { channel: 'dm-1', participants: ['alice-chen', 'bob-kim'] },
  ];
  const el = renderInto(() => (
    <ChannelSidebar
      channels={[]} dms={dms} unreads={[]} users={users}
      activeChannel="" onSelectChannel={() => {}}
      currentUsername="bob-kim"
    />
  ));
  const dmEl = el.querySelector('.sf-dm span');
  expect(dmEl?.textContent).toBe('alice-chen');
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/components/sidebar.test.tsx`
Expected: FAIL — `currentUsername` prop not recognized, DM still shows wrong name.

**Step 3: Implement**

Update `web/src/components/sidebar.tsx` — add `currentUsername` to props and use it instead of `'me'`:

```tsx
interface SidebarProps {
  channels: Channel[];
  dms: DM[];
  unreads: UnreadCount[];
  users: User[];
  activeChannel: string;
  onSelectChannel: (channel: string) => void;
  currentUsername: string;
}
```

Change the DM participant filter from:
```typescript
const other = () => dm.participants.find((p) => p !== 'me') ?? dm.participants[0];
```
to:
```typescript
const other = () => dm.participants.find((p) => p !== props.currentUsername) ?? dm.participants[0];
```

Update `web/src/index.tsx` — `SidebarLoaded` passes the current username from `useAuth()`:

```tsx
import { useAuth } from '@workfort/ui-solid';

function SidebarLoaded() {
  const { channels, users, unread } = getStores();
  const { user } = useAuth();

  return (
    <ChannelSidebar
      channels={channels.channels()}
      dms={channels.dms()}
      unreads={unread.unreads()}
      users={users.users()}
      activeChannel={channels.activeChannel()}
      onSelectChannel={channels.setActiveChannel}
      currentUsername={user()?.name ?? ''}
    />
  );
}
```

Note: Add `@workfort/ui-solid` to `web/package.json` dependencies. Check if the scope repo's packages are published to npm or consumed via workspace link. If workspace link, use the documentation repo's pattern: `"@workfort/ui-solid": "link:../../scope/lead/web/packages/solid"`. If published, use `"latest"`.

Also add it to the MF shared config in `web/vite.config.ts`:
```typescript
shared: {
  'solid-js': { singleton: true },
  '@workfort/ui': { singleton: true },
  '@workfort/ui-solid': { singleton: true },
},
```

**Step 4: Update existing sidebar tests**

All existing sidebar tests need to pass `currentUsername="me"` (or any string) to `ChannelSidebar` since it's now a required prop. Update the test fixtures.

**Step 5: Run tests**

Run: `cd web && pnpm test`
Expected: All tests pass.

**Step 6: Commit**

```bash
git add web/src/components/sidebar.tsx web/src/index.tsx web/test/components/sidebar.test.tsx web/package.json web/vite.config.ts
git commit -m "feat: resolve current user identity via useAuth() for DM display"
```

---

### Task 3: Presence — Online/Away/Offline + setState()

**Files:**
- Modify: `web/src/components/sidebar.tsx`
- Modify: `web/src/stores/index.ts` (add `setState` call)
- Create: `web/src/hooks/use-idle.ts`
- Modify: `web/test/components/sidebar.test.tsx`

**Step 1: Write failing test for away presence**

Add to `web/test/components/sidebar.test.tsx`:

```tsx
it('shows away status for online+idle user', () => {
  const usersWithIdle: User[] = [
    { username: 'alice-chen', online: true, type: 'user', state: 'idle' },
  ];
  const dms: DM[] = [
    { channel: 'dm-1', participants: ['alice-chen', 'testuser'] },
  ];
  const el = renderInto(() => (
    <ChannelSidebar
      channels={[]} dms={dms} unreads={[]} users={usersWithIdle}
      activeChannel="" onSelectChannel={() => {}}
      currentUsername="testuser"
    />
  ));
  const dot = el.querySelector('wf-status-dot');
  expect(dot?.getAttribute('status')).toBe('away');
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/components/sidebar.test.tsx`
Expected: FAIL — dot shows `online` instead of `away`.

**Step 3: Fix presence mapping in sidebar**

In `web/src/components/sidebar.tsx`, change:

```typescript
const presenceStatus = () => (status()?.online ? 'online' : 'offline');
```

to:

```typescript
const presenceStatus = () => {
  const s = status();
  if (!s?.online) return 'offline';
  return s.state === 'idle' ? 'away' : 'online';
};
```

**Step 4: Add `setState()` calls and idle detection**

Create `web/src/hooks/use-idle.ts`:

```typescript
import type { SharkfinClient } from '@workfort/sharkfin-client';

/**
 * Track user activity and set presence state.
 * Sets 'active' on connect and activity, 'idle' after 5 minutes of inactivity.
 * Returns a cleanup function — caller is responsible for invoking it on unmount.
 */
export function useIdleDetection(client: SharkfinClient): () => void {
  const IDLE_TIMEOUT = 5 * 60 * 1000; // 5 minutes
  let timer: ReturnType<typeof setTimeout>;

  function setActive() {
    clearTimeout(timer);
    client.setState('active').catch(() => {});
    timer = setTimeout(() => {
      client.setState('idle').catch(() => {});
    }, IDLE_TIMEOUT);
  }

  // Set active on start.
  setActive();

  // Track user activity.
  const events = ['mousemove', 'keydown', 'click', 'scroll'];
  const throttledSetActive = throttle(setActive, 30_000); // At most once per 30s
  events.forEach((e) => document.addEventListener(e, throttledSetActive, { passive: true }));

  // Track tab visibility.
  function onVisibilityChange() {
    if (document.hidden) {
      client.setState('idle').catch(() => {});
    } else {
      setActive();
    }
  }
  document.addEventListener('visibilitychange', onVisibilityChange);

  return () => {
    clearTimeout(timer);
    events.forEach((e) => document.removeEventListener(e, throttledSetActive));
    document.removeEventListener('visibilitychange', onVisibilityChange);
  };
}

function throttle(fn: () => void, ms: number): () => void {
  let last = 0;
  return () => {
    const now = Date.now();
    if (now - last >= ms) {
      last = now;
      fn();
    }
  };
}
```

Call `useIdleDetection` from `SharkfinChat` component's `onMount`, not from `initApp()`. This ensures cleanup runs when the component unmounts:

```tsx
// In chat.tsx onMount:
import { useIdleDetection } from '../hooks/use-idle';
import { getClient } from '../client';

onMount(async () => {
  try {
    await initApp();
    const disposeIdle = useIdleDetection(getClient());
    onCleanup(disposeIdle);
  } catch { /* ... */ }
});
```

**Step 5: Run tests**

Run: `cd web && pnpm test`
Expected: All tests pass.

**Step 6: Commit**

```bash
git add web/src/components/sidebar.tsx web/src/hooks/use-idle.ts web/src/stores/index.ts web/test/
git commit -m "feat: add full presence (online/away/offline) with idle detection and setState()"
```

---

### Task 4: DM Creation Dialog

**Files:**
- Create: `web/src/components/dm-dialog.tsx`
- Create: `web/test/components/dm-dialog.test.tsx`
- Modify: `web/src/components/sidebar.tsx` (add "New DM" button)
- Modify: `web/src/index.tsx` (wire dialog)

**Step 1: Write failing test**

Create `web/test/components/dm-dialog.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render } from 'solid-js/web';
import { DMDialog } from '../../src/components/dm-dialog';
import type { User } from '@workfort/sharkfin-client';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('DMDialog', () => {
  const users: User[] = [
    { username: 'alice', online: true, type: 'user' },
    { username: 'bob', online: false, type: 'agent' },
  ];

  it('renders user list', () => {
    const el = renderInto(() => (
      <DMDialog
        users={users}
        currentUsername="me"
        open={true}
        onSelect={() => {}}
        onClose={() => {}}
      />
    ));
    const dialog = el.querySelector('wf-dialog');
    expect(dialog).toBeTruthy();
  });

  it('filters out current user from list', () => {
    const el = renderInto(() => (
      <DMDialog
        users={[...users, { username: 'me', online: true, type: 'user' }]}
        currentUsername="me"
        open={true}
        onSelect={() => {}}
        onClose={() => {}}
      />
    ));
    const items = el.querySelectorAll('wf-list-item');
    expect(items.length).toBe(2); // alice and bob, not "me"
  });

  it('calls onSelect when user is clicked', () => {
    const onSelect = vi.fn();
    const el = renderInto(() => (
      <DMDialog
        users={users}
        currentUsername="me"
        open={true}
        onSelect={onSelect}
        onClose={() => {}}
      />
    ));
    const firstItem = el.querySelector('wf-list-item') as HTMLElement;
    firstItem?.dispatchEvent(new CustomEvent('wf-select', { bubbles: true }));
    expect(onSelect).toHaveBeenCalledWith('alice');
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/components/dm-dialog.test.tsx`
Expected: FAIL — module not found.

**Step 3: Implement DMDialog**

Create `web/src/components/dm-dialog.tsx`:

```tsx
import { For, createEffect } from 'solid-js';
import type { User } from '@workfort/sharkfin-client';
import { initials } from '../utils';

interface DMDialogProps {
  users: User[];
  currentUsername: string;
  open: boolean;
  onSelect: (username: string) => void;
  onClose: () => void;
}

export function DMDialog(props: DMDialogProps) {
  let dialogRef!: HTMLElement & { show(): void; hide(): void };

  createEffect(() => {
    if (props.open) dialogRef?.show();
    else dialogRef?.hide();
  });

  const otherUsers = () => props.users.filter((u) => u.username !== props.currentUsername);

  return (
    <wf-dialog
      ref={dialogRef}
      header="New Direct Message"
      on:wf-close={props.onClose}
    >
      <wf-list>
        <For each={otherUsers()}>
          {(user) => (
            <wf-list-item on:wf-select={() => props.onSelect(user.username)}>
              <div class="sf-dm__avatar" style="margin-right: var(--wf-space-sm);">
                {initials(user.username)}
                <wf-status-dot
                  status={user.online ? (user.state === 'idle' ? 'away' : 'online') : 'offline'}
                  style="position:absolute;bottom:-1px;right:-1px;"
                />
              </div>
              <span>{user.username}</span>
            </wf-list-item>
          )}
        </For>
      </wf-list>
    </wf-dialog>
  );
}
```

**Step 4: Wire into sidebar and index**

Add a "New DM" button to the sidebar's DM section header. In `web/src/components/sidebar.tsx`, after the "Direct Messages" section label, add an `onNewDM` prop and a `+` button:

Add to `SidebarProps`:
```typescript
onNewDM?: () => void;
```

After the "Direct Messages" section label div:
```tsx
<div class="sf-section-label" style="display: flex; justify-content: space-between; align-items: center;">
  Direct Messages
  <Show when={props.onNewDM}>
    <wf-button style="padding: 1px 5px; font-size: 12px;" title="New DM" on:click={props.onNewDM!}>+</wf-button>
  </Show>
</div>
```

In `web/src/index.tsx`, the `SidebarLoaded` component manages the DM dialog state and calls `client.dmOpen()` on selection:

```tsx
import { createSignal } from 'solid-js';
import { DMDialog } from './components/dm-dialog';
import { getClient } from './client';

function SidebarLoaded() {
  const { channels, users, unread } = getStores();
  const { user } = useAuth();
  const [dmDialogOpen, setDmDialogOpen] = createSignal(false);

  async function handleDMSelect(username: string) {
    setDmDialogOpen(false);
    const result = await getClient().dmOpen(username);
    channels.setActiveChannel(result.channel);
    // Refresh DM list.
    getClient().dmList().then((dms) => channels.setDms?.(dms)).catch(() => {});
  }

  return (
    <>
      <ChannelSidebar
        channels={channels.channels()}
        dms={channels.dms()}
        unreads={unread.unreads()}
        users={users.users()}
        activeChannel={channels.activeChannel()}
        onSelectChannel={channels.setActiveChannel}
        currentUsername={user()?.name ?? ''}
        onNewDM={() => setDmDialogOpen(true)}
      />
      <DMDialog
        users={users.users()}
        currentUsername={user()?.name ?? ''}
        open={dmDialogOpen()}
        onSelect={handleDMSelect}
        onClose={() => setDmDialogOpen(false)}
      />
    </>
  );
}
```

**Step 5: Run tests**

Run: `cd web && pnpm test`
Expected: All tests pass.

**Step 6: Commit**

```bash
git add web/src/components/dm-dialog.tsx web/test/components/dm-dialog.test.tsx web/src/components/sidebar.tsx web/src/index.tsx
git commit -m "feat: add DM creation dialog with user list and dmOpen()"
```

---

### Task 5: Listener Cleanup on Unmount

**Files:**
- Modify: `web/src/stores/messages.ts`
- Modify: `web/src/stores/users.ts`
- Modify: `web/src/stores/unread.ts`

**Step 1: Add `onCleanup` to each store**

Each store registers `client.on(...)` listeners but never removes them. Wrap each store's event registration with cleanup:

In `messages.ts`:
```typescript
import { createSignal, createEffect, onCleanup, type Accessor } from 'solid-js';

// After client.on('message', handler):
const handler = (msg: BroadcastMessage) => { ... };
client.on('message', handler);
onCleanup(() => client.off('message', handler));
```

Same pattern in `users.ts` (presence listener) and `unread.ts` (message listener).

**Step 2: Run tests**

Run: `cd web && pnpm test`
Expected: All tests pass (cleanup doesn't affect test behavior).

**Step 3: Commit**

```bash
git add web/src/stores/messages.ts web/src/stores/users.ts web/src/stores/unread.ts
git commit -m "fix: add listener cleanup on store unmount"
```

---

### Task 6: Permission-Gated UI via `capabilities()`

**Files:**
- Modify: `web/src/stores/index.ts` (fetch capabilities on connect)
- Create: `web/src/stores/permissions.ts`
- Create: `web/test/stores/permissions.test.ts`
- Modify: `web/src/components/sidebar.tsx` (hide buttons if no permission)
- Modify: `web/src/components/chat.tsx` (hide input if no `send_message`)
- Modify: `web/src/components/channel-header.tsx` (hide invite if no `invite_channel`)

The server enforces permissions — if a user calls `createChannel()` without `create_channel` permission, it returns an error. But the UI should not show buttons the user can't use. The client's `capabilities()` method returns `string[]` of the user's permissions.

**Permission → UI mapping:**

| Permission | UI Element | If Missing |
|---|---|---|
| `create_channel` | "+" new channel button | Hide |
| `invite_channel` | "Invite" button in header | Hide |
| `join_channel` | Join action on non-member channels | Hide |
| `send_message` | InputBar | Replace with read-only notice |
| `dm_open` | "+" new DM button | Hide |
| `dm_list` | DM section in sidebar | Hide |

**Step 1: Write failing test**

Create `web/test/stores/permissions.test.ts`:

```typescript
import { describe, it, expect } from 'vitest';
import { createRoot } from 'solid-js';
import { createPermissionStore } from '../../src/stores/permissions';
import { createMockClient, flushPromises } from '../helpers';

describe('createPermissionStore', () => {
  it('fetches capabilities on creation', async () => {
    const client = createMockClient();
    (client as any).capabilities = vi.fn().mockResolvedValue(['send_message', 'channel_list', 'create_channel']);

    let store!: ReturnType<typeof createPermissionStore>;
    createRoot(() => {
      store = createPermissionStore(client);
    });

    await flushPromises();
    expect(store.can('send_message')).toBe(true);
    expect(store.can('create_channel')).toBe(true);
    expect(store.can('invite_channel')).toBe(false);
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/stores/permissions.test.ts`
Expected: FAIL — module not found.

**Step 3: Implement permission store**

Create `web/src/stores/permissions.ts`:

```typescript
import { createSignal } from 'solid-js';
import type { SharkfinClient } from '@workfort/sharkfin-client';

export function createPermissionStore(client: SharkfinClient) {
  const [permissions, setPermissions] = createSignal<Set<string>>(new Set());

  client.capabilities().then((perms) => setPermissions(new Set(perms))).catch(() => {});

  function can(permission: string): boolean {
    return permissions().has(permission);
  }

  return { can, permissions };
}
```

**Step 4: Wire into stores/index.ts**

In `initApp()`, after creating stores, also create the permission store:

```typescript
import { createPermissionStore } from './permissions';

// In initStores():
const permissions = createPermissionStore(client);
return { channels, messages, users, unread, permissions };
```

**Step 5: Gate UI elements**

In `web/src/components/sidebar.tsx`, accept a `can` function prop:

```typescript
interface SidebarProps {
  // ... existing props ...
  can: (permission: string) => boolean;
}
```

Wrap the "+" new channel button:
```tsx
<Show when={props.can('create_channel')}>
  <wf-button ...>+</wf-button>
</Show>
```

Wrap the "+" new DM button:
```tsx
<Show when={props.can('dm_open')}>
  <wf-button ...>+</wf-button>
</Show>
```

Wrap the DM section:
```tsx
<Show when={props.can('dm_list')}>
  <div class="sf-section-label">Direct Messages</div>
  ...
</Show>
```

In `web/src/components/chat.tsx`, gate the InputBar:
```tsx
<Show when={getStores().permissions.can('send_message')} fallback={
  <div class="sf-typing" style="color: var(--wf-color-text-muted); font-size: var(--wf-text-xs);">
    You don't have permission to send messages in this channel.
  </div>
}>
  <InputBar ... />
</Show>
```

In `web/src/components/channel-header.tsx`, the invite button already has a `Show when` — add the permission check:
```tsx
<Show when={!props.isPublic && props.onInvite && props.can?.('invite_channel')}>
```

In `web/src/index.tsx`, pass `can` from the permissions store to `ChannelSidebar`:
```tsx
<ChannelSidebar
  ...
  can={getStores().permissions.can}
/>
```

**Step 6: Run tests**

Run: `cd web && pnpm test`
Expected: All tests pass. Update existing sidebar/chat tests to pass a `can` prop that returns `true` for all permissions (so existing tests don't break).

**Step 7: Commit**

```bash
git add web/src/stores/permissions.ts web/test/stores/permissions.test.ts web/src/stores/index.ts web/src/components/ web/src/index.tsx
git commit -m "feat: add permission-gated UI via capabilities()"
```
