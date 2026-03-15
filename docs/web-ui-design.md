# Sharkfin Web UI — Design Spec

**Goal:** Build the Sharkfin web UI as a SolidJS Module Federation remote that integrates into the WorkFort shell. Uses the existing `@workfort/sharkfin-client` TypeScript client for WebSocket communication and `@workfort/ui` for components.

**Key Principle:** The web UI replicates the TUI's functionality (channels, DMs, messages, presence) in a browser-based Slack/Discord-style layout. The shell handles auth, theming, and routing — Sharkfin only owns the chat experience.

---

## Architecture

```
WorkFort Shell (host)
  ├── Loads sharkfin/remoteEntry.js via Module Federation
  ├── Provides: auth context, theme tokens, routing
  └── Proxies: /forts/{fort}/api/sharkfin/* → Sharkfin daemon

Sharkfin Web UI (remote)
  ├── SolidJS components using @workfort/ui
  ├── @workfort/sharkfin-client for WebSocket protocol
  └── Serves built UI at /ui/* on the Sharkfin daemon's HTTP server
```

### Module Federation Remote

The Sharkfin daemon serves the UI bundle. The shell discovers it via the service tracker (`ui: true` when `/ui/health` responds). The remote entry is at `/forts/{fort}/api/sharkfin/ui/remoteEntry.js`.

### ServiceModule Export

```typescript
// src/index.tsx
import { SharkfinChat } from './components/chat';

export default function SharkfinApp(props: { connected: boolean }) {
  return <SharkfinChat connected={props.connected} />;
}

export const manifest = {
  name: 'sharkfin',
  label: 'Chat',
  route: '/chat',
};

export { ChannelSidebar as SidebarContent } from './components/sidebar';
```

The shell renders `SidebarContent` in the sidebar slot and the default export in the main content area. This gives us the two-column layout without the shell knowing anything about chat.

---

## Project Structure

```
sharkfin/
├── clients/ts/              # Existing @workfort/sharkfin-client (unchanged)
├── web/                     # NEW — web UI
│   ├── package.json         # @workfort/sharkfin-ui
│   ├── vite.config.ts       # Module Federation remote config
│   ├── tsconfig.json
│   ├── src/
│   │   ├── index.tsx        # ServiceModule exports
│   │   ├── client.ts        # WebSocket client singleton + SolidJS reactive wrapper
│   │   ├── stores/
│   │   │   ├── channels.ts  # Channel list state
│   │   │   ├── messages.ts  # Messages per channel
│   │   │   ├── users.ts     # User list + presence
│   │   │   └── unread.ts    # Unread counts
│   │   └── components/
│   │       ├── chat.tsx      # Main layout container
│   │       ├── sidebar.tsx   # Channel + DM list (exported as SidebarContent)
│   │       ├── message-area.tsx  # Message display + scroll
│   │       ├── message.tsx   # Single message bubble
│   │       ├── input-bar.tsx # Message input with send
│   │       ├── channel-header.tsx  # Channel name + topic
│   │       └── typing-indicator.tsx
│   └── styles/
│       └── chat.css          # Chat-specific styles (sf-* prefix)
├── cmd/daemon/               # Existing Go daemon
│   └── ...                   # Add: static file handler for /ui/*
```

---

## Dependencies

```json
{
  "dependencies": {
    "@workfort/sharkfin-client": "workspace:*",
    "@workfort/ui": "latest",
    "solid-js": "^1.9.0"
  },
  "devDependencies": {
    "@module-federation/vite": "^1.1.0",
    "vite": "^6.0.0",
    "vite-plugin-solid": "^2.11.0",
    "typescript": "^5.6.0"
  }
}
```

`@workfort/ui` and `solid-js` are shared singletons via Module Federation — they're loaded from the shell, not bundled.

---

## WebSocket Integration

### Client Singleton

```typescript
// src/client.ts
import { SharkfinClient } from '@workfort/sharkfin-client';
import { createSignal } from 'solid-js';

let client: SharkfinClient | null = null;

export function getClient(): SharkfinClient {
  if (!client) {
    // WebSocket URL is relative — proxied by the BFF
    client = new SharkfinClient({ url: getWebSocketUrl() });
  }
  return client;
}
```

The WebSocket URL is derived from the current page URL — the BFF proxies `/forts/{fort}/api/sharkfin/ws` to the daemon's WebSocket endpoint. No hardcoded URLs.

### Reactive Stores

Each store wraps the client's events into SolidJS signals:

```typescript
// stores/channels.ts
import { createSignal } from 'solid-js';
import { getClient } from '../client';

const [channels, setChannels] = createSignal([]);
const [activeChannel, setActiveChannel] = createSignal('general');

// On connect, fetch channel list
getClient().on('ready', async () => {
  setChannels(await getClient().channels());
});

// Listen for real-time updates
getClient().on('channel_created', (ch) => {
  setChannels(prev => [...prev, ch]);
});

export { channels, activeChannel, setActiveChannel };
```

---

## UI Components

All components use `@workfort/ui` web components and the `--wf-*` design tokens. Chat-specific styles use the `sf-` prefix (as established in the Storybook mockup).

### Layout

The shell provides the outer frame (nav bar, service tabs). Sharkfin provides:
- **SidebarContent** — channel list + DM list (rendered in the shell's sidebar slot)
- **Default export** — channel header + message area + input bar (rendered in the shell's content area)

This means the two-column layout is handled by the shell's existing grid — Sharkfin doesn't need its own grid.

### Components using @workfort/ui

| Component | wf-* components used |
|-----------|---------------------|
| Channel list | `wf-list`, `wf-list-item`, `wf-badge` (unread counts) |
| DM list | `wf-list`, `wf-list-item`, `wf-status-dot` (presence) |
| Message input | `wf-button` (send) |
| Channel header | `wf-divider` |
| Modals | `wf-dialog` (create channel, invite user) |
| Loading states | `wf-spinner`, `wf-skeleton` |
| Error states | `wf-banner` (connection lost) |
| Typing indicator | Custom (CSS animation, same as mockup) |

### Reference Mockup

The Storybook mockup at `Documentation/storybook/lit/stories/SharkfinChat.stories.ts` is the visual reference. The SolidJS implementation should match this design.

---

## Go Daemon Changes

The Sharkfin daemon needs one addition: serve the UI bundle as static files.

```go
// In the daemon's HTTP handler setup:
uiFS := http.FileServer(http.Dir("./web/dist"))
mux.Handle("/ui/", http.StripPrefix("/ui/", uiFS))
mux.HandleFunc("/ui/health", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
})
```

The `/ui/health` endpoint tells the WorkFort shell's service tracker that this service has a UI to load. Without it, `ui: true` won't be set and the shell won't try to load the remote.

For production, the UI assets can be embedded via `go:embed` (same pattern as the shell).

---

## Build & Development

### Development

```bash
# Terminal 1: Sharkfin daemon
cd sharkfin/lead && mise run dev

# Terminal 2: Sharkfin web UI (Vite dev server)
cd sharkfin/lead/web && pnpm dev

# Terminal 3: WorkFort shell (Vite dev server)
cd scope/lead/web/shell && pnpm dev

# Terminal 4: WorkFort BFF
cd scope/lead && mise run dev:go
```

The BFF proxies the Vite dev server for the UI during development. In production, the daemon serves the built assets.

### Vite Config

```typescript
// web/vite.config.ts
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

---

## Success Criteria

1. Shell loads Sharkfin UI via Module Federation
2. Channel sidebar renders in the shell's sidebar slot
3. Messages display in the content area with real-time updates
4. User can send messages
5. Presence indicators (online/away/offline) update live
6. Unread badges update on new messages
7. Channel switching works
8. DMs work
9. Theming follows `--wf-*` tokens (dark + light mode)
10. Works on mobile viewport (responsive)
