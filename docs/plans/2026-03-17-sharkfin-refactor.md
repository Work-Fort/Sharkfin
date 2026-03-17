# Sharkfin Refactor to Extracted Components — Plan 8

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refactor Sharkfin's web UI to use the extracted `@workfort/ui` utilities, web components, and `@workfort/ui-solid` hooks, replacing local implementations.

**Architecture:** Local utility functions replaced with `@workfort/ui` imports. `InputBar` becomes a thin wrapper around `<wf-compose-input>`. DM/Invite dialogs use `<wf-user-picker>`. `useIdleDetection` and permission store use `@workfort/ui-solid` hooks. Local files are deleted after migration.

**Tech Stack:** SolidJS, `@workfort/ui`, `@workfort/ui-solid`

**Repo:** `sharkfin/lead` (web UI at `web/`)

**Prerequisites:** Plans 5, 6, 7 must be complete.

---

### Task 1: Replace Local Utilities with @workfort/ui Imports

**Files:**
- Delete: `web/src/utils.ts`
- Modify: `web/src/components/message.tsx` (import from `@workfort/ui`)
- Modify: `web/src/components/message-area.tsx` (import from `@workfort/ui`)
- Modify: `web/src/components/sidebar.tsx` (import from `@workfort/ui`)

Replace all imports:

```typescript
// Old:
import { initials } from '../utils';
// New:
import { initials } from '@workfort/ui';

// Old (in message.tsx):
function formatTime(iso: string): string { ... }
// New:
import { formatTime } from '@workfort/ui';

// Old (in message-area.tsx):
function formatDateLabel(iso: string): string { ... }
function isSameDay(a: string, b: string): boolean { ... }
// New:
import { formatDateLabel, isSameDay } from '@workfort/ui';
```

Delete the local function definitions. Delete `web/src/utils.ts`.

Run: `cd web && pnpm test`
Expected: All tests pass (imports resolve to `@workfort/ui` which is a workspace link).

**Commit:** `refactor: replace local utilities with @workfort/ui imports`

---

### Task 2: Replace InputBar with wf-compose-input

**Files:**
- Modify: `web/src/components/input-bar.tsx` (thin wrapper)
- Modify: `web/test/components/input-bar.test.tsx` (update for web component)

Replace `InputBar` implementation with a wrapper:

```tsx
interface InputBarProps {
  channel: string;
  onSend: (body: string) => void;
}

export function InputBar(props: InputBarProps) {
  return (
    <div class="sf-input">
      <wf-compose-input
        placeholder={`Message #${props.channel}`}
        on:wf-send={(e: CustomEvent) => props.onSend(e.detail.body)}
      />
    </div>
  );
}
```

The `sf-input` wrapper div provides the Sharkfin-specific padding. The web component handles all the interaction logic.

Update tests to work with the new structure — the textarea and send behavior are now inside the web component.

**Commit:** `refactor: replace InputBar with wf-compose-input wrapper`

---

### Task 3: Replace DM/Invite Dialogs with wf-user-picker

**Files:**
- Modify: `web/src/components/dm-dialog.tsx`
- Modify: `web/src/components/invite-dialog.tsx`
- Modify: `web/test/components/dm-dialog.test.tsx`
- Modify: `web/test/components/invite-dialog.test.tsx`

Replace `DMDialog`:

```tsx
import type { User } from '@workfort/sharkfin-client';

interface DMDialogProps {
  users: User[];
  currentUsername: string;
  open: boolean;
  onSelect: (username: string) => void;
  onClose: () => void;
}

export function DMDialog(props: DMDialogProps) {
  return (
    <wf-user-picker
      header="New Direct Message"
      open={props.open}
      exclude={props.currentUsername}
      users={props.users}
      on:wf-select={(e: CustomEvent) => props.onSelect(e.detail.username)}
      on:wf-close={props.onClose}
    />
  );
}
```

Replace `InviteDialog` similarly, with `header={`Invite to #${props.channel}`}`.

**Commit:** `refactor: replace DM/Invite dialogs with wf-user-picker`

---

### Task 4: Replace useIdleDetection with @workfort/ui-solid Hook

**Files:**
- Delete: `web/src/hooks/use-idle.ts`
- Modify: `web/src/components/chat.tsx`

Replace:

```typescript
// Old:
import { useIdleDetection } from '../hooks/use-idle';
// ...
const disposeIdle = useIdleDetection(getClient());
onCleanup(disposeIdle);

// New:
import { useIdleDetection } from '@workfort/ui-solid';
import { getClient } from '../client';
// ...
const client = getClient();
useIdleDetection({
  onActive: () => client.setState('active').catch(() => {}),
  onIdle: () => client.setState('idle').catch(() => {}),
});
```

The `@workfort/ui-solid` hook handles `onCleanup` internally.

Delete `web/src/hooks/use-idle.ts`.

**Commit:** `refactor: replace local useIdleDetection with @workfort/ui-solid hook`

---

### Task 5: Replace Permission Store with @workfort/ui-solid Hook

**Files:**
- Modify: `web/src/stores/permissions.ts`
- Modify: `web/src/stores/index.ts`

Replace `createPermissionStore`:

```typescript
// Old:
import { createSignal } from 'solid-js';
import type { SharkfinClient } from '@workfort/sharkfin-client';

export function createPermissionStore(client: SharkfinClient) {
  const [permissions, setPermissions] = createSignal<Set<string>>(new Set());
  client.capabilities().then((perms) => setPermissions(new Set(perms))).catch(() => {});
  function can(permission: string): boolean { return permissions().has(permission); }
  return { can, permissions };
}

// New:
import { usePermissions } from '@workfort/ui-solid';
import type { SharkfinClient } from '@workfort/sharkfin-client';

export function createPermissionStore(client: SharkfinClient) {
  const { can, update, permissions } = usePermissions();
  client.capabilities().then((perms) => update(perms)).catch(() => {});
  return { can, permissions };
}
```

**Commit:** `refactor: replace local permission store with @workfort/ui-solid usePermissions`

---

### Task 6: Clean Up and Verify

- Run all tests: `cd web && pnpm test`
- Build: `cd web && pnpm build`
- Verify no local copies of extracted code remain
- Delete `web/src/utils.ts` if not already deleted
- Delete `web/src/hooks/use-idle.ts` if not already deleted

**Commit:** `chore: remove residual local utility files after extraction`
