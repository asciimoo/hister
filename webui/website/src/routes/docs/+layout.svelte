<script lang="ts">
  import { page } from '$app/state';
  import BookOpenIcon from '@lucide/svelte/icons/book-open';
  import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
  import DocsNavigation from '#lib/DocsNavigation.svelte';
  import DocsSearch from '#lib/DocsSearch.svelte';

  let { children, data } = $props();

  const isIndex = $derived(page.url.pathname === '/docs' || page.url.pathname === '/docs/');

  const currentDoc = $derived(
    !isIndex ? data.docs.find((d) => page.url.pathname === `/docs/${d.slug}`) : null,
  );
  const currentCategory = $derived(
    !isIndex
      ? data.categories.find((category) =>
          category.docs.some((doc) => doc.slug === currentDoc?.slug),
        )
      : null,
  );
</script>

{#if isIndex}
  {@render children()}
{:else}
  <!-- Dark header banner -->
  <header class="w-full bg-(--text-primary) px-6 py-4 md:py-14">
    <div class="mx-auto max-w-[1920px]">
      <nav
        aria-label="Breadcrumb"
        class="font-space mb-2 flex items-center gap-2 text-[11px] font-bold tracking-[2px] text-white/65 uppercase md:mb-4"
      >
        <a
          href="/docs"
          class="font-space text-[11px] font-bold tracking-[2px] text-white/65 no-underline transition-colors hover:text-white"
          >Docs</a
        >
        <span aria-hidden="true">/</span>
        <span aria-current="page" class="text-white/80">{currentDoc?.title}</span>
      </nav>
      <h1
        class="font-space text-3xl leading-tight font-black tracking-[-1px] text-white md:text-5xl"
      >
        {currentDoc?.title}
      </h1>
      <div class="mt-4 md:mt-7">
        <DocsSearch tone="dark" />
      </div>
    </div>
  </header>

  <div class="border-brutal-border bg-brutal-bg sticky top-0 z-30 border-b-[3px] md:hidden">
    <details class="group">
      <summary
        class="flex cursor-pointer list-none items-center gap-3 px-6 py-3.5 [&::-webkit-details-marker]:hidden"
      >
        <BookOpenIcon aria-hidden="true" size={19} class="text-hister-indigo shrink-0" />
        <span class="min-w-0 flex-1">
          <span
            class="font-space block text-[9px] font-bold tracking-[1.5px] text-(--text-secondary) uppercase"
            >{currentCategory?.name ?? 'Documentation'}</span
          >
          <span class="font-inter block truncate text-sm font-semibold text-(--text-primary)"
            >{currentDoc?.title}</span
          >
        </span>
        <span
          class="font-space text-[9px] font-bold tracking-[1.25px] text-(--text-secondary) uppercase"
          >Browse</span
        >
        <ChevronDownIcon
          aria-hidden="true"
          size={18}
          class="shrink-0 transition-transform group-open:rotate-180"
        />
      </summary>
      <div
        class="border-brutal-border max-h-[70vh] overflow-y-auto border-t-[2px] bg-(--page-bg) p-4"
      >
        <DocsNavigation categories={data.categories} currentSlug={currentDoc?.slug ?? ''} />
      </div>
    </details>
  </div>

  <div class="mx-auto flex max-w-[1920px] flex-col gap-10 px-6 py-6 md:flex-row md:px-12 md:py-10">
    <aside class="hidden w-64 shrink-0 md:block">
      <div class="sticky top-6 max-h-[calc(100vh-3rem)] overflow-y-auto pr-2 pb-4">
        <DocsNavigation categories={data.categories} currentSlug={currentDoc?.slug ?? ''} />
      </div>
    </aside>

    <div class="min-w-0 flex-1">
      {@render children()}
    </div>
  </div>
{/if}
