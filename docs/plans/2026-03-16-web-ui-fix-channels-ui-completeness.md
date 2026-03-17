# Web UI Fix-Up — Channel Management & UI Completeness (Plan 4 of 4)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the remaining 7 gaps: create-channel dialog, invite-user dialog, join-channel for non-members, date dividers in messages, sidebar search filtering, `wf-list`/`wf-list-item` for channel/DM lists, and responsive mobile CSS.

**Architecture:** Dialogs use `wf-dialog` with form inputs. Channel/DM lists migrate from raw divs to `wf-list`/`wf-list-item` with `wf-select` events and `data-wf="trailing"` for badges. Date dividers are computed from message timestamps. Responsive CSS uses a single breakpoint that hides the sidebar and shows a toggle.

**Tech Stack:** SolidJS 1.9, `@workfort/ui` (`wf-dialog`, `wf-list`, `wf-list-item`, `wf-divider`), CSS `@media`

**Prerequisite:** Plan 3 must be complete — connection lifecycle, identity, and presence are working.

---

## Coverage Checklist

| Gap # | Description | Task |
|-------|-------------|------|
| 3 | No create-channel dialog | Task 1 |
| 4 | No invite-user dialog | Task 2 |
| 12 | No `joinChannel()` for non-members | Task 1 (join button on non-member channels) |
| 9 | Date dividers not rendered | Task 3 |
| 11 | Search input non-functional | Task 4 |
| 13 | `wf-list`/`wf-list-item` not used | Task 5 |
| 14 | `wf-divider` not used in channel header | Task 5 |
| 8 (partial) | Responsive mobile CSS | Task 6 |

| Success Criterion | Verified By |
|---|---|
| 7. Channel switching works | Task 5 (wf-list-item events) |
| 8. DMs work | Plan 3 + Task 5 |
| 10. Mobile responsive | Task 6 |

---

### Task 1: Create Channel Dialog + Join Channel

**Prerequisite:** Plan 3 Task 1 exports `setChannels` and `setDms` from the channels store. Verify these exist before implementing this task.

**Files:**
- Create: `web/src/components/create-channel-dialog.tsx`
- Create: `web/test/components/create-channel-dialog.test.tsx`
- Modify: `web/src/components/sidebar.tsx` (wire `+` button)
- Modify: `web/src/index.tsx` (dialog state + `createChannel()` call)

**Step 1: Write failing test**

Create `web/test/components/create-channel-dialog.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render } from 'solid-js/web';
import { CreateChannelDialog } from '../../src/components/create-channel-dialog';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('CreateChannelDialog', () => {
  it('renders dialog with channel name input', () => {
    const el = renderInto(() => (
      <CreateChannelDialog open={true} onCreate={() => {}} onClose={() => {}} />
    ));
    expect(el.querySelector('wf-dialog')).toBeTruthy();
    expect(el.querySelector('input[placeholder*="channel"]')).toBeTruthy();
  });

  it('renders public/private toggle', () => {
    const el = renderInto(() => (
      <CreateChannelDialog open={true} onCreate={() => {}} onClose={() => {}} />
    ));
    const labels = el.querySelectorAll('label');
    expect(labels.length).toBeGreaterThanOrEqual(1);
  });

  it('calls onCreate with name and visibility', () => {
    const onCreate = vi.fn();
    const el = renderInto(() => (
      <CreateChannelDialog open={true} onCreate={onCreate} onClose={() => {}} />
    ));
    const input = el.querySelector('input[type="text"]') as HTMLInputElement;
    input.value = 'new-channel';
    input.dispatchEvent(new Event('input', { bubbles: true }));

    const submit = el.querySelector('wf-button[title="Create"]') as HTMLElement;
    submit?.click();

    expect(onCreate).toHaveBeenCalledWith('new-channel', true); // public by default
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/components/create-channel-dialog.test.tsx`
Expected: FAIL — module not found.

**Step 3: Implement CreateChannelDialog**

Create `web/src/components/create-channel-dialog.tsx`:

```tsx
import { createSignal, createEffect } from 'solid-js';

interface CreateChannelDialogProps {
  open: boolean;
  onCreate: (name: string, isPublic: boolean) => void;
  onClose: () => void;
}

export function CreateChannelDialog(props: CreateChannelDialogProps) {
  const [name, setName] = createSignal('');
  const [isPublic, setIsPublic] = createSignal(true);

  let dialogRef!: HTMLElement & { show(): void; hide(): void };

  createEffect(() => {
    if (props.open) dialogRef?.show();
    else dialogRef?.hide();
  });

  function handleCreate() {
    const n = name().trim();
    if (!n) return;
    props.onCreate(n, isPublic());
    setName('');
    setIsPublic(true);
  }

  return (
    <wf-dialog
      ref={dialogRef}
      header="Create Channel"
      on:wf-close={props.onClose}
    >
      <div style="display: flex; flex-direction: column; gap: var(--wf-space-md); padding: var(--wf-space-sm);">
        <input
          type="text"
          class="sf-sidebar__search"
          placeholder="Channel name"
          style="width: 100%; padding: var(--wf-space-xs) var(--wf-space-sm); border-radius: var(--wf-radius-sm); border: 1px solid var(--wf-color-border); background: var(--wf-color-bg); color: var(--wf-color-text); font-family: inherit; font-size: var(--wf-text-sm); outline: none; box-sizing: border-box;"
          value={name()}
          on:input={(e: Event) => setName((e.target as HTMLInputElement).value)}
          on:keydown={(e: KeyboardEvent) => { if (e.key === 'Enter') handleCreate(); }}
        />
        <label style="display: flex; align-items: center; gap: var(--wf-space-sm); font-size: var(--wf-text-sm); color: var(--wf-color-text-secondary);">
          <input
            type="checkbox"
            checked={isPublic()}
            on:change={(e: Event) => setIsPublic((e.target as HTMLInputElement).checked)}
          />
          Public channel
        </label>
        <div style="display: flex; justify-content: flex-end; gap: var(--wf-space-sm);">
          <wf-button variant="text" on:click={props.onClose}>Cancel</wf-button>
          <wf-button title="Create" on:click={handleCreate}>Create</wf-button>
        </div>
      </div>
    </wf-dialog>
  );
}
```

**Step 4: Wire into sidebar**

The sidebar's `+` button next to "Channels" already exists but does nothing. Change it to call an `onNewChannel` prop:

Add to `SidebarProps`:
```typescript
onNewChannel?: () => void;
```

Update the Channels header's `+` button:
```tsx
<wf-button style="padding: 2px 6px; font-size: 14px;" title="New channel" on:click={() => props.onNewChannel?.()}>
  +
</wf-button>
```

In `web/src/index.tsx`, `SidebarContent` manages the create-channel dialog and calls `client.createChannel()`. Note: `getClient` must be imported from `'./client'`:

```tsx
import { CreateChannelDialog } from './components/create-channel-dialog';

// Inside SidebarContent:
const [createChannelOpen, setCreateChannelOpen] = createSignal(false);

async function handleCreateChannel(name: string, isPublic: boolean) {
  setCreateChannelOpen(false);
  await getClient().createChannel(name, isPublic);
  // Refresh channel list.
  getClient().channels().then((chs) => channels.setChannels?.(chs)).catch(() => {});
  channels.setActiveChannel(name);
}

// In JSX, add after ChannelSidebar:
<CreateChannelDialog
  open={createChannelOpen()}
  onCreate={handleCreateChannel}
  onClose={() => setCreateChannelOpen(false)}
/>
```

And pass `onNewChannel={() => setCreateChannelOpen(true)}` to `ChannelSidebar`.

**Step 5: Add join button for non-member channels**

In the sidebar's channel list, if `ch.member` is false, show a "Join" button instead of navigating:

In `web/src/components/sidebar.tsx`, add `onJoinChannel` to props:
```typescript
onJoinChannel?: (channel: string) => void;
```

In the channel `<For>` render, wrap the click handler:
```tsx
on:click={() => {
  if (ch.member) {
    props.onSelectChannel(ch.name);
  } else {
    props.onJoinChannel?.(ch.name);
  }
}}
```

And add visual differentiation for non-member channels (e.g., italic name):
```tsx
<span class="sf-channel__name" style={ch.member ? undefined : 'font-style: italic; opacity: 0.7;'}>
  {ch.name}
</span>
```

In `web/src/index.tsx`, handle join:
```tsx
async function handleJoinChannel(channel: string) {
  await getClient().joinChannel(channel);
  getClient().channels().then((chs) => channels.setChannels?.(chs)).catch(() => {});
  channels.setActiveChannel(channel);
}

// Pass to ChannelSidebar:
onJoinChannel={handleJoinChannel}
```

**Step 6: Run tests**

Run: `cd web && pnpm test`
Expected: All tests pass.

**Step 7: Commit**

```bash
git add web/src/components/create-channel-dialog.tsx web/test/components/create-channel-dialog.test.tsx web/src/components/sidebar.tsx web/src/index.tsx
git commit -m "feat: add create-channel dialog and join-channel for non-members"
```

---

### Task 2: Invite User Dialog

**Files:**
- Create: `web/src/components/invite-dialog.tsx`
- Create: `web/test/components/invite-dialog.test.tsx`
- Modify: `web/src/components/channel-header.tsx` (add invite button for private channels)
- Modify: `web/src/components/chat.tsx` (wire dialog)

**Step 1: Write failing test**

Create `web/test/components/invite-dialog.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render } from 'solid-js/web';
import { InviteDialog } from '../../src/components/invite-dialog';
import type { User } from '@workfort/sharkfin-client';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('InviteDialog', () => {
  const users: User[] = [
    { username: 'alice', online: true, type: 'user' },
    { username: 'bob', online: false, type: 'agent' },
  ];

  it('renders dialog with user list', () => {
    const el = renderInto(() => (
      <InviteDialog channel="private-ch" users={users} open={true} onInvite={() => {}} onClose={() => {}} />
    ));
    expect(el.querySelector('wf-dialog')).toBeTruthy();
    const items = el.querySelectorAll('wf-list-item');
    expect(items.length).toBe(2);
  });

  it('calls onInvite with channel and username', () => {
    const onInvite = vi.fn();
    const el = renderInto(() => (
      <InviteDialog channel="private-ch" users={users} open={true} onInvite={onInvite} onClose={() => {}} />
    ));
    const firstItem = el.querySelector('wf-list-item') as HTMLElement;
    firstItem?.dispatchEvent(new CustomEvent('wf-select', { bubbles: true }));
    expect(onInvite).toHaveBeenCalledWith('private-ch', 'alice');
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/components/invite-dialog.test.tsx`
Expected: FAIL.

**Step 3: Implement InviteDialog**

Create `web/src/components/invite-dialog.tsx`:

```tsx
import { For, createEffect } from 'solid-js';
import type { User } from '@workfort/sharkfin-client';
import { initials } from '../utils';

interface InviteDialogProps {
  channel: string;
  users: User[];
  open: boolean;
  onInvite: (channel: string, username: string) => void;
  onClose: () => void;
}

export function InviteDialog(props: InviteDialogProps) {
  let dialogRef!: HTMLElement & { show(): void; hide(): void };

  createEffect(() => {
    if (props.open) dialogRef?.show();
    else dialogRef?.hide();
  });

  return (
    <wf-dialog
      ref={dialogRef}
      header={`Invite to #${props.channel}`}
      on:wf-close={props.onClose}
    >
      <wf-list>
        <For each={props.users}>
          {(user) => (
            <wf-list-item on:wf-select={() => props.onInvite(props.channel, user.username)}>
              <div class="sf-dm__avatar" style="margin-right: var(--wf-space-sm);">
                {initials(user.username)}
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

**Step 4: Wire into channel header and chat**

Add an "Invite" button to `ChannelHeader` — only shown for private channels. Add `isPublic` and `onInvite` to `ChannelHeaderProps`:

```tsx
interface ChannelHeaderProps {
  name: string;
  topic?: string;
  isPublic?: boolean;
  onInvite?: () => void;
}

// In the header JSX, after topic:
<Show when={!props.isPublic && props.onInvite}>
  <wf-button style="padding: 2px 8px; font-size: var(--wf-text-xs);" on:click={props.onInvite!}>
    Invite
  </wf-button>
</Show>
```

In `web/src/components/chat.tsx`, `ChatContent` manages the invite dialog state:

```tsx
import { InviteDialog } from './invite-dialog';

// Inside ChatContent:
const [inviteOpen, setInviteOpen] = createSignal(false);
const { channels, messages, users } = getStores();

const activeChannelObj = () => channels.channels().find(c => c.name === channels.activeChannel());

async function handleInvite(channel: string, username: string) {
  setInviteOpen(false);
  await getClient().inviteToChannel(channel, username);
}

// In JSX:
<ChannelHeader
  name={channels.activeChannel()}
  isPublic={activeChannelObj()?.public ?? true}
  onInvite={() => setInviteOpen(true)}
/>
// ... other components ...
<InviteDialog
  channel={channels.activeChannel()}
  users={users.users()}
  open={inviteOpen()}
  onInvite={handleInvite}
  onClose={() => setInviteOpen(false)}
/>
```

**Step 5: Run tests**

Run: `cd web && pnpm test`
Expected: All tests pass.

**Step 6: Commit**

```bash
git add web/src/components/invite-dialog.tsx web/test/components/invite-dialog.test.tsx web/src/components/channel-header.tsx web/src/components/chat.tsx
git commit -m "feat: add invite-user dialog for private channels"
```

---

### Task 3: Date Dividers in Message Area

**Files:**
- Modify: `web/src/components/message-area.tsx`
- Modify: `web/test/components/message-area.test.tsx`

**Step 1: Write failing test**

Add to `web/test/components/message-area.test.tsx`:

```tsx
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
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/components/message-area.test.tsx`
Expected: FAIL — no `.sf-divider` elements rendered.

**Step 3: Implement date dividers**

In `web/src/components/message-area.tsx`, add date comparison logic inside the `<For>`:

```tsx
function formatDateLabel(iso: string): string {
  const d = new Date(iso);
  const today = new Date();
  if (d.toDateString() === today.toDateString()) return 'Today';
  const yesterday = new Date(today);
  yesterday.setDate(today.getDate() - 1);
  if (d.toDateString() === yesterday.toDateString()) return 'Yesterday';
  return d.toLocaleDateString(undefined, { month: 'long', day: 'numeric', year: 'numeric' });
}

function isSameDay(a: string, b: string): boolean {
  return new Date(a).toDateString() === new Date(b).toDateString();
}
```

In the `<For>` callback, before each message, check if the date changed:

```tsx
<For each={props.messages}>
  {(msg, i) => {
    const prev = () => (i() > 0 ? props.messages[i() - 1] : undefined);
    const isContinuation = () => prev()?.from === msg.from && prev()?.sentAt && isSameDay(prev()!.sentAt, msg.sentAt);
    const showDivider = () => !prev() || !isSameDay(prev()!.sentAt, msg.sentAt);

    return (
      <>
        {showDivider() && i() > 0 && (
          <div class="sf-divider">
            <div class="sf-divider__line" />
            <span class="sf-divider__text">{formatDateLabel(msg.sentAt)}</span>
            <div class="sf-divider__line" />
          </div>
        )}
        <Message
          from={msg.from}
          body={msg.body}
          sentAt={msg.sentAt}
          continuation={isContinuation()}
        />
      </>
    );
  }}
</For>
```

Note: continuation messages should also require same day — two messages from the same author on different days should not be grouped.

**Step 4: Run tests**

Run: `cd web && pnpm test`
Expected: All tests pass.

**Step 5: Commit**

```bash
git add web/src/components/message-area.tsx web/test/components/message-area.test.tsx
git commit -m "feat: add date dividers between message groups"
```

---

### Task 4: Sidebar Search Filtering

**Prerequisite:** Plan 3 Task 2 adds `currentUsername` to `SidebarProps`. If implementing this task before Plan 3, add the prop here and update all existing sidebar test fixtures to include `currentUsername="me"`.

**Files:**
- Modify: `web/src/components/sidebar.tsx`
- Modify: `web/test/components/sidebar.test.tsx`

**Step 1: Write failing test**

Add to `web/test/components/sidebar.test.tsx`:

```tsx
it('filters channels by search term', () => {
  const el = renderInto(() => (
    <ChannelSidebar
      channels={channels} dms={dms} unreads={unreads} users={users}
      activeChannel="general" onSelectChannel={() => {}}
      currentUsername="me"
    />
  ));
  const searchInput = el.querySelector('input[type="text"]') as HTMLInputElement;
  searchInput.value = 'ran';
  searchInput.dispatchEvent(new Event('input', { bubbles: true }));

  const names = el.querySelectorAll('.sf-channel__name');
  expect(names.length).toBe(1);
  expect(names[0].textContent).toBe('random');
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/components/sidebar.test.tsx`
Expected: FAIL — search doesn't filter.

**Step 3: Implement search**

In `web/src/components/sidebar.tsx`, add a `searchTerm` signal and filter channels/DMs:

```tsx
import { createSignal, For, Show } from 'solid-js';

// Inside ChannelSidebar:
const [searchTerm, setSearchTerm] = createSignal('');

const filteredChannels = () => {
  const term = searchTerm().toLowerCase();
  if (!term) return props.channels;
  return props.channels.filter((ch) => ch.name.toLowerCase().includes(term));
};

const filteredDms = () => {
  const term = searchTerm().toLowerCase();
  if (!term) return props.dms;
  return props.dms.filter((dm) => {
    const other = dm.participants.find((p) => p !== props.currentUsername) ?? dm.participants[0];
    return other.toLowerCase().includes(term);
  });
};
```

Update the search input to bind:
```tsx
<input
  type="text"
  placeholder="Search conversations\u2026"
  on:input={(e: Event) => setSearchTerm((e.target as HTMLInputElement).value)}
/>
```

Use `filteredChannels()` and `filteredDms()` in the `<For>` loops instead of `props.channels` and `props.dms`.

**Step 4: Run tests**

Run: `cd web && pnpm test`
Expected: All tests pass.

**Step 5: Commit**

```bash
git add web/src/components/sidebar.tsx web/test/components/sidebar.test.tsx
git commit -m "feat: add sidebar search filtering for channels and DMs"
```

---

### Task 5: Migrate to `wf-list`/`wf-list-item` + `wf-divider`

**Files:**
- Modify: `web/src/components/sidebar.tsx`
- Modify: `web/src/components/channel-header.tsx`
- Modify: `web/test/components/sidebar.test.tsx`
- Modify: `web/test/components/channel-header.test.tsx`

**Step 1: Update sidebar to use `wf-list`/`wf-list-item`**

Replace the channel list's `div.sf-channel` elements with `wf-list-item`:

```tsx
<wf-list>
  <For each={filteredChannels()}>
    {(ch) => {
      const count = () => unreadFor(ch.name)?.unreadCount ?? 0;
      return (
        <wf-list-item
          active={ch.name === props.activeChannel}
          on:wf-select={() => ch.member ? props.onSelectChannel(ch.name) : props.onJoinChannel?.(ch.name)}
        >
          <span class="sf-channel__hash">#</span>
          <span class="sf-channel__name" style={ch.member ? undefined : 'font-style: italic; opacity: 0.7;'}>
            {ch.name}
          </span>
          <Show when={count() > 0}>
            <wf-badge data-wf="trailing" count={count()} size="sm" />
          </Show>
        </wf-list-item>
      );
    }}
  </For>
</wf-list>
```

Same for the DM list — replace `div.sf-dm` with `wf-list-item`.

**Step 2: Update channel header to use `wf-divider`**

In `web/src/components/channel-header.tsx`, replace the `sf-main__header` border-bottom with a `wf-divider` below the header:

```tsx
export function ChannelHeader(props: ChannelHeaderProps) {
  return (
    <>
      <div class="sf-main__header" style="border-bottom: none;">
        <span class="sf-main__channel-hash">#</span>
        <span class="sf-main__channel-name">{props.name}</span>
        <Show when={props.topic}>
          <span class="sf-main__topic">{props.topic}</span>
        </Show>
        <Show when={!props.isPublic && props.onInvite}>
          <wf-button style="padding: 2px 8px; font-size: var(--wf-text-xs);" on:click={props.onInvite!}>
            Invite
          </wf-button>
        </Show>
      </div>
      <wf-divider />
    </>
  );
}
```

**Step 3: Update tests**

Update sidebar tests for the new DOM structure:

Channel name queries:
```tsx
// Old: el.querySelectorAll('.sf-channel__name')
// New: el.querySelectorAll('wf-list-item .sf-channel__name')
```

Channel selection click test:
```tsx
// Old:
const randomCh = el.querySelectorAll('.sf-channel')[1] as HTMLElement;
randomCh.click();

// New:
const items = el.querySelectorAll('wf-list-item');
items[1].dispatchEvent(new CustomEvent('wf-select', { bubbles: true }));
```

Active channel test:
```tsx
// Old: el.querySelector('.sf-channel--active .sf-channel__name')
// New: el.querySelector('wf-list-item[active] .sf-channel__name')
```

DM rendering test:
```tsx
// Old: el.querySelectorAll('.sf-dm')
// New: query wf-list-item elements within the DM section
```

Unread badge test — badges now use `data-wf="trailing"`:
```tsx
// Old: el.querySelectorAll('wf-badge')
// New: el.querySelectorAll('wf-list-item wf-badge')
```

Update channel-header tests to check for `wf-divider` instead of border:
```tsx
it('renders wf-divider', () => {
  const el = renderInto(() => <ChannelHeader name="general" />);
  expect(el.querySelector('wf-divider')).toBeTruthy();
});
```

**Step 4: Run tests**

Run: `cd web && pnpm test`
Expected: All tests pass.

**Step 5: Commit**

```bash
git add web/src/components/sidebar.tsx web/src/components/channel-header.tsx web/test/
git commit -m "feat: migrate channel/DM lists to wf-list/wf-list-item, add wf-divider"
```

---

### Task 6: Responsive Mobile CSS

**Files:**
- Modify: `web/src/styles/chat.css`

**Step 1: Add responsive breakpoint**

The shell handles the two-column grid (sidebar + content). On mobile, the sidebar should collapse. Since the shell renders `SidebarContent` in its sidebar slot, the shell likely handles responsive sidebar collapse. But Sharkfin's internal CSS needs to adapt too.

Add to `web/src/styles/chat.css`:

```css
/* Responsive — mobile viewport */
@media (max-width: 640px) {
  .sf-sidebar__search { display: none; }

  .sf-main__header {
    padding: var(--wf-space-xs) var(--wf-space-md);
  }

  .sf-messages {
    padding: var(--wf-space-sm) var(--wf-space-md);
  }

  .sf-input {
    padding: 0 var(--wf-space-md) var(--wf-space-sm);
  }

  .sf-msg__avatar {
    width: 1.5rem;
    height: 1.5rem;
    font-size: 0.5rem;
  }
}
```

This tightens spacing on mobile. The sidebar collapse itself is the shell's responsibility — Sharkfin just needs to look good when the content area is full-width.

**Step 2: Verify build**

Run: `cd web && pnpm build`
Expected: Build succeeds.

**Step 3: Commit**

```bash
git add web/src/styles/chat.css
git commit -m "feat: add responsive CSS for mobile viewport"
```

---

### Task 7: Embed Web UI in Go Binary + PKGBUILD + AUR Local Build Task

**Files:**
- Create: `web/embed.go` (dev build — empty FS)
- Create: `web/embed_ui.go` (production build — `go:embed all:dist`)
- Modify: `pkg/daemon/ui_handler.go` (accept `fs.FS` parameter)
- Modify: `pkg/daemon/ui_handler_test.go` (update calls, add embed test)
- Modify: `pkg/daemon/server.go` (pass embedded FS)
- Modify: `aur/PKGBUILD` (add pnpm/nodejs makedepends, web build step)
- Create: `.mise/tasks/build/aur` (local AUR test build)

The design spec says: "For production, the UI assets can be embedded via `go:embed`." Following the scope repo's pattern: two embed files gated by a `ui` build tag. Dev builds have an empty FS (use `--ui-dir` or Vite proxy). Production builds (`go build -tags ui`) embed `web/dist/`.

**Step 1: Create the two embed files**

Create `web/embed.go` (dev, no tag):

```go
//go:build !ui

// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import "embed"

// Dist is empty when built without the "ui" tag. Use --ui-dir to serve
// from disk during development.
var Dist embed.FS
```

Create `web/embed_ui.go` (production, with tag):

```go
//go:build ui

// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import "embed"

// Dist holds the Vite build output. Built via:
//   cd web && pnpm build
//   go build -tags ui
//
//go:embed all:dist
var Dist embed.FS
```

Note: `web/` is not currently a Go package. These files make it one (`package web`), importable as `github.com/Work-Fort/sharkfin/web`.

**Step 2: Update `registerUIRoutes` to accept an `fs.FS` fallback**

Modify `pkg/daemon/ui_handler.go`:

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"io/fs"
	"net/http"
)

// registerUIRoutes adds the /ui/health probe and static file serving.
// If uiDir is set, files are served from disk (dev mode).
// Otherwise, embeddedFS is used (production mode with go:embed).
// If both are empty/nil, only /ui/health is registered.
func registerUIRoutes(mux *http.ServeMux, uiDir string, embeddedFS fs.FS) {
	mux.HandleFunc("GET /ui/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	var fileServer http.Handler
	if uiDir != "" {
		fileServer = http.FileServer(http.Dir(uiDir))
	} else if embeddedFS != nil {
		// The embedded FS has files under "dist/", so we sub into it.
		sub, err := fs.Sub(embeddedFS, "dist")
		if err == nil {
			fileServer = http.FileServer(http.FS(sub))
		}
	}

	if fileServer != nil {
		mux.Handle("/ui/", http.StripPrefix("/ui/", fileServer))
	}
}
```

**Step 3: Update `NewServer` to pass the embedded FS**

Modify `pkg/daemon/server.go` — add an import for the web package and pass it:

```go
import (
	// ... existing imports ...
	"github.com/Work-Fort/sharkfin/web"
)

// In NewServer, change the registerUIRoutes call:
registerUIRoutes(mux, uiDir, web.Dist)
```

**Step 4: Update tests**

Modify `pkg/daemon/ui_handler_test.go` — all calls to `registerUIRoutes` now take a third `fs.FS` parameter. Pass `nil` for tests that don't test embedded FS:

```go
func TestUIHealthReturns200(t *testing.T) {
	mux := http.NewServeMux()
	registerUIRoutes(mux, "", nil)
	// ... rest unchanged
}

func TestUIStaticFileServing(t *testing.T) {
	// ... dir setup ...
	registerUIRoutes(mux, dir, nil)
	// ... rest unchanged
}

func TestUINoStaticWhenDirEmpty(t *testing.T) {
	mux := http.NewServeMux()
	registerUIRoutes(mux, "", nil)
	// ... rest unchanged
}
```

Add a test for the embedded FS fallback:

```go
func TestUIEmbeddedFS(t *testing.T) {
	// Create an in-memory FS that mimics the embed structure.
	fsys := fstest.MapFS{
		"dist/test.js": &fstest.MapFile{Data: []byte("embedded")},
	}

	mux := http.NewServeMux()
	registerUIRoutes(mux, "", fsys)

	req := httptest.NewRequest("GET", "/ui/test.js", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "embedded" {
		t.Fatalf("unexpected body: %s", body)
	}
}
```

Add `"testing/fstest"` to the imports.

**Step 5: Update PKGBUILD**

Modify `aur/PKGBUILD` — add `nodejs` and `pnpm` to makedepends, build web UI before Go, use `-tags ui`:

```bash
makedepends=('go' 'nodejs' 'pnpm')

build() {
    cd "Sharkfin-${pkgver}"

    # Build web UI first — go:embed requires dist/ to exist.
    (cd web && pnpm install --frozen-lockfile && pnpm build)

    # Build Go binary with embedded UI.
    export CGO_ENABLED=0
    go build -tags ui -trimpath \
        -ldflags "-s -w -X github.com/Work-Fort/sharkfin/cmd.Version=v${pkgver}" \
        -o sharkfin
}

check() {
    cd "Sharkfin-${pkgver}"
    CGO_ENABLED=0 go test ./...
}
```

**Step 6: Create AUR local build task**

Create `.mise/tasks/build/aur` (following the scope repo's pattern):

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#MISE description="Test building the AUR package locally"
set -euo pipefail

BUILD_DIR="build/aur-test"

echo "==> Preparing AUR test build in $BUILD_DIR..."
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR"

# Copy PKGBUILD
cp aur/PKGBUILD "$BUILD_DIR/"

# Create a source tarball from the current tree (simulating the GitHub release archive)
VERSION=$(grep "pkgver=" aur/PKGBUILD | cut -d= -f2)
ARCHIVE_DIR="$BUILD_DIR/Sharkfin-${VERSION}"
mkdir -p "$ARCHIVE_DIR"

# Copy everything except build artifacts and node_modules
rsync -a --exclude='build/' --exclude='node_modules/' --exclude='.git/' \
  --exclude='dist/' --exclude='web/dist/' --exclude='web/node_modules/' \
  --exclude='web/.__mf__temp/' \
  . "$ARCHIVE_DIR/"

# Create the tarball
(cd "$BUILD_DIR" && tar czf "v${VERSION}.tar.gz" "Sharkfin-${VERSION}")
rm -rf "$ARCHIVE_DIR"

# Build the package (--nodeps skips pacman dependency install since we have mise)
echo "==> Running makepkg..."
(cd "$BUILD_DIR" && makepkg -sf --noconfirm --skipchecksums --nodeps)

# Show result
PKG=$(find "$BUILD_DIR" -name "*.pkg.tar.*" | head -1)
if [ -n "$PKG" ]; then
  echo "==> Package built successfully: $PKG"
  ls -lh "$PKG"
else
  echo "==> ERROR: Package build failed"
  exit 1
fi
```

Make it executable: `chmod +x .mise/tasks/build/aur`

**Step 7: Add build/aur-test to .gitignore**

Add to `.gitignore`:
```
build/aur-test/
```

**Step 8: Run Go tests**

Run: `go test ./pkg/daemon/ -run TestUI -v`
Expected: All 4 tests pass (3 existing + 1 new embedded FS test).

**Step 7: Verify full build with embedded UI**

```bash
cd web && pnpm build && cd ..
go build -o /tmp/sharkfin-test .
```

Expected: Binary builds successfully with embedded UI assets.

**Step 8: Commit**

```bash
git add web/embed.go web/embed_ui.go pkg/daemon/ui_handler.go pkg/daemon/ui_handler_test.go pkg/daemon/server.go aur/PKGBUILD .mise/tasks/build/aur .gitignore
git commit -m "feat: embed web UI via go:embed build tag, update PKGBUILD, add AUR local build task"
```

---

### Task 8: Playwright E2E — MCP to UI Message Delivery

**Files:**
- Create: `web/test/e2e/setup.ts` (daemon + JWKS stub launcher)
- Create: `web/test/e2e/chat.spec.ts` (Playwright tests)
- Modify: `playwright.config.ts` (point to web/test/e2e)
- Modify: `web/package.json` (add playwright dev dependency + test:e2e script)

These tests verify the complete flow: daemon starts with JWKS stub → web UI loads in browser → user sees channels → a second WS client sends a message → message appears in the browser. This is what unit tests with mock clients cannot catch.

**Architecture:**
- A TypeScript setup script (`globalSetup`) builds the Go binary, starts the daemon with a JWKS stub (reimplemented in TS using `jose`), signs JWTs for test users
- The daemon serves the embedded UI at `/ui/`
- Playwright navigates to `/ui/` and verifies the rendered output
- A `SharkfinClient` instance (from `@workfort/sharkfin-client`) acts as a second user sending messages
- The `getWebSocketUrl()` function needs to handle non-`/forts/` paths for direct daemon access — add a fallback: if no `/forts/{fort}` match, use `ws://${location.host}/ws`

**Step 1: Add Playwright and jose dependencies**

Add to `web/package.json` devDependencies:
```json
"@playwright/test": "^1.50.0",
"jose": "^6.0.0"
```

Add script:
```json
"test:e2e": "playwright test"
```

Run: `cd web && pnpm install`

**Step 2: Update `getWebSocketUrl()` for direct daemon access**

In `web/src/client.ts`, update the fallback when there's no `/forts/{fort}/` match:

```typescript
export function getWebSocketUrl(): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const match = location.pathname.match(/^\/forts\/([^/]+)/);
  if (match) {
    return `${proto}//${location.host}/forts/${match[1]}/api/sharkfin/ws`;
  }
  // Direct daemon access (e2e tests, standalone mode).
  return `${proto}//${location.host}/ws`;
}
```

**Step 3: Update playwright.config.ts**

```typescript
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './web/test/e2e',
  fullyParallel: false, // Tests share a daemon instance.
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  reporter: 'html',
  use: {
    trace: 'on-first-retry',
    baseURL: 'http://localhost:0', // Set dynamically by globalSetup.
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  globalSetup: './web/test/e2e/setup.ts',
});
```

**Step 4: Create the global setup (daemon + JWKS stub)**

Create `web/test/e2e/setup.ts`:

```typescript
import { execSync, spawn, type ChildProcess } from 'child_process';
import { mkdtempSync, writeFileSync } from 'fs';
import { tmpdir } from 'os';
import { join } from 'path';
import * as jose from 'jose';
import { createServer, type Server } from 'http';

let daemon: ChildProcess;
let jwksServer: Server;
const PROJECT_ROOT = join(__dirname, '..', '..', '..');

export default async function globalSetup() {
  // 1. Build the binary with embedded UI.
  execSync('cd web && pnpm build', { cwd: PROJECT_ROOT, stdio: 'inherit' });
  execSync('go build -o /tmp/sharkfin-e2e .', { cwd: PROJECT_ROOT, stdio: 'inherit' });

  // 2. Start JWKS stub.
  const { privateKey, publicKey } = await jose.generateKeyPair('RS256');
  const publicJWK = await jose.exportJWK(publicKey);
  publicJWK.kid = 'test-key-1';
  publicJWK.alg = 'RS256';
  const jwks = { keys: [publicJWK] };

  jwksServer = createServer((req, res) => {
    if (req.url === '/v1/jwks') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify(jwks));
    } else if (req.url === '/v1/verify-api-key' && req.method === 'POST') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ valid: true, key: { userId: 'bridge-id', metadata: { username: 'bridge', name: 'Bridge', type: 'service' } } }));
    } else {
      res.writeHead(404);
      res.end();
    }
  });
  await new Promise<void>((resolve) => jwksServer.listen(0, '127.0.0.1', resolve));
  const jwksPort = (jwksServer.address() as any).port;

  // 3. Sign JWTs for test users.
  async function signJWT(sub: string, username: string, name: string, type: string): Promise<string> {
    return new jose.SignJWT({ username, name, display_name: name, type })
      .setProtectedHeader({ alg: 'RS256', kid: 'test-key-1' })
      .setSubject(sub)
      .setIssuer('passport-stub')
      .setAudience('sharkfin')
      .setIssuedAt()
      .setExpirationTime('1h')
      .sign(privateKey);
  }

  const adminToken = await signJWT('admin-id', 'admin', 'Admin', 'user');
  const aliceToken = await signJWT('alice-id', 'alice', 'Alice', 'user');

  // 4. Start daemon.
  const xdgDir = mkdtempSync(join(tmpdir(), 'sharkfin-e2e-'));
  const daemonPort = 16100 + Math.floor(Math.random() * 900);
  const daemonAddr = `127.0.0.1:${daemonPort}`;

  daemon = spawn('/tmp/sharkfin-e2e', [
    'daemon',
    '--daemon', daemonAddr,
    '--passport-url', `http://127.0.0.1:${jwksPort}`,
    '--log-level', 'disabled',
    '--ui-dir', join(PROJECT_ROOT, 'web', 'dist'),
  ], {
    env: { ...process.env, XDG_CONFIG_HOME: join(xdgDir, 'config'), XDG_STATE_HOME: join(xdgDir, 'state') },
    stdio: ['ignore', 'inherit', 'inherit'],
  });

  // Wait for daemon to be ready.
  for (let i = 0; i < 50; i++) {
    try {
      const res = await fetch(`http://${daemonAddr}/ui/health`);
      if (res.ok) break;
    } catch { /* not ready yet */ }
    await new Promise((r) => setTimeout(r, 100));
  }

  // 5. Grant admin role so test user has all permissions.
  execSync(`/tmp/sharkfin-e2e admin set-role admin admin --db ${join(xdgDir, 'state', 'sharkfin', 'sharkfin.db')}`, {
    env: { ...process.env, XDG_CONFIG_HOME: join(xdgDir, 'config'), XDG_STATE_HOME: join(xdgDir, 'state') },
  });

  // 6. Store test config for specs to use.
  const testConfig = { daemonAddr, adminToken, aliceToken, xdgDir };
  writeFileSync(join(tmpdir(), 'sharkfin-e2e-config.json'), JSON.stringify(testConfig));
  process.env.SHARKFIN_E2E_CONFIG = join(tmpdir(), 'sharkfin-e2e-config.json');

  return async () => {
    daemon?.kill();
    jwksServer?.close();
  };
}
```

**Step 5: Write the Playwright test**

Create `web/test/e2e/chat.spec.ts`:

```typescript
import { test, expect } from '@playwright/test';
import { readFileSync } from 'fs';
import { tmpdir } from 'os';
import { join } from 'path';
import { SharkfinClient } from '@workfort/sharkfin-client';
import WebSocket from 'ws';

function getConfig() {
  const path = process.env.SHARKFIN_E2E_CONFIG || join(tmpdir(), 'sharkfin-e2e-config.json');
  return JSON.parse(readFileSync(path, 'utf-8'));
}

test.describe('Sharkfin Web UI E2E', () => {
  let config: { daemonAddr: string; adminToken: string; aliceToken: string };

  test.beforeAll(() => {
    config = getConfig();
  });

  test('UI loads and shows channels', async ({ page }) => {
    await page.goto(`http://${config.daemonAddr}/ui/`);
    // Wait for the UI to connect and load channels.
    await page.waitForSelector('.sf-main', { timeout: 10000 });
    // The default 'general' channel should exist after admin connects.
    await expect(page.locator('.sf-main__channel-name')).toBeVisible({ timeout: 5000 });
  });

  test('message sent via WS appears in browser', async ({ page }) => {
    await page.goto(`http://${config.daemonAddr}/ui/`);
    await page.waitForSelector('.sf-messages', { timeout: 10000 });

    // Connect a second client (Alice) and send a message.
    const alice = new SharkfinClient(`ws://${config.daemonAddr}/ws`, {
      token: config.aliceToken,
      WebSocket: WebSocket as any,
    });
    await alice.connect();

    // Get or create the channel that the browser is viewing.
    const channels = await alice.channels();
    const activeChannel = channels[0]?.name ?? 'general';

    await alice.sendMessage(activeChannel, 'Hello from Alice via WS!');

    // Verify the message appears in the browser.
    await expect(page.locator('.sf-msg__text', { hasText: 'Hello from Alice via WS!' }))
      .toBeVisible({ timeout: 5000 });

    alice.close();
  });

  test('unread badge updates on new message', async ({ page }) => {
    await page.goto(`http://${config.daemonAddr}/ui/`);
    await page.waitForSelector('.sf-messages', { timeout: 10000 });

    // Create a second channel and send a message to it.
    const alice = new SharkfinClient(`ws://${config.daemonAddr}/ws`, {
      token: config.aliceToken,
      WebSocket: WebSocket as any,
    });
    await alice.connect();
    await alice.createChannel('e2e-test-ch', true);
    await alice.sendMessage('e2e-test-ch', 'unread test');

    // The badge should appear in the sidebar for the non-active channel.
    await expect(page.locator('wf-badge')).toBeVisible({ timeout: 5000 });

    alice.close();
  });

  test('presence indicator updates', async ({ page }) => {
    await page.goto(`http://${config.daemonAddr}/ui/`);
    await page.waitForSelector('.sf-messages', { timeout: 10000 });

    // Alice connects — should trigger a presence update visible in DM list.
    const alice = new SharkfinClient(`ws://${config.daemonAddr}/ws`, {
      token: config.aliceToken,
      WebSocket: WebSocket as any,
    });
    await alice.connect();
    await alice.setState('active');

    // Verify Alice appears in user list or presence updates.
    // This depends on DM existing — create one.
    await alice.dmOpen('admin');

    // Wait briefly for the presence update to propagate.
    await page.waitForTimeout(1000);

    // Status dot should show online.
    const statusDot = page.locator('wf-status-dot[status="online"]');
    await expect(statusDot).toBeVisible({ timeout: 5000 });

    alice.close();
  });
});
```

**Step 6: Add `ws` dependency for Node.js WS client in tests**

Add to `web/package.json` devDependencies:
```json
"ws": "^8.0.0",
"@types/ws": "^8.0.0"
```

**Step 7: Run the e2e tests**

```bash
cd web && pnpm exec playwright install chromium
pnpm test:e2e
```

Expected: All 4 tests pass — UI loads, message appears, unread badge updates, presence shows.

**Step 8: Commit**

```bash
git add web/test/e2e/ playwright.config.ts web/package.json
git commit -m "feat: add Playwright e2e tests — MCP to UI message delivery verification"
```

---

### Task 9: Final Verification

**Step 1: Run all web tests**

Run: `cd web && pnpm test`
Expected: All tests pass.

**Step 2: Run Go tests**

Run: `cd /home/kazw/Work/WorkFort/sharkfin/lead && go test ./pkg/daemon/ -run TestUI -v`
Expected: All pass.

**Step 3: Build**

Run: `cd web && pnpm build`
Expected: Build succeeds with `remoteEntry.js`.

**Step 4: Verify coverage checklist**

Manually verify each gap from the gap analysis is addressed:

| Gap # | Description | Status |
|-------|-------------|--------|
| 1 | `connect()` called | Plan 3 Task 1 |
| 2 | DM participant identity | Plan 3 Task 2 |
| 3 | Create-channel dialog | Plan 4 Task 1 |
| 4 | Invite-user dialog | Plan 4 Task 2 |
| 5 | DM creation | Plan 3 Task 4 |
| 6 | Loading states | Plan 3 Task 1 |
| 7 | Reconnection UI | Plan 3 Task 1 |
| 8 | DMs work overall | Plans 3+4 |
| 9 | Date dividers | Plan 4 Task 3 |
| 10 | Away presence | Plan 3 Task 3 |
| 11 | Search filtering | Plan 4 Task 4 |
| 12 | Join channel | Plan 4 Task 1 |
| 13 | wf-list/wf-list-item | Plan 4 Task 5 |
| 14 | wf-divider | Plan 4 Task 5 |
| 15 | setState() | Plan 3 Task 3 |
| 16 | capabilities() / permission-gated UI | Plan 3 Task 6 |

| Success Criterion | Status |
|---|---|
| 1. Shell loads MF | ✅ (Plan 1) |
| 2. Sidebar in shell slot | ✅ (Plan 1) |
| 3. Messages + real-time | ✅ (Plan 3 Task 1) |
| 4. Send messages | ✅ (Plan 3 Task 1) |
| 5. Presence online/away/offline | ✅ (Plan 3 Task 3) |
| 6. Unread badges | ✅ (Plan 3 Task 1) |
| 7. Channel switching | ✅ (Plan 4 Task 5) |
| 8. DMs work | ✅ (Plans 3+4) |
| 9. Theming | ✅ (Plan 2) |
| 10. Mobile responsive | ✅ (Plan 4 Task 6) |

**All 16 gaps addressed. All 10 success criteria covered.**

**Step 5: Commit if any fixes needed**

```bash
git add -A web/
git commit -m "fix: final integration adjustments"
```

---

### Task 8: Component Extraction Review for @workfort/ui

After all tasks are complete and the UI is working, review every Sharkfin component and utility for potential extraction into `@workfort/ui` or `@workfort/ui-solid` so other WorkFort services can reuse them.

**Evaluation criteria for extraction:**
- Is this component domain-agnostic? (Could another service use it without modification?)
- Does it duplicate something that should exist at the platform level?
- Would extracting it reduce code duplication across services?

**Candidates to evaluate:**

| Component | Sharkfin-specific? | Extraction candidate? | Notes |
|---|---|---|---|
| `Message` | Message rendering with avatars, timestamps, continuations | Evaluate | Any service with a feed/timeline could use this |
| `MessageArea` | Scrollable list with date dividers and grouping | Evaluate | Feed/activity patterns are common |
| `InputBar` | Textarea with Enter-to-send + send button | Evaluate | Any service with a compose input could use this |
| `TypingIndicator` | Typing status display | Evaluate | Chat-specific but could be generic presence indicator |
| `ChannelSidebar` | Channel/DM list with badges and presence | Likely no | Too coupled to chat domain |
| `ChannelHeader` | Channel name + topic + actions | Likely no | Too coupled to chat domain |
| `CreateChannelDialog` | Channel creation form | Likely no | Chat-specific |
| `InviteDialog` | User picker for invites | Evaluate | User picker is generic — the invite action is chat-specific |
| `DMDialog` | User picker for DMs | Evaluate | Same user picker pattern as InviteDialog |
| `initials()` utility | Extract initials from username | Likely yes | Any avatar display needs this |
| `useIdleDetection` hook | Activity tracking + idle state | Likely yes | Any service with presence could use this |
| `formatTime()` utility | ISO → HH:MM display | Likely yes | Timestamp formatting is universal |
| `formatDateLabel()` utility | ISO → "Today"/"Yesterday"/date | Likely yes | Date grouping is common |
| Permission store pattern | `capabilities()` → `can()` signal | Evaluate | Other services have permissions too |

**Step 1: Audit each candidate**

For each "Evaluate" item above:
1. Read the implementation
2. Identify what's Sharkfin-specific vs generic
3. Determine if it can be extracted as-is or needs generalization
4. Note the target package (`@workfort/ui` for web components, `@workfort/ui-solid` for SolidJS hooks/utilities)

**Step 2: Write findings**

Create `docs/2026-03-16-component-extraction-review.md` documenting:
- Which components/utilities should be extracted
- What changes are needed to make them generic
- Target package for each
- Priority (extract now vs later)

**Step 3: Commit the review document**

```bash
git add docs/2026-03-16-component-extraction-review.md
git commit -m "docs: component extraction review for @workfort/ui"
```

Note: This task produces a document, not code changes. Actual extraction is a separate effort coordinated with the scope team lead since it involves changes to `@workfort/ui` and `@workfort/ui-solid` in the scope repo.
