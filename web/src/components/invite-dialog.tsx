import { For, createEffect } from 'solid-js';
import type { User } from '@workfort/sharkfin-client';
import { initials } from '@workfort/ui';

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
    if (props.open) dialogRef?.show?.();
    else dialogRef?.hide?.();
  });

  return (
    <wf-dialog ref={dialogRef} header={`Invite to #${props.channel}`} on:wf-close={props.onClose}>
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
