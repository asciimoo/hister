<script lang="ts">
  import { goto } from '$app/navigation';
  import SearchIcon from '@lucide/svelte/icons/search';
  import type { DocsSearchEntry, DocsSearchIndex } from '#lib/docs-search.js';

  interface RankedResult {
    entry: DocsSearchEntry;
    label: string;
    excerpt: string;
    score: number;
  }

  interface HighlightPart {
    text: string;
    match: boolean;
  }

  let { tone = 'light' }: { tone?: 'light' | 'dark' } = $props();

  let root: HTMLDivElement;
  let input: HTMLInputElement;
  let query = $state('');
  let index = $state<DocsSearchEntry[] | null>(null);
  let loading = $state(false);
  let loadError = $state('');
  let open = $state(false);
  let activeIndex = $state(0);

  const terms = $derived(tokenize(query));
  const results = $derived.by(() => rankResults(index ?? [], terms));
  const showPanel = $derived(open && query.trim().length > 0);

  $effect(() => {
    void query;
    activeIndex = 0;
  });

  function tokenize(value: string): string[] {
    return [...new Set(value.toLocaleLowerCase().match(/[\p{L}\p{N}_.:/-]+/gu) ?? [])];
  }

  function countMatches(value: string, term: string): number {
    let count = 0;
    let offset = 0;
    while ((offset = value.indexOf(term, offset)) !== -1) {
      count += 1;
      offset += term.length;
    }
    return count;
  }

  function makeExcerpt(entry: DocsSearchEntry, searchTerms: string[]): string {
    const source = entry.content || entry.description;
    if (source.length <= 190) return source;

    const normalized = source.toLocaleLowerCase();
    const positions = searchTerms
      .map((term) => normalized.indexOf(term))
      .filter((position) => position >= 0);
    const firstMatch = positions.length > 0 ? Math.min(...positions) : 0;
    let start = Math.max(0, firstMatch - 70);
    let end = Math.min(source.length, start + 190);

    if (start > 0) {
      const nextSpace = source.indexOf(' ', start);
      if (nextSpace >= 0 && nextSpace < firstMatch) start = nextSpace + 1;
    }
    if (end < source.length) {
      const previousSpace = source.lastIndexOf(' ', end);
      if (previousSpace > start) end = previousSpace;
    }

    return `${start > 0 ? '…' : ''}${source.slice(start, end).trim()}${end < source.length ? '…' : ''}`;
  }

  function rankResults(entries: DocsSearchEntry[], searchTerms: string[]): RankedResult[] {
    if (searchTerms.length === 0) return [];

    return entries
      .map((entry) => {
        const pageTitle = entry.pageTitle.toLocaleLowerCase();
        const heading = entry.heading?.toLocaleLowerCase() ?? '';
        const description = entry.description.toLocaleLowerCase();
        const content = entry.content.toLocaleLowerCase();
        const combined = `${pageTitle} ${heading} ${description} ${content}`;
        if (!searchTerms.every((term) => combined.includes(term))) return null;

        let score = 0;
        const titleMatchesEveryTerm = searchTerms.every((term) => pageTitle.includes(term));
        if (titleMatchesEveryTerm) score += entry.heading ? 40 : 160;

        for (const term of searchTerms) {
          if (pageTitle === term) score += 50;
          else if (pageTitle.startsWith(term)) score += 30;
          else if (pageTitle.includes(term)) score += 18;

          if (heading === term) score += 45;
          else if (heading.startsWith(term)) score += 28;
          else if (heading.includes(term)) score += 16;

          score += Math.min(countMatches(description, term), 3) * 6;
          score += Math.min(countMatches(content, term), 5) * 2;
        }

        return {
          entry,
          label: entry.heading ?? entry.pageTitle,
          excerpt: makeExcerpt(entry, searchTerms),
          score,
        };
      })
      .filter((result): result is RankedResult => result !== null)
      .sort(
        (a, b) =>
          b.score - a.score ||
          a.entry.pageTitle.localeCompare(b.entry.pageTitle) ||
          a.entry.url.localeCompare(b.entry.url),
      )
      .slice(0, 8);
  }

  function escapeRegex(value: string): string {
    return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  }

  function highlightParts(value: string, searchTerms: string[]): HighlightPart[] {
    if (searchTerms.length === 0) return [{ text: value, match: false }];
    const expression = new RegExp(`(${searchTerms.map(escapeRegex).join('|')})`, 'giu');
    return value
      .split(expression)
      .filter(Boolean)
      .map((text) => ({
        text,
        match: searchTerms.some((term) => text.toLocaleLowerCase() === term),
      }));
  }

  async function loadIndex() {
    if (index || loading) return;
    loading = true;
    loadError = '';
    try {
      const response = await fetch('/docs/search.json');
      if (!response.ok) throw new Error(`Search index request failed with ${response.status}`);
      const data = (await response.json()) as DocsSearchIndex;
      index = data.entries;
    } catch {
      loadError = 'Documentation search could not be loaded.';
    } finally {
      loading = false;
    }
  }

  function focusSearch() {
    open = true;
    void loadIndex();
    input.focus();
  }

  function handleShortcut(event: KeyboardEvent) {
    const target = event.target;
    const editing =
      target instanceof HTMLInputElement ||
      target instanceof HTMLTextAreaElement ||
      (target instanceof HTMLElement && target.isContentEditable);
    const commandShortcut = (event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k';
    const slashShortcut = event.key === '/' && !event.ctrlKey && !event.metaKey && !event.altKey;

    if (commandShortcut || (slashShortcut && !editing)) {
      event.preventDefault();
      focusSearch();
    }
  }

  function handleWindowClick(event: MouseEvent) {
    if (root && !root.contains(event.target as Node)) open = false;
  }

  function handleInputKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      open = false;
      input.blur();
      return;
    }
    if (!showPanel || results.length === 0) return;

    if (event.key === 'ArrowDown') {
      event.preventDefault();
      activeIndex = (activeIndex + 1) % results.length;
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      activeIndex = (activeIndex - 1 + results.length) % results.length;
    } else if (event.key === 'Enter') {
      event.preventDefault();
      open = false;
      void goto(results[activeIndex]?.entry.url ?? results[0].entry.url);
    }
  }
</script>

<svelte:window onkeydown={handleShortcut} onclick={handleWindowClick} />

<div bind:this={root} class:dark={tone === 'dark'} class="docs-search relative w-full">
  <div class="search-shell border-brutal-border border-[3px] p-3 sm:p-4">
    <div class="mb-2.5 flex items-center justify-between gap-4">
      <label
        for="docs-search-input"
        class="search-label font-space flex items-center gap-2 text-[11px] font-black tracking-[1.4px] uppercase"
      >
        <span aria-hidden="true" class="search-label-mark size-2.5"></span>
        Search all documentation
      </label>
      <span class="search-hint font-fira hidden text-[9px] sm:block">Press Ctrl K or /</span>
    </div>

    <div class="search-field relative flex items-center">
      <span
        aria-hidden="true"
        class="search-icon pointer-events-none absolute left-2.5 flex size-8 items-center justify-center"
      >
        <SearchIcon size={22} />
      </span>
      <input
        bind:this={input}
        id="docs-search-input"
        type="search"
        role="combobox"
        aria-autocomplete="list"
        aria-expanded={showPanel}
        aria-controls="docs-search-results"
        aria-activedescendant={showPanel && results.length > 0
          ? `docs-search-result-${activeIndex}`
          : undefined}
        aria-busy={loading}
        autocomplete="off"
        placeholder="Try OAuth, browser import, or semantic search"
        bind:value={query}
        onfocus={() => {
          open = true;
          void loadIndex();
        }}
        oninput={() => (open = true)}
        onkeydown={handleInputKeydown}
        class="font-inter w-full py-3.5 pr-4 pl-13 text-sm font-medium outline-none focus-visible:outline-none"
      />
    </div>
  </div>

  {#if showPanel}
    <div
      id="docs-search-results"
      role="listbox"
      aria-label="Documentation search results"
      class="results border-brutal-border bg-brutal-card absolute top-[calc(100%+0.5rem)] right-0 left-0 z-40 max-h-[min(65vh,34rem)] overflow-y-auto border-[3px] shadow-[6px_6px_0_var(--brutal-shadow)]"
    >
      {#if loading}
        <p class="font-inter m-0 px-4 py-5 text-sm text-(--text-secondary)" role="status">
          Loading documentation search…
        </p>
      {:else if loadError}
        <p class="font-inter m-0 px-4 py-5 text-sm text-(--text-secondary)" role="status">
          {loadError}
        </p>
      {:else if results.length === 0}
        <p class="font-inter m-0 px-4 py-5 text-sm text-(--text-secondary)" role="status">
          No documentation matches “{query.trim()}”.
        </p>
      {:else}
        {#each results as result, resultIndex (result.entry.url)}
          <a
            id="docs-search-result-{resultIndex}"
            href={result.entry.url}
            role="option"
            aria-selected={activeIndex === resultIndex}
            onmouseenter={() => (activeIndex = resultIndex)}
            onclick={() => (open = false)}
            class="result border-brutal-border block border-b-2 px-4 py-3 text-(--text-primary) no-underline last:border-b-0"
          >
            <span class="font-space block text-sm leading-snug font-extrabold">
              {#each highlightParts(result.label, terms) as part}
                {#if part.match}<mark>{part.text}</mark>{:else}{part.text}{/if}
              {/each}
            </span>
            {#if result.entry.heading}
              <span
                class="font-fira mt-1 block text-[9px] font-semibold tracking-[0.8px] text-(--text-secondary) uppercase"
              >
                {result.entry.pageTitle}
              </span>
            {/if}
            <span class="font-inter mt-1.5 block text-xs leading-relaxed text-(--text-secondary)">
              {#each highlightParts(result.excerpt, terms) as part}
                {#if part.match}<mark>{part.text}</mark>{:else}{part.text}{/if}
              {/each}
            </span>
          </a>
        {/each}
      {/if}
    </div>
  {/if}
</div>

<style>
  .search-shell {
    background: color-mix(in srgb, var(--nogon-indigo) 18%, var(--brutal-bg));
    box-shadow: 7px 7px 0 var(--nogon-indigo);
  }

  .search-label {
    color: var(--text-primary);
  }

  .search-label-mark {
    background: var(--nogon-coral);
    box-shadow: 2px 2px 0 var(--brutal-border);
  }

  .search-hint {
    color: var(--text-secondary);
  }

  .search-field {
    color: var(--text-secondary);
    border: 2px solid var(--brutal-border);
    background: var(--brutal-card);
    box-shadow: inset 3px 3px 0 color-mix(in srgb, var(--brutal-shadow) 10%, transparent);
  }

  .search-field:focus-within {
    box-shadow:
      inset 3px 3px 0 color-mix(in srgb, var(--brutal-shadow) 10%, transparent),
      0 0 0 3px var(--nogon-indigo);
  }

  .search-icon {
    color: var(--text-secondary);
    background: var(--brutal-card);
  }

  .search-field:focus-within .search-icon {
    color: var(--nogon-indigo);
  }

  input {
    color: var(--text-primary);
    background: transparent;
  }

  input::placeholder {
    color: var(--text-secondary);
  }

  .dark .search-field {
    background: var(--brutal-card);
  }

  .dark .search-shell {
    border-color: color-mix(in srgb, white 55%, var(--brutal-border));
    background: color-mix(in srgb, var(--nogon-indigo) 24%, var(--brutal-bg));
    box-shadow: 7px 7px 0 var(--nogon-coral);
  }

  .result[aria-selected='true'],
  .result:hover {
    background: color-mix(in srgb, var(--nogon-indigo) 14%, var(--brutal-card));
  }

  mark {
    color: inherit;
    background: color-mix(in srgb, var(--nogon-amber) 65%, transparent);
    padding: 0 0.08em;
  }
</style>
