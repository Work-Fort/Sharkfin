# Component Extraction: wf-nav-sidebar — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract a reusable `<wf-nav-sidebar>` web component from sharkfin's `ChannelSidebar` that can be shared across MF remotes (sharkfin Chat, Hive, and future services).

**Architecture:** The component provides the structural shell — header with title + action button, search input, collapsible sections with items — while leaving item rendering to the consumer via slots. This keeps the component framework-agnostic (works with SolidJS, React, Svelte, Vue) while standardizing the sidebar layout, search filtering, and section collapse behavior. Lives in `@workfort/ui` (scope repo).

**Tech Stack:** Lit (light DOM), CSS custom properties, named slots, SolidJS (first consumer)

---

## Context

Sharkfin's `ChannelSidebar` (134 lines) is entirely generic: a title, search, two sections (Channels + DMs) with items that have labels, badges, and status indicators. The Hive UI will need a similar nav sidebar. Rather than duplicating the pattern, we extract the structural shell as a web component.

### Design decisions

1. **Slots over props for items** — The sidebar renders heterogeneous items (channels with `#` prefix, DMs with avatars, badges). Passing these as props would require a complex data model. Slots let each consumer render items however they want.

2. **Sections as child elements** — `<wf-nav-section heading="Channels">` provides collapsible section headers. Items are slotted inside.

3. **Search is built-in** — The search input is part of the component. It emits a `wf-search` event with the current term; the consumer filters their own data.

4. **No business logic** — No permissions, no unread counts, no join behavior. The consumer handles all of that and just renders the appropriate items.

### Component API

```html
<wf-nav-sidebar heading="Sharkfin">
  <wf-button slot="actions" title="New channel">+</wf-button>

  <wf-nav-section heading="Channels">
    <wf-list-item active>
      <span>#</span> general
      <wf-badge data-wf="trailing" count="3" size="sm" />
    </wf-list-item>
    <wf-list-item># random</wf-list-item>
  </wf-nav-section>

  <wf-nav-section heading="Direct Messages">
    <wf-button slot="section-actions" title="New DM">+</wf-button>
    <wf-list-item>
      <wf-avatar username="bob" size="sm" status="online" />
      bob
    </wf-list-item>
  </wf-nav-section>
</wf-nav-sidebar>
```

---

### Task 1: Create `<wf-nav-section>` component

**Files:**
- Create: `~/Work/WorkFort/scope/lead/web/packages/ui/src/layout/wf-nav-section.ts`
- Modify: `~/Work/WorkFort/scope/lead/web/packages/ui/src/styles/components.css`
- Modify: `~/Work/WorkFort/scope/lead/web/packages/ui/src/index.ts`

**Step 1: Write the component**

Create `~/Work/WorkFort/scope/lead/web/packages/ui/src/layout/wf-nav-section.ts`:

```typescript
import { html } from 'lit';
import { property } from 'lit/decorators.js';
import { WfElement } from '../base.js';

/**
 * `<wf-nav-section>` — A collapsible section inside a nav sidebar.
 *
 * @element wf-nav-section
 * @attr heading - Section heading text.
 * @attr collapsed - Whether the section is collapsed.
 * @slot - Default slot for list items.
 * @slot section-actions - Action buttons shown in the section header.
 */
export class WfNavSection extends WfElement {
  @property({ type: String }) heading = '';
  @property({ type: Boolean, reflect: true }) collapsed = false;

  private _userContent: Node[] = [];
  private _didSetup = false;

  connectedCallback(): void {
    super.connectedCallback();
    this.classList.add('wf-nav-section');
    if (!this._didSetup) {
      this._userContent = Array.from(this.childNodes);
    }
  }

  protected override updated(): void {
    super.updated();
    if (!this._didSetup) {
      this._didSetup = true;
      this._distributeContent();
    }
  }

  private _distributeContent(): void {
    const actionsSlot = this.querySelector('.wf-nav-section__actions');
    const body = this.querySelector('.wf-nav-section__body');
    for (const node of this._userContent) {
      if (node instanceof Element && node.getAttribute('slot') === 'section-actions' && actionsSlot) {
        actionsSlot.appendChild(node);
      } else if (body) {
        body.appendChild(node);
      }
    }
  }

  private _toggle(): void {
    this.collapsed = !this.collapsed;
  }

  render() {
    return html`
      <div class="wf-nav-section__header" @click=${() => this._toggle()}>
        <span class="wf-nav-section__heading">${this.heading}</span>
        <span class="wf-nav-section__actions"></span>
      </div>
      <div class="wf-nav-section__body" ?hidden=${this.collapsed}></div>
    `;
  }
}

customElements.define('wf-nav-section', WfNavSection);

declare global {
  interface HTMLElementTagNameMap {
    'wf-nav-section': WfNavSection;
  }
}
```

**Step 2: Add CSS**

Add to `components.css`:

```css
/* Nav Section */
.wf-nav-section {
  display: block;
}

.wf-nav-section__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--wf-space-sm) var(--wf-space-md);
  cursor: pointer;
  user-select: none;
}

.wf-nav-section__heading {
  font-size: var(--wf-text-xs);
  font-weight: var(--wf-weight-semibold);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--wf-color-text-secondary);
}

.wf-nav-section__actions {
  display: flex;
  align-items: center;
  gap: var(--wf-space-xs);
}

.wf-nav-section__body[hidden] {
  display: none;
}
```

**Step 3: Export**

Add to `index.ts`:

```typescript
export { WfNavSection } from './layout/wf-nav-section.js';
```

**Step 4: Build**

Run: `cd ~/Work/WorkFort/scope/lead/web/packages/ui && pnpm build`

**Step 5: Commit**

```bash
cd ~/Work/WorkFort/scope/lead
git add web/packages/ui/src/layout/wf-nav-section.ts web/packages/ui/src/styles/components.css web/packages/ui/src/index.ts
git commit -m "feat: add wf-nav-section component with collapsible sections"
```

---

### Task 2: Create `<wf-nav-sidebar>` component

**Files:**
- Create: `~/Work/WorkFort/scope/lead/web/packages/ui/src/layout/wf-nav-sidebar.ts`
- Modify: `~/Work/WorkFort/scope/lead/web/packages/ui/src/styles/components.css`
- Modify: `~/Work/WorkFort/scope/lead/web/packages/ui/src/index.ts`

**Step 1: Write the component**

Create `~/Work/WorkFort/scope/lead/web/packages/ui/src/layout/wf-nav-sidebar.ts`:

```typescript
import { html } from 'lit';
import { property } from 'lit/decorators.js';
import { WfElement } from '../base.js';
import './wf-nav-section.js';

/**
 * `<wf-nav-sidebar>` — Structural shell for a service navigation sidebar.
 *
 * Provides a heading, optional action buttons, a search input, and slots
 * for wf-nav-section children. Search emits `wf-search` events; filtering
 * is the consumer's responsibility.
 *
 * @element wf-nav-sidebar
 * @attr heading - Sidebar title (e.g. service name).
 * @attr search-placeholder - Placeholder text for the search input.
 * @slot actions - Action buttons shown in the header next to the title.
 * @slot - Default slot for wf-nav-section children.
 * @fires wf-search - When the search input value changes (detail: { term: string }).
 */
export class WfNavSidebar extends WfElement {
  @property({ type: String }) heading = '';
  @property({ attribute: 'search-placeholder', type: String }) searchPlaceholder = 'Search…';

  private _userContent: Node[] = [];
  private _didSetup = false;

  connectedCallback(): void {
    super.connectedCallback();
    this.classList.add('wf-nav-sidebar');
    if (!this._didSetup) {
      this._userContent = Array.from(this.childNodes);
    }
  }

  protected override updated(): void {
    super.updated();
    if (!this._didSetup) {
      this._didSetup = true;
      this._distributeContent();
    }
  }

  private _distributeContent(): void {
    const actionsSlot = this.querySelector('.wf-nav-sidebar__actions');
    const body = this.querySelector('.wf-nav-sidebar__body');
    for (const node of this._userContent) {
      if (node instanceof Element && node.getAttribute('slot') === 'actions' && actionsSlot) {
        actionsSlot.appendChild(node);
      } else if (body) {
        body.appendChild(node);
      }
    }
  }

  private _onSearch(e: Event): void {
    const term = (e.target as HTMLInputElement).value;
    this.dispatchEvent(
      new CustomEvent('wf-search', {
        bubbles: true,
        composed: true,
        detail: { term },
      }),
    );
  }

  render() {
    return html`
      <div class="wf-nav-sidebar__header">
        <span class="wf-nav-sidebar__title">${this.heading}</span>
        <span class="wf-nav-sidebar__actions"></span>
      </div>
      <div class="wf-nav-sidebar__search">
        <wf-text-input
          placeholder=${this.searchPlaceholder}
          @input=${(e: Event) => this._onSearch(e)}
        ></wf-text-input>
      </div>
      <div class="wf-nav-sidebar__body"></div>
    `;
  }
}

customElements.define('wf-nav-sidebar', WfNavSidebar);

declare global {
  interface HTMLElementTagNameMap {
    'wf-nav-sidebar': WfNavSidebar;
  }
}
```

**Step 2: Add CSS**

Add to `components.css`:

```css
/* Nav Sidebar */
.wf-nav-sidebar {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border-right: 1px solid var(--wf-color-border);
  background: var(--wf-color-bg);
  font-family: var(--wf-font-sans);
  color: var(--wf-color-text);
}

.wf-nav-sidebar__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--wf-space-md) var(--wf-space-md) var(--wf-space-sm);
}

.wf-nav-sidebar__title {
  font-size: var(--wf-text-sm);
  font-weight: var(--wf-weight-semibold);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--wf-color-text-secondary);
}

.wf-nav-sidebar__actions {
  display: flex;
  align-items: center;
  gap: var(--wf-space-xs);
}

.wf-nav-sidebar__search {
  padding: 0 var(--wf-space-sm) var(--wf-space-sm);
}

.wf-nav-sidebar__body {
  flex: 1;
  overflow-y: auto;
}
```

**Step 3: Export**

Add to `index.ts`:

```typescript
export { WfNavSidebar } from './layout/wf-nav-sidebar.js';
```

**Step 4: Build**

Run: `cd ~/Work/WorkFort/scope/lead/web/packages/ui && pnpm build`

**Step 5: Commit**

```bash
cd ~/Work/WorkFort/scope/lead
git add web/packages/ui/src/layout/wf-nav-sidebar.ts web/packages/ui/src/styles/components.css web/packages/ui/src/index.ts
git commit -m "feat: add wf-nav-sidebar component with search and slot-based sections"
```

---

### Task 3: Refactor sharkfin sidebar to use `<wf-nav-sidebar>` + `<wf-nav-section>`

**Files:**
- Modify: `~/Work/WorkFort/sharkfin/lead/web/src/components/sidebar.tsx`
- Modify: `~/Work/WorkFort/sharkfin/lead/web/src/styles/chat.css`

**Step 1: Rewrite sidebar.tsx to use the new components**

Replace the manual sidebar markup with the web components. The structure becomes:

```tsx
export function ChannelSidebar(props: SidebarProps) {
  const canCreateChannel = createMemo(() => !props.can || props.can('create_channel'));
  const canChannelList = createMemo(() => !props.can || props.can('channel_list'));
  const canJoinChannel = createMemo(() => !props.can || props.can('join_channel'));
  const canDmList = createMemo(() => !props.can || props.can('dm_list'));
  const canDmOpen = createMemo(() => !props.can || props.can('dm_open'));
  const [searchTerm, setSearchTerm] = createSignal('');

  // ... existing filter/helper functions unchanged ...

  return (
    <wf-nav-sidebar
      heading="Sharkfin"
      search-placeholder="Search conversations…"
      on:wf-search={(e: CustomEvent) => setSearchTerm(e.detail.term)}
    >
      <Show when={canCreateChannel()}>
        <wf-button slot="actions" style="padding: var(--wf-space-xs) var(--wf-space-sm); font-size: var(--wf-text-sm);" title="New channel" on:click={() => props.onNewChannel?.()}>
          +
        </wf-button>
      </Show>

      <Show when={canChannelList()} fallback={
        <wf-nav-section heading="Channels">
          <div style="padding: var(--wf-space-sm) var(--wf-space-md); font-size: var(--wf-text-xs); color: var(--wf-color-text-muted);">
            No channel access
          </div>
        </wf-nav-section>
      }>
        <wf-nav-section heading="Channels">
          <wf-list>
            <For each={filteredChannels()}>
              {(ch) => {
                const count = () => unreadFor(ch.name)?.unreadCount ?? 0;
                return (
                  <wf-list-item
                    active={ch.name === props.activeChannel}
                    on:wf-select={() => {
                      if (ch.member) {
                        props.onSelectChannel(ch.name);
                      } else if (canJoinChannel()) {
                        props.onJoinChannel?.(ch.name);
                      }
                    }}
                  >
                    <span>#</span>
                    <span style={ch.member ? undefined : 'font-style: italic; color: var(--wf-color-text-muted);'}>{ch.name}</span>
                    <Show when={count() > 0}>
                      <wf-badge data-wf="trailing" count={count()} size="sm" />
                    </Show>
                  </wf-list-item>
                );
              }}
            </For>
          </wf-list>
        </wf-nav-section>
      </Show>

      <Show when={canDmList()}>
        <wf-nav-section heading="Direct Messages">
          <Show when={props.onNewDM && canDmOpen()}>
            <wf-button slot="section-actions" style="padding: 0 var(--wf-space-xs); font-size: var(--wf-text-xs);" title="New DM" on:click={() => props.onNewDM!()}>+</wf-button>
          </Show>
          <wf-list>
            <For each={filteredDms()}>
              {(dm) => {
                const other = () => dm.participants.find((p) => p !== props.currentUsername) ?? dm.participants[0];
                const status = () => userStatus(other());
                const presenceStatus = () => {
                  const s = status();
                  if (!s?.online) return 'offline';
                  return s.state === 'idle' ? 'away' : 'online';
                };
                return (
                  <wf-list-item on:wf-select={() => props.onSelectChannel(dm.channel)}>
                    <wf-avatar username={other()} size="sm" status={presenceStatus()} />
                    <span>{other()}</span>
                  </wf-list-item>
                );
              }}
            </For>
          </wf-list>
        </wf-nav-section>
      </Show>
    </wf-nav-sidebar>
  );
}
```

**Step 2: Remove orphaned CSS**

In chat.css, remove:
- `.sf-sidebar` and all `.sf-sidebar__*` rules (header, title, search, search input)
- `.sf-channels`, `.sf-section-label`
- `.sf-channel`, `.sf-channel__hash`, `.sf-channel__name` (channel item styles handled by `wf-list-item`)
- `.sf-dm__avatar` (handled by `wf-avatar`)

Keep only styles that are still needed for the DM/channel item layout if `wf-list-item` doesn't cover them. Check by building and visually verifying.

**Step 3: Build both**

Run: `cd ~/Work/WorkFort/scope/lead/web/packages/ui && pnpm build`
Run: `cd ~/Work/WorkFort/scope/lead && mise run web:build`
Run: `cd ~/Work/WorkFort/sharkfin/lead/web && pnpm build`

**Step 4: Commit**

```bash
cd ~/Work/WorkFort/sharkfin/lead
git add web/src/components/sidebar.tsx web/src/styles/chat.css
git commit -m "refactor: use wf-nav-sidebar and wf-nav-section in sharkfin sidebar"
```

---

### Task 4: Visual verification

**Step 1: Restart services**

Deploy sharkfin with the new UI:
```bash
cd ~/Work/WorkFort/sharkfin/lead && mise run deploy
```

The scope-server serves the shell SPA from its dist directory — no restart needed since files are served from disk.

**Step 2: Verify in browser**

Open the shell and check:
- Sidebar heading "Sharkfin" + create channel button visible
- Search input works (filters channels and DMs)
- Channel list shows `#` prefix, unread badges, active state
- Non-member channels shown in muted color
- DM list shows avatars with status dots
- Section headers ("Channels", "Direct Messages") are clickable to collapse
- Mobile responsive: sidebar behavior at 640px breakpoint

**Step 3: Commit any tweaks**

If visual adjustments are needed (spacing, alignment), fix and commit.

---

### Task 5: Rebuild scope shell SPA

**Files:**
- No code changes — just rebuild to pick up the new @workfort/ui components

**Step 1: Rebuild**

```bash
cd ~/Work/WorkFort/scope/lead && mise run web:build
```

This ensures the shell SPA bundles the latest @workfort/ui with `wf-avatar`, `wf-nav-sidebar`, and `wf-nav-section` available for any MF remote that imports them.
