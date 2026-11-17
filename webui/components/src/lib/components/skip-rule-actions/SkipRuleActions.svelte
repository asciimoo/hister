<script lang="ts">
  import { Button } from '@nogon/components/ui/button';
  import StickyNote from '@lucide/svelte/icons/sticky-note';
  import Globe from '@lucide/svelte/icons/globe';

  interface Props {
    onAddSkipRule?: (type: 'url' | 'domain', deleteMatches: boolean) => void;
    urlLabel?: string;
    class?: string;
  }

  let { onAddSkipRule, urlLabel = 'This URL', class: className = '' }: Props = $props();
  let deleteMatches = $state(false);
</script>

<div class={className}>
  <p class="font-outfit mb-2 text-xs font-bold tracking-widest uppercase">Disable indexing</p>
  <label
    class="font-inter text-text-brand-secondary mb-3 flex cursor-pointer items-center gap-2 text-xs select-none"
  >
    <input
      type="checkbox"
      bind:checked={deleteMatches}
      class="accent-nogon-rose h-3.5 w-3.5 cursor-pointer"
    />
    Delete matching documents
  </label>
  <div class="flex gap-2">
    <Button
      variant="outline"
      size="sm"
      class="border-brutal-border font-outfit hover:border-nogon-rose h-9 flex-1 border-[3px] text-xs font-bold tracking-wide transition-all hover:shadow-[3px_3px_0_var(--brutal-shadow)]"
      onclick={() => onAddSkipRule?.('url', deleteMatches)}
    >
      <StickyNote class="size-3.5" />
      {urlLabel}
    </Button>
    <Button
      variant="outline"
      size="sm"
      class="border-brutal-border font-outfit hover:border-nogon-rose h-9 flex-1 border-[3px] text-xs font-bold tracking-wide transition-all hover:shadow-[3px_3px_0_var(--brutal-shadow)]"
      onclick={() => onAddSkipRule?.('domain', deleteMatches)}
    >
      <Globe class="size-3.5" />
      This Domain
    </Button>
  </div>
</div>
