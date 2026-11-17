<!-- SPDX-FileContributor: Adam Tauber <asciimoo@gmail.com> -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->

<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { ArrowUpDown, Clock3, Filter, Link2, SearchCheck, TextCursorInput } from '@lucide/svelte';
  import type { QuerySuggestion } from '#lib/query-suggestions.js';

  interface Props {
    activeIndex: number;
    floating?: boolean;
    id: string;
    loading?: boolean;
    open: boolean;
    suggestions: QuerySuggestion[];
    onactivechange: (index: number) => void;
    onselect: (suggestion: QuerySuggestion) => void;
  }

  let {
    activeIndex,
    floating = true,
    id,
    loading = false,
    open,
    suggestions,
    onactivechange,
    onselect,
  }: Props = $props();

  const suggestionIcons = {
    alias: Link2,
    facet: Filter,
    field: TextCursorInput,
    recent: Clock3,
    sort: ArrowUpDown,
    spelling: SearchCheck,
  };

  const minimumPanelHeight = 128;
  const panelResizeStep = 24;

  let panelEl: HTMLDivElement | undefined = $state();
  let listboxEl: HTMLDivElement | undefined = $state();
  let panelHeight: number | undefined = $state();
  let resizePointerId: number | undefined;
  let resizeStartHeight = 0;
  let resizeStartY = 0;

  function panelHeightStorageKey() {
    return `hister-${id}-height`;
  }

  function rememberPanelHeight() {
    if (panelHeight === undefined) return;
    localStorage.setItem(panelHeightStorageKey(), String(Math.round(panelHeight)));
  }

  function maximumPanelHeight() {
    if (!panelEl) return minimumPanelHeight;
    return Math.max(
      minimumPanelHeight,
      window.innerHeight - panelEl.getBoundingClientRect().top - 16,
    );
  }

  function setPanelHeight(height: number) {
    panelHeight = Math.min(Math.max(height, minimumPanelHeight), maximumPanelHeight());
  }

  function startResize(event: PointerEvent) {
    if (!panelEl) return;

    const handle = event.currentTarget as HTMLElement;
    resizePointerId = event.pointerId;
    resizeStartY = event.clientY;
    resizeStartHeight = panelEl.getBoundingClientRect().height;
    handle.setPointerCapture(event.pointerId);
    event.preventDefault();
  }

  function resizePanel(event: PointerEvent) {
    if (event.pointerId !== resizePointerId) return;
    setPanelHeight(resizeStartHeight + event.clientY - resizeStartY);
  }

  function stopResize(event: PointerEvent) {
    if (event.pointerId !== resizePointerId) return;
    resizePointerId = undefined;
    rememberPanelHeight();
  }

  function resizePanelWithKeyboard(event: KeyboardEvent) {
    if (event.key !== 'ArrowUp' && event.key !== 'ArrowDown') return;
    const currentHeight = panelEl?.getBoundingClientRect().height ?? minimumPanelHeight;
    const direction = event.key === 'ArrowUp' ? -1 : 1;
    setPanelHeight(currentHeight + direction * panelResizeStep);
    rememberPanelHeight();
    event.preventDefault();
  }

  onMount(() => {
    const storedHeight = Number.parseFloat(localStorage.getItem(panelHeightStorageKey()) ?? '');
    if (!Number.isFinite(storedHeight) || storedHeight < minimumPanelHeight) return;
    panelHeight = Math.min(storedHeight, Math.max(minimumPanelHeight, window.innerHeight - 16));
  });

  $effect(() => {
    const index = activeIndex;
    if (!open || index < 0) return;
    tick().then(() => {
      const option = listboxEl?.querySelector<HTMLElement>(`[data-suggestion-index="${index}"]`);
      if (!listboxEl || !option) return;

      const optionTop = option.offsetTop;
      const optionBottom = optionTop + option.offsetHeight;
      if (optionTop < listboxEl.scrollTop) {
        listboxEl.scrollTop = optionTop;
      } else if (optionBottom > listboxEl.scrollTop + listboxEl.clientHeight) {
        listboxEl.scrollTop = optionBottom - listboxEl.clientHeight;
      }
    });
  });
</script>

{#if open && (suggestions.length > 0 || loading)}
  <div
    bind:this={panelEl}
    style:height={panelHeight === undefined ? undefined : `${panelHeight}px`}
    class="border-brutal-border bg-card-surface z-[70] flex flex-col overflow-hidden {floating
      ? 'absolute top-full right-0 left-0 mt-2 border-[3px] shadow-[5px_5px_0_var(--brutal-shadow)]'
      : 'relative border-x-0 border-t-0 border-b-[3px] shadow-none'} {panelHeight === undefined
      ? floating
        ? 'max-h-[min(24rem,55vh)]'
        : 'max-h-[min(18rem,40vh)]'
      : floating
        ? 'max-h-[calc(100vh-1rem)]'
        : 'max-h-[calc(100vh-5rem)]'}"
  >
    <div
      bind:this={listboxEl}
      {id}
      role="listbox"
      aria-label="Search suggestions"
      class="min-h-0 flex-1 overflow-y-auto py-2"
    >
      {#each suggestions as suggestion, index (suggestion.id)}
        {@const Icon = suggestionIcons[suggestion.kind]}
        {#if index === 0 || suggestions[index - 1].group !== suggestion.group}
          <div
            class="font-space text-text-brand-muted px-3 pt-2 pb-1 text-[10px] font-bold tracking-[1.5px] uppercase first:pt-0"
          >
            {suggestion.group}
          </div>
        {/if}
        <button
          id={`${id}-option-${index}`}
          data-suggestion-index={index}
          type="button"
          role="option"
          aria-selected={index === activeIndex}
          class="font-inter flex w-full cursor-pointer items-center gap-3 px-3 py-2 text-left transition-colors {index ===
          activeIndex
            ? 'bg-nogon-indigo/10 text-text-brand'
            : 'text-text-brand-secondary hover:bg-muted-surface hover:text-text-brand'}"
          onpointerdown={(event) => event.preventDefault()}
          onmouseenter={() => onactivechange(index)}
          onclick={() => onselect(suggestion)}
        >
          <span class="text-nogon-indigo flex size-5 shrink-0 items-center justify-center">
            <Icon class="size-4" />
          </span>
          <span class="min-w-0 flex-1">
            <span class="block truncate text-sm font-semibold">{suggestion.label}</span>
            <span class="text-text-brand-muted block truncate text-xs">{suggestion.detail}</span>
          </span>
          <code
            class="font-fira bg-muted-surface text-text-brand-muted hidden max-w-52 shrink-0 truncate px-1.5 py-0.5 text-[10px] md:block"
            >{suggestion.insertText}</code
          >
        </button>
      {/each}
      {#if loading && suggestions.length === 0}
        <div class="font-inter text-text-brand-muted flex items-center gap-2 px-3 py-3 text-sm">
          <span class="bg-nogon-indigo size-2 animate-pulse"></span>
          Loading values…
        </div>
      {/if}
      <div
        class="border-border-brand-muted font-inter text-text-brand-muted mt-1 flex items-center gap-3 border-t px-3 pt-2 text-[10px]"
      >
        <span>↑ ↓ choose</span>
        <span>Tab insert</span>
        <span>Esc close</span>
      </div>
    </div>
    <button
      type="button"
      aria-label="Resize search suggestions. Use the up and down arrow keys to resize."
      title="Drag to resize suggestions"
      class="group flex h-3 shrink-0 cursor-row-resize touch-none items-center justify-center outline-none"
      onpointerdown={startResize}
      onpointermove={resizePanel}
      onpointerup={stopResize}
      onpointercancel={stopResize}
      onlostpointercapture={stopResize}
      onkeydown={resizePanelWithKeyboard}
    >
      <span
        aria-hidden="true"
        class="bg-border-brand-muted group-hover:bg-nogon-indigo group-focus-visible:bg-nogon-indigo h-0.5 w-12 transition-colors"
      ></span>
    </button>
  </div>
{/if}
