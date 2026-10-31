<script lang="ts">
  import ArrowRightIcon from '@lucide/svelte/icons/arrow-right';
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

  {#each data.categories as category}
    <div class="mb-8">
      <div class="mb-3 flex items-center gap-2">
        <div class="h-2.5 w-2.5 bg-hister-{category.color}"></div>
        <span
          class="font-space text-xs font-bold tracking-[2px] text-[var(--text-secondary)] uppercase"
        >
          {category.name}
        </span>
      </div>

      <ul class="m-0 flex list-none flex-col gap-3 p-0">
        {#each category.docs as doc}
          <li>
            <Card.Root href="/docs/{doc.slug}" class="bg-brutal-card p-5">
              <h2 class="font-space text-lg font-extrabold tracking-[0.5px] text-(--text-primary)">
                {doc.title}
              </h2>
              {#if doc.description}
                <p class="font-inter mt-2 text-sm leading-relaxed text-(--text-secondary)">
                  {doc.description}
                </p>
              {/if}
            </Card.Root>
          </li>
        {/each}
      </ul>
    </div>
  {/each}

  <div class="mb-8">
    <div class="mb-3 flex items-center gap-2">
      <div class="bg-hister-indigo h-2.5 w-2.5"></div>
      <span class="font-space text-xs font-bold tracking-[2px] text-(--text-secondary) uppercase">
        Browser Extensions
      </span>
    </div>

    <ul class="m-0 flex list-none flex-col gap-3 p-0">
      <li>
        <Card.Root
          href="https://chromewebstore.google.com/detail/hister/cciilamhchpmbdnniabclekddabkifhb"
          target="_blank"
          rel="noopener noreferrer"
          class="bg-brutal-card flex-row items-center gap-4 p-5"
        >
          <Globe size={20} class="shrink-0 text-(--text-secondary)" />
          <h2 class="font-space text-lg font-extrabold tracking-[0.5px] text-(--text-primary)">
            Chrome Extension
          </h2>
        </Card.Root>
      </li>
      <li>
        <Card.Root
          href="https://addons.mozilla.org/en-US/firefox/addon/hister/"
          target="_blank"
          rel="noopener noreferrer"
          class="bg-brutal-card flex-row items-center gap-4 p-5"
        >
          <Globe size={20} class="shrink-0 text-(--text-secondary)" />
          <h2 class="font-space text-lg font-extrabold tracking-[0.5px] text-(--text-primary)">
            Firefox Add-on
          </h2>
        </Card.Root>
      </li>
    </ul>
  </div>
</section>
