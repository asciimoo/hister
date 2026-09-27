<script lang="ts">
  import { Button } from '@hister/components/ui/button';
  import DeleteMatchingDocumentsOption from '$lib/components/DeleteMatchingDocumentsOption.svelte';
  import type { RuleType } from '$lib/rules';
  import * as Dialog from '@hister/components/ui/dialog';
  import { Label } from '@hister/components/ui/label';
  import { Textarea } from '@hister/components/ui/textarea';
  import { ListPlus, Plus, X } from '@lucide/svelte';

  interface Props {
    open: boolean;
    patterns: string;
    ruleType: RuleType;
    deleteMatches: boolean;
    saving: boolean;
    newCount: number;
    duplicateCount: number;
    onAdd: () => void | Promise<void>;
  }

  let {
    open = $bindable(),
    patterns = $bindable(),
    ruleType = $bindable(),
    deleteMatches = $bindable(),
    saving,
    newCount,
    duplicateCount,
    onAdd,
  }: Props = $props();

  const summaryLabel = $derived.by(() => {
    if (!patterns.trim()) return '';
    if (!duplicateCount) return `${newCount} new`;
    return `${newCount} new · ${duplicateCount} duplicate${duplicateCount === 1 ? '' : 's'}`;
  });
  const addLabel = $derived.by(() => {
    if (saving) return 'Saving…';
    if (newCount === 0) return 'Add rules';
    return `Add ${newCount} rule${newCount === 1 ? '' : 's'}`;
  });
</script>

<Dialog.Root bind:open>
  <Dialog.Content
    showCloseButton={false}
    class="border-border-brand bg-card-surface max-w-lg gap-0 overflow-hidden rounded-none border-[3px] p-0 shadow-[6px_6px_0px_var(--hister-coral)]"
  >
    <Dialog.Header class="bg-hister-coral flex-row items-center justify-between gap-2 px-5 py-4">
      <div>
        <Dialog.Title class="flex items-center gap-2">
          <ListPlus class="size-5 text-white" />
          <span class="font-outfit text-lg font-extrabold text-white">Bulk add rules</span>
        </Dialog.Title>
        <Dialog.Description class="font-inter mt-1 text-sm text-white/80">
          Add several rules of the same type at once.
        </Dialog.Description>
      </div>
      <Dialog.Close class="p-0.5 text-white/70 hover:text-white" aria-label="Close bulk add dialog">
        <X class="size-5" />
      </Dialog.Close>
    </Dialog.Header>

    <div class="space-y-4 px-5 py-5">
      <div class="space-y-1.5">
        <Label for="bulk-rule-patterns" class="font-outfit text-text-brand text-sm font-bold"
          >Patterns</Label
        >
        <Textarea
          id="bulk-rule-patterns"
          bind:value={patterns}
          placeholder={'^https://example\\.com/\n^https://docs\\.example\\.com/'}
          aria-describedby="bulk-rule-patterns-help"
          wrap="off"
          class="bg-page-bg border-brutal-border font-fira text-text-brand placeholder:text-text-brand-muted focus-visible:border-hister-coral field-sizing-fixed h-48 max-h-48 min-h-48 w-full resize-none overflow-auto rounded-none border-[3px] p-3 text-sm whitespace-pre"
        />
        <div
          id="bulk-rule-patterns-help"
          class="font-inter text-text-brand-muted flex flex-wrap justify-between gap-2 text-xs"
        >
          <span>One Go regexp per line. Long rules scroll horizontally.</span>
          <span class="font-fira">{summaryLabel}</span>
        </div>
      </div>

      <div class="space-y-1.5">
        <Label for="bulk-rule-type" class="font-outfit text-text-brand text-sm font-bold"
          >Type</Label
        >
        <select
          id="bulk-rule-type"
          value={ruleType}
          onchange={(event) => {
            ruleType = (event.currentTarget as HTMLSelectElement).value as RuleType;
            if (ruleType !== 'skip') deleteMatches = false;
          }}
          class="bg-page-bg border-brutal-border font-space text-text-brand h-10 w-full cursor-pointer appearance-none border-[3px] px-3 text-xs font-bold tracking-[0.5px] outline-none"
        >
          <option value="allow">ALLOW</option>
          <option value="skip">SKIP</option>
          <option value="priority">PRIORITY</option>
          <option value="versioning">VERSION</option>
        </select>
      </div>

      {#if ruleType === 'skip'}
        <DeleteMatchingDocumentsOption bind:checked={deleteMatches} />
      {/if}
    </div>

    <Dialog.Footer class="border-border-brand-muted bg-muted-surface border-t-[3px] px-5 py-3">
      <Dialog.Close
        class="border-border-brand-muted bg-card-surface text-text-brand-secondary font-space h-9 border-[2px] px-4 text-xs font-bold uppercase"
      >
        Cancel
      </Dialog.Close>
      <Button
        type="button"
        onclick={onAdd}
        disabled={saving || !patterns.trim()}
        class="bg-hister-coral font-space border-brutal-border brutal-press h-9 gap-2 rounded-none border-[3px] px-4 text-xs font-bold tracking-[0.5px] text-white uppercase"
      >
        <Plus class="size-4" />
        {addLabel}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
