<script lang="ts">
  import type { Component } from 'svelte';
  import { cn } from '../../utils.js';

  let {
    icon,
    title,
    description,
    bg = 'bg-brutal-card',
    textColor = 'text-[var(--text-primary)]',
    descColor = 'text-[var(--text-secondary)]',
    iconBg = 'bg-nogon-indigo',
    iconColor = 'text-white',
    iconSize = 24,
    horizontal = false,
    class: className = '',
  }: {
    icon: Component<{ size?: number; class?: string }>;
    title: string;
    description: string;
    bg?: string;
    textColor?: string;
    descColor?: string;
    iconBg?: string;
    iconColor?: string;
    iconSize?: number;
    horizontal?: boolean;
    class?: string;
  } = $props();
</script>

<div
  class={cn(
    bg,
    'border-brutal-border shadow-brutal-lg border-[3px] p-7',
    horizontal ? 'flex items-center gap-5' : 'flex flex-col gap-4',
    className,
  )}
>
  {#if icon}
    {@const Icon = icon}
    <div class={cn(iconBg, 'flex h-12 w-12 items-center justify-center', horizontal && 'shrink-0')}>
      <Icon size={iconSize} class={iconColor} />
    </div>
  {/if}
  {#if horizontal}
    <div class="flex flex-col gap-2">
      <h3
        class="{textColor} font-space text-xl font-extrabold tracking-[1px] uppercase md:text-3xl"
      >
        {title}
      </h3>
      <p class="{descColor} font-inter text-sm leading-relaxed md:text-lg">{@html description}</p>
    </div>
  {:else}
    <h3 class="{textColor} font-space text-xl font-extrabold tracking-[1px] uppercase md:text-3xl">
      {title}
    </h3>
    <p class="{descColor} font-inter text-sm leading-relaxed md:text-lg">{@html description}</p>
  {/if}
</div>
