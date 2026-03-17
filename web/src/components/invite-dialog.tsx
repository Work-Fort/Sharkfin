import type { User } from '@workfort/sharkfin-client';

interface InviteDialogProps {
  channel: string;
  users: User[];
  open: boolean;
  onInvite: (channel: string, username: string) => void;
  onClose: () => void;
}

export function InviteDialog(props: InviteDialogProps) {
  return (
    <wf-user-picker
      header={`Invite to #${props.channel}`}
      open={props.open}
      users={props.users}
      on:wf-select={(e: CustomEvent) => props.onInvite(props.channel, e.detail.username)}
      on:wf-close={props.onClose}
    />
  );
}
