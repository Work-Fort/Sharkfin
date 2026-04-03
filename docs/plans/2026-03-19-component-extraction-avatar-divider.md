# Component Extraction: wf-avatar + wf-divider label — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract a reusable `<wf-avatar>` web component and extend `<wf-divider>` with an optional label, then refactor sharkfin to use them.

**Architecture:** Both components go into `@workfort/ui` (scope repo) as light-DOM Lit web components following the existing `WfElement` base class pattern. CSS goes into `components.css`. Sharkfin's chat CSS and SolidJS components are then updated to consume the new components, removing duplicated avatar/divider markup.

**Tech Stack:** Lit (light DOM), CSS custom properties, SolidJS (consumer)

---

## Context

The avatar-with-initials-and-status-dot pattern is duplicated in three places: sharkfin sidebar (DM list), sharkfin messages, and @workfort/ui's `wf-user-picker`. A `<wf-avatar>` component eliminates this duplication.

The date divider in sharkfin's message area manually builds a "line + centered text + line" pattern. The existing `<wf-divider>` is CSS-only with no label support. Adding an optional `label` attribute eliminates the manual markup.

---

### Task 1: Create `<wf-avatar>` component

**Files:**
- Create: `~/Work/WorkFort/scope/lead/web/packages/ui/src/components/avatar.ts`
- Modify: `~/Work/WorkFort/scope/lead/web/packages/ui/src/styles/components.css`
- Modify: `~/Work/WorkFort/scope/lead/web/packages/ui/src/index.ts` (export)

**Step 1: Write the component**

Create `~/Work/WorkFort/scope/lead/web/packages/ui/src/components/avatar.ts`:

```typescript
import { html } from 'lit';
import { property } from 'lit/decorators.js';
import { WfElement } from '../base.js';
import { initials } from '../utils/initials.js';

/**
 * `<wf-avatar>` — Circular avatar showing initials with optional status dot.
 *
 * @element wf-avatar
 * @attr username - Username to derive initials from.
 * @attr size - Size variant: "sm" (1.5rem), "md" (2rem). Default: "md".
 * @attr status - Optional presence status: "online", "away", "offline". If omitted, no dot is shown.
 */
export class WfAvatar extends WfElement {
  @property({ type: String }) username = '';
  @property({ type: String, reflect: true }) size: 'sm' | 'md' = 'md';
  @property({ type: String }) status?: string;

  connectedCallback(): void {
    super.connectedCallback();
    this.classList.add('wf-avatar');
  }

  render() {
    return html`
      <span class="wf-avatar__initials">${initials(this.username)}</span>
      ${this.status
        ? html`<wf-status-dot class="wf-avatar__dot" status=${this.status}></wf-status-dot>`
        : ''}
    `;
  }
}

customElements.define('wf-avatar', WfAvatar);

declare global {
  interface HTMLElementTagNameMap {
    'wf-avatar': WfAvatar;
  }
}
```

**Step 2: Add CSS to components.css**

Add to `~/Work/WorkFort/scope/lead/web/packages/ui/src/styles/components.css`:

```css
/* Avatar */
.wf-avatar {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--wf-radius-full);
  background: var(--wf-color-bg-elevated);
  font-weight: var(--wf-weight-semibold);
  color: var(--wf-color-text-secondary);
  flex-shrink: 0;
  position: relative;
  width: var(--wf-space-8);
  height: var(--wf-space-8);
  font-size: var(--wf-text-xs);
}

.wf-avatar[size="sm"] {
  width: var(--wf-space-6);
  height: var(--wf-space-6);
  font-size: var(--wf-text-2xs);
}

.wf-avatar__dot {
  position: absolute;
  bottom: -1px;
  right: -1px;
}
```

**Step 3: Export from index.ts**

Add to the exports in `~/Work/WorkFort/scope/lead/web/packages/ui/src/index.ts`:

```typescript
export { WfAvatar } from './components/avatar.js';
```

**Step 4: Build**

Run: `cd ~/Work/WorkFort/scope/lead/web/packages/ui && pnpm build`

**Step 5: Commit**

```bash
cd ~/Work/WorkFort/scope/lead
git add web/packages/ui/src/components/avatar.ts web/packages/ui/src/styles/components.css web/packages/ui/src/index.ts
git commit -m "feat: add wf-avatar component with initials + status dot"
```

---

### Task 2: Extend `<wf-divider>` with optional label

**Files:**
- Modify: `~/Work/WorkFort/scope/lead/web/packages/ui/src/components/divider.ts`
- Modify: `~/Work/WorkFort/scope/lead/web/packages/ui/src/styles/components.css`

**Step 1: Add label property and render method**

Replace `~/Work/WorkFort/scope/lead/web/packages/ui/src/components/divider.ts` with:

```typescript
import { html, nothing } from 'lit';
import { property } from 'lit/decorators.js';
import { WfElement } from '../base.js';

/**
 * `<wf-divider>` — Horizontal separator with optional centered label.
 *
 * @element wf-divider
 * @attr label - Optional text label centered on the divider line.
 */
export class WfDivider extends WfElement {
  @property({ type: String }) label?: string;

  connectedCallback(): void {
    super.connectedCallback();
    this.classList.add('wf-divider');
    this.setAttribute('role', 'separator');
  }

  render() {
    if (!this.label) return nothing;
    return html`
      <span class="wf-divider__line"></span>
      <span class="wf-divider__text">${this.label}</span>
      <span class="wf-divider__line"></span>
    `;
  }
}

customElements.define('wf-divider', WfDivider);

declare global {
  interface HTMLElementTagNameMap {
    'wf-divider': WfDivider;
  }
}
```

When no label is set, `render()` returns `nothing` — the component remains a CSS-only 1px line (existing behavior preserved). When a label is set, it renders the line + text + line pattern.

**Step 2: Add labeled divider CSS**

Add to `components.css` after the existing `.wf-divider` rule:

```css
.wf-divider:has(.wf-divider__text) {
  display: flex;
  align-items: center;
  gap: var(--wf-space-sm);
  height: auto;
  background: none;
}

.wf-divider__line {
  flex: 1;
  height: 1px;
  background: var(--wf-color-border);
}

.wf-divider__text {
  font-size: var(--wf-text-xs);
  color: var(--wf-color-text-muted);
  white-space: nowrap;
}
```

**Step 3: Build**

Run: `cd ~/Work/WorkFort/scope/lead/web/packages/ui && pnpm build`

**Step 4: Commit**

```bash
cd ~/Work/WorkFort/scope/lead
git add web/packages/ui/src/components/divider.ts web/packages/ui/src/styles/components.css
git commit -m "feat: extend wf-divider with optional label attribute"
```

---

### Task 3: Refactor sharkfin messages to use `<wf-avatar>`

**Files:**
- Modify: `~/Work/WorkFort/sharkfin/lead/web/src/components/message.tsx`
- Modify: `~/Work/WorkFort/sharkfin/lead/web/src/styles/chat.css`

**Step 1: Replace avatar markup in message.tsx**

In `message.tsx`, the avatar section currently renders something like:

```tsx
<div class="sf-msg__avatar">{initials(props.from)}</div>
```

Replace with:

```tsx
<wf-avatar class="sf-msg__avatar" username={props.from} size="md" />
```

Remove the `initials` import if no longer used elsewhere in this file.

**Step 2: Simplify avatar CSS**

In `chat.css`, the `.sf-msg__avatar` rule currently sets width, height, border-radius, background, font-size, color, etc. These are now handled by `<wf-avatar>`. Reduce `.sf-msg__avatar` to only structural properties the component doesn't own:

```css
.sf-msg__avatar {
  margin-top: var(--wf-space-1);
}
```

**Step 3: Build and verify**

Run: `cd ~/Work/WorkFort/sharkfin/lead/web && pnpm build`

**Step 4: Commit**

```bash
cd ~/Work/WorkFort/sharkfin/lead
git add web/src/components/message.tsx web/src/styles/chat.css
git commit -m "refactor: use wf-avatar in message component"
```

---

### Task 4: Refactor sharkfin sidebar DM list to use `<wf-avatar>`

**Files:**
- Modify: `~/Work/WorkFort/sharkfin/lead/web/src/components/sidebar.tsx`
- Modify: `~/Work/WorkFort/sharkfin/lead/web/src/styles/chat.css`

**Step 1: Replace DM avatar markup in sidebar.tsx**

The DM list currently renders:

```tsx
<div class="sf-dm__avatar">
  {initials(other())}
  <wf-status-dot status={presenceStatus()} style="position:absolute;bottom:-1px;right:-1px;" />
</div>
```

Replace with:

```tsx
<wf-avatar class="sf-dm__avatar" username={other()} size="sm" status={presenceStatus()} />
```

Remove the `initials` import from sidebar.tsx if no longer used elsewhere in the file.

**Step 2: Simplify DM avatar CSS**

Reduce `.sf-dm__avatar` in chat.css to structural-only (the component handles visual styling):

```css
.sf-dm__avatar {
  flex-shrink: 0;
}
```

Remove the media query override for `.sf-dm__avatar` dimensions — the component handles sizing via the `size` attribute.

**Step 3: Build and verify**

Run: `cd ~/Work/WorkFort/sharkfin/lead/web && pnpm build`

**Step 4: Commit**

```bash
cd ~/Work/WorkFort/sharkfin/lead
git add web/src/components/sidebar.tsx web/src/styles/chat.css
git commit -m "refactor: use wf-avatar in sidebar DM list"
```

---

### Task 5: Refactor sharkfin date divider to use `<wf-divider label>`

**Files:**
- Modify: `~/Work/WorkFort/sharkfin/lead/web/src/components/message-area.tsx`
- Modify: `~/Work/WorkFort/sharkfin/lead/web/src/styles/chat.css`

**Step 1: Replace manual divider markup**

In message-area.tsx, the date divider currently renders:

```tsx
<div class="sf-divider">
  <div class="sf-divider__line" />
  <span class="sf-divider__text">{formatDateLabel(msg.sentAt)}</span>
  <div class="sf-divider__line" />
</div>
```

Replace with:

```tsx
<wf-divider label={formatDateLabel(msg.sentAt)} />
```

**Step 2: Remove orphaned CSS**

In chat.css, remove the `.sf-divider`, `.sf-divider__line`, `.sf-divider__text` rules — the `<wf-divider>` component handles all styling.

**Step 3: Build and verify**

Run: `cd ~/Work/WorkFort/sharkfin/lead/web && pnpm build`

**Step 4: Commit**

```bash
cd ~/Work/WorkFort/sharkfin/lead
git add web/src/components/message-area.tsx web/src/styles/chat.css
git commit -m "refactor: use wf-divider label for date separators"
```

---

### Task 6: Refactor wf-user-picker to use `<wf-avatar>`

**Files:**
- Modify: `~/Work/WorkFort/scope/lead/web/packages/ui/src/components/user-picker.ts`

**Step 1: Replace manual avatar construction**

In user-picker.ts, the `_renderOption` method manually builds an avatar div with initials + status dot (lines ~57-65). Replace with:

```typescript
const avatar = document.createElement('wf-avatar');
avatar.setAttribute('username', user.username);
avatar.setAttribute('size', 'sm');
if (user.online) {
  avatar.setAttribute('status', user.state === 'idle' ? 'away' : 'online');
} else {
  avatar.setAttribute('status', 'offline');
}
avatar.style.marginRight = 'var(--wf-space-sm)';
```

**Step 2: Build**

Run: `cd ~/Work/WorkFort/scope/lead/web/packages/ui && pnpm build`

**Step 3: Rebuild downstream**

Run: `cd ~/Work/WorkFort/scope/lead && mise run web:build`
Run: `cd ~/Work/WorkFort/sharkfin/lead/web && pnpm build`

**Step 4: Commit**

```bash
cd ~/Work/WorkFort/scope/lead
git add web/packages/ui/src/components/user-picker.ts
git commit -m "refactor: use wf-avatar in user-picker component"
```
