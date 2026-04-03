# Token Cleanup + Hardcoded Style Fixes — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace all hardcoded visual values in the sharkfin UI with `--wf-*` design tokens, and swap raw `<input>` elements for `<wf-input>`.

**Architecture:** Add missing primitive tokens to `@workfort/ui`, then mechanically replace raw values in sharkfin CSS and inline styles. Replace raw HTML inputs with the existing `<wf-input>` web component. No behavioral changes — purely visual consistency.

**Tech Stack:** CSS custom properties, Lit web components, SolidJS JSX

---

## Context

The sharkfin UI audit found 13 hardcoded style violations across 4 files, plus 2 raw `<input>` elements that should use `<wf-input>`. Most violations are dimension values (`1.5rem`, `2rem`) that already have direct `--wf-space-*` token equivalents. Two sub-xs font sizes need a new `--wf-text-2xs` token.

---

### Task 1: Add missing primitive tokens

**Files:**
- Modify: `~/Work/WorkFort/scope/lead/web/packages/ui/src/styles/primitives.css`

**Step 1: Add `--wf-text-2xs` token**

In the `/* Text */` section, after `--wf-text-xs: 0.75rem;`, add:

```css
--wf-text-2xs: 0.625rem;
```

This covers the `0.625rem` and `0.6875rem` sizes found in chat.css. We'll use `--wf-text-2xs` for the smallest text (avatar initials) and `--wf-text-xs` for section labels/timestamps (bumping them slightly from 0.6875rem to 0.75rem for scale consistency).

**Step 2: Rebuild @workfort/ui**

Run: `cd ~/Work/WorkFort/scope/lead/web/packages/ui && pnpm build`
Expected: clean build

**Step 3: Commit**

```bash
cd ~/Work/WorkFort/scope/lead
git add web/packages/ui/src/styles/primitives.css
git commit -m "feat: add --wf-text-2xs token (0.625rem)"
```

---

### Task 2: Replace hardcoded dimensions in chat.css

**Files:**
- Modify: `~/Work/WorkFort/sharkfin/lead/web/src/styles/chat.css`

**Step 1: Replace all hardcoded dimension values**

Make these replacements (line numbers are approximate — match by selector):

| Selector | Property | Current | Replace with |
|----------|----------|---------|-------------|
| `.sf-channel__hash` | `width` | `1.25rem` | `var(--wf-space-5)` |
| `.sf-dm__avatar` | `width` | `1.5rem` | `var(--wf-space-6)` |
| `.sf-dm__avatar` | `height` | `1.5rem` | `var(--wf-space-6)` |
| `.sf-msg__avatar` | `width` | `2rem` | `var(--wf-space-8)` |
| `.sf-msg__avatar` | `height` | `2rem` | `var(--wf-space-8)` |
| `.sf-typing` | `height` | `1.25rem` | `var(--wf-space-5)` |
| `.sf-input__field` | `min-height` | `1.5rem` | `var(--wf-space-6)` |
| `@media` `.sf-dm__avatar` | `width` | `1.5rem` | `var(--wf-space-6)` |
| `@media` `.sf-dm__avatar` | `height` | `1.5rem` | `var(--wf-space-6)` |

**Step 2: Replace hardcoded font sizes**

| Selector | Current | Replace with |
|----------|---------|-------------|
| `.sf-section-label` | `0.6875rem` | `var(--wf-text-xs)` |
| `.sf-dm__avatar` | `0.625rem` | `var(--wf-text-2xs)` |
| `.sf-msg__time` | `0.6875rem` | `var(--wf-text-xs)` |
| `@media` `.sf-dm__avatar` | `0.5rem` | `var(--wf-text-2xs)` |

**Step 3: Replace hardcoded transitions**

| Selector | Current | Replace with |
|----------|---------|-------------|
| `.sf-channel` | `transition: background 0.1s` | `transition: background var(--wf-duration-fast) var(--wf-ease-in-out)` |
| `.sf-dm` | `transition: background 0.1s` | `transition: background var(--wf-duration-fast) var(--wf-ease-in-out)` |
| `.sf-input` | `transition: border-color 0.15s` | `transition: border-color var(--wf-duration-normal) var(--wf-ease-in-out)` |

**Step 4: Build and verify**

Run: `cd ~/Work/WorkFort/sharkfin/lead/web && pnpm build`
Expected: clean build, no errors

**Step 5: Commit**

```bash
cd ~/Work/WorkFort/sharkfin/lead
git add web/src/styles/chat.css
git commit -m "fix: replace hardcoded dimensions, font sizes, transitions with tokens"
```

---

### Task 3: Replace hardcoded inline styles in components

**Files:**
- Modify: `~/Work/WorkFort/sharkfin/lead/web/src/components/sidebar.tsx`
- Modify: `~/Work/WorkFort/sharkfin/lead/web/src/components/channel-header.tsx`

**Step 1: Fix sidebar.tsx button styles**

Line ~55 (new channel `+` button):
```
Current:  style="padding: 2px 6px; font-size: 14px;"
Replace:  style="padding: var(--wf-space-xs) var(--wf-space-sm); font-size: var(--wf-text-sm);"
```

Line ~101 (new DM `+` button):
```
Current:  style="padding: 1px 5px; font-size: 12px;"
Replace:  style="padding: 0 var(--wf-space-xs); font-size: var(--wf-text-xs);"
```

Line ~86 (non-member channel name): Replace `font-style: italic; opacity: 0.7;` with `font-style: italic; color: var(--wf-color-text-muted);`

**Step 2: Fix channel-header.tsx button style**

Line ~23 (Invite button):
```
Current:  style="padding: 2px 8px; font-size: var(--wf-text-xs);"
Replace:  style="padding: var(--wf-space-xs) var(--wf-space-sm); font-size: var(--wf-text-xs);"
```

**Step 3: Build and verify**

Run: `cd ~/Work/WorkFort/sharkfin/lead/web && pnpm build`

**Step 4: Commit**

```bash
git add web/src/components/sidebar.tsx web/src/components/channel-header.tsx
git commit -m "fix: replace hardcoded inline styles with tokens in sidebar and header"
```

---

### Task 4: Replace raw `<input>` with `<wf-input>` in create-channel-dialog

**Files:**
- Modify: `~/Work/WorkFort/sharkfin/lead/web/src/components/create-channel-dialog.tsx`

**Step 1: Read the current dialog code**

The dialog has a raw `<input type="text">` with a long inline style string for the channel name field.

**Step 2: Replace with `<wf-input>`**

Replace the raw `<input>` with `<wf-input>`. The `wf-input` component (actually `wf-text-input` or just the `text-input.ts` in @workfort/ui) accepts standard attributes. Check the component API by reading `~/Work/WorkFort/scope/lead/web/packages/ui/src/components/text-input.ts`.

The replacement should be:
```tsx
<wf-text-input
  placeholder="channel-name"
  value={name()}
  on:input={(e: Event) => setName((e.target as HTMLInputElement).value)}
/>
```

Remove the inline style string — `<wf-text-input>` handles its own styling via tokens.

**Step 3: Build and verify**

Run: `cd ~/Work/WorkFort/sharkfin/lead/web && pnpm build`

**Step 4: Commit**

```bash
git add web/src/components/create-channel-dialog.tsx
git commit -m "fix: replace raw input with wf-text-input in create channel dialog"
```

---

### Task 5: Replace raw `<input>` with `<wf-text-input>` in sidebar search

**Files:**
- Modify: `~/Work/WorkFort/sharkfin/lead/web/src/components/sidebar.tsx`
- Modify: `~/Work/WorkFort/sharkfin/lead/web/src/styles/chat.css`

**Step 1: Replace the search input**

In sidebar.tsx, the `.sf-sidebar__search` div contains a raw `<input>`. Replace with:

```tsx
<wf-text-input
  placeholder="Search conversations…"
  on:input={(e: Event) => setSearchTerm((e.target as HTMLInputElement).value)}
/>
```

**Step 2: Remove orphaned CSS**

In chat.css, remove the `.sf-sidebar__search input`, `.sf-sidebar__search input::placeholder`, and `.sf-sidebar__search input:focus` rules — they style the raw input that no longer exists. Keep the `.sf-sidebar__search` container rule for padding.

**Step 3: Build and verify**

Run: `cd ~/Work/WorkFort/sharkfin/lead/web && pnpm build`

**Step 4: Visual check**

Open the browser and verify the search input looks correct — proper border, focus ring, placeholder color.

**Step 5: Commit**

```bash
git add web/src/components/sidebar.tsx web/src/styles/chat.css
git commit -m "fix: replace raw search input with wf-text-input in sidebar"
```

---

### Task 6: Fix skeleton height in index.tsx

**Files:**
- Modify: `~/Work/WorkFort/sharkfin/lead/web/src/index.tsx`

**Step 1: Replace raw pixel skeleton height**

Find the `<wf-skeleton>` with `height="200px"` and change to `height="12rem"` (a rem-based value that communicates the same intent).

**Step 2: Build and verify**

Run: `cd ~/Work/WorkFort/sharkfin/lead/web && pnpm build`

**Step 3: Commit**

```bash
git add web/src/index.tsx
git commit -m "fix: use rem-based skeleton height instead of raw pixels"
```
