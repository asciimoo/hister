<script lang="ts">
  import type { Snippet } from 'svelte';
  import { cn } from '@nogon/components/utils';

  let {
    color = 'nogon-indigo',
    size = 'md',
    tag = 'h1',
    truncate = false,
    class: className = '',
    children,
  }: {
    color?: string;
    size?: 'xs' | 'sm' | 'md' | 'lg';
    tag?: string;
    truncate?: boolean;
    class?: string;
    children: Snippet;
  } = $props();

  const sizes = {
    xs: { gap: 'gap-2', bar: 'w-1 h-6', text: 'text-sm md:text-lg font-extrabold' },
    sm: {
      gap: 'gap-2',
      bar: 'w-1.5 h-8',
      text: 'text-lg md:text-xl font-extrabold',
    },
    md: {
      gap: 'gap-3',
      bar: 'w-1.5 h-8',
      text: 'text-2xl md:text-3xl font-extrabold',
    },
    lg: { gap: 'gap-6', bar: 'w-1.5 h-10', text: 'text-3xl md:text-5xl font-black' },
  };

  const s = $derived(sizes[size]);
</script>

<svelte:element this={tag} class={cn('flex items-center', s.gap, className)}>
  <span class={cn('shrink-0', s.bar)} style="background-color: var(--{color});"></span>
  <span class={cn('font-space text-text-brand uppercase', s.text, truncate && 'truncate')}>
    {@render children()}
  </span>
</svelte:element>
