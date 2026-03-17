import { For, createEffect } from 'solid-js';
import type { User } from '@workfort/sharkfin-client';
import { initials } from '@workfort/ui';

interface DMDialogProps {
  users: User[];
  currentUsername: string;
  open: boolean;
  onSelect: (username: string) => void;
  onClose: () => void;
}

export function DMDialog(props: DMDialogProps) {
  let dialogRef!: HTMLElement & { show(): void; hide(): void };

  createEffect(() => {
    if (props.open) dialogRef?.show?.();
    else dialogRef?.hide?.();
  });

  const otherUsers = () => props.users.filter((u) => u.username !== props.currentUsername);

  return (
    <wf-dialog
      ref={dialogRef}
      header="New Direct Message"
      on:wf-close={props.onClose}
    >
      <wf-list>
        <For each={otherUsers()}>
          {(user) => (
            <wf-list-item on:wf-select={() => props.onSelect(user.username)}>
              <div class="sf-dm__avatar" style="margin-right: var(--wf-space-sm);">
                {initials(user.username)}
                <wf-status-dot
                  status={user.online ? (user.state === 'idle' ? 'away' : 'online') : 'offline'}
                  style="position:absolute;bottom:-1px;right:-1px;"
                />
              </div>
              <span>{user.username}</span>
            </wf-list-item>
          )}
        </For>
      </wf-list>
    </wf-dialog>
  );
}
