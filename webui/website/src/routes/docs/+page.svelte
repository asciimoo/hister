<script lang="ts">
  import ArrowRightIcon from '@lucide/svelte/icons/arrow-right';
  import ArrowUpRightIcon from '@lucide/svelte/icons/arrow-up-right';
  import Globe from '@lucide/svelte/icons/globe';
  import * as Card from '@hister/components/ui/card';
  import DocsSearch from '#lib/DocsSearch.svelte';
  import Seo from '#lib/Seo.svelte';

  let { data } = $props();
  const setupSteps = [
    {
      title: 'Install Hister',
      description: 'Install and start a local Hister server.',
      href: '/docs/installing',
    },
    {
      title: 'Connect your browser',
      description: 'Add the extension and check its connection.',
      href: '/docs/quickstart#installing-a-browser-extension',
    },
    {
      title: 'Find your first page',
      description: 'Visit a page, then search for its contents.',
      href: '/docs/quickstart#accessing-the-web-interface',
    },
  ];
</script>

<Seo
  title="Documentation | Hister"
  description="Learn how to install, configure, and use Hister across the web, terminal, browser extensions, API, and MCP."
  path="/docs"
/>

<section class="mx-auto max-w-4xl px-6 py-12 md:px-12">
  <h1
    class="font-space text-3xl font-black tracking-[-1px] text-(--text-primary) uppercase sm:text-4xl md:text-5xl"
  >
    Documentation
  </h1>
  <div class="mt-6 mb-10">
    <DocsSearch />
  </div>

  <section
    aria-labelledby="getting-started-heading"
    class="border-brutal-border bg-hister-indigo/10 mb-10 border-[3px] p-5 sm:p-6"
  >
    <h2
      id="getting-started-heading"
      class="font-space text-2xl font-extrabold text-(--text-primary)"
    >
      Start searching with Hister
    </h2>
    <p class="font-inter mt-2 text-sm leading-relaxed text-(--text-secondary)">
      Set up your own search engine in three steps. No configuration file needed.
    </p>
    <ol class="m-0 mt-6 grid list-none gap-4 p-0 md:grid-cols-3">
      {#each setupSteps as step, i}
        <li>
          <Card.Root href={step.href} class="bg-brutal-card h-full p-4">
            <span
              aria-hidden="true"
              class="font-fira border-brutal-border bg-hister-indigo mb-3 flex size-8 items-center justify-center border-[2px] text-sm font-bold text-white"
              >{i + 1}</span
            >
            <h3 class="font-space flex items-center justify-between gap-2 text-base font-bold">
              {step.title}
              <ArrowRightIcon aria-hidden="true" size={16} class="shrink-0" />
            </h3>
            <p class="font-inter mt-2 text-sm leading-relaxed text-(--text-secondary)">
              {step.description}
            </p>
          </Card.Root>
        </li>
      {/each}
    </ol>
  </section>

  {#each data.categories as category, i}
    <section
      aria-labelledby="docs-category-{i}"
      class="docs-category"
      style="--section-accent: var(--hister-{category.color})"
    >
      <div class="docs-category-heading">
        <span class="docs-category-mark" aria-hidden="true"></span>
        <h2 id="docs-category-{i}">{category.name}</h2>
        <span class="docs-category-rule" aria-hidden="true"></span>
      </div>

      <ul class="docs-list">
        {#each category.docs as doc}
          <li>
            <a href="/docs/{doc.slug}" class="docs-link">
              <div class="docs-copy">
                <h3>{doc.title}</h3>
                {#if doc.description}
                  <p>{doc.description}</p>
                {/if}
              </div>
              <span class="docs-arrow" aria-hidden="true"><ArrowRightIcon size={18} /></span>
            </a>
          </li>
        {/each}
      </ul>
    </section>
  {/each}

  <section
    aria-labelledby="browser-extensions-heading"
    class="docs-category"
    style="--section-accent: var(--hister-indigo)"
  >
    <div class="docs-category-heading">
      <span class="docs-category-mark" aria-hidden="true"></span>
      <h2 id="browser-extensions-heading">Browser Extensions</h2>
      <span class="docs-category-rule" aria-hidden="true"></span>
    </div>

    <ul class="docs-list">
      <li>
        <a
          href="https://chromewebstore.google.com/detail/hister/cciilamhchpmbdnniabclekddabkifhb"
          target="_blank"
          rel="noopener noreferrer"
          class="docs-link"
        >
          <Globe size={20} aria-hidden="true" class="shrink-0 text-(--section-accent)" />
          <div class="docs-copy"><h3>Chrome Extension</h3></div>
          <span class="docs-arrow" aria-hidden="true"><ArrowUpRightIcon size={18} /></span>
          <span class="sr-only">(opens in a new tab)</span>
        </a>
      </li>
      <li>
        <a
          href="https://addons.mozilla.org/en-US/firefox/addon/hister/"
          target="_blank"
          rel="noopener noreferrer"
          class="docs-link"
        >
          <Globe size={20} aria-hidden="true" class="shrink-0 text-(--section-accent)" />
          <div class="docs-copy"><h3>Firefox Add-on</h3></div>
          <span class="docs-arrow" aria-hidden="true"><ArrowUpRightIcon size={18} /></span>
          <span class="sr-only">(opens in a new tab)</span>
        </a>
      </li>
    </ul>
  </section>
</section>

<style>
  .docs-category {
    margin-bottom: 2.75rem;
  }

  .docs-category-heading {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    margin-bottom: 0.75rem;
  }

  .docs-category-heading h2 {
    font-family: var(--font-outfit);
    font-size: 1.75rem;
    font-weight: 800;
    line-height: 1.2;
    letter-spacing: -0.025em;
  }

  .docs-category-mark {
    width: 0.35rem;
    height: 1.75rem;
    flex-shrink: 0;
    background: var(--section-accent);
  }

  .docs-category-rule {
    height: 1px;
    flex: 1;
    margin-left: 0.25rem;
    background: color-mix(in srgb, var(--section-accent) 25%, transparent);
  }

  .docs-list {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .docs-list > li + li {
    border-top: 1px solid color-mix(in srgb, var(--text-primary) 12%, transparent);
  }

  .docs-link {
    position: relative;
    display: flex;
    align-items: center;
    gap: 1rem;
    margin-inline: -0.75rem;
    padding: 1rem 0.75rem;
    color: var(--text-primary);
    text-decoration: none;
    transition: background-color 160ms ease;
  }

  .docs-link::before {
    position: absolute;
    inset-block: 1rem;
    left: 0;
    width: 2px;
    background: var(--section-accent);
    content: '';
    opacity: 0;
    transition: opacity 160ms ease;
  }

  .docs-copy {
    min-width: 0;
    flex: 1;
  }

  .docs-copy h3 {
    font-family: var(--font-outfit);
    font-size: 1.125rem;
    font-weight: 700;
    line-height: 1.4;
  }

  .docs-copy p {
    margin-top: 0.25rem;
    color: var(--text-secondary);
    font-size: 0.875rem;
    line-height: 1.6;
  }

  .docs-arrow {
    display: flex;
    flex-shrink: 0;
    color: var(--text-secondary);
    opacity: 0.5;
    transition:
      color 160ms ease,
      opacity 160ms ease,
      translate 160ms ease;
  }

  .docs-link:hover,
  .docs-link:focus-visible {
    background: color-mix(in srgb, var(--section-accent) 7%, transparent);
  }

  .docs-link:hover::before,
  .docs-link:focus-visible::before {
    opacity: 1;
  }

  .docs-link:hover .docs-arrow,
  .docs-link:focus-visible .docs-arrow {
    color: var(--section-accent);
    opacity: 1;
    translate: 2px 0;
  }

  @media (prefers-reduced-motion: reduce) {
    .docs-link,
    .docs-link::before,
    .docs-arrow {
      transition: none;
    }
  }
</style>
