import { Show, createMemo } from 'solid-js';

interface ChannelHeaderProps {
  name: string;
  topic?: string;
  isPublic?: boolean;
  onInvite?: () => void;
  can?: (permission: string) => boolean;
}

export function ChannelHeader(props: ChannelHeaderProps) {
  const canInvite = createMemo(() => !props.isPublic && !!props.onInvite && (!props.can || props.can('invite_channel')));

  return (
    <>
      <div class="sf-main__header" style="border-bottom: none;">
        <span class="sf-main__channel-hash">#</span>
        <span class="sf-main__channel-name">{props.name}</span>
        <Show when={props.topic}>
          <span class="sf-main__topic">{props.topic}</span>
        </Show>
        <Show when={canInvite()}>
          <wf-button style="padding: var(--wf-space-xs) var(--wf-space-sm); font-size: var(--wf-text-xs);" on:click={() => props.onInvite!()}>
            Invite
          </wf-button>
        </Show>
      </div>
      <wf-divider />
    </>
  );
}
