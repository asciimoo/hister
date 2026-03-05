<script lang="ts">
  import ArrowLeft from '@lucide/svelte/icons/arrow-left';
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import { Button, Separator } from '@hister/components';

  let { data } = $props();

  interface TocEntry {
    id: string;
    text: string;
    level: number;
  }

  let toc = $state<TocEntry[]>([]);
  let activeId = $state('');

  $effect(() => {
    // Track data.content as a dependency so this re-runs when navigating between docs
    void data.content;
    activeId = '';

    const article = document.querySelector('[data-doc-content]');
    if (!article) return;

    const headings = article.querySelectorAll('h2, h3');
    toc = Array.from(headings).map((h) => ({
      id: h.id,
      text: h.textContent ?? '',
      level: h.tagName === 'H2' ? 2 : 3,
    }));

    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) {
            activeId = entry.target.id;
          }
        }
      },
      { rootMargin: '-80px 0px -60% 0px', threshold: 0 },
    );

    headings.forEach((h) => observer.observe(h));
    return () => observer.disconnect();
  });
</script>

<svelte:head>
  <title>{data.meta.title} | Hister Documentation</title>
</svelte:head>

<div class="flex gap-10">
  <article class="flex-1 min-w-0" data-doc-content>
    <div class="content doc-content">
      <data.content />
    </div>

    <!-- Prev / Next -->
    <Separator class="mt-12 bg-brutal-border h-[3px]" />
    <nav class="pt-8 flex items-center justify-between gap-4">
      {#if data.prev}
        <Button
          variant="ghost"
          href="/docs/{data.prev.slug}"
          class="group flex items-center gap-3 no-underline text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-colors h-auto rounded-none px-2 py-2"
        >
          <ArrowLeft size={18} class="transition-transform group-hover:-translate-x-1" />
          <div class="flex flex-col items-start">
            <span
              class="font-space text-[10px] font-bold tracking-[2px] uppercase text-[var(--text-secondary)]"
              >Previous</span
            >
            <span class="font-inter text-sm font-semibold">{data.prev.title}</span>
          </div>
        </Button>
      {:else}
        <div></div>
      {/if}

      {#if data.next}
        <Button
          variant="ghost"
          href="/docs/{data.next.slug}"
          class="group flex items-center gap-3 no-underline text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-colors text-right h-auto rounded-none px-2 py-2"
        >
          <div class="flex flex-col items-end">
            <span
              class="font-space text-[10px] font-bold tracking-[2px] uppercase text-[var(--text-secondary)]"
              >Next</span
            >
            <span class="font-inter text-sm font-semibold">{data.next.title}</span>
          </div>
          <ArrowRight size={18} class="transition-transform group-hover:translate-x-1" />
        </Button>
      {:else}
        <div></div>
      {/if}
    </nav>
  </article>

  <!-- TOC Sidebar (xl only) -->
  {#if toc.length > 0}
    <aside class="w-48 shrink-0 hidden xl:block">
      <nav class="sticky top-24 flex flex-col gap-0.5">
        <span
          class="font-space text-[10px] font-bold tracking-[2px] uppercase text-[var(--text-secondary)] mb-3"
          >On This Page</span
        >
        {#each toc as entry}
          <Button
            variant="ghost"
            href="#{entry.id}"
            class="justify-start font-inter text-[13px] py-1 no-underline transition-colors border-l-2 h-auto rounded-none
              {entry.level === 3 ? 'pl-5' : 'pl-3'}
              {activeId === entry.id
              ? 'border-hister-indigo text-[var(--text-primary)] font-medium'
              : 'border-transparent text-[var(--text-secondary)] hover:text-[var(--text-primary)] hover:border-brutal-border'}"
          >
            {entry.text}
          </Button>
        {/each}
      </nav>
    </aside>
  {/if}
</div>
