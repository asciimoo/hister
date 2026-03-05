<script lang="ts">
  import { cn, type WithElementRef } from '@hister/components/utils';
  import type { HTMLAttributes } from 'svelte/elements';

  let {
    ref = $bindable(null),
    class: className,
    children,
    ...restProps
  }: WithElementRef<HTMLAttributes<HTMLTableRowElement>> = $props();
</script>

<tr
  bind:this={ref}
  data-slot="table-row"
  class={cn('data-[state=selected]:bg-muted border-b transition-colors', className)}
  {...restProps}
>
  {@render children?.()}
</tr>
