<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script lang="ts">
  import { Button } from '@nogon/components/ui/button';
  import * as Dialog from '@nogon/components/ui/dialog';
  import Trash2 from '@lucide/svelte/icons/trash-2';

  interface Props {
    open: boolean;
    matched: number;
    ruleCount?: number;
    deleting?: boolean;
    onCancel: () => void;
    onConfirm: () => void | Promise<void>;
  }

  let {
    open = $bindable(),
    matched,
    ruleCount = 1,
    deleting = false,
    onCancel,
    onConfirm,
  }: Props = $props();

  const documentLabel = $derived(`document${matched === 1 ? '' : 's'}`);
  const ruleLabel = $derived(`rule${ruleCount === 1 ? '' : 's'}`);
  const matchVerb = $derived(matched === 1 ? 'matches' : 'match');
  const deleteLabel = $derived(deleting ? 'Deleting…' : `Delete ${matched} ${documentLabel}`);
</script>

<Dialog.Root bind:open>
  <Dialog.Content
    escapeKeydownBehavior="ignore"
    onInteractOutside={(event) => event.preventDefault()}
    showCloseButton={false}
    class="border-border-brand bg-card-surface max-w-md gap-0 overflow-hidden rounded-none border-[3px] p-0 shadow-[6px_6px_0px_var(--brutal-shadow)]"
  >
    <Dialog.Header class="bg-nogon-rose flex-row items-center gap-2 px-5 py-4">
      <Dialog.Title class="flex items-center gap-2">
        <Trash2 class="size-5 text-white" />
        <span class="font-outfit text-lg font-extrabold text-white">Delete matching documents?</span
        >
      </Dialog.Title>
    </Dialog.Header>
    <div class="space-y-3 px-5 py-5">
      <p class="font-inter text-text-brand-secondary text-sm">
        {matched} existing {documentLabel}
        {matchVerb} the saved skip {ruleLabel}.
      </p>
      <p class="font-inter text-text-brand-muted text-xs">
        This permanently deletes the matching documents from the index. The saved skip {ruleLabel}
        will remain if you cancel.
      </p>
    </div>
    <Dialog.Footer class="border-border-brand-muted bg-muted-surface border-t-[3px] px-5 py-3">
      <Button
        type="button"
        variant="outline"
        disabled={deleting}
        onclick={onCancel}
        class="border-border-brand-muted text-text-brand-secondary rounded-none"
      >
        Cancel
      </Button>
      <Button
        type="button"
        disabled={deleting}
        onclick={onConfirm}
        class="bg-nogon-rose font-space border-brutal-border rounded-none border-[3px] text-xs font-bold text-white uppercase"
      >
        {deleteLabel}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
