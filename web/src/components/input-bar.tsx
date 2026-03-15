import { createSignal } from 'solid-js';

interface InputBarProps {
  channel: string;
  onSend: (body: string) => void;
}

export function InputBar(props: InputBarProps) {
  const [text, setText] = createSignal('');

  function handleKeyDown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      const body = text().trim();
      if (!body) return;
      props.onSend(body);
      setText('');
      const textarea = e.target as HTMLTextAreaElement;
      textarea.value = '';
    }
  }

  return (
    <div class="sf-input">
      <div class="sf-input__box">
        <textarea
          class="sf-input__field"
          placeholder={`Message #${props.channel}`}
          rows={1}
          on:input={(e: Event) => setText((e.currentTarget as HTMLTextAreaElement).value)}
          on:keydown={(e: KeyboardEvent) => handleKeyDown(e)}
        />
        <wf-button
          style="padding: 4px 10px;"
          title="Send"
          onClick={() => {
            const body = text().trim();
            if (!body) return;
            props.onSend(body);
            setText('');
          }}
        >
          ↑
        </wf-button>
      </div>
    </div>
  );
}
