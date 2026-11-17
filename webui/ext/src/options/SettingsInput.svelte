<script lang="ts">
  import { Input } from '@nogon/components/ui/input';
  import { Label } from '@nogon/components/ui/label';

  let {
    label,
    value = $bindable(),
    placeholder = '',
    description = '',
    type = 'text',
  }: {
    label: string;
    value: string;
    placeholder?: string;
    description?: string;
    type?: string;
  } = $props();
</script>

<div class="space-y-2">
  <Label class="font-outfit text-text-brand text-sm font-bold">{label}</Label>
  <Input
    {type}
    bind:value
    {placeholder}
    class="bg-page-bg border-nogon-indigo font-fira text-text-brand placeholder:text-text-brand-muted focus-visible:border-nogon-coral h-12 w-full border-[3px] px-4 text-sm shadow-none transition-colors focus-visible:ring-0"
  />
  {#if description}
    <p class="text-text-brand-muted font-inter text-xs">{description}</p>
  {/if}
</div>
