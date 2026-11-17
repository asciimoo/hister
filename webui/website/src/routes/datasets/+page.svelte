<script lang="ts">
  import ArrowUpRight from '@lucide/svelte/icons/arrow-up-right';
  import BookOpen from '@lucide/svelte/icons/book-open';
  import Download from '@lucide/svelte/icons/download';
  import GitPullRequest from '@lucide/svelte/icons/git-pull-request';
  import Search from '@lucide/svelte/icons/search';
  import SlidersHorizontal from '@lucide/svelte/icons/sliders-horizontal';
  import Terminal from '@lucide/svelte/icons/terminal';
  import X from '@lucide/svelte/icons/x';
  import Seo from '#lib/Seo.svelte';
  import { focusTrap } from '#lib/focus-trap.js';
  import type { Dataset } from './+page.ts';

  let { data } = $props();

  const allTags = $derived([...new Set(data.datasets.flatMap((d: Dataset) => d.tags))].sort());
  const allLicenses = $derived([...new Set(data.datasets.map((d: Dataset) => d.license))].sort());
  const allAuthors = $derived([...new Set(data.datasets.map((d: Dataset) => d.author))].sort());

  let searchQuery = $state('');
  let selectedTags = $state<string[]>([]);
  let selectedLicense = $state('');
  let selectedAuthor = $state('');
  let mobileFiltersOpen = $state(false);
  let submitModalOpen = $state(false);

  const sampleJson = `{
  "name": "My Dataset Name",
  "description": "A brief description of what the dataset contains.",
  "downloadUrl": "https://example.com/path/to/dataset.json",
  "image": null,
  "tags": ["tag1", "tag2"],
  "license": "CC-BY 4.0",
  "author": "Your Name",
  "diskSizeBytes": 10485760,
  "documentCount": 500,
  "latestUpdate": "2025-01-01"
}`;

  function formatBytes(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  }

  function matchesQuery(d: Dataset, q: string): boolean {
    return (
      d.name.toLowerCase().includes(q) ||
      d.description.toLowerCase().includes(q) ||
      d.license.toLowerCase().includes(q) ||
      d.author.toLowerCase().includes(q) ||
      d.tags.some((t) => t.toLowerCase().includes(q))
    );
  }

  // Full filtered result
  const filtered = $derived(
    data.datasets.filter((d: Dataset) => {
      const q = searchQuery.trim().toLowerCase();
      if (q && !matchesQuery(d, q)) return false;
      if (selectedTags.length > 0 && !selectedTags.every((t) => d.tags.includes(t))) return false;
      if (selectedLicense && d.license !== selectedLicense) return false;
      if (selectedAuthor && d.author !== selectedAuthor) return false;
      return true;
    }),
  );

  // Filtered set ignoring the tag dimension — used to compute per-tag counts
  const baseWithoutTags = $derived(
    data.datasets.filter((d: Dataset) => {
      const q = searchQuery.trim().toLowerCase();
      if (q && !matchesQuery(d, q)) return false;
      if (selectedLicense && d.license !== selectedLicense) return false;
      if (selectedAuthor && d.author !== selectedAuthor) return false;
      return true;
    }),
  );

  // Filtered set ignoring the license dimension
  const baseWithoutLicense = $derived(
    data.datasets.filter((d: Dataset) => {
      const q = searchQuery.trim().toLowerCase();
      if (q && !matchesQuery(d, q)) return false;
      if (selectedTags.length > 0 && !selectedTags.every((t) => d.tags.includes(t))) return false;
      if (selectedAuthor && d.author !== selectedAuthor) return false;
      return true;
    }),
  );

  // Filtered set ignoring the author dimension
  const baseWithoutAuthor = $derived(
    data.datasets.filter((d: Dataset) => {
      const q = searchQuery.trim().toLowerCase();
      if (q && !matchesQuery(d, q)) return false;
      if (selectedTags.length > 0 && !selectedTags.every((t) => d.tags.includes(t))) return false;
      if (selectedLicense && d.license !== selectedLicense) return false;
      return true;
    }),
  );

  // How many results would remain if this tag were toggled on (or kept if already on)
  function tagCount(tag: string): number {
    const tagsToCheck = selectedTags.includes(tag) ? selectedTags : [...selectedTags, tag];
    return baseWithoutTags.filter((d) => tagsToCheck.every((t) => d.tags.includes(t))).length;
  }

  function licenseCount(license: string): number {
    return baseWithoutLicense.filter((d) => d.license === license).length;
  }

  function authorCount(author: string): number {
    return baseWithoutAuthor.filter((d) => d.author === author).length;
  }

  const hasFilters = $derived(
    searchQuery.trim() !== '' ||
      selectedTags.length > 0 ||
      selectedLicense !== '' ||
      selectedAuthor !== '',
  );

  function toggleTag(tag: string) {
    if (selectedTags.includes(tag)) {
      selectedTags = selectedTags.filter((t) => t !== tag);
    } else {
      selectedTags = [...selectedTags, tag];
    }
  }

  function clearFilters() {
    searchQuery = '';
    selectedTags = [];
    selectedLicense = '';
    selectedAuthor = '';
  }

  function closeSubmitModal() {
    submitModalOpen = false;
  }
</script>

<Seo
  title="Datasets | Nogon"
  description="Browse public datasets compatible with Nogon. Filter by tag, license or author and download what you need."
  path="/datasets"
/>

<div class="px-6 py-12 md:px-12">
  <!-- Page header -->
  <div class="mb-10">
    <h1
      class="font-space text-4xl font-black tracking-[-1px] text-(--text-primary) uppercase md:text-5xl"
    >
      Datasets
    </h1>
    <p class="font-inter mt-3 max-w-[70em] text-base text-(--text-secondary)">
      Public datasets you can import into Nogon to extend your local index. <button
        onclick={() => (submitModalOpen = true)}
        aria-haspopup="dialog"
        class="font-space border-brutal-border brutal-press cursor-pointer border-[2px] bg-(--nogon-cyan) px-4 py-2.5 text-[12px] font-bold tracking-[1px] text-white uppercase"
        >Submit new dataset</button
      >
    </p>
  </div>

  <!-- Sidebar + content layout -->
  <div class="flex flex-col gap-8 lg:flex-row lg:items-start lg:gap-10">
    <!-- ── Sidebar (filters) ── -->
    <aside class="lg:w-64 lg:shrink-0 xl:w-72">
      <!-- Mobile toggle -->
      <button
        onclick={() => (mobileFiltersOpen = !mobileFiltersOpen)}
        aria-expanded={mobileFiltersOpen}
        aria-controls="dataset-filters"
        class="font-space border-brutal-border bg-brutal-card mb-4 flex w-full cursor-pointer items-center justify-between border-[3px] px-4 py-3 text-[12px] font-bold tracking-[1.5px] text-(--text-primary) uppercase lg:hidden"
      >
        <span class="flex items-center gap-2">
          <SlidersHorizontal size={14} />
          Filters
          {#if hasFilters}
            <span class="bg-(--nogon-indigo) px-1.5 py-0.5 text-[10px] font-bold text-white">
              {[
                selectedTags.length > 0 ? 1 : 0,
                selectedLicense ? 1 : 0,
                selectedAuthor ? 1 : 0,
                searchQuery.trim() ? 1 : 0,
              ].reduce((a, b) => a + b, 0)}
            </span>
          {/if}
        </span>
        <X size={14} class="transition-transform {mobileFiltersOpen ? '' : 'rotate-45'}" />
      </button>

      <!-- Filter panel -->
      <div
        id="dataset-filters"
        class="border-brutal-border bg-brutal-card flex flex-col gap-6 border-[3px] p-5 lg:sticky lg:top-6 {mobileFiltersOpen
          ? 'block'
          : 'hidden lg:flex'}"
      >
        <!-- Search -->
        <div class="flex flex-col gap-2">
          <label
            for="dataset-search"
            class="font-space font-bold tracking-[1.5px] text-(--text-secondary) uppercase"
            >Search</label
          >
          <div
            class="border-brutal-border focus-within:border-nogon-indigo focus-within:ring-nogon-indigo/35 relative flex items-center border-[2px] transition-[border-color,box-shadow] focus-within:ring-[3px]"
          >
            <span class="pointer-events-none absolute left-3 text-(--text-secondary)">
              <Search size={14} />
            </span>
            <input
              id="dataset-search"
              type="text"
              placeholder="Name or description..."
              bind:value={searchQuery}
              class="font-inter w-full bg-transparent py-2 pr-8 pl-8 text-sm text-(--text-primary) outline-none placeholder:text-(--text-secondary) focus-visible:outline-none"
            />
            {#if searchQuery}
              <button
                onclick={() => (searchQuery = '')}
                class="absolute right-2 cursor-pointer text-(--text-secondary) hover:text-(--text-primary)"
                aria-label="Clear search"
              >
                <X size={12} />
              </button>
            {/if}
          </div>
        </div>

        <!-- Tags -->
        {#if allTags.length > 0}
          <div class="flex flex-col gap-2">
            <span class="font-space font-bold tracking-[1.5px] text-(--text-secondary) uppercase"
              >Tags</span
            >
            <ul class="m-0 flex list-none flex-col gap-1 p-0">
              {#each allTags as tag}
                {@const count = tagCount(tag)}
                {@const active = selectedTags.includes(tag)}
                <li>
                  <button
                    onclick={() => toggleTag(tag)}
                    aria-pressed={active}
                    class="font-inter flex w-full cursor-pointer items-center justify-between gap-2 px-2 py-1.5 text-sm transition-colors {active
                      ? 'bg-(--text-primary) text-white'
                      : count === 0
                        ? 'text-(--text-secondary) opacity-35'
                        : 'text-(--text-primary) hover:bg-(--muted-surface)'}"
                  >
                    <span class="truncate">{tag}</span>
                    <span
                      class="font-space shrink-0 font-bold {active
                        ? 'text-white/70'
                        : 'text-(--text-secondary)'}">{count}</span
                    >
                  </button>
                </li>
              {/each}
            </ul>
          </div>
        {/if}

        <!-- License -->
        {#if allLicenses.length > 0}
          <div class="flex flex-col gap-2">
            <span class="font-space font-bold tracking-[1.5px] text-(--text-secondary) uppercase"
              >License</span
            >
            <ul class="m-0 flex list-none flex-col gap-1 p-0">
              {#each allLicenses as license}
                {@const count = licenseCount(license)}
                {@const active = selectedLicense === license}
                <li>
                  <button
                    onclick={() => (selectedLicense = active ? '' : license)}
                    aria-pressed={active}
                    class="font-inter flex w-full cursor-pointer items-center justify-between gap-2 px-2 py-1.5 text-sm transition-colors {active
                      ? 'bg-(--text-primary) text-white'
                      : count === 0
                        ? 'text-(--text-secondary) opacity-35'
                        : 'text-(--text-primary) hover:bg-(--muted-surface)'}"
                  >
                    <span class="truncate">{license}</span>
                    <span
                      class="font-space shrink-0 font-bold {active
                        ? 'text-white/70'
                        : 'text-(--text-secondary)'}">{count}</span
                    >
                  </button>
                </li>
              {/each}
            </ul>
          </div>
        {/if}

        <!-- Author -->
        {#if allAuthors.length > 0}
          <div class="flex flex-col gap-2">
            <span class="font-space font-bold tracking-[1.5px] text-(--text-secondary) uppercase"
              >Author</span
            >
            <ul class="m-0 flex list-none flex-col gap-1 p-0">
              {#each allAuthors as author}
                {@const count = authorCount(author)}
                {@const active = selectedAuthor === author}
                <li>
                  <button
                    onclick={() => (selectedAuthor = active ? '' : author)}
                    aria-pressed={active}
                    class="font-inter flex w-full cursor-pointer items-center justify-between gap-2 px-2 py-1.5 text-sm transition-colors {active
                      ? 'bg-(--text-primary) text-white'
                      : count === 0
                        ? 'text-(--text-secondary) opacity-35'
                        : 'text-(--text-primary) hover:bg-(--muted-surface)'}"
                  >
                    <span class="truncate">{author}</span>
                    <span
                      class="font-space shrink-0 font-bold {active
                        ? 'text-white/70'
                        : 'text-(--text-secondary)'}">{count}</span
                    >
                  </button>
                </li>
              {/each}
            </ul>
          </div>
        {/if}

        <!-- Clear filters -->
        {#if hasFilters}
          <button
            onclick={clearFilters}
            class="font-space border-brutal-border flex cursor-pointer items-center justify-center gap-1.5 border-[2px] px-3 py-2 text-[11px] font-semibold tracking-[0.5px] text-(--text-secondary) uppercase transition-colors hover:border-(--text-primary) hover:text-(--text-primary)"
          >
            <X size={12} />
            Clear all filters
          </button>
        {/if}
      </div>
    </aside>

    <!-- ── Main content ── -->
    <div class="min-w-0 flex-1">
      <!-- Result count bar -->
      <div class="mb-6 flex items-center justify-between gap-4">
        <p aria-live="polite" class="font-inter text-sm text-(--text-secondary)">
          <span class="font-bold text-(--text-primary)">{filtered.length}</span>
          of {data.datasets.length} dataset{data.datasets.length !== 1 ? 's' : ''}
        </p>
      </div>

      <!-- Cards grid -->
      <ul
        class="m-0 grid list-none [grid-template-columns:repeat(auto-fill,minmax(min(100%,400px),1fr))] gap-6 p-0"
      >
        <li
          aria-labelledby="kiwix-heading"
          class="border-brutal-border bg-brutal-card shadow-brutal mx-auto flex w-full max-w-3xl min-w-0 flex-col border-[3px]"
        >
          <div class="bg-nogon-cyan/5 flex flex-1 flex-col justify-between gap-5 p-5">
            <div class="flex min-w-0 items-start gap-3">
              <div
                class="border-brutal-border bg-nogon-cyan flex size-10 shrink-0 items-center justify-center border-[2px] text-white"
              >
                <BookOpen size={21} aria-hidden="true" />
              </div>
              <div class="min-w-0">
                <h2
                  id="kiwix-heading"
                  class="font-space text-lg leading-tight font-extrabold text-(--text-primary)"
                >
                  Explore Kiwix
                </h2>
                <p class="font-inter mt-1 text-sm leading-relaxed text-(--text-secondary)">
                  An external dataset library offering Wikipedia, reference works, and educational
                  collections. Download ZIM archives from Kiwix, then import them into Nogon.
                </p>
              </div>
            </div>
            <a
              href="https://browse.library.kiwix.org/#lang=eng"
              target="_blank"
              rel="noopener noreferrer"
              class="font-space border-brutal-border brutal-press bg-nogon-cyan inline-flex shrink-0 items-center justify-center gap-2 self-start border-[2px] px-3 py-2 text-xs font-bold tracking-[0.5px] text-white uppercase no-underline"
            >
              Browse Kiwix
              <ArrowUpRight size={16} aria-hidden="true" />
              <span class="sr-only">(opens in a new tab)</span>
            </a>
          </div>
          <div
            class="border-brutal-border flex min-w-0 flex-col gap-2 border-t-[2px] bg-[#2b2b2b] px-5 py-3 text-white"
          >
            <div class="flex min-w-0 items-center gap-2">
              <Terminal size={16} aria-hidden="true" class="shrink-0 text-white/60" />
              <code
                class="font-fira min-w-0 text-xs leading-6 break-words select-all sm:text-[13px]"
                >nogon import zim <span class="text-[#b9d8ce]">collection.zim</span></code
              >
            </div>
            <a
              href="/docs/import#importing-kiwix-archives"
              class="font-space shrink-0 self-start text-xs leading-6 font-semibold text-white/80 underline underline-offset-4 hover:text-white"
              >ZIM import guide</a
            >
          </div>
        </li>
        {#if filtered.length > 0}
          {#each filtered as dataset (dataset.slug)}
            <li
              class="border-brutal-border brutal-press-card bg-brutal-card flex flex-col border-[3px]"
            >
              {#if dataset.image}
                <div class="border-brutal-border overflow-hidden border-b-[3px]">
                  <img src={dataset.image} alt={dataset.name} class="h-40 w-full object-cover" />
                </div>
              {/if}

              <div class="flex flex-1 flex-col gap-3 p-6">
                <h2
                  class="font-space text-lg leading-tight font-extrabold tracking-[0.5px] text-(--text-primary)"
                >
                  {dataset.name}
                </h2>

                <div class="flex flex-wrap gap-2">
                  <span
                    class="font-space border-brutal-border border-[2px] bg-(--nogon-teal) px-2 py-0.5 text-[10px] font-bold tracking-[1px] text-white uppercase"
                  >
                    {dataset.license}
                  </span>
                  <span
                    class="font-space border-brutal-border border-[2px] bg-transparent px-2 py-0.5 text-[10px] font-semibold tracking-[0.5px] text-(--text-secondary) uppercase"
                  >
                    {dataset.author}
                  </span>
                </div>

                <p class="font-inter flex-1 text-sm leading-relaxed text-(--text-secondary)">
                  {dataset.description}
                </p>

                {#if dataset.documentCount !== null || dataset.diskSizeBytes !== null || dataset.latestUpdate !== null}
                  <dl
                    class="font-inter grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1 text-[12px] text-(--text-secondary)"
                  >
                    {#if dataset.documentCount !== null}
                      <dt class="font-space font-semibold tracking-[0.5px] uppercase">Documents</dt>
                      <dd class="m-0">{dataset.documentCount.toLocaleString()}</dd>
                    {/if}
                    {#if dataset.diskSizeBytes !== null}
                      <dt class="font-space font-semibold tracking-[0.5px] uppercase">Size</dt>
                      <dd class="m-0">{formatBytes(dataset.diskSizeBytes)}</dd>
                    {/if}
                    {#if dataset.latestUpdate !== null}
                      <dt class="font-space font-semibold tracking-[0.5px] uppercase">Updated</dt>
                      <dd class="m-0">{dataset.latestUpdate}</dd>
                    {/if}
                  </dl>
                {/if}

                {#if dataset.tags.length > 0}
                  <div class="flex flex-wrap gap-1.5">
                    {#each dataset.tags as tag}
                      <button
                        onclick={() => toggleTag(tag)}
                        aria-pressed={selectedTags.includes(tag)}
                        class="font-space cursor-pointer border-[1.5px] px-2 py-0.5 text-[10px] font-semibold tracking-[0.5px] uppercase transition-all {selectedTags.includes(
                          tag,
                        )
                          ? 'border-brutal-border bg-(--text-primary) text-white'
                          : 'border-(--text-secondary) text-(--text-secondary) hover:border-(--text-primary) hover:text-(--text-primary)'}"
                      >
                        {tag}
                      </button>
                    {/each}
                  </div>
                {/if}
              </div>

              <div class="border-brutal-border border-t-[3px] p-4">
                <a
                  href={dataset.downloadUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  class="font-space border-brutal-border brutal-press flex w-full items-center justify-center gap-2 border-[2px] bg-(--nogon-indigo) px-4 py-2.5 text-[12px] font-bold tracking-[1px] text-white uppercase no-underline"
                >
                  <Download size={14} class="shrink-0" />
                  Download
                </a>
              </div>
            </li>
          {/each}
        {:else}
          <li
            class="border-brutal-border bg-brutal-card flex flex-col items-center justify-center gap-4 border-[3px] px-6 py-16 text-center"
          >
            <p class="font-space text-lg font-bold text-(--text-secondary) uppercase">
              No datasets match your filters.
            </p>
            <button
              onclick={clearFilters}
              class="font-space border-brutal-border cursor-pointer border-[2px] px-5 py-2 text-[12px] font-semibold tracking-[1px] text-(--text-primary) uppercase transition-colors hover:bg-(--text-primary) hover:text-white"
            >
              Clear filters
            </button>
          </li>
        {/if}
      </ul>
    </div>
  </div>
</div>

<!-- Submit dataset modal -->
{#if submitModalOpen}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/70 p-4 md:p-10"
    onclick={(e) => {
      if (e.target === e.currentTarget) closeSubmitModal();
    }}
    onkeydown={(e) => {
      if (e.target === e.currentTarget && e.key === 'Escape') closeSubmitModal();
    }}
  >
    <div
      id="submit-dataset-dialog"
      use:focusTrap={{ onEscape: closeSubmitModal }}
      role="dialog"
      aria-modal="true"
      aria-labelledby="submit-modal-title"
      aria-describedby="submit-modal-description"
      tabindex="-1"
      class="border-brutal-border bg-brutal-card relative my-auto w-full max-w-2xl border-[3px] shadow-[8px_8px_0_var(--brutal-border)]"
    >
      <!-- Header -->
      <div class="border-brutal-border flex items-center justify-between border-b-[3px] px-6 py-4">
        <h2
          id="submit-modal-title"
          class="font-space text-xl font-black tracking-[0.5px] text-(--text-primary) uppercase"
        >
          Submit a New Dataset
        </h2>
        <button
          onclick={closeSubmitModal}
          aria-label="Close"
          class="cursor-pointer text-(--text-secondary) transition-colors hover:text-(--text-primary)"
        >
          <X size={18} />
        </button>
      </div>

      <!-- Body -->
      <div class="flex flex-col gap-6 px-6 py-6">
        <a
          href="https://github.com/asciimoo/nogon"
          target="_blank"
          rel="noopener noreferrer"
          class="font-space border-brutal-border brutal-press flex w-full items-center justify-center gap-2 self-start border-[2px] bg-(--nogon-indigo) px-5 py-2.5 text-[12px] font-bold tracking-[1px] text-white uppercase no-underline"
        >
          <GitPullRequest size={14} class="shrink-0" />
          Open a Pull Request
        </a>
        <p
          id="submit-modal-description"
          class="font-inter text-sm leading-relaxed text-(--text-secondary)"
        >
          To add your dataset to this page, open a pull request on
          <a
            href="https://github.com/asciimoo/nogon"
            target="_blank"
            rel="noopener noreferrer"
            class="text-(--text-primary) underline">github.com/asciimoo/nogon</a
          >
          and add a JSON file at
          <code class="bg-(--muted-surface) px-1 py-0.5 text-[12px] text-(--text-primary)"
            >webui/website/src/content/datasets/your-dataset-name.json</code
          >
          following the format below.
        </p>

        <div class="flex flex-col gap-2">
          <span
            class="font-space text-[11px] font-bold tracking-[1.5px] text-(--text-secondary) uppercase"
            >JSON Format</span
          >
          <pre
            class="border-brutal-border overflow-x-auto border-[2px] bg-black p-4 text-[13px] leading-relaxed text-green-400">{sampleJson}</pre>
        </div>

        <dl class="font-inter grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1.5 text-sm">
          {#each [['name', 'Display name of the dataset (string, required)'], ['description', 'Short description of the dataset contents (string, required)'], ['downloadUrl', 'Direct URL to the Nogon dataset JSON file (string, required)'], ['image', 'Optional cover image URL, or null (string | null)'], ['tags', 'List of topic tags (string[], required)'], ['license', 'License identifier, e.g. "CC-BY 4.0" (string, required)'], ['author', 'Name of the dataset author or publisher (string, required)'], ['diskSizeBytes', 'File size of the download in bytes (number, required)'], ['documentCount', 'Number of documents in the dataset (number, required)'], ['latestUpdate', 'Date the dataset was last updated, YYYY-MM-DD (string, required)']] as [field, desc]}
            <dt
              class="font-space font-semibold tracking-[0.5px] whitespace-nowrap text-(--text-primary)"
            >
              {field}
            </dt>
            <dd class="m-0 text-(--text-secondary)">{desc}</dd>
          {/each}
        </dl>
      </div>
    </div>
  </div>
{/if}
