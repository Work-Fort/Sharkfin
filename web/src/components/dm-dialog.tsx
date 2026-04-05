// SPDX-License-Identifier: Apache-2.0
import type { User } from '@workfort/sharkfin-client';

interface DMDialogProps {
  users: User[];
  currentUsername: string;
  open: boolean;
  onSelect: (username: string) => void;
  onClose: () => void;
}

export function DMDialog(props: DMDialogProps) {
  return (
    <wf-user-picker
      header="New Direct Message"
      open={props.open}
      exclude={props.currentUsername}
      users={props.users}
      on:wf-select={(e: CustomEvent) => props.onSelect(e.detail.username)}
      on:wf-close={props.onClose}
    />
  );
}
