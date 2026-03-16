import { For, createEffect, on } from 'solid-js';
import type { Message as Msg } from '@workfort/sharkfin-client';
import { Message } from './message';

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

interface MessageAreaProps {
  messages: Msg[];
}

export function MessageArea(props: MessageAreaProps) {
  let messagesEl!: HTMLDivElement;

  // Auto-scroll when messages change.
  createEffect(on(
    () => props.messages.length,
    () => {
      if (messagesEl) {
        queueMicrotask(() => { messagesEl.scrollTop = messagesEl.scrollHeight; });
      }
    }
  ));

  return (
    <div class="sf-messages" ref={messagesEl}>
      <For each={props.messages}>
        {(msg, i) => {
          const prev = () => (i() > 0 ? props.messages[i() - 1] : undefined);
          const isContinuation = () => prev()?.from === msg.from && prev()?.sentAt != null && isSameDay(prev()!.sentAt, msg.sentAt);
          const showDivider = () => i() > 0 && prev()?.sentAt != null && !isSameDay(prev()!.sentAt, msg.sentAt);

          return (
            <>
              {showDivider() && (
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
    </div>
  );
}
