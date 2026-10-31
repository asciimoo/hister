<script lang="ts">
  import { mount, unmount } from 'svelte';
  import ArrowLeft from '@lucide/svelte/icons/arrow-left';
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
  import CopyIcon from '@lucide/svelte/icons/copy';
  import ListIcon from '@lucide/svelte/icons/list';
  import { Button, Separator } from '@hister/components';
  import ImageLightbox from '#lib/ImageLightbox.svelte';
  import Seo from '#lib/Seo.svelte';

  let { data } = $props();

  interface TocEntry {
    id: string;
    text: string;
    level: number;
  }

  let toc = $state<TocEntry[]>([]);
  let activeId = $state('');
  const activeEntry = $derived(toc.find((entry) => entry.id === activeId) ?? toc[0]);

  function closePageNavigation(event: MouseEvent) {
    (event.currentTarget as HTMLElement).closest('details')?.removeAttribute('open');
  }

  $effect(() => {
    void data.content;

    const cleanups: (() => void)[] = [];
    for (const pre of document.querySelectorAll<HTMLPreElement>('.doc-content pre')) {
      const code = pre.querySelector('code');
      if (!code) continue;

      const wrapper = document.createElement('div');
      wrapper.className = 'code-block';
      wrapper.style.backgroundColor = pre.style.backgroundColor;
      const controls = document.createElement('div');
      controls.className = 'code-controls';
      const status = document.createElement('span');
      status.setAttribute('role', 'status');
      const button = document.createElement('button');
      button.type = 'button';
      button.title = 'Copy code';
      button.setAttribute('aria-label', 'Copy code block');
      const icon = mount(CopyIcon, { target: button, props: { size: 16, 'aria-hidden': true } });

      controls.append(status, button);
      pre.before(wrapper);
      wrapper.append(pre, controls);

      let disposed = false;
      let resetTimer: ReturnType<typeof setTimeout> | undefined;

      async function copyCode() {
        clearTimeout(resetTimer);
        button.disabled = true;
        status.textContent = 'Copying…';
        try {
          await navigator.clipboard.writeText(code?.textContent ?? '');
          if (disposed) return;
          status.textContent = 'Copied!';
          resetTimer = setTimeout(() => {
            status.textContent = '';
          }, 3000);
        } catch {
          if (disposed) return;
          status.textContent = 'Could not copy. Select the code and copy it manually.';
        } finally {
          if (!disposed) button.disabled = false;
        }
      }

      button.addEventListener('click', copyCode);
      cleanups.push(() => {
        disposed = true;
        clearTimeout(resetTimer);
        button.removeEventListener('click', copyCode);
        void unmount(icon);
        wrapper.replaceWith(pre);
      });
    }

    return () => cleanups.forEach((cleanup) => cleanup());
  });

  $effect(() => {
    // Track data.content as a dependency so this re-runs when navigating between docs
    void data.content;
    activeId = '';

    const article = document.querySelector('[data-doc-content]');
    if (!article) return;

    const headings = article.querySelectorAll('h2, h3');
    const nextToc = Array.from(headings).map((h) => ({
      id: h.id,
      text: h.textContent ?? '',
      level: h.tagName === 'H2' ? 2 : 3,
    }));
    toc = nextToc;
    activeId = nextToc[0]?.id ?? '';

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

<Seo
  title={`${data.meta.title} | Hister Documentation`}
  description={(data.meta.description as string) ??
    `Learn about ${data.meta.title} in the Hister documentation.`}
  path={`/docs/${data.slug}`}
  type="article"
  publishedTime={data.meta.date as string | undefined}
  modifiedTime={(data.meta.updated as string | undefined) ?? (data.meta.date as string | undefined)}
  author="Hister contributors"
  section="Documentation"
  schemaType="TechArticle"
  schemaTitle={data.meta.title as string}
  breadcrumbs={[
    { name: 'Home', path: '/' },
    { name: 'Documentation', path: '/docs' },
    { name: data.meta.title as string, path: `/docs/${data.slug}` },
  ]}
/>

<div class="flex gap-10">
  <article class="min-w-0 flex-1" data-doc-content>
    {#if toc.length > 0}
      <details class="group border-brutal-border bg-brutal-card mb-8 border-[3px] xl:hidden">
        <summary
          class="flex cursor-pointer list-none items-center gap-3 px-4 py-3 [&::-webkit-details-marker]:hidden"
        >
          <ListIcon aria-hidden="true" size={18} class="text-hister-indigo shrink-0" />
          <span class="min-w-0 flex-1">
            <span
              class="font-space block text-[9px] font-bold tracking-[1.5px] text-(--text-secondary) uppercase"
              >On this page</span
            >
            <span class="font-inter block truncate text-sm font-semibold text-(--text-primary)"
              >{activeEntry?.text}</span
            >
          </span>
          <ChevronDownIcon
            aria-hidden="true"
            size={18}
            class="shrink-0 transition-transform group-open:rotate-180"
          />
        </summary>
        <nav
          aria-label="On this page"
          class="border-brutal-border max-h-[50vh] overflow-y-auto border-t-[2px] p-2"
        >
          {#each toc as entry}
            <a
              href="#{entry.id}"
              aria-current={activeId === entry.id ? 'location' : undefined}
              onclick={closePageNavigation}
              class="font-inter block border-l-[3px] py-2 text-sm no-underline transition-colors {entry.level ===
              3
                ? 'pl-6'
                : 'pl-3'} {activeId === entry.id
                ? 'border-hister-indigo bg-hister-indigo/10 font-semibold text-(--text-primary)'
                : 'border-transparent text-(--text-secondary) hover:bg-(--muted-surface) hover:text-(--text-primary)'}"
            >
              {entry.text}
            </a>
          {/each}
        </nav>
      </details>
    {/if}

    <div class="content doc-content">
      <data.content />
    </div>

    <!-- Prev / Next -->
    <Separator class="bg-brutal-border mt-12 h-0.75" />
    <nav
      aria-label="Documentation pagination"
      class="flex flex-col items-stretch justify-between gap-4 pt-8 sm:flex-row sm:items-center"
    >
      {#if data.prev}
        <Button
          variant="ghost"
          href="/docs/{data.prev.slug}"
          class="group flex h-auto w-full items-center justify-start gap-3 rounded-none px-2 py-2 text-(--text-secondary) no-underline transition-colors hover:text-(--text-primary) sm:w-auto"
        >
          <ArrowLeft size={18} class="transition-transform group-hover:-translate-x-1" />
          <div class="flex flex-col items-start">
            <span
              class="font-space text-[10px] font-bold tracking-[2px] text-(--text-secondary) uppercase"
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
          class="group flex h-auto w-full items-center justify-end gap-3 rounded-none px-2 py-2 text-right text-(--text-secondary) no-underline transition-colors hover:text-(--text-primary) sm:w-auto"
        >
          <div class="flex flex-col items-end">
            <span
              class="font-space text-[10px] font-bold tracking-[2px] text-(--text-secondary) uppercase"
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
    <aside class="hidden w-52 shrink-0 xl:block">
      <nav
        aria-label="On this page"
        class="sticky top-6 flex max-h-[calc(100vh-3rem)] flex-col gap-0.5 overflow-y-auto pb-4"
      >
        <span
          class="font-space mb-3 text-[10px] font-bold tracking-[2px] text-(--text-secondary) uppercase"
          >On This Page</span
        >
        {#each toc as entry}
          <Button
            variant="ghost"
            href="#{entry.id}"
            aria-current={activeId === entry.id ? 'location' : undefined}
            class="font-inter h-auto justify-start rounded-none border-l-2 py-1 text-left text-[13px] whitespace-normal no-underline transition-colors
              {entry.level === 3 ? 'pl-5' : 'pl-3'}
              {activeId === entry.id
              ? 'border-hister-indigo font-medium text-(--text-primary)'
              : 'hover:border-brutal-border border-transparent text-(--text-secondary) hover:text-(--text-primary)'}"
          >
            {entry.text}
          </Button>
        {/each}
      </nav>
    </aside>
  {/if}
</div>

<ImageLightbox contentKey={data.slug} />
