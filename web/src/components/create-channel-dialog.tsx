import { createSignal, createEffect } from 'solid-js';

interface CreateChannelDialogProps {
  open: boolean;
  onCreate: (name: string, isPublic: boolean) => void;
  onClose: () => void;
}

export function CreateChannelDialog(props: CreateChannelDialogProps) {
  const [name, setName] = createSignal('');
  const [isPublic, setIsPublic] = createSignal(true);

  let dialogRef!: HTMLElement & { show(): void; hide(): void };

  createEffect(() => {
    if (props.open) dialogRef?.show?.();
    else dialogRef?.hide?.();
  });

  function handleCreate() {
    const n = name().trim();
    if (!n) return;
    props.onCreate(n, isPublic());
    setName('');
    setIsPublic(true);
  }

  return (
    <wf-dialog ref={dialogRef} header="Create Channel" on:wf-close={props.onClose}>
      <div style="display: flex; flex-direction: column; gap: var(--wf-space-md); padding: var(--wf-space-sm);">
        <wf-input
          placeholder="Channel name"
          value={name()}
          on:input={(e: Event) => setName((e.target as HTMLInputElement).value)}
          on:keydown={(e: KeyboardEvent) => { if (e.key === 'Enter') handleCreate(); }}
        />
        <label style="display: flex; align-items: center; gap: var(--wf-space-sm); font-size: var(--wf-text-sm); color: var(--wf-color-text-secondary);">
          <input
            type="checkbox"
            checked={isPublic()}
            on:change={(e: Event) => setIsPublic((e.target as HTMLInputElement).checked)}
          />
          Public channel
        </label>
        <div style="display: flex; justify-content: flex-end; gap: var(--wf-space-sm);">
          <wf-button variant="text" on:click={props.onClose}>Cancel</wf-button>
          <wf-button title="Create" on:click={handleCreate}>Create</wf-button>
        </div>
      </div>
    </wf-dialog>
  );
}
