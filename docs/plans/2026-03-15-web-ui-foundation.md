# Sharkfin Web UI — Foundation (Plan 1 of 2)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Set up the web UI project scaffolding, Go daemon UI serving, WebSocket client wrapper, and all four SolidJS reactive stores — producing a testable data layer with no visual components.

**Architecture:** Factory-function stores accept a `SharkfinClient` instance and return SolidJS signals. The client wrapper derives the WebSocket URL from the page location (proxied by the BFF). The Go daemon serves the built bundle at `/ui/*` and exposes `/ui/health` for the shell's service tracker.

**Tech Stack:** SolidJS 1.9, Vite 6, `@module-federation/vite`, `@workfort/sharkfin-client` (workspace link), vitest, Go 1.25

---

### Task 1: Project Scaffolding

**Files:**
- Create: `pnpm-workspace.yaml`
- Create: `web/package.json`
- Create: `web/tsconfig.json`
- Create: `web/vite.config.ts`
- Create: `web/vitest.config.ts`
- Create: `web/src/index.tsx` (placeholder)

**Step 1: Create pnpm-workspace.yaml at project root**

```yaml
packages:
  - 'clients/ts'
  - 'web'
```

**Step 2: Create web/package.json**

```json
{
  "name": "@workfort/sharkfin-ui",
  "version": "0.0.1",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "test": "vitest run",
    "test:watch": "vitest"
  },
  "dependencies": {
    "@workfort/sharkfin-client": "workspace:*",
    "@workfort/ui": "latest",
    "solid-js": "^1.9.0"
  },
  "devDependencies": {
    "@module-federation/vite": "^1.1.0",
    "jsdom": "^25.0.0",
    "typescript": "^5.6.0",
    "vite": "^6.0.0",
    "vite-plugin-solid": "^2.11.0",
    "vitest": "^3.0.0"
  }
}
```

**Step 3: Create web/tsconfig.json**

```json
{
  "compilerOptions": {
    "target": "ESNext",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "jsx": "preserve",
    "jsxImportSource": "solid-js",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "outDir": "dist",
    "declaration": true,
    "types": ["vite/client"]
  },
  "include": ["src"]
}
```

**Step 4: Create web/vite.config.ts**

```typescript
import { defineConfig } from 'vite';
import solid from 'vite-plugin-solid';
import { federation } from '@module-federation/vite';

export default defineConfig({
  plugins: [
    solid(),
    federation({
      name: 'sharkfin',
      filename: 'remoteEntry.js',
      exposes: {
        './index': './src/index.tsx',
      },
      shared: {
        'solid-js': { singleton: true },
        '@workfort/ui': { singleton: true },
      },
    }),
  ],
  build: {
    target: 'esnext',
    outDir: 'dist',
  },
});
```

**Step 5: Create web/vitest.config.ts**

```typescript
import { defineConfig } from 'vitest/config';
import solid from 'vite-plugin-solid';

export default defineConfig({
  plugins: [solid()],
  test: {
    environment: 'jsdom',
    globals: true,
  },
  resolve: {
    conditions: ['development', 'browser'],
  },
});
```

**Step 6: Create web/src/index.tsx (placeholder)**

```tsx
export default function SharkfinApp(props: { connected: boolean }) {
  return <div>Sharkfin Chat (connected: {String(props.connected)})</div>;
}

export const manifest = {
  name: 'sharkfin',
  label: 'Chat',
  route: '/chat',
};
```

**Step 7: Install dependencies**

```bash
cd web && pnpm install
```

Note: build the TS client first so its types are available:
```bash
cd clients/ts && pnpm install && pnpm build
```

**Step 8: Verify setup**

Run: `cd web && pnpm build`
Expected: Build succeeds, `dist/` directory created with `remoteEntry.js`.

**Step 9: Commit**

```bash
git add pnpm-workspace.yaml web/
git commit -m "build: scaffold web UI project with Vite + Module Federation"
```

---

### Task 2: Go Daemon UI Serving

**Files:**
- Create: `pkg/daemon/ui_handler.go`
- Create: `pkg/daemon/ui_handler_test.go`
- Modify: `pkg/daemon/server.go:28` (add `uiDir` param to `NewServer`)
- Modify: `pkg/daemon/server.go:59-62` (register UI routes on mux)
- Modify: `cmd/daemon/daemon.go:55` (pass `uiDir` to `NewServer`)
- Modify: `cmd/daemon/daemon.go:84-91` (add `--ui-dir` flag)

**Step 1: Write failing test for /ui/health**

Create `pkg/daemon/ui_handler_test.go`:

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestUIHealthReturns200(t *testing.T) {
	mux := http.NewServeMux()
	registerUIRoutes(mux, "")

	req := httptest.NewRequest("GET", "/ui/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestUIStaticFileServing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.js"), []byte("console.log('hi')"), 0644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	registerUIRoutes(mux, dir)

	req := httptest.NewRequest("GET", "/ui/test.js", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "console.log('hi')" {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestUINoStaticWhenDirEmpty(t *testing.T) {
	mux := http.NewServeMux()
	registerUIRoutes(mux, "")

	req := httptest.NewRequest("GET", "/ui/test.js", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("expected non-200 when uiDir is empty, got %d", rec.Code)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/daemon/ -run TestUI -v`
Expected: FAIL — `registerUIRoutes` undefined.

**Step 3: Implement registerUIRoutes**

Create `pkg/daemon/ui_handler.go`:

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import "net/http"

// registerUIRoutes adds the /ui/health probe and optional static file
// serving to the given mux. The health endpoint always responds 200 so
// the shell's service tracker can detect UI availability. Static files
// are served only when uiDir is non-empty.
func registerUIRoutes(mux *http.ServeMux, uiDir string) {
	mux.HandleFunc("GET /ui/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	if uiDir != "" {
		mux.Handle("/ui/", http.StripPrefix("/ui/", http.FileServer(http.Dir(uiDir))))
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./pkg/daemon/ -run TestUI -v`
Expected: PASS (all 3 tests).

**Step 5: Wire into NewServer and daemon command**

Modify `pkg/daemon/server.go` — add `uiDir string` parameter to `NewServer` (line 28) and call `registerUIRoutes` before the auth-wrapped routes (after line 58, before the `mux.Handle` lines):

```go
// NewServer signature becomes:
func NewServer(ctx context.Context, addr string, store domain.Store, pongTimeout time.Duration, webhookURL string, bus domain.EventBus, version string, passportURL string, uiDir string) (*Server, error) {
```

Add this call in the mux setup block, before the `mux.Handle("/mcp", ...)` line:

```go
	registerUIRoutes(mux, uiDir)
```

Modify `cmd/daemon/daemon.go`:

Add after line 53 (`webhookURL := ...`):
```go
			uiDir := viper.GetString("ui-dir")
```

Update the `NewServer` call (line 55) to pass `uiDir`:
```go
			srv, err := pkgdaemon.NewServer(cmd.Context(), addr, store, pongTimeout, webhookURL, bus, version, passportURL, uiDir)
```

Add the flag after the `passport-url` flag block (after line 91):
```go
	cmd.Flags().String("ui-dir", "", "Directory containing built web UI assets (optional)")
	_ = viper.BindPFlag("ui-dir", cmd.Flags().Lookup("ui-dir"))
```

**Step 6: Run full daemon tests**

Run: `go test ./pkg/daemon/ -v`
Expected: All tests pass (existing + new).

**Step 7: Commit**

```bash
git add pkg/daemon/ui_handler.go pkg/daemon/ui_handler_test.go pkg/daemon/server.go cmd/daemon/daemon.go
git commit -m "feat: add /ui/health endpoint and static file serving for web UI"
```

---

### Task 3: Test Helpers + Client Wrapper

**Files:**
- Create: `web/test/helpers.ts`
- Create: `web/src/client.ts`
- Create: `web/test/client.test.ts`

**Step 1: Create test helpers (mock client)**

Create `web/test/helpers.ts`:

```typescript
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
```

**Step 2: Write failing test for getWebSocketUrl**

Create `web/test/client.test.ts`:

```typescript
import { describe, it, expect, beforeEach } from 'vitest';
import { getWebSocketUrl } from '../src/client';

describe('getWebSocketUrl', () => {
  beforeEach(() => {
    // Reset location for each test — jsdom allows setting window.location
    Object.defineProperty(window, 'location', {
      value: {
        protocol: 'https:',
        host: 'app.example.com',
        pathname: '/forts/myfort/chat',
      },
      writable: true,
    });
  });

  it('derives wss URL from HTTPS page', () => {
    expect(getWebSocketUrl()).toBe('wss://app.example.com/forts/myfort/api/sharkfin/ws');
  });

  it('derives ws URL from HTTP page', () => {
    (window.location as any).protocol = 'http:';
    expect(getWebSocketUrl()).toBe('ws://app.example.com/forts/myfort/api/sharkfin/ws');
  });

  it('handles nested chat routes', () => {
    (window.location as any).pathname = '/forts/myfort/chat/dm/alice';
    expect(getWebSocketUrl()).toBe('wss://app.example.com/forts/myfort/api/sharkfin/ws');
  });
});
```

**Step 3: Run test to verify it fails**

Run: `cd web && pnpm test -- test/client.test.ts`
Expected: FAIL — `getWebSocketUrl` not found.

**Step 4: Implement client.ts**

Create `web/src/client.ts`:

```typescript
import { SharkfinClient } from '@workfort/sharkfin-client';

/**
 * Derive the WebSocket URL from the current page URL.
 * The BFF proxies /forts/{fort}/api/sharkfin/ws → daemon WS endpoint.
 */
export function getWebSocketUrl(): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const match = location.pathname.match(/^\/forts\/([^/]+)/);
  const fort = match?.[1] ?? '';
  return `${proto}//${location.host}/forts/${fort}/api/sharkfin/ws`;
}

let _client: SharkfinClient | null = null;

/** Get or create the singleton SharkfinClient. */
export function getClient(): SharkfinClient {
  if (!_client) {
    _client = new SharkfinClient(getWebSocketUrl(), { reconnect: true });
  }
  return _client;
}

/** Reset the singleton (for tests). */
export function resetClient(): void {
  _client?.close();
  _client = null;
}
```

**Step 5: Run test to verify it passes**

Run: `cd web && pnpm test -- test/client.test.ts`
Expected: PASS.

**Step 6: Commit**

```bash
git add web/test/helpers.ts web/src/client.ts web/test/client.test.ts
git commit -m "feat: add WebSocket client wrapper with URL derivation"
```

---

### Task 4: Channels Store

**Files:**
- Create: `web/src/stores/channels.ts`
- Create: `web/test/stores/channels.test.ts`

**Step 1: Write failing test**

Create `web/test/stores/channels.test.ts`:

```typescript
import { describe, it, expect, vi } from 'vitest';
import { createRoot } from 'solid-js';
import { createChannelStore } from '../../src/stores/channels';
import { createMockClient, flushPromises } from '../helpers';

describe('createChannelStore', () => {
  it('fetches channels on creation', async () => {
    const client = createMockClient();
    client.channels.mockResolvedValue([
      { name: 'general', public: true, member: true },
      { name: 'random', public: true, member: true },
    ]);

    let store!: ReturnType<typeof createChannelStore>;
    createRoot(() => {
      store = createChannelStore(client);
    });

    await flushPromises();
    expect(store.channels()).toEqual([
      { name: 'general', public: true, member: true },
      { name: 'random', public: true, member: true },
    ]);
  });

  it('defaults active channel to empty string', () => {
    const client = createMockClient();
    let store!: ReturnType<typeof createChannelStore>;
    createRoot(() => {
      store = createChannelStore(client);
    });
    expect(store.activeChannel()).toBe('');
  });

  it('updates active channel', () => {
    const client = createMockClient();
    let store!: ReturnType<typeof createChannelStore>;
    createRoot(() => {
      store = createChannelStore(client);
      store.setActiveChannel('random');
    });
    expect(store.activeChannel()).toBe('random');
  });

  it('fetches DMs on creation', async () => {
    const client = createMockClient();
    client.dmList.mockResolvedValue([
      { channel: 'dm-abc', participants: ['alice', 'bob'] },
    ]);

    let store!: ReturnType<typeof createChannelStore>;
    createRoot(() => {
      store = createChannelStore(client);
    });

    await flushPromises();
    expect(store.dms()).toEqual([
      { channel: 'dm-abc', participants: ['alice', 'bob'] },
    ]);
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/stores/channels.test.ts`
Expected: FAIL — module not found.

**Step 3: Implement channels store**

Create `web/src/stores/channels.ts`:

```typescript
import { createSignal } from 'solid-js';
import type { SharkfinClient, Channel, DM } from '@workfort/sharkfin-client';

export function createChannelStore(client: SharkfinClient) {
  const [channels, setChannels] = createSignal<Channel[]>([]);
  const [activeChannel, setActiveChannel] = createSignal('');
  const [dms, setDms] = createSignal<DM[]>([]);

  // Fetch initial data.
  client.channels().then(setChannels);
  client.dmList().then(setDms);

  return { channels, activeChannel, setActiveChannel, dms };
}
```

**Step 4: Run test to verify it passes**

Run: `cd web && pnpm test -- test/stores/channels.test.ts`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/stores/channels.ts web/test/stores/channels.test.ts
git commit -m "feat: add channels store"
```

---

### Task 5: Messages Store

**Files:**
- Create: `web/src/stores/messages.ts`
- Create: `web/test/stores/messages.test.ts`

**Step 1: Write failing test**

Create `web/test/stores/messages.test.ts`:

```typescript
import { describe, it, expect, vi } from 'vitest';
import { createRoot, createSignal } from 'solid-js';
import { createMessageStore } from '../../src/stores/messages';
import { createMockClient, flushPromises } from '../helpers';
import type { BroadcastMessage, Message } from '@workfort/sharkfin-client';

describe('createMessageStore', () => {
  it('fetches history when active channel is set', async () => {
    const client = createMockClient();
    const msgs: Message[] = [
      { id: 1, from: 'alice', body: 'hello', sentAt: '2026-03-15T09:00:00Z' },
    ];
    client.history.mockResolvedValue(msgs);

    let store!: ReturnType<typeof createMessageStore>;
    const [active, setActive] = createSignal('general');

    createRoot(() => {
      store = createMessageStore(client, active);
    });

    await flushPromises();
    expect(client.history).toHaveBeenCalledWith('general', { limit: 50 });
    expect(store.messages()).toEqual(msgs);
  });

  it('refetches history on channel switch', async () => {
    const client = createMockClient();
    client.history.mockResolvedValue([]);

    let store!: ReturnType<typeof createMessageStore>;
    const [active, setActive] = createSignal('general');

    createRoot(() => {
      store = createMessageStore(client, active);
      setActive('random');
    });

    await flushPromises();
    expect(client.history).toHaveBeenCalledWith('random', { limit: 50 });
  });

  it('appends broadcast messages for active channel', async () => {
    const client = createMockClient();
    client.history.mockResolvedValue([]);

    let store!: ReturnType<typeof createMessageStore>;
    const [active] = createSignal('general');

    createRoot(() => {
      store = createMessageStore(client, active);
    });

    await flushPromises();

    const broadcast: BroadcastMessage = {
      id: 2,
      channel: 'general',
      channelType: 'public',
      from: 'bob',
      body: 'hey',
      sentAt: '2026-03-15T09:01:00Z',
    };
    client._emit('message', broadcast);

    expect(store.messages()).toHaveLength(1);
    expect(store.messages()[0].body).toBe('hey');
  });

  it('does not append messages for other channels', async () => {
    const client = createMockClient();
    client.history.mockResolvedValue([]);

    let store!: ReturnType<typeof createMessageStore>;
    const [active] = createSignal('general');

    createRoot(() => {
      store = createMessageStore(client, active);
    });

    await flushPromises();

    client._emit('message', {
      id: 3, channel: 'random', channelType: 'public',
      from: 'bob', body: 'hey', sentAt: '2026-03-15T09:01:00Z',
    } as BroadcastMessage);

    expect(store.messages()).toHaveLength(0);
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/stores/messages.test.ts`
Expected: FAIL — module not found.

**Step 3: Implement messages store**

Create `web/src/stores/messages.ts`:

```typescript
import { createSignal, createEffect, type Accessor } from 'solid-js';
import type { SharkfinClient, Message, BroadcastMessage } from '@workfort/sharkfin-client';

export function createMessageStore(client: SharkfinClient, activeChannel: Accessor<string>) {
  const [messages, setMessages] = createSignal<Message[]>([]);

  // Refetch history whenever active channel changes.
  createEffect(() => {
    const ch = activeChannel();
    if (!ch) return;
    client.history(ch, { limit: 50 }).then(setMessages);
  });

  // Append incoming messages for the active channel.
  client.on('message', (msg: BroadcastMessage) => {
    if (msg.channel === activeChannel()) {
      setMessages((prev) => [...prev, {
        id: msg.id,
        channel: msg.channel,
        from: msg.from,
        body: msg.body,
        sentAt: msg.sentAt,
        threadId: msg.threadId,
        mentions: msg.mentions,
      }]);
    }
  });

  async function sendMessage(body: string): Promise<void> {
    const ch = activeChannel();
    if (ch) await client.sendMessage(ch, body);
  }

  return { messages, sendMessage };
}
```

**Step 4: Run test to verify it passes**

Run: `cd web && pnpm test -- test/stores/messages.test.ts`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/stores/messages.ts web/test/stores/messages.test.ts
git commit -m "feat: add messages store with history fetch and live updates"
```

---

### Task 6: Users Store

**Files:**
- Create: `web/src/stores/users.ts`
- Create: `web/test/stores/users.test.ts`

**Step 1: Write failing test**

Create `web/test/stores/users.test.ts`:

```typescript
import { describe, it, expect } from 'vitest';
import { createRoot } from 'solid-js';
import { createUserStore } from '../../src/stores/users';
import { createMockClient, flushPromises } from '../helpers';
import type { PresenceUpdate, User } from '@workfort/sharkfin-client';

describe('createUserStore', () => {
  it('fetches users on creation', async () => {
    const client = createMockClient();
    const userList: User[] = [
      { username: 'alice', online: true, type: 'user' },
      { username: 'bob', online: false, type: 'agent' },
    ];
    client.users.mockResolvedValue(userList);

    let store!: ReturnType<typeof createUserStore>;
    createRoot(() => {
      store = createUserStore(client);
    });

    await flushPromises();
    expect(store.users()).toEqual(userList);
  });

  it('updates user online status on presence event', async () => {
    const client = createMockClient();
    client.users.mockResolvedValue([
      { username: 'alice', online: true, type: 'user' },
      { username: 'bob', online: false, type: 'agent' },
    ]);

    let store!: ReturnType<typeof createUserStore>;
    createRoot(() => {
      store = createUserStore(client);
    });

    await flushPromises();

    const update: PresenceUpdate = { username: 'bob', status: 'online' };
    client._emit('presence', update);

    const bob = store.users().find((u) => u.username === 'bob');
    expect(bob?.online).toBe(true);
  });

  it('updates user state on presence event', async () => {
    const client = createMockClient();
    client.users.mockResolvedValue([
      { username: 'alice', online: true, type: 'user', state: 'active' },
    ]);

    let store!: ReturnType<typeof createUserStore>;
    createRoot(() => {
      store = createUserStore(client);
    });

    await flushPromises();

    client._emit('presence', { username: 'alice', status: 'online', state: 'idle' } as PresenceUpdate);

    const alice = store.users().find((u) => u.username === 'alice');
    expect(alice?.state).toBe('idle');
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/stores/users.test.ts`
Expected: FAIL — module not found.

**Step 3: Implement users store**

Create `web/src/stores/users.ts`:

```typescript
import { createSignal } from 'solid-js';
import type { SharkfinClient, User, PresenceUpdate } from '@workfort/sharkfin-client';

export function createUserStore(client: SharkfinClient) {
  const [users, setUsers] = createSignal<User[]>([]);

  client.users().then(setUsers);

  client.on('presence', (update: PresenceUpdate) => {
    setUsers((prev) =>
      prev.map((u) =>
        u.username === update.username
          ? { ...u, online: update.status === 'online', state: update.state ?? u.state }
          : u,
      ),
    );
  });

  return { users };
}
```

**Step 4: Run test to verify it passes**

Run: `cd web && pnpm test -- test/stores/users.test.ts`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/stores/users.ts web/test/stores/users.test.ts
git commit -m "feat: add users store with presence tracking"
```

---

### Task 7: Unread Store

**Files:**
- Create: `web/src/stores/unread.ts`
- Create: `web/test/stores/unread.test.ts`

**Step 1: Write failing test**

Create `web/test/stores/unread.test.ts`:

```typescript
import { describe, it, expect, vi } from 'vitest';
import { createRoot, createSignal } from 'solid-js';
import { createUnreadStore } from '../../src/stores/unread';
import { createMockClient, flushPromises } from '../helpers';
import type { BroadcastMessage, UnreadCount } from '@workfort/sharkfin-client';

describe('createUnreadStore', () => {
  it('fetches unread counts on creation', async () => {
    const client = createMockClient();
    const counts: UnreadCount[] = [
      { channel: 'general', type: 'public', unreadCount: 3, mentionCount: 1 },
    ];
    client.unreadCounts.mockResolvedValue(counts);

    let store!: ReturnType<typeof createUnreadStore>;
    const [active] = createSignal('');

    createRoot(() => {
      store = createUnreadStore(client, active);
    });

    await flushPromises();
    expect(store.unreads()).toEqual(counts);
    expect(store.totalUnread()).toBe(3);
  });

  it('increments unread on message in non-active channel', async () => {
    const client = createMockClient();
    client.unreadCounts.mockResolvedValue([
      { channel: 'general', type: 'public', unreadCount: 0, mentionCount: 0 },
      { channel: 'random', type: 'public', unreadCount: 0, mentionCount: 0 },
    ]);

    let store!: ReturnType<typeof createUnreadStore>;
    const [active] = createSignal('general');

    createRoot(() => {
      store = createUnreadStore(client, active);
    });

    await flushPromises();

    client._emit('message', {
      id: 1, channel: 'random', channelType: 'public',
      from: 'alice', body: 'hey', sentAt: '2026-03-15T09:00:00Z',
    } as BroadcastMessage);

    const random = store.unreads().find((u) => u.channel === 'random');
    expect(random?.unreadCount).toBe(1);
    expect(store.totalUnread()).toBe(1);
  });

  it('does not increment unread for active channel', async () => {
    const client = createMockClient();
    client.unreadCounts.mockResolvedValue([
      { channel: 'general', type: 'public', unreadCount: 0, mentionCount: 0 },
    ]);

    let store!: ReturnType<typeof createUnreadStore>;
    const [active] = createSignal('general');

    createRoot(() => {
      store = createUnreadStore(client, active);
    });

    await flushPromises();

    client._emit('message', {
      id: 1, channel: 'general', channelType: 'public',
      from: 'alice', body: 'hey', sentAt: '2026-03-15T09:00:00Z',
    } as BroadcastMessage);

    expect(store.totalUnread()).toBe(0);
  });

  it('resets unread and marks read on channel switch', async () => {
    const client = createMockClient();
    client.unreadCounts.mockResolvedValue([
      { channel: 'general', type: 'public', unreadCount: 5, mentionCount: 2 },
      { channel: 'random', type: 'public', unreadCount: 3, mentionCount: 0 },
    ]);

    let store!: ReturnType<typeof createUnreadStore>;
    const [active, setActive] = createSignal('');

    createRoot(() => {
      store = createUnreadStore(client, active);
      setActive('general');
    });

    await flushPromises();

    const general = store.unreads().find((u) => u.channel === 'general');
    expect(general?.unreadCount).toBe(0);
    expect(general?.mentionCount).toBe(0);
    expect(client.markRead).toHaveBeenCalledWith('general');
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/stores/unread.test.ts`
Expected: FAIL — module not found.

**Step 3: Implement unread store**

Create `web/src/stores/unread.ts`:

```typescript
import { createSignal, createEffect, type Accessor } from 'solid-js';
import type { SharkfinClient, UnreadCount, BroadcastMessage } from '@workfort/sharkfin-client';

export function createUnreadStore(client: SharkfinClient, activeChannel: Accessor<string>) {
  const [unreads, setUnreads] = createSignal<UnreadCount[]>([]);

  client.unreadCounts().then(setUnreads);

  // Increment count for messages arriving in non-active channels.
  client.on('message', (msg: BroadcastMessage) => {
    if (msg.channel !== activeChannel()) {
      setUnreads((prev) =>
        prev.map((u) =>
          u.channel === msg.channel
            ? { ...u, unreadCount: u.unreadCount + 1 }
            : u,
        ),
      );
    }
  });

  // Mark read and reset counts when switching channels.
  createEffect(() => {
    const ch = activeChannel();
    if (!ch) return;
    client.markRead(ch);
    setUnreads((prev) =>
      prev.map((u) =>
        u.channel === ch ? { ...u, unreadCount: 0, mentionCount: 0 } : u,
      ),
    );
  });

  const totalUnread = () => unreads().reduce((sum, u) => sum + u.unreadCount, 0);

  return { unreads, totalUnread };
}
```

**Step 4: Run test to verify it passes**

Run: `cd web && pnpm test -- test/stores/unread.test.ts`
Expected: PASS.

**Step 5: Run all web tests**

Run: `cd web && pnpm test`
Expected: All tests pass.

**Step 6: Commit**

```bash
git add web/src/stores/unread.ts web/test/stores/unread.test.ts
git commit -m "feat: add unread store with count tracking and mark-read"
```
