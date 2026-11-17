<script lang="ts">
  import type { HTMLAttributes } from 'svelte/elements';
  import { cn, type WithElementRef } from '@nogon/components/utils';

  let {
    ref = $bindable(null),
    color,
    href,
    target,
    rel,
    class: className,
    children,
    ...restProps
  }: WithElementRef<HTMLAttributes<HTMLDivElement>> & {
    color?: string;
    href?: string;
    target?: string;
    rel?: string;
  } = $props();
</script>

<svelte:element
  this={href ? 'a' : 'div'}
  bind:this={ref}
  data-slot="card"
  {href}
  {target}
  {rel}
  class={cn(
    'bg-card-surface text-card-foreground border-brutal-border flex flex-col gap-0 overflow-hidden rounded-none border-[3px] py-0 shadow-[6px_6px_0_var(--brutal-shadow)]',
    color && 'card-accented',
    href && 'brutal-press-card block no-underline',
    className,
  )}
  style={color ? `--card-accent-color: var(--${color});` : undefined}
  {...restProps}
>
  {@render children?.()}
</svelte:element>
