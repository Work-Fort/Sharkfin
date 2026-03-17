# UI Extraction + Shell Chrome Redesign — Design

## Goal

Extract reusable components from Sharkfin into `@workfort/ui` as framework-agnostic Lit web components and pure utilities. Redesign the shell chrome for horizontal tabs, hamburger collapse, and responsive mobile layouts with configurable button positioning for handedness.

---

## 1. Utility Extraction

Pure functions extracted from Sharkfin into `@workfort/ui/src/utils/`:

| Function | Source | Description |
|---|---|---|
| `initials(username)` | `sharkfin/web/src/utils.ts` | Extract initials from username (e.g., "alice-chen" → "AC") |
| `formatTime(iso)` | `sharkfin/web/src/components/message.tsx` | ISO timestamp → "HH:MM" |
| `formatDateLabel(iso)` | `sharkfin/web/src/components/message-area.tsx` | ISO → "Today" / "Yesterday" / "March 15, 2026" |
| `isSameDay(a, b)` | `sharkfin/web/src/components/message-area.tsx` | Compare two ISO timestamps by calendar day |
| `throttle(fn, ms)` | `sharkfin/web/src/hooks/use-idle.ts` | Throttle a function to at most once per `ms` |

Exported from `@workfort/ui` package index. No framework dependency.

After extraction, Sharkfin imports from `@workfort/ui` instead of local files.

**Storybook:** Documentation page in the Lit storybook showing usage examples.

---

## 2. Web Component Extraction

### `wf-compose-input`

A message composition input — textarea with send button.

| Property | Type | Default | Description |
|---|---|---|---|
| `placeholder` | `string` | `""` | Placeholder text |
| `disabled` | `boolean` | `false` | Disables input and send |

| Event | Detail | Description |
|---|---|---|
| `wf-send` | `{ body: string }` | Fired on Enter or send button click |

Behavior:
- Enter sends (fires `wf-send`), Shift+Enter inserts newline
- Empty messages are not sent
- Input clears after send
- Textarea auto-resizes with content (up to a max height)
- Uses `--wf-*` tokens, light DOM, extends `WfElement`

### `wf-user-picker`

A dialog for selecting a user from a list, with avatar initials and presence.

| Property | Type | Default | Description |
|---|---|---|---|
| `header` | `string` | `""` | Dialog title |
| `open` | `boolean` | `false` | Controls visibility |
| `exclude` | `string` | `""` | Username to filter out (current user) |

| Event | Detail | Description |
|---|---|---|
| `wf-select` | `{ username: string }` | Fired when a user is selected |
| `wf-close` | — | Fired when dialog is dismissed |

Accepts user data via a `users` property (JSON array of `{ username, online, state?, type }`) or via slotted `wf-list-item` children.

Uses `wf-dialog`, `wf-list`, `wf-list-item`, `wf-status-dot` internally.

**Storybook:** Interactive stories for both components in `documentation/storybook/lit/stories/`. Multiple states: empty, with content, disabled, with users, search filtering.

After extraction, Sharkfin's `InputBar` becomes a thin wrapper around `<wf-compose-input>`, and DM/Invite dialogs use `<wf-user-picker>`.

---

## 3. Hook Extraction

### `IdleDetector` (core class in `@workfort/ui`)

Framework-agnostic activity tracker.

```
new IdleDetector({
  onActive: () => void,
  onIdle: () => void,
  timeout: number,     // ms before idle (default 300000 = 5min)
  throttle: number,    // ms between activity checks (default 30000)
})
```

Methods: `start()`, `stop()`, `dispose()`

Manages `mousemove`, `keydown`, `click`, `scroll` listeners and `visibilitychange`. Pure JS, no framework dependency.

### Framework hooks

| Package | Hook | Description |
|---|---|---|
| `@workfort/ui-solid` | `useIdleDetection(detector)` | Calls `start()` on mount, `stop()` via `onCleanup` |
| `@workfort/ui-react` | `useIdleDetection(detector)` | Calls `start()` in `useEffect`, `stop()` in cleanup |
| `@workfort/ui-svelte` | `idleDetection(detector)` | Svelte action or `onMount`/`onDestroy` |
| `@workfort/ui-vue` | `useIdleDetection(detector)` | `onMounted`/`onUnmounted` |

### `PermissionSet` (core class in `@workfort/ui`)

```
new PermissionSet(permissions: string[])
can(permission: string): boolean
```

Framework adapters wrap this in reactive state (signals, refs, stores).

**Storybook:** Doc pages per framework showing hook usage.

---

## 4. Shell Chrome Redesign

### Desktop (>1024px)
- Brand name (fort) on the left
- Service tabs horizontal — only services with `ui: true` shown
- Active tab highlighted, `wf-status-dot` for connected services
- If tabs overflow available space, excess collapse into a "more" dropdown
- Theme toggle and future user menu on the right

### Tablet (641px–1024px)
- Brand name + tabs that fit + hamburger for overflow
- Theme toggle moves into hamburger menu

### Mobile (≤640px)
- Hamburger button only (configurable position)
- Brand name hidden
- Hamburger opens full menu: brand, service tabs (vertical), theme toggle, preferences
- Sidebar overlays content instead of side-by-side grid

### New components

**`wf-nav-bar`** — Reusable shell navigation bar.
- Slots: `brand`, `tabs`, `actions`
- Uses `ResizeObserver` to detect overflow and collapse tabs
- Responsive via CSS `@media` queries

**`wf-hamburger`** — Hamburger menu button + overlay.
- Property: `position: 'top-left' | 'top-right' | 'bottom-left' | 'bottom-right'` (default: `'top-right'`)
- Opens a slide-out or overlay panel with slotted content

### Handedness preference

A shell-level setting: `handedness: 'left' | 'right'` (default `'right'`).

When set, all positional navigation controls (hamburger, sidebar toggle) align to the preferred side:
- Right-handed → controls on the right (thumb-reachable on the right side of the screen)
- Left-handed → controls on the left

Stored as a user preference (localStorage for now, user settings in Passport later).

Flows down to `wf-hamburger` and the Sharkfin sidebar toggle via CSS custom property or context.

**Storybook:** Stories at desktop, tablet, mobile widths. Interactive hamburger open/close. Different tab counts for overflow. Left-handed and right-handed configurations.

---

## 5. Sharkfin Responsive Layout

### Desktop (>1024px)
- Two-column: sidebar + main. Handled by the shell grid. No change.

### Tablet (641px–1024px)
- Same two-column, sidebar may narrow. Shell grid handles this.

### Mobile (≤640px)
- Single column. Shell grid collapses.
- Sidebar becomes an overlay panel with a toggle button.
- Toggle button position follows the handedness preference (same as hamburger).
- "Back to channels" action when viewing messages.
- Message input stays pinned to bottom.
- Message avatars shrink (already done).

Sharkfin's job is to look good in full-width single-column. The sidebar overlay is the shell's responsibility.

**Storybook:** Sharkfin chat stories at each breakpoint — channel list view and message view on mobile, full layout on desktop.

---

## 6. Storybook Organization

```
documentation/storybook/lit/stories/
├── @workfort-ui/
│   ├── ComposeInput.stories.ts
│   ├── UserPicker.stories.ts
│   ├── NavBar.stories.ts
│   ├── Hamburger.stories.ts
│   └── ... (existing: Button, Badge, etc.)
├── Shell/
│   ├── ShellChrome.stories.ts        (full nav at multiple viewports)
│   └── ResponsiveLayout.stories.ts   (grid behavior)
└── Sharkfin/
    ├── SharkfinChat.stories.ts        (existing, updated)
    ├── ChatMobile.stories.ts          (mobile layout)
    └── MessageBubble.stories.ts       (message component showcase)
```

Each section has interactive stories showing real behavior, not just static mockups.

---

## Changes by Repo

### `@workfort/ui` (scope/lead/web/packages/ui)
- Add `src/utils/`: initials, formatTime, formatDateLabel, isSameDay, throttle
- Add `src/components/compose-input.ts`: `wf-compose-input`
- Add `src/components/user-picker.ts`: `wf-user-picker`
- Add `src/core/idle-detector.ts`: `IdleDetector` class
- Add `src/core/permission-set.ts`: `PermissionSet` class
- Add `src/layout/nav-bar.ts`: `wf-nav-bar`
- Add `src/layout/hamburger.ts`: `wf-hamburger`
- Export all from package index

### `@workfort/ui-solid` (scope/lead/web/packages/solid)
- Add `useIdleDetection` hook
- Add `usePermissions` hook

### `@workfort/ui-react` (scope/lead/web/packages/react)
- Add `useIdleDetection` hook
- Add `usePermissions` hook

### `@workfort/ui-svelte` (scope/lead/web/packages/svelte)
- Add `idleDetection` action/hook
- Add `permissions` store

### `@workfort/ui-vue` (scope/lead/web/packages/vue)
- Add `useIdleDetection` composable
- Add `usePermissions` composable

### Shell (scope/lead/web/shell)
- Replace nav-bar.tsx with `<wf-nav-bar>` usage
- Update global.css for responsive grid
- Add handedness preference to theme store
- Update shell-layout.tsx for sidebar overlay on mobile

### Sharkfin (sharkfin/lead/web)
- Replace local utilities with `@workfort/ui` imports
- Replace `InputBar` with `<wf-compose-input>` wrapper
- Replace DM/Invite dialogs with `<wf-user-picker>`
- Replace `useIdleDetection` with `@workfort/ui-solid` hook
- Replace permission store with `@workfort/ui-solid` hook
- Add sidebar toggle button with configurable position
- Update responsive CSS for mobile single-column

### Documentation (documentation/)
- Add all Storybook stories organized by section
- Add documentation pages for utilities and hooks
