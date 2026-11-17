<script lang="ts">
  import { cn, type WithElementRef } from '@nogon/components/utils';
  import type { HTMLAttributes } from 'svelte/elements';

  let {
    ref = $bindable(null),
    class: className,
    children,
    ...restProps
  }: WithElementRef<HTMLAttributes<HTMLDivElement>> = $props();
</script>

<div
  bind:this={ref}
  data-slot="card-footer"
  class={cn(
    'bg-muted-surface border-brutal-border flex items-center border-t-[3px] px-4 py-4 md:px-5 md:py-5',
    className,
  )}
  {...restProps}
>
  {@render children?.()}
</div>
