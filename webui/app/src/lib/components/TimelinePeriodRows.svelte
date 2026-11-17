<script lang="ts">
  import { Button } from '@nogon/components/ui/button';
  import { Badge } from '@nogon/components/ui/badge';
  import { ChevronDown, ChevronRight } from '@lucide/svelte';
  import {
    getColorVar,
    getGroupColor,
    timelineBucketLabel,
    type TimelinePeriodRendererProps,
  } from '#lib/history-timeline.js';

  let {
    buckets,
    colorOffset,
    activeKey,
    expandedPeriods,
    periodDays,
    loadingPeriods,
    periodErrors,
    onToggle,
    onSelect,
  }: TimelinePeriodRendererProps = $props();
</script>

{#each buckets as bucket, i (bucket.key)}
  {@const isExpanded = expandedPeriods.has(bucket.key)}
  {@const color = getGroupColor(i + colorOffset)}
  <Button
    variant="ghost"
    class="hover:border-border-brand hover:bg-muted-surface text-text-brand-secondary ml-4 flex h-auto w-[calc(100%-1rem)] cursor-pointer items-center justify-start gap-2 rounded-none border-[2px] border-transparent px-3 py-2"
    disabled={bucket.count === 0}
    aria-expanded={isExpanded}
    onclick={() => onToggle(bucket)}
  >
    {#if isExpanded}<ChevronDown class="size-3.5 shrink-0" />{:else}<ChevronRight
        class="size-3.5 shrink-0"
      />{/if}
    <span class="font-inter truncate text-xs font-semibold">
      {timelineBucketLabel(bucket, 'month')}
    </span>
    <Badge
      variant="secondary"
      class="bg-muted-surface text-text-brand-muted ml-auto h-4 shrink-0 border-0 px-1.5 py-0 text-xs"
    >
      {bucket.count.toLocaleString()}
    </Badge>
  </Button>
  {#if isExpanded}
    {#if loadingPeriods.has(bucket.key)}
      <p class="font-inter text-text-brand-muted ml-8 px-3 py-1 text-xs">Loading days...</p>
    {:else if periodErrors.has(bucket.key)}
      <p class="font-inter text-nogon-coral ml-8 px-3 py-1 text-xs">
        {periodErrors.get(bucket.key)}
      </p>
    {:else}
      {#each periodDays.get(bucket.key) ?? [] as day (day.key)}
        {@const isActive = activeKey === day.key}
        <Button
          variant="ghost"
          class="ml-8 flex h-auto w-[calc(100%-2rem)] cursor-pointer items-center justify-start gap-2 rounded-none border-[2px] px-3 py-1.5 shadow-[2px_2px_0_transparent] hover:shadow-[2px_2px_0_var(--brutal-shadow)] {isActive
            ? 'border-brutal-border text-primary-foreground hover:text-primary-foreground'
            : 'hover:border-border-brand hover:bg-muted-surface border-transparent'}"
          style={isActive ? `background-color: ${getColorVar(color)};` : ''}
          disabled={day.count === 0}
          onclick={() => onSelect(day)}
        >
          <span
            class="h-1.5 w-1.5 shrink-0 rounded-full"
            style={isActive
              ? 'background-color: white;'
              : `background-color: ${getColorVar(color)};`}
          ></span>
          <span class="font-inter text-xs" class:font-semibold={isActive}>
            {timelineBucketLabel(day, 'day')}
          </span>
          <Badge
            variant="secondary"
            class="ml-auto h-4 shrink-0 border-0 px-1.5 py-0 text-xs {isActive
              ? 'text-primary-foreground bg-white/20'
              : 'bg-muted-surface text-text-brand-muted'}"
          >
            {day.count.toLocaleString()}
          </Badge>
        </Button>
      {/each}
    {/if}
  {/if}
{/each}
