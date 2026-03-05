<script lang="ts">
  import { Search } from 'lucide-svelte';
  import { Input } from '@hister/components/ui/input';

  let {
    value = $bindable(''),
    placeholder = 'Filter...',
  }: {
    value?: string;
    placeholder?: string;
  } = $props();
</script>

<div class="flex items-center gap-3 h-11 px-4 bg-card-surface border-b-[3px] border-brutal-border">
  <Search class="size-4 text-text-brand-muted shrink-0" />
  <Input
    bind:value
    {placeholder}
    class="flex-1 h-full bg-transparent font-inter text-sm font-medium text-text-brand placeholder:text-text-brand-muted border-0 shadow-none focus-visible:ring-0 p-0"
  />
</div>
