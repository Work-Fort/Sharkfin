import { Show } from 'solid-js';
import { initials, formatTime } from '@workfort/ui';

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
