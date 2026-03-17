# Sharkfin Web UI — Components & Integration (Plan 2 of 2)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build all SolidJS UI components matching the Storybook mockup at `~/Work/WorkFort/documentation/storybook/lit/stories/SharkfinChat.stories.ts`, wire them to the stores from Plan 1, and export the ServiceModule for the shell.

**Architecture:** Components use `@workfort/ui` web components (`wf-badge`, `wf-button`, `wf-status-dot`) and `--wf-*` design tokens from the shell. Sharkfin owns only structural layout via `sf-*` CSS classes — zero custom colors, typography values, or theming. The shell renders `SidebarContent` in its sidebar slot and the default export in the main content area.

**Tech Stack:** SolidJS 1.9, `@workfort/ui` web components, CSS with `--wf-*` tokens

**Prerequisite:** Plan 1 (foundation) must be complete — stores and client wrapper exist.

---

### Task 1: CSS Layout Stylesheet

**Files:**
- Create: `web/src/styles/chat.css`

**Step 1: Create the CSS file**

Port the structural CSS from the Storybook mockup. This is layout-only — every color, font, spacing, and radius value references a `--wf-*` token. The `sf-*` prefix is for Sharkfin-specific structural classes.

Create `web/src/styles/chat.css`:

```css
/* Sharkfin Chat — structural layout only. All visual properties use --wf-* tokens. */

/* Sidebar */
.sf-sidebar {
  background: var(--wf-color-bg-secondary);
  border-right: 1px solid var(--wf-color-border);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.sf-sidebar__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--wf-space-md) var(--wf-space-md) var(--wf-space-sm);
}
.sf-sidebar__title {
  font-size: var(--wf-text-sm);
  font-weight: var(--wf-weight-semibold);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--wf-color-text-secondary);
}
.sf-sidebar__search {
  padding: 0 var(--wf-space-sm) var(--wf-space-sm);
}
.sf-sidebar__search input {
  width: 100%;
  padding: var(--wf-space-xs) var(--wf-space-sm);
  border-radius: var(--wf-radius-sm);
  border: 1px solid var(--wf-color-border);
  background: var(--wf-color-bg);
  color: var(--wf-color-text);
  font-family: inherit;
  font-size: var(--wf-text-xs);
  outline: none;
  box-sizing: border-box;
}
.sf-sidebar__search input::placeholder { color: var(--wf-color-text-muted); }
.sf-sidebar__search input:focus { border-color: var(--wf-color-border-strong); }

.sf-section-label {
  padding: var(--wf-space-sm) var(--wf-space-md) var(--wf-space-xs);
  font-size: 0.6875rem;
  font-weight: var(--wf-weight-semibold);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--wf-color-text-muted);
}

/* Channel list items */
.sf-channels {
  flex: 1;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: var(--wf-color-border) transparent;
}
.sf-channel {
  display: flex;
  align-items: center;
  gap: var(--wf-space-sm);
  padding: var(--wf-space-xs) var(--wf-space-md);
  cursor: pointer;
  font-size: var(--wf-text-sm);
  color: var(--wf-color-text-secondary);
  transition: background 0.1s;
}
.sf-channel:hover { background: var(--wf-color-bg-elevated); }
.sf-channel--active { background: var(--wf-color-bg-elevated); color: var(--wf-color-text); }
.sf-channel__hash {
  font-size: var(--wf-text-md);
  font-weight: var(--wf-weight-medium);
  color: var(--wf-color-text-muted);
  width: 1.25rem;
  text-align: center;
  flex-shrink: 0;
}
.sf-channel--active .sf-channel__hash { color: var(--wf-color-text-secondary); }
.sf-channel__name { flex: 1; }

/* DM list items */
.sf-dm {
  display: flex;
  align-items: center;
  gap: var(--wf-space-sm);
  padding: var(--wf-space-xs) var(--wf-space-md);
  cursor: pointer;
  font-size: var(--wf-text-sm);
  color: var(--wf-color-text-secondary);
  transition: background 0.1s;
}
.sf-dm:hover { background: var(--wf-color-bg-elevated); }
.sf-dm__avatar {
  width: 1.5rem;
  height: 1.5rem;
  border-radius: var(--wf-radius-full);
  background: var(--wf-color-bg-elevated);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 0.625rem;
  font-weight: var(--wf-weight-semibold);
  color: var(--wf-color-text-secondary);
  flex-shrink: 0;
  position: relative;
}

/* Main content area */
.sf-main {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.sf-main__header {
  display: flex;
  align-items: center;
  gap: var(--wf-space-sm);
  padding: var(--wf-space-sm) var(--wf-space-lg);
  border-bottom: 1px solid var(--wf-color-border);
  flex-shrink: 0;
}
.sf-main__channel-name {
  font-size: var(--wf-text-base);
  font-weight: var(--wf-weight-semibold);
  color: var(--wf-color-text);
}
.sf-main__channel-hash { color: var(--wf-color-text-muted); }
.sf-main__topic {
  font-size: var(--wf-text-xs);
  color: var(--wf-color-text-muted);
  flex: 1;
}

/* Messages */
.sf-messages {
  flex: 1;
  overflow-y: auto;
  padding: var(--wf-space-md) var(--wf-space-lg);
  display: flex;
  flex-direction: column;
  gap: 2px;
  scrollbar-width: thin;
  scrollbar-color: var(--wf-color-border) transparent;
}
.sf-msg {
  display: flex;
  gap: var(--wf-space-sm);
  padding: var(--wf-space-xs) 0;
}
.sf-msg + .sf-msg:not(.sf-msg--cont) { margin-top: var(--wf-space-xs); }
.sf-msg__avatar {
  width: 2rem;
  height: 2rem;
  border-radius: var(--wf-radius-md);
  background: var(--wf-color-bg-elevated);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: var(--wf-text-xs);
  font-weight: var(--wf-weight-semibold);
  color: var(--wf-color-text-secondary);
  flex-shrink: 0;
  margin-top: 2px;
}
.sf-msg__avatar--hidden { visibility: hidden; }
.sf-msg__body { flex: 1; min-width: 0; }
.sf-msg__header {
  display: flex;
  align-items: baseline;
  gap: var(--wf-space-sm);
  margin-bottom: 2px;
}
.sf-msg__author {
  font-size: var(--wf-text-sm);
  font-weight: var(--wf-weight-semibold);
  color: var(--wf-color-text);
}
.sf-msg__time {
  font-size: 0.6875rem;
  color: var(--wf-color-text-muted);
  font-family: var(--wf-font-mono);
}
.sf-msg__text {
  font-size: var(--wf-text-sm);
  line-height: var(--wf-leading-normal);
  color: var(--wf-color-text);
  word-wrap: break-word;
}

/* Date divider */
.sf-divider {
  display: flex;
  align-items: center;
  gap: var(--wf-space-md);
  padding: var(--wf-space-md) 0 var(--wf-space-sm);
}
.sf-divider__line { flex: 1; height: 1px; background: var(--wf-color-border); }
.sf-divider__text {
  font-size: var(--wf-text-xs);
  font-weight: var(--wf-weight-medium);
  color: var(--wf-color-text-muted);
}

/* Typing indicator */
.sf-typing {
  padding: var(--wf-space-xs) var(--wf-space-lg);
  font-size: var(--wf-text-xs);
  color: var(--wf-color-text-muted);
  height: 1.25rem;
}

/* Input bar */
.sf-input {
  padding: 0 var(--wf-space-lg) var(--wf-space-md);
}
.sf-input__box {
  display: flex;
  align-items: flex-end;
  gap: var(--wf-space-sm);
  border: 1px solid var(--wf-color-border-strong);
  border-radius: var(--wf-radius-lg);
  padding: var(--wf-space-sm);
  background: var(--wf-color-bg-secondary);
  transition: border-color 0.15s;
}
.sf-input__box:focus-within { border-color: var(--wf-color-text-secondary); }
.sf-input__field {
  flex: 1;
  border: none;
  background: transparent;
  color: var(--wf-color-text);
  font-family: inherit;
  font-size: var(--wf-text-sm);
  line-height: var(--wf-leading-normal);
  resize: none;
  outline: none;
  min-height: 1.5rem;
}
.sf-input__field::placeholder { color: var(--wf-color-text-muted); }
```

**Step 2: Commit**

```bash
git add web/src/styles/chat.css
git commit -m "feat: add chat layout CSS with wf-* design tokens"
```

---

### Task 2: Message Component

**Files:**
- Create: `web/src/components/message.tsx`
- Create: `web/test/components/message.test.tsx`

**Step 1: Write failing test**

Create `web/test/components/message.test.tsx`:

```tsx
import { describe, it, expect } from 'vitest';
import { render } from 'solid-js/web';
import { Message } from '../../src/components/message';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('Message', () => {
  it('renders author, time, and body', () => {
    const el = renderInto(() => (
      <Message from="alice" body="hello world" sentAt="2026-03-15T09:14:00Z" />
    ));
    expect(el.querySelector('.sf-msg__author')?.textContent).toBe('alice');
    expect(el.querySelector('.sf-msg__text')?.textContent).toBe('hello world');
    expect(el.querySelector('.sf-msg__time')?.textContent).toBeTruthy();
  });

  it('renders avatar initials from username', () => {
    const el = renderInto(() => (
      <Message from="alice-chen" body="hi" sentAt="2026-03-15T09:14:00Z" />
    ));
    expect(el.querySelector('.sf-msg__avatar')?.textContent?.trim()).toBe('AC');
  });

  it('hides avatar and header for continuation messages', () => {
    const el = renderInto(() => (
      <Message from="alice" body="continued" sentAt="2026-03-15T09:14:00Z" continuation />
    ));
    expect(el.querySelector('.sf-msg--cont')).toBeTruthy();
    expect(el.querySelector('.sf-msg__avatar--hidden')).toBeTruthy();
    expect(el.querySelector('.sf-msg__header')).toBeFalsy();
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/components/message.test.tsx`
Expected: FAIL — module not found.

**Step 3: Implement Message component**

Create `web/src/components/message.tsx`:

```tsx
import { Show } from 'solid-js';

/** Extract initials from a username like "alice-chen" → "AC" or "bob" → "BO". */
function initials(username: string): string {
  const parts = username.split(/[-_.\s]+/);
  if (parts.length >= 2) {
    return (parts[0][0] + parts[1][0]).toUpperCase();
  }
  return username.slice(0, 2).toUpperCase();
}

/** Format ISO timestamp to HH:MM. */
function formatTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
}

interface MessageProps {
  from: string;
  body: string;
  sentAt: string;
  continuation?: boolean;
}

export function Message(props: MessageProps) {
  return (
    <div class={`sf-msg${props.continuation ? ' sf-msg--cont' : ''}`}>
      <div class={`sf-msg__avatar${props.continuation ? ' sf-msg__avatar--hidden' : ''}`}>
        {initials(props.from)}
      </div>
      <div class="sf-msg__body">
        <Show when={!props.continuation}>
          <div class="sf-msg__header">
            <span class="sf-msg__author">{props.from}</span>
            <span class="sf-msg__time">{formatTime(props.sentAt)}</span>
          </div>
        </Show>
        <div class="sf-msg__text">{props.body}</div>
      </div>
    </div>
  );
}
```

**Step 4: Run test to verify it passes**

Run: `cd web && pnpm test -- test/components/message.test.tsx`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/message.tsx web/test/components/message.test.tsx
git commit -m "feat: add Message component"
```

---

### Task 3: Message Area Component

**Files:**
- Create: `web/src/components/message-area.tsx`
- Create: `web/test/components/message-area.test.tsx`

**Step 1: Write failing test**

Create `web/test/components/message-area.test.tsx`:

```tsx
import { describe, it, expect } from 'vitest';
import { render } from 'solid-js/web';
import { MessageArea } from '../../src/components/message-area';
import type { Message as Msg } from '@workfort/sharkfin-client';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('MessageArea', () => {
  it('renders messages', () => {
    const msgs: Msg[] = [
      { id: 1, from: 'alice', body: 'hello', sentAt: '2026-03-15T09:00:00Z' },
      { id: 2, from: 'bob', body: 'hi', sentAt: '2026-03-15T09:01:00Z' },
    ];
    const el = renderInto(() => <MessageArea messages={msgs} />);
    const msgEls = el.querySelectorAll('.sf-msg');
    expect(msgEls.length).toBe(2);
  });

  it('groups consecutive messages from same author as continuations', () => {
    const msgs: Msg[] = [
      { id: 1, from: 'alice', body: 'first', sentAt: '2026-03-15T09:00:00Z' },
      { id: 2, from: 'alice', body: 'second', sentAt: '2026-03-15T09:00:30Z' },
    ];
    const el = renderInto(() => <MessageArea messages={msgs} />);
    const contMsgs = el.querySelectorAll('.sf-msg--cont');
    expect(contMsgs.length).toBe(1);
  });

  it('does not group messages from different authors', () => {
    const msgs: Msg[] = [
      { id: 1, from: 'alice', body: 'hi', sentAt: '2026-03-15T09:00:00Z' },
      { id: 2, from: 'bob', body: 'hey', sentAt: '2026-03-15T09:01:00Z' },
    ];
    const el = renderInto(() => <MessageArea messages={msgs} />);
    const contMsgs = el.querySelectorAll('.sf-msg--cont');
    expect(contMsgs.length).toBe(0);
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/components/message-area.test.tsx`
Expected: FAIL — module not found.

**Step 3: Implement MessageArea component**

Create `web/src/components/message-area.tsx`:

```tsx
import { For } from 'solid-js';
import type { Message as Msg } from '@workfort/sharkfin-client';
import { Message } from './message';

interface MessageAreaProps {
  messages: Msg[];
}

export function MessageArea(props: MessageAreaProps) {
  let messagesEl!: HTMLDivElement;

  // Auto-scroll to bottom when messages change.
  const scrollToBottom = () => {
    if (messagesEl) {
      messagesEl.scrollTop = messagesEl.scrollHeight;
    }
  };

  return (
    <div class="sf-messages" ref={messagesEl}>
      <For each={props.messages}>
        {(msg, i) => {
          const prev = () => (i() > 0 ? props.messages[i() - 1] : undefined);
          const isContinuation = () => prev()?.from === msg.from;

          // Scroll after each render.
          queueMicrotask(scrollToBottom);

          return (
            <Message
              from={msg.from}
              body={msg.body}
              sentAt={msg.sentAt}
              continuation={isContinuation()}
            />
          );
        }}
      </For>
    </div>
  );
}
```

**Step 4: Run test to verify it passes**

Run: `cd web && pnpm test -- test/components/message-area.test.tsx`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/message-area.tsx web/test/components/message-area.test.tsx
git commit -m "feat: add MessageArea component with grouping and auto-scroll"
```

---

### Task 4: Channel Header Component

**Files:**
- Create: `web/src/components/channel-header.tsx`
- Create: `web/test/components/channel-header.test.tsx`

**Step 1: Write failing test**

Create `web/test/components/channel-header.test.tsx`:

```tsx
import { describe, it, expect } from 'vitest';
import { render } from 'solid-js/web';
import { ChannelHeader } from '../../src/components/channel-header';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('ChannelHeader', () => {
  it('renders channel name with hash', () => {
    const el = renderInto(() => <ChannelHeader name="general" />);
    expect(el.querySelector('.sf-main__channel-hash')?.textContent).toBe('#');
    expect(el.querySelector('.sf-main__channel-name')?.textContent).toBe('general');
  });

  it('renders topic when provided', () => {
    const el = renderInto(() => <ChannelHeader name="general" topic="Team updates" />);
    expect(el.querySelector('.sf-main__topic')?.textContent).toBe('Team updates');
  });

  it('renders without topic', () => {
    const el = renderInto(() => <ChannelHeader name="random" />);
    expect(el.querySelector('.sf-main__topic')).toBeFalsy();
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/components/channel-header.test.tsx`
Expected: FAIL.

**Step 3: Implement ChannelHeader**

Create `web/src/components/channel-header.tsx`:

```tsx
import { Show } from 'solid-js';

interface ChannelHeaderProps {
  name: string;
  topic?: string;
}

export function ChannelHeader(props: ChannelHeaderProps) {
  return (
    <div class="sf-main__header">
      <span class="sf-main__channel-hash">#</span>
      <span class="sf-main__channel-name">{props.name}</span>
      <Show when={props.topic}>
        <span class="sf-main__topic">{props.topic}</span>
      </Show>
    </div>
  );
}
```

**Step 4: Run test to verify it passes**

Run: `cd web && pnpm test -- test/components/channel-header.test.tsx`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/channel-header.tsx web/test/components/channel-header.test.tsx
git commit -m "feat: add ChannelHeader component"
```

---

### Task 5: Input Bar Component

**Files:**
- Create: `web/src/components/input-bar.tsx`
- Create: `web/test/components/input-bar.test.tsx`

**Step 1: Write failing test**

Create `web/test/components/input-bar.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render } from 'solid-js/web';
import { InputBar } from '../../src/components/input-bar';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('InputBar', () => {
  it('renders textarea with placeholder', () => {
    const el = renderInto(() => <InputBar channel="general" onSend={() => {}} />);
    const textarea = el.querySelector('textarea');
    expect(textarea).toBeTruthy();
    expect(textarea?.placeholder).toBe('Message #general');
  });

  it('calls onSend and clears input on Enter', () => {
    const onSend = vi.fn();
    const el = renderInto(() => <InputBar channel="general" onSend={onSend} />);
    const textarea = el.querySelector('textarea')!;

    // Simulate typing
    textarea.value = 'hello world';
    textarea.dispatchEvent(new Event('input', { bubbles: true }));

    // Simulate Enter key
    textarea.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));

    expect(onSend).toHaveBeenCalledWith('hello world');
    expect(textarea.value).toBe('');
  });

  it('does not send on Shift+Enter', () => {
    const onSend = vi.fn();
    const el = renderInto(() => <InputBar channel="general" onSend={onSend} />);
    const textarea = el.querySelector('textarea')!;

    textarea.value = 'hello';
    textarea.dispatchEvent(new Event('input', { bubbles: true }));
    textarea.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', shiftKey: true, bubbles: true }));

    expect(onSend).not.toHaveBeenCalled();
  });

  it('does not send empty messages', () => {
    const onSend = vi.fn();
    const el = renderInto(() => <InputBar channel="general" onSend={onSend} />);
    const textarea = el.querySelector('textarea')!;

    textarea.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));

    expect(onSend).not.toHaveBeenCalled();
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/components/input-bar.test.tsx`
Expected: FAIL.

**Step 3: Implement InputBar**

Create `web/src/components/input-bar.tsx`:

```tsx
import { createSignal } from 'solid-js';

interface InputBarProps {
  channel: string;
  onSend: (body: string) => void;
}

export function InputBar(props: InputBarProps) {
  const [text, setText] = createSignal('');

  function handleKeyDown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      const body = text().trim();
      if (!body) return;
      props.onSend(body);
      setText('');
      // Clear the textarea element directly.
      const textarea = e.target as HTMLTextAreaElement;
      textarea.value = '';
    }
  }

  return (
    <div class="sf-input">
      <div class="sf-input__box">
        <textarea
          class="sf-input__field"
          placeholder={`Message #${props.channel}`}
          rows={1}
          onInput={(e) => setText(e.currentTarget.value)}
          onKeyDown={handleKeyDown}
        />
        <wf-button
          style="padding: 4px 10px;"
          title="Send"
          onClick={() => {
            const body = text().trim();
            if (!body) return;
            props.onSend(body);
            setText('');
          }}
        >
          ↑
        </wf-button>
      </div>
    </div>
  );
}
```

**Step 4: Run test to verify it passes**

Run: `cd web && pnpm test -- test/components/input-bar.test.tsx`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/input-bar.tsx web/test/components/input-bar.test.tsx
git commit -m "feat: add InputBar component with Enter-to-send"
```

---

### Task 6: Typing Indicator Component

**Files:**
- Create: `web/src/components/typing-indicator.tsx`
- Create: `web/test/components/typing-indicator.test.tsx`

**Step 1: Write failing test**

Create `web/test/components/typing-indicator.test.tsx`:

```tsx
import { describe, it, expect } from 'vitest';
import { render } from 'solid-js/web';
import { TypingIndicator } from '../../src/components/typing-indicator';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('TypingIndicator', () => {
  it('renders empty when no one is typing', () => {
    const el = renderInto(() => <TypingIndicator typingUsers={[]} />);
    const indicator = el.querySelector('.sf-typing');
    expect(indicator?.textContent?.trim()).toBe('');
  });

  it('shows single user typing', () => {
    const el = renderInto(() => <TypingIndicator typingUsers={['alice']} />);
    expect(el.querySelector('.sf-typing')?.textContent).toContain('alice is typing');
  });

  it('shows multiple users typing', () => {
    const el = renderInto(() => <TypingIndicator typingUsers={['alice', 'bob']} />);
    expect(el.querySelector('.sf-typing')?.textContent).toContain('alice and bob are typing');
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/components/typing-indicator.test.tsx`
Expected: FAIL.

**Step 3: Implement TypingIndicator**

Create `web/src/components/typing-indicator.tsx`:

```tsx
interface TypingIndicatorProps {
  typingUsers: string[];
}

export function TypingIndicator(props: TypingIndicatorProps) {
  const text = () => {
    const users = props.typingUsers;
    if (users.length === 0) return '';
    if (users.length === 1) return `${users[0]} is typing\u2026`;
    return `${users.join(' and ')} are typing\u2026`;
  };

  return <div class="sf-typing">{text()}</div>;
}
```

**Step 4: Run test to verify it passes**

Run: `cd web && pnpm test -- test/components/typing-indicator.test.tsx`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/typing-indicator.tsx web/test/components/typing-indicator.test.tsx
git commit -m "feat: add TypingIndicator component"
```

---

### Task 7: Sidebar Component

**Files:**
- Create: `web/src/components/sidebar.tsx`
- Create: `web/test/components/sidebar.test.tsx`

**Step 1: Write failing test**

Create `web/test/components/sidebar.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render } from 'solid-js/web';
import { ChannelSidebar } from '../../src/components/sidebar';
import type { Channel, DM, UnreadCount, User } from '@workfort/sharkfin-client';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('ChannelSidebar', () => {
  const channels: Channel[] = [
    { name: 'general', public: true, member: true },
    { name: 'random', public: true, member: true },
  ];
  const dms: DM[] = [
    { channel: 'dm-1', participants: ['alice-chen', 'me'] },
  ];
  const unreads: UnreadCount[] = [
    { channel: 'general', type: 'public', unreadCount: 3, mentionCount: 1 },
    { channel: 'random', type: 'public', unreadCount: 0, mentionCount: 0 },
  ];
  const users: User[] = [
    { username: 'alice-chen', online: true, type: 'user' },
  ];

  it('renders channel names', () => {
    const el = renderInto(() => (
      <ChannelSidebar
        channels={channels} dms={dms} unreads={unreads} users={users}
        activeChannel="general" onSelectChannel={() => {}}
      />
    ));
    const names = el.querySelectorAll('.sf-channel__name');
    expect(names.length).toBe(2);
    expect(names[0].textContent).toBe('general');
    expect(names[1].textContent).toBe('random');
  });

  it('marks active channel', () => {
    const el = renderInto(() => (
      <ChannelSidebar
        channels={channels} dms={dms} unreads={unreads} users={users}
        activeChannel="general" onSelectChannel={() => {}}
      />
    ));
    const active = el.querySelector('.sf-channel--active .sf-channel__name');
    expect(active?.textContent).toBe('general');
  });

  it('shows unread badge on channel', () => {
    const el = renderInto(() => (
      <ChannelSidebar
        channels={channels} dms={dms} unreads={unreads} users={users}
        activeChannel="random" onSelectChannel={() => {}}
      />
    ));
    const badges = el.querySelectorAll('wf-badge');
    expect(badges.length).toBeGreaterThan(0);
  });

  it('calls onSelectChannel when clicked', () => {
    const onSelect = vi.fn();
    const el = renderInto(() => (
      <ChannelSidebar
        channels={channels} dms={dms} unreads={unreads} users={users}
        activeChannel="general" onSelectChannel={onSelect}
      />
    ));
    const randomCh = el.querySelectorAll('.sf-channel')[1] as HTMLElement;
    randomCh.click();
    expect(onSelect).toHaveBeenCalledWith('random');
  });

  it('renders DM participants', () => {
    const el = renderInto(() => (
      <ChannelSidebar
        channels={channels} dms={dms} unreads={unreads} users={users}
        activeChannel="general" onSelectChannel={() => {}}
      />
    ));
    const dmEls = el.querySelectorAll('.sf-dm');
    expect(dmEls.length).toBe(1);
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/components/sidebar.test.tsx`
Expected: FAIL.

**Step 3: Implement Sidebar**

Create `web/src/components/sidebar.tsx`:

```tsx
import { For, Show } from 'solid-js';
import type { Channel, DM, UnreadCount, User } from '@workfort/sharkfin-client';

/** Extract initials from a username. */
function initials(username: string): string {
  const parts = username.split(/[-_.\s]+/);
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
  return username.slice(0, 2).toUpperCase();
}

interface SidebarProps {
  channels: Channel[];
  dms: DM[];
  unreads: UnreadCount[];
  users: User[];
  activeChannel: string;
  onSelectChannel: (channel: string) => void;
}

export function ChannelSidebar(props: SidebarProps) {
  const unreadFor = (channel: string) =>
    props.unreads.find((u) => u.channel === channel);

  const userStatus = (username: string) =>
    props.users.find((u) => u.username === username);

  return (
    <div class="sf-sidebar">
      <div class="sf-sidebar__header">
        <span class="sf-sidebar__title">Sharkfin</span>
        <wf-button style="padding: 2px 6px; font-size: 14px;" title="New channel">
          +
        </wf-button>
      </div>
      <div class="sf-sidebar__search">
        <input type="text" placeholder="Search conversations\u2026" />
      </div>
      <div class="sf-channels">
        <div class="sf-section-label">Channels</div>
        <For each={props.channels}>
          {(ch) => {
            const count = () => unreadFor(ch.name)?.unreadCount ?? 0;
            return (
              <div
                class={`sf-channel${ch.name === props.activeChannel ? ' sf-channel--active' : ''}`}
                onClick={() => props.onSelectChannel(ch.name)}
              >
                <span class="sf-channel__hash">#</span>
                <span class="sf-channel__name">{ch.name}</span>
                <Show when={count() > 0}>
                  <wf-badge count={count()} size="sm" />
                </Show>
              </div>
            );
          }}
        </For>

        <div class="sf-section-label">Direct Messages</div>
        <For each={props.dms}>
          {(dm) => {
            const other = () => dm.participants.find((p) => p !== 'me') ?? dm.participants[0];
            const status = () => userStatus(other());
            const presenceStatus = () => (status()?.online ? 'online' : 'offline');
            return (
              <div class="sf-dm" onClick={() => props.onSelectChannel(dm.channel)}>
                <div class="sf-dm__avatar">
                  {initials(other())}
                  <wf-status-dot
                    status={presenceStatus()}
                    style="position:absolute;bottom:-1px;right:-1px;"
                  />
                </div>
                <span>{other()}</span>
              </div>
            );
          }}
        </For>
      </div>
    </div>
  );
}
```

**Step 4: Run test to verify it passes**

Run: `cd web && pnpm test -- test/components/sidebar.test.tsx`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/sidebar.tsx web/test/components/sidebar.test.tsx
git commit -m "feat: add ChannelSidebar component with channels, DMs, and unread badges"
```

---

### Task 8: Chat Container + ServiceModule Exports

**Files:**
- Create: `web/src/components/chat.tsx`
- Modify: `web/src/index.tsx` (replace placeholder with real exports)
- Create: `web/test/components/chat.test.tsx`

**Step 1: Write failing test**

Create `web/test/components/chat.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render } from 'solid-js/web';

// Mock the client module so we don't need a real WebSocket.
vi.mock('../../src/client', () => {
  const { createMockClient } = require('../helpers');
  const mock = createMockClient();
  mock.channels.mockResolvedValue([{ name: 'general', public: true, member: true }]);
  mock.users.mockResolvedValue([]);
  mock.dmList.mockResolvedValue([]);
  mock.unreadCounts.mockResolvedValue([]);
  mock.history.mockResolvedValue([]);
  return { getClient: () => mock, resetClient: vi.fn() };
});

import { SharkfinChat } from '../../src/components/chat';
import { flushPromises } from '../helpers';

function renderInto(component: () => any) {
  const container = document.createElement('div');
  render(component, container);
  return container;
}

describe('SharkfinChat', () => {
  it('renders main layout structure', async () => {
    const el = renderInto(() => <SharkfinChat connected={true} />);
    await flushPromises();
    expect(el.querySelector('.sf-main')).toBeTruthy();
    expect(el.querySelector('.sf-main__header')).toBeTruthy();
    expect(el.querySelector('.sf-messages')).toBeTruthy();
    expect(el.querySelector('.sf-input')).toBeTruthy();
  });
});
```

**Step 2: Run test to verify it fails**

Run: `cd web && pnpm test -- test/components/chat.test.tsx`
Expected: FAIL.

**Step 3: Implement Chat container**

Create `web/src/components/chat.tsx`:

```tsx
import { Show, onCleanup } from 'solid-js';
import '../styles/chat.css';
import { getClient, resetClient } from '../client';
import { createChannelStore } from '../stores/channels';
import { createMessageStore } from '../stores/messages';
import { createUserStore } from '../stores/users';
import { createUnreadStore } from '../stores/unread';
import { ChannelHeader } from './channel-header';
import { MessageArea } from './message-area';
import { TypingIndicator } from './typing-indicator';
import { InputBar } from './input-bar';

interface SharkfinChatProps {
  connected: boolean;
}

export function SharkfinChat(props: SharkfinChatProps) {
  const client = getClient();
  const channelStore = createChannelStore(client);
  const messageStore = createMessageStore(client, channelStore.activeChannel);
  const userStore = createUserStore(client);
  const unreadStore = createUnreadStore(client, channelStore.activeChannel);

  // Auto-select first channel once loaded.
  const checkFirstChannel = () => {
    const chs = channelStore.channels();
    if (chs.length > 0 && !channelStore.activeChannel()) {
      channelStore.setActiveChannel(chs[0].name);
    }
  };
  // Poll briefly for initial load — channels() is async.
  const timer = setInterval(checkFirstChannel, 50);
  setTimeout(() => clearInterval(timer), 2000);
  onCleanup(() => clearInterval(timer));

  return (
    <div class="sf-main">
      <Show when={props.connected} fallback={
        <wf-banner variant="warning" headline="Chat is reconnecting\u2026" />
      }>
        <ChannelHeader name={channelStore.activeChannel()} />
        <MessageArea messages={messageStore.messages()} />
        <TypingIndicator typingUsers={[]} />
        <InputBar
          channel={channelStore.activeChannel()}
          onSend={(body) => messageStore.sendMessage(body)}
        />
      </Show>
    </div>
  );
}
```

**Step 4: Update index.tsx with real ServiceModule exports**

Replace `web/src/index.tsx` with:

```tsx
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

Note: The `SidebarContent` export requires the sidebar to receive props from the shell. Since the shell renders it independently, `SidebarContent` needs to be a self-contained component that gets its data from the same client singleton. Modify the export to be a wrapper:

```tsx
import { SharkfinChat } from './components/chat';
import { ChannelSidebar } from './components/sidebar';
import { getClient } from './client';
import { createChannelStore } from './stores/channels';
import { createUserStore } from './stores/users';
import { createUnreadStore } from './stores/unread';

export default function SharkfinApp(props: { connected: boolean }) {
  return <SharkfinChat connected={props.connected} />;
}

export const manifest = {
  name: 'sharkfin',
  label: 'Chat',
  route: '/chat',
};

export function SidebarContent() {
  const client = getClient();
  const channelStore = createChannelStore(client);
  const userStore = createUserStore(client);
  const unreadStore = createUnreadStore(client, channelStore.activeChannel);

  return (
    <ChannelSidebar
      channels={channelStore.channels()}
      dms={channelStore.dms()}
      unreads={unreadStore.unreads()}
      users={userStore.users()}
      activeChannel={channelStore.activeChannel()}
      onSelectChannel={channelStore.setActiveChannel}
    />
  );
}
```

**Step 5: Run test to verify it passes**

Run: `cd web && pnpm test -- test/components/chat.test.tsx`
Expected: PASS.

**Step 6: Run all tests**

Run: `cd web && pnpm test`
Expected: All tests pass.

**Step 7: Verify build**

Run: `cd web && pnpm build`
Expected: Build succeeds, `dist/remoteEntry.js` created.

**Step 8: Commit**

```bash
git add web/src/components/chat.tsx web/src/index.tsx web/test/components/chat.test.tsx
git commit -m "feat: add SharkfinChat container and ServiceModule exports"
```

---

### Task 9: Final Integration Verification

**Step 1: Run all web tests**

Run: `cd web && pnpm test`
Expected: All tests pass.

**Step 2: Run Go tests**

Run: `mise run test`
Expected: All Go tests pass (including new UI handler tests).

**Step 3: Build everything**

Run: `cd web && pnpm build`
Expected: Build succeeds with `remoteEntry.js` in `dist/`.

**Step 4: Verify ServiceModule contract**

Check that `dist/remoteEntry.js` is generated and the exports match the shell's `ServiceModule` interface:
- `default`: function component accepting `{ connected: boolean }`
- `manifest`: `{ name: 'sharkfin', label: 'Chat', route: '/chat' }`
- `SidebarContent`: function component (optional, present)

**Step 5: Final commit**

If any adjustments were needed:
```bash
git add -A web/
git commit -m "fix: integration adjustments for web UI"
```
