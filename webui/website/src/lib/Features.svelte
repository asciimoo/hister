<script lang="ts">
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import Bot from '@lucide/svelte/icons/bot';
  import Boxes from '@lucide/svelte/icons/boxes';
  import FileStack from '@lucide/svelte/icons/files';
  import Languages from '@lucide/svelte/icons/languages';
  import SearchCheck from '@lucide/svelte/icons/search-check';
  import ServerCog from '@lucide/svelte/icons/server-cog';
  import SlidersHorizontal from '@lucide/svelte/icons/sliders-horizontal';

  const groups = [
    {
      icon: FileStack,
      title: 'Index automatically',
      description:
        'Browser extensions can index visited pages automatically. File watching, history imports, and the crawler add other sources.',
      features: ['Browser extensions', 'Local file watching', 'History import', 'Website crawler'],
      color: 'var(--nogon-coral)',
    },
    {
      icon: SearchCheck,
      title: 'Search precisely',
      description:
        'Full text search supports field filters, quoted phrases, wildcards, negation, date ranges, and query aliases.',
      features: ['Field filters', 'Quoted phrases', 'Wildcards and negation', 'Query aliases'],
      color: 'var(--nogon-cyan)',
    },
    {
      icon: Boxes,
      title: 'Extract content',
      description:
        'Content extractors handle structured data from supported formats and websites. Semantic search is optional.',
      features: [
        'Content extractors',
        'Semantic search',
        'Language aware indexes',
        'Readable previews',
      ],
      color: 'var(--nogon-amber)',
    },
    {
      icon: SlidersHorizontal,
      title: 'Apply index rules',
      description:
        'Skip and priority rules control indexing and ranking. Versioning can retain earlier document content.',
      features: ['Skip rules', 'Priority rules', 'Version tracking', 'Sensitive content checks'],
      color: 'var(--nogon-rose)',
    },
    {
      icon: Bot,
      title: 'Use multiple interfaces',
      description:
        'The same index is available through the web interface, terminal client, CLI, HTTP API, and MCP server.',
      features: ['Web interface', 'Terminal interface', 'HTTP API and CLI', 'MCP server'],
      color: 'var(--nogon-lime)',
    },
    {
      icon: ServerCog,
      title: 'Choose a deployment',
      description:
        'A single binary can run locally. Shared servers support user scoped access with SQLite or PostgreSQL.',
      features: [
        'No config quickstart',
        'SQLite or PostgreSQL',
        'Multiple users',
        'Docker and Nix',
      ],
      color: 'var(--nogon-indigo)',
    },
  ];
</script>

<section
  id="features"
  class="border-brutal-border border-b-[3px] bg-[#eeeae1] px-6 py-16 md:px-12 md:py-24"
>
  <div class="mx-auto max-w-[1400px]">
    <header class="flex flex-col gap-7 lg:flex-row lg:items-end lg:justify-between">
      <div>
        <div class="mb-5 flex items-center gap-3">
          <span class="bg-nogon-indigo h-3 w-3"></span>
          <p
            class="font-fira text-xs font-bold tracking-[1.8px] text-[var(--text-secondary)] uppercase"
          >
            Built for real recall
          </p>
        </div>
        <h2
          class="font-outfit max-w-[850px] text-4xl leading-[0.92] font-black tracking-[-0.04em] text-[var(--text-primary)] uppercase md:text-6xl lg:text-7xl"
        >
          One index. Many ways back in.
        </h2>
      </div>
      <p class="max-w-[470px] text-base leading-[1.75] text-[var(--text-secondary)] lg:text-right">
        Hister indexes visited pages, watched files, imported browser history, and crawled websites.
        The index is available through web, terminal, CLI, HTTP API, and MCP interfaces.
      </p>
    </header>

    <div class="mt-12 grid gap-6 md:grid-cols-2 lg:mt-16 lg:grid-cols-3">
      {#each groups as group}
        <article
          class="feature-group border-brutal-border bg-brutal-card relative flex min-h-[420px] flex-col overflow-hidden border-[3px] p-7 shadow-[7px_7px_0_var(--brutal-shadow)]"
          style="--feature-color: {group.color}"
        >
          <div class="feature-accent absolute inset-x-0 top-0 h-3 bg-[var(--feature-color)]"></div>
          <div class="mt-4 flex items-center gap-5">
            <div
              class="border-brutal-border flex h-14 w-14 shrink-0 items-center justify-center border-[3px] bg-[var(--feature-color)]"
            >
              <svelte:component this={group.icon} size={26} strokeWidth={2.2} />
            </div>
            <h3
              class="font-space text-2xl leading-tight font-black tracking-[-0.02em] text-[var(--text-primary)] uppercase"
            >
              {group.title}
            </h3>
          </div>
          <p class="mt-4 text-sm leading-[1.7] text-[var(--text-secondary)]">
            {group.description}
          </p>

          <ul class="mt-auto grid list-none gap-x-4 gap-y-2 p-0 pt-8 sm:grid-cols-2">
            {#each group.features as feature}
              <li
                class="font-fira flex items-start gap-2 border-t border-[var(--text-primary)]/15 pt-2 text-[10px] leading-tight font-semibold text-[var(--text-primary)]"
              >
                <span class="mt-0.5 h-2 w-2 shrink-0 bg-[var(--feature-color)]"></span>
                {feature}
              </li>
            {/each}
          </ul>
        </article>
      {/each}
    </div>

    <div
      class="border-brutal-border bg-brutal-card shadow-brutal-lg mt-12 grid border-[3px] md:grid-cols-[auto_1fr_auto] md:items-center"
    >
      <div
        class="border-brutal-border bg-nogon-teal flex h-full items-center justify-center border-b-[3px] p-5 text-white md:border-r-[3px] md:border-b-0"
      >
        <Languages size={28} />
      </div>
      <div class="px-6 py-5">
        <h3 class="font-space text-base font-black text-[var(--text-primary)] uppercase">
          Additional capabilities
        </h3>
        <p class="mt-1 text-sm leading-relaxed text-[var(--text-secondary)]">
          Hister also supports multiple crawler backends, language specific indexes, content
          versioning, ownership rules, and configurable extractors.
        </p>
      </div>
      <a
        href="/docs"
        class="font-space border-brutal-border shadow-brutal-sm m-5 inline-flex items-center justify-center gap-2 border-[2px] bg-[var(--text-primary)] px-5 py-3 text-xs font-bold tracking-[1px] text-white uppercase no-underline transition-all hover:translate-x-[2px] hover:translate-y-[2px] hover:shadow-none"
      >
        Explore docs
        <ArrowRight size={15} />
      </a>
    </div>
  </div>
</section>

<style>
  .feature-group {
    transition:
      translate 200ms ease,
      box-shadow 200ms ease;
  }

  .feature-group:hover {
    translate: 3px 3px;
    box-shadow: 4px 4px 0 var(--feature-color);
  }

  .feature-accent {
    transform-origin: left;
    transition: scale 260ms ease;
  }

  .feature-group:hover .feature-accent {
    scale: 0.72 1;
  }

  @media (prefers-reduced-motion: reduce) {
    .feature-group,
    .feature-accent {
      transition: none;
    }
  }
</style>
