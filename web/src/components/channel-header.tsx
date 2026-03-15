import { Show } from 'solid-js';

interface ChannelHeaderProps {
  name: string;
  topic?: string;
}

export function ChannelHeader(props: ChannelHeaderProps) {
  return (
    <div class="sf-main__header">
      <span class="sf-main__channel-hash">#</span>
      <span class="sf-main__channel-name">{props.name}</span>
      <Show when={props.topic}>
        <span class="sf-main__topic">{props.topic}</span>
      </Show>
    </div>
  );
}
