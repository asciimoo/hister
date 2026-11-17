<script lang="ts">
  import { Button } from '@nogon/components/ui/button';
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
  {@const color = getGroupColor(i + colorOffset)}
  {@const isExpanded = expandedPeriods.has(bucket.key)}
  <Button
    variant="ghost"
    size="sm"
    class="font-inter border-brutal-border text-text-brand-secondary hover:bg-muted-surface h-8 shrink-0 gap-1 rounded-none border-[2px] px-3 text-xs font-semibold"
    aria-expanded={isExpanded}
    onclick={() => onToggle(bucket)}
  >
    {#if isExpanded}<ChevronDown class="size-3" />{:else}<ChevronRight class="size-3" />{/if}
    {timelineBucketLabel(bucket, 'month')} ({bucket.count.toLocaleString()})
  </Button>
  {#if isExpanded}
    {#if loadingPeriods.has(bucket.key)}
      <span class="font-inter text-text-brand-muted shrink-0 text-xs">Loading days...</span>
    {:else if periodErrors.has(bucket.key)}
      <span class="font-inter text-nogon-coral shrink-0 text-xs">
        {periodErrors.get(bucket.key)}
      </span>
    {:else}
      {#each periodDays.get(bucket.key) ?? [] as day (day.key)}
        {@const isActive = activeKey === day.key}
        <Button
          variant="ghost"
          size="sm"
          class="font-inter border-brutal-border h-8 shrink-0 rounded-none border-[2px] px-3 text-xs font-semibold {isActive
            ? 'text-primary-foreground hover:text-primary-foreground'
            : 'text-text-brand-secondary hover:bg-muted-surface'}"
          style={isActive ? `background-color: ${getColorVar(color)};` : ''}
          disabled={day.count === 0}
          onclick={() => onSelect(day)}
        >
          {timelineBucketLabel(day, 'day')} ({day.count.toLocaleString()})
        </Button>
      {/each}
    {/if}
  {/if}
{/each}
