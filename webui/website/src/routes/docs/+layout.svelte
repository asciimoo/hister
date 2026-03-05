<script lang="ts">
  import { page } from '$app/state';

  let { children, data } = $props();

  const isIndex = $derived(page.url.pathname === '/docs' || page.url.pathname === '/docs/');

  const currentDoc = $derived(
    !isIndex ? data.docs.find((d) => page.url.pathname === `/docs/${d.slug}`) : null,
  );

  const categoryColors: Record<string, string> = {
    'Getting Started': 'bg-hister-indigo',
    Reference: 'bg-hister-teal',
    Deployment: 'bg-hister-coral',
  };
</script>

{#if isIndex}
  {@render children()}
{:else}
  <!-- Dark header banner -->
  <header class="w-full bg-[var(--text-primary)] px-6 py-10 md:py-14">
    <div class="max-w-7xl mx-auto">
      <nav
        class="flex items-center gap-2 font-space text-[11px] font-bold tracking-[2px] uppercase text-white/40 mb-4"
      >
        <a
          href="/docs"
          class="hover:text-white/60 text-white/40 transition-colors no-underline font-space text-[11px] font-bold tracking-[2px]"
          >Docs</a
        >
        <span>/</span>
        <span class="text-white/70">{currentDoc?.title}</span>
      </nav>
      <h1
        class="font-space text-3xl md:text-5xl font-black text-white tracking-[-1px] leading-tight"
      >
        {currentDoc?.title}
      </h1>
    </div>
  </header>

  <!-- Sidebar + Content -->
  <div class="max-w-7xl mx-auto px-6 md:px-12 py-10 flex flex-col md:flex-row gap-10">
    <aside class="md:w-56 shrink-0 hidden md:block">
      <nav class="md:sticky md:top-24 flex flex-col gap-5">
        {#each data.categories as category}
          <div class="flex flex-col gap-1">
            <div class="flex items-center gap-2 mb-1">
              <div class="w-2 h-2 {categoryColors[category.name] ?? 'bg-brutal-border'}"></div>
              <span
                class="font-space text-[10px] font-bold tracking-[2px] uppercase text-[var(--text-secondary)]"
                >{category.name}</span
              >
            </div>
            {#each category.docs as doc}
              <a
                href="/docs/{doc.slug}"
                class="font-inter text-sm py-2 px-3 no-underline border-l-[3px] transition-colors {page
                  .url.pathname === `/docs/${doc.slug}`
                  ? 'border-hister-indigo text-[var(--text-primary)] font-semibold bg-hister-indigo/5'
                  : 'border-transparent text-[var(--text-secondary)] hover:text-[var(--text-primary)] hover:border-brutal-border'}"
              >
                {doc.title}
              </a>
            {/each}
          </div>
        {/each}
      </nav>
    </aside>

    <main class="flex-1 min-w-0">
      {@render children()}
    </main>
  </div>
{/if}
