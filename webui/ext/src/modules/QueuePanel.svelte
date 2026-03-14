<script lang="ts">
  import { Button } from '@hister/components/ui/button';
  import { timeAgo } from './queue-store.svelte';
  import type { ConnectionStatus } from './status';
  import type { QueueItem } from './queue-store.svelte';

  interface Props {
    status: ConnectionStatus;
    count: number;
    expanded: boolean;
    items: QueueItem[];
    onretry: () => void;
    onclear: () => void;
    ontoggle: () => void;
    onremove: (id: number) => void;
    compact?: boolean;
  }

  let {
    status,
    count,
    expanded,
    items,
    onretry,
    onclear,
    ontoggle,
    onremove,
    compact = false,
  }: Props = $props();

  const labels: Record<ConnectionStatus, string> = {
    connected: 'Connected',
    checking: 'Checking...',
    offline: 'Server offline',
    error: 'Connection error',
  };

  const dots: Record<ConnectionStatus, string> = {
    connected: 'bg-hister-teal',
    checking: 'animate-pulse bg-gray-400',
    offline: 'bg-hister-rose',
    error: 'bg-hister-coral',
  };
</script>

<!-- Status indicator -->
<div
  class={compact
    ? 'border-brutal-border flex items-center gap-2 border-b-[3px] px-5 py-3'
    : 'bg-card-surface border-brutal-border flex items-center gap-3 border-[3px] px-7 py-4 shadow-[4px_4px_0_var(--brutal-shadow)]'}
>
  <span class="inline-block {compact ? 'h-2.5 w-2.5' : 'h-3 w-3'} rounded-full {dots[status]}"
  ></span>
  <span class="font-outfit text-text-brand text-sm font-bold">{labels[status]}</span>
</div>

<!-- Queue list -->
{#if count > 0}
  <div class={compact ? 'border-brutal-border border-b-[3px]' : ''}>
    <div class="flex items-center justify-between {compact ? 'px-5 py-3' : 'px-7 py-4'}">
      <button
        onclick={ontoggle}
        class="font-outfit text-text-brand flex items-center gap-{compact
          ? '1.5'
          : '2'} text-sm font-bold"
      >
        <span class="text-xs transition-transform {expanded ? 'rotate-90' : ''}">&#9654;</span>
        {count} page{count !== 1 ? 's' : ''} queued
      </button>
      <div class="flex items-center gap-{compact ? '2' : '3'}">
        <Button
          variant="outline"
          size="sm"
          onclick={onretry}
          class="border-hister-indigo text-hister-indigo border-2 text-xs {compact
            ? ''
            : 'font-bold'}"
        >
          Retry now
        </Button>
        <button
          onclick={onclear}
          class="font-inter text-text-brand-muted text-xs underline hover:no-underline"
        >
          Clear
        </button>
      </div>
    </div>

    {#if expanded && items.length > 0}
      <div
        class="{compact ? 'border-brutal-border' : 'border-hister-indigo'} {compact
          ? 'max-h-48'
          : 'max-h-72'} overflow-y-auto border-t-[3px]"
      >
        {#each items as item, i (item.id)}
          <div
            class="group flex items-center gap-3 {compact ? 'px-5 py-2.5' : 'px-7 py-3'} {i <
            items.length - 1
              ? 'border-brutal-border border-b-2'
              : ''}"
          >
            <div class="min-w-0 flex-1">
              <p
                class="font-outfit text-text-brand truncate {compact
                  ? 'text-xs'
                  : 'text-sm'} font-bold"
              >
                {item.title || 'Untitled'}
              </p>
              <p
                class="font-inter text-text-brand-muted mt-0.5 flex items-center gap-{compact
                  ? '1.5'
                  : '2'} truncate text-xs"
              >
                <span class="truncate">{item.url}</span>
                <span class="shrink-0 opacity-50">&middot;</span>
                <span class="shrink-0 opacity-50">{timeAgo(item.createdAt)}</span>
              </p>
            </div>
            <button
              onclick={() => onremove(item.id)}
              class="border-brutal-border text-text-brand hover:bg-hister-rose hover:border-hister-rose flex {compact
                ? 'h-5 w-5'
                : 'h-6 w-6'} shrink-0 items-center justify-center border-2 text-xs font-bold transition-colors hover:text-white"
              title="Remove from queue"
            >
              &times;
            </button>
          </div>
        {/each}
      </div>
    {/if}
  </div>
{/if}
