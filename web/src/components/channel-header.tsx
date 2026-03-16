import { Show } from 'solid-js';

interface ChannelHeaderProps {
  name: string;
  topic?: string;
  isPublic?: boolean;
  onInvite?: () => void;
  can?: (permission: string) => boolean;
}

export function ChannelHeader(props: ChannelHeaderProps) {
  return (
    <div class="sf-main__header">
      <span class="sf-main__channel-hash">#</span>
      <span class="sf-main__channel-name">{props.name}</span>
      <Show when={props.topic}>
        <span class="sf-main__topic">{props.topic}</span>
      </Show>
      <Show when={!props.isPublic && props.onInvite && (!props.can || props.can('invite_channel'))}>
        <wf-button style="padding: 2px 8px; font-size: var(--wf-text-xs);" on:click={() => props.onInvite!()}>
          Invite
        </wf-button>
      </Show>
    </div>
  );
}
