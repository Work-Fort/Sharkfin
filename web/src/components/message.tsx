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
