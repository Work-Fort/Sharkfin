// SPDX-License-Identifier: Apache-2.0
interface InputBarProps {
  channel: string;
  onSend: (body: string) => void;
}

export function InputBar(props: InputBarProps) {
  return (
    <div class="sf-input">
      <wf-compose-input
        placeholder={`Message #${props.channel}`}
        on:wf-send={(e: CustomEvent) => props.onSend(e.detail.body)}
      />
    </div>
  );
}
