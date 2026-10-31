<script lang="ts">
  import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
  import ArrowRightIcon from '@lucide/svelte/icons/arrow-right';

  interface DocEntry {
    slug: string;
    title: string;
  }

  interface DocCategory {
    name: string;
    color: string;
    docs: DocEntry[];
  }

  let {
    categories,
    currentSlug,
  }: {
    categories: DocCategory[];
    currentSlug: string;
  } = $props();
</script>

<div class="flex flex-col gap-4">
  <a
    href="/docs"
    class="font-space border-brutal-border bg-brutal-card flex items-center gap-2 border-[2px] px-3 py-2.5 text-[11px] font-bold tracking-[1.25px] text-(--text-primary) uppercase no-underline transition-colors hover:bg-(--muted-surface)"
  >
    <ArrowLeftIcon size={15} />
    All documentation
  </a>

  <nav aria-label="Documentation" class="flex flex-col gap-5">
    {#each categories as category}
      <section>
        <div class="mb-1.5 flex items-center gap-2 px-1">
          <span aria-hidden="true" class="h-2 w-2 shrink-0 bg-hister-{category.color}"></span>
          <h2
            class="font-inter text-sm font-bold {category.docs.some(
              (doc) => doc.slug === currentSlug,
            )
              ? 'text-(--text-primary)'
              : 'text-(--text-secondary)'}"
          >
            {category.name}
          </h2>
        </div>

        <div class="flex flex-col gap-0.5">
          {#each category.docs as doc}
            <a
              href="/docs/{doc.slug}"
              aria-current={doc.slug === currentSlug ? 'page' : undefined}
              class="font-inter flex items-center justify-between gap-2 border-l-[3px] px-3 py-2 text-sm no-underline transition-colors {doc.slug ===
              currentSlug
                ? 'border-hister-indigo bg-hister-indigo/10 text-(--text-primary)'
                : 'hover:border-brutal-border border-transparent text-(--text-secondary) hover:bg-(--muted-surface) hover:text-(--text-primary)'}"
            >
              <span>{doc.title}</span>
              {#if doc.slug === currentSlug}
                <ArrowRightIcon aria-hidden="true" size={14} class="text-hister-indigo shrink-0" />
              {/if}
            </a>
          {/each}
        </div>
      </section>
    {/each}
  </nav>
</div>
