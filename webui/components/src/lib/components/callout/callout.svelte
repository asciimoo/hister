<script lang="ts" module>
  import { type VariantProps, tv } from 'tailwind-variants';

  export const calloutVariants = tv({
    base: [
      'relative flex items-start gap-4 border-l-[6px] p-5',
      'bg-[var(--foreground)] text-[var(--background)]',
      'shadow-[4px_4px_0_var(--callout-color)]',
      '[&_code]:text-[var(--foreground)]',
    ],
    variants: {
      variant: {
        note: '[--callout-color:var(--nogon-indigo)] border-l-[var(--nogon-indigo)]',
        warning: '[--callout-color:var(--nogon-amber)] border-l-[var(--nogon-amber)]',
        danger: '[--callout-color:var(--nogon-rose)] border-l-[var(--nogon-rose)]',
        tip: '[--callout-color:var(--nogon-lime)] border-l-[var(--nogon-lime)]',
      },
    },
    defaultVariants: {
      variant: 'note',
    },
  });

  export type CalloutVariant = VariantProps<typeof calloutVariants>['variant'];
</script>

<script lang="ts">
  import type { Component, Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';
  import { cn, type WithElementRef } from '@nogon/components/utils';
  import InfoIcon from '@lucide/svelte/icons/info';
  import AlertTriangleIcon from '@lucide/svelte/icons/triangle-alert';
  import AlertOctagonIcon from '@lucide/svelte/icons/octagon-alert';
  import LightbulbIcon from '@lucide/svelte/icons/lightbulb';

  const variantIcons: Record<
    NonNullable<CalloutVariant>,
    Component<{ size?: number; class?: string }>
  > = {
    note: InfoIcon,
    warning: AlertTriangleIcon,
    danger: AlertOctagonIcon,
    tip: LightbulbIcon,
  };

  let {
    ref = $bindable(null),
    class: className,
    variant = 'note',
    title,
    children,
    ...restProps
  }: WithElementRef<HTMLAttributes<HTMLDivElement>> & {
    variant?: CalloutVariant;
    title?: string;
    children?: Snippet;
  } = $props();

  const Icon = $derived(variantIcons[variant ?? 'note']);
</script>

<div bind:this={ref} role="note" class={cn(calloutVariants({ variant }), className)} {...restProps}>
  <div class="mt-0.5 shrink-0 text-[var(--callout-color)]">
    <Icon size={22} />
  </div>
  <div class="flex flex-col gap-1.5">
    {#if title}
      <div class="font-space text-base font-extrabold uppercase">{title}</div>
    {/if}
    <div class="font-inter text-base leading-relaxed opacity-90">
      {@render children?.()}
    </div>
  </div>
</div>
