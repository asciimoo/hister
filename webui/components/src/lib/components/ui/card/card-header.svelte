<script lang="ts">
  import { cn, type WithElementRef } from '@nogon/components/utils';
  import type { HTMLAttributes } from 'svelte/elements';

  let {
    ref = $bindable(null),
    color,
    class: className,
    children,
    ...restProps
  }: WithElementRef<HTMLAttributes<HTMLDivElement>> & {
    color?: string;
  } = $props();
</script>

<div
  bind:this={ref}
  data-slot="card-header"
  class={cn('flex items-center gap-4 px-6 py-6', color && 'card-header-accented', className)}
  style={color ? `--card-header-color: var(--${color});` : undefined}
  {...restProps}
>
  {@render children?.()}
</div>
