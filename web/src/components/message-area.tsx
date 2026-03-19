import { For, createEffect, on } from 'solid-js';
import type { Message as Msg } from '@workfort/sharkfin-client';
import { formatDateLabel, isSameDay } from '@workfort/ui';
import { Message } from './message';

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
                <wf-divider label={formatDateLabel(msg.sentAt)} />
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
