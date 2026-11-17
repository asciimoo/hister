<script lang="ts">
  import { onMount } from 'svelte';
  import { resolve } from '$app/paths';
  import { fetchConfig, type AppConfig } from '#lib/api.js';
  import { extensionStores } from '#lib/extension.js';
  import { Button } from '@nogon/components/ui/button';
  import {
    ArrowRight,
    ArrowUpRight,
    BookOpen,
    Database,
    FileText,
    FolderOpen,
    Globe,
    LifeBuoy,
    Search,
    Settings2,
    Terminal,
    Waypoints,
  } from '@lucide/svelte';

  let config = $state<AppConfig | null>(null);

  onMount(() => {
    fetchConfig()
      .then((value) => (config = value))
      .catch(() => {});
  });

  const searchExamples = [
    { query: '"climate change"', description: 'Match an exact phrase.' },
    { query: 'domain:github.com', description: 'Limit results to github.com.' },
    { query: 'type:file budget', description: 'Find indexed files containing "budget".' },
    { query: 'golang -javascript', description: 'Match "golang" and exclude "javascript".' },
    {
      query: 'added:<7d sort:date',
      description: 'Show documents added in the last 7 days, newest first.',
    },
  ];

  const guides = [
    {
      title: 'Configuration',
      description: 'Server settings, access tokens, hotkeys, and optional semantic search.',
      path: 'configuration',
      icon: Settings2,
    },
    {
      title: 'Local files',
      description: 'Index directories and update indexed content when files change.',
      path: 'configuration#local-directory-indexing',
      icon: FolderOpen,
    },
    {
      title: 'Terminal client',
      description: 'Command line search, imports, exports, and the interactive terminal UI.',
      path: 'terminal-client',
      icon: Terminal,
    },
    {
      title: 'MCP integration',
      description: "Connect AI assistants to Nogon's search, preview, and history tools.",
      path: 'mcp',
      icon: Waypoints,
    },
    {
      title: 'Data storage and backups',
      description: 'Storage locations, preview retention, deletion, exports, and backups.',
      path: 'data-lifecycle',
      icon: Database,
    },
    {
      title: 'Troubleshooting',
      description: 'Diagnose common server, extension, and import problems.',
      path: 'troubleshooting',
      icon: LifeBuoy,
    },
  ];

  const communityLinks = [
    { label: 'Website', href: 'https://hister.org/' },
    { label: 'GitHub', href: 'https://github.com/asciimoo/hister' },
    { label: 'Codeberg', href: 'https://codeberg.org/asciimoo/hister' },
    { label: 'Report an issue', href: 'https://github.com/asciimoo/hister/issues' },
    { label: 'Discord', href: 'https://discord.gg/beEyuHxRSs' },
  ];
</script>

<svelte:head>
  <title>Nogon · About</title>
  <meta
    name="description"
    content="Nogon setup, indexing methods, search syntax, and links to configuration and usage documentation."
  />
</svelte:head>

{#snippet docLink(path: string, label: string)}
  <a
    class="text-link"
    href={`https://hister.org/docs/${path}`}
    target="_blank"
    rel="noopener noreferrer"
  >
    {label}<ArrowUpRight class="size-4 shrink-0" aria-hidden="true" />
  </a>
{/snippet}

<div class="flex-1 overflow-y-auto px-4 py-6 md:px-10 md:py-12">
  <div class="mx-auto max-w-6xl space-y-12 md:space-y-16">
    <section
      class="hero grid gap-8 p-6 md:p-10 lg:grid-cols-[1.4fr_1fr] lg:gap-12"
      aria-labelledby="about-title"
    >
      <div>
        <h1
          id="about-title"
          class="font-outfit text-4xl leading-tight font-bold tracking-tight sm:text-5xl"
        >
          About Nogon
        </h1>
        <p class="text-text-brand-secondary mt-5 max-w-xl text-base leading-relaxed">
          Nogon indexes web pages and files for full text search. It runs locally or on a server you
          manage, with a web interface and a command line client.
        </p>
        <div class="mt-7 flex flex-wrap items-center gap-4">
          <Button
            href={resolve('/')}
            class="font-space shadow-brutal-sm gap-3 px-5 font-bold hover:no-underline"
          >
            Search<ArrowRight class="size-4" aria-hidden="true" />
          </Button>
          {@render docLink('quickstart', 'Quickstart guide')}
        </div>
        <p class="text-text-brand-secondary mt-6 flex items-center gap-2 text-xs leading-relaxed">
          <Database class="text-nogon-teal size-4 shrink-0" aria-hidden="true" />
          Indexed content is stored on the Nogon server.
        </p>
      </div>

      <section class="workflow self-center" aria-labelledby="workflow-title">
        <div class="border-border-brand-muted flex items-center gap-2 border-b px-5 py-3">
          <span class="bg-nogon-teal size-2" aria-hidden="true"></span>
          <h2 id="workflow-title" class="eyebrow">Core features</h2>
        </div>
        <div class="space-y-6 p-5 md:p-6">
          <div class="flex gap-4">
            <Globe class="text-nogon-coral mt-1 size-6 shrink-0" aria-hidden="true" />
            <div>
              <h3 class="font-outfit text-lg font-bold">Indexing</h3>
              <p class="text-text-brand-secondary mt-1 text-sm leading-relaxed">
                Add content through the browser extension, imports, or the crawler.
              </p>
            </div>
          </div>
          <div class="flex gap-4">
            <Search class="text-nogon-indigo mt-1 size-6 shrink-0" aria-hidden="true" />
            <div>
              <h3 class="font-outfit text-lg font-bold">Search</h3>
              <p class="text-text-brand-secondary mt-1 text-sm leading-relaxed">
                Search text, titles, URLs, and domains. Filter by field, date, or document type.
              </p>
            </div>
          </div>
          <div class="flex gap-4">
            <FileText class="text-nogon-teal mt-1 size-6 shrink-0" aria-hidden="true" />
            <div>
              <h3 class="font-outfit text-lg font-bold">Previews</h3>
              <p class="text-text-brand-secondary mt-1 text-sm leading-relaxed">
                Read saved page content when previews are enabled.
              </p>
            </div>
          </div>
        </div>
      </section>
    </section>

    <section aria-labelledby="getting-started-title">
      <p class="eyebrow mb-2" style="--eyebrow-color: var(--nogon-coral)">Setup</p>
      <h2 id="getting-started-title" class="section-title">Getting started</h2>
      <p class="section-intro">
        After connecting the extension or importing content, search for a phrase from an indexed
        page to verify the setup.
      </p>

      {#if config?.public && !config.canWrite}
        <p
          class="border-nogon-indigo/40 bg-nogon-indigo/10 mt-5 border-l-4 px-4 py-3 text-sm leading-relaxed"
        >
          You are browsing a public collection without write access. Follow the quickstart guide to
          set up a server for your own content.
        </p>
      {/if}

      <ol class="mt-6 grid gap-4 md:grid-cols-3">
        <li class="onboarding-card" style="--step-color: var(--nogon-coral)">
          <span class="step-number" aria-hidden="true">01</span>
          <h3 class="card-title">Connect the browser extension</h3>
          <p class="card-copy">
            Install the extension to save pages as you browse. In its settings, enter your Nogon
            server URL and access token if required.
          </p>
          <div class="mt-4 flex flex-wrap gap-x-4 gap-y-2">
            {#each Object.entries(extensionStores) as [browser, store] (browser)}
              <a class="text-link" href={store.href} target="_blank" rel="noopener noreferrer">
                {browser === 'firefox' ? 'Firefox' : 'Chrome / Edge'}
                <ArrowUpRight class="size-4 shrink-0" aria-hidden="true" />
              </a>
            {/each}
          </div>
          <p class="card-copy mt-4">
            The Nogon server must be running for the extension to submit pages.
          </p>
          <div class="mt-auto pt-5">{@render docLink('browser-extension', 'Extension setup')}</div>
        </li>

        <li class="onboarding-card" style="--step-color: var(--nogon-indigo)">
          <span class="step-number" aria-hidden="true">02</span>
          <h3 class="card-title">Import existing content</h3>
          <p class="card-copy">
            The extension indexes new visits. Use imports to add existing browser history,
            bookmarks, files, or content from supported reading services.
          </p>
          <div class="mt-4">{@render docLink('import', 'Import guide')}</div>
          <p class="card-copy mt-4">
            Use the crawler to index a website by following links from a starting URL.
          </p>
          <div class="mt-auto flex flex-wrap gap-x-4 gap-y-2 pt-5">
            {@render docLink('crawler', 'Crawling guide')}
            {#if config?.canWrite}
              <a class="text-link" href={resolve('/add')}
                >Add a page<ArrowRight class="size-4" aria-hidden="true" /></a
              >
            {/if}
          </div>
        </li>

        <li class="onboarding-card" style="--step-color: var(--nogon-teal)">
          <span class="step-number" aria-hidden="true">03</span>
          <h3 class="card-title">Configure indexing rules</h3>
          <p class="card-copy">
            Allow and skip rules control which URLs are indexed. Priority rules boost matching
            results. Aliases expand frequently used query terms.
          </p>
          <p class="card-copy mt-4">
            Changing allow or skip rules does not immediately remove existing documents.
          </p>
          <div class="mt-auto flex flex-wrap gap-x-4 gap-y-2 pt-5">
            {@render docLink('rules', 'Rules reference')}
            {#if config?.canWrite}
              <a class="text-link" href={resolve('/rules')}
                >Manage rules<ArrowRight class="size-4" aria-hidden="true" /></a
              >
            {/if}
          </div>
        </li>
      </ol>
    </section>

    <section class="grid gap-8 lg:grid-cols-[1.5fr_1fr] lg:gap-12" aria-labelledby="search-title">
      <div class="min-w-0">
        <p class="eyebrow mb-2" style="--eyebrow-color: var(--nogon-indigo)">Usage</p>
        <h2 id="search-title" class="section-title">Search syntax</h2>
        <p class="section-intro">
          Enter keywords to search indexed content. Combine them with phrases and filters to narrow
          the results. Select an example to run it against this server's index.
        </p>
        <ul class="border-border-brand mt-6 border-y">
          {#each searchExamples as example (example.query)}
            <li class="border-border-brand-muted border-b last:border-b-0">
              <a
                class="search-example group"
                href={`${resolve('/')}?q=${encodeURIComponent(example.query)}`}
              >
                <span class="min-w-0">
                  <code class="font-fira text-text-brand bg-transparent p-0 text-sm break-words"
                    >{example.query}</code
                  >
                  <span class="text-text-brand-secondary mt-1 block text-sm"
                    >{example.description}</span
                  >
                </span>
                <ArrowRight
                  class="text-text-brand-muted group-hover:text-text-brand size-4 shrink-0"
                  aria-hidden="true"
                />
              </a>
            </li>
          {/each}
        </ul>
        <div class="mt-5">{@render docLink('query-language', 'Full search syntax')}</div>
      </div>

      <aside
        class="border-border-brand bg-card-surface self-start border p-6 lg:mt-8"
        aria-labelledby="daily-tips-title"
      >
        <BookOpen class="text-nogon-teal mb-4 size-6" aria-hidden="true" />
        <h3 id="daily-tips-title" class="font-outfit text-2xl font-bold">Using search results</h3>
        <div class="mt-5 space-y-5 text-sm leading-relaxed">
          <div>
            <h4 class="font-semibold">Saved previews</h4>
            <p class="text-text-brand-secondary mt-1">
              When previews are enabled, open a result's preview to read saved content without
              revisiting the website.
            </p>
          </div>
          <div>
            <h4 class="font-semibold">Search history</h4>
            <p class="text-text-brand-secondary mt-1">
              When history is enabled, revisit earlier searches and the results you opened.
            </p>
            {#if config?.canWrite && config.historyEnabled}
              <a class="text-link mt-2" href={resolve('/history')}
                >Open history<ArrowRight class="size-4" aria-hidden="true" /></a
              >
            {/if}
          </div>
          <div>
            <h4 class="font-semibold">Keyboard shortcuts</h4>
            <p class="text-text-brand-secondary mt-1">
              Open the search page's menu for your configured keyboard shortcuts.
            </p>
            <a class="text-link mt-2" href={resolve('/help')}
              >Search help<ArrowRight class="size-4" aria-hidden="true" /></a
            >
          </div>
        </div>
      </aside>
    </section>

    <section aria-labelledby="docs-title">
      <div class="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p class="eyebrow mb-2" style="--eyebrow-color: var(--nogon-teal)">Reference</p>
          <h2 id="docs-title" class="section-title">Documentation</h2>
        </div>
        {@render docLink('', 'All documentation')}
      </div>
      <div class="mt-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {#each guides as guide (guide.path)}
          <a
            class="guide-link group"
            href={`https://hister.org/docs/${guide.path}`}
            target="_blank"
            rel="noopener noreferrer"
          >
            <div class="mb-4 flex items-center justify-between gap-3">
              <guide.icon class="text-nogon-teal size-5" aria-hidden="true" />
              <ArrowUpRight
                class="text-text-brand-muted group-hover:text-text-brand size-4"
                aria-hidden="true"
              />
            </div>
            <h3 class="font-outfit text-lg font-bold">{guide.title}</h3>
            <p class="text-text-brand-secondary mt-2 text-sm leading-relaxed">
              {guide.description}
            </p>
          </a>
        {/each}
      </div>
    </section>

    <footer
      class="border-border-brand flex flex-col gap-6 border-t pt-8 pb-4 md:flex-row md:justify-between md:gap-12"
    >
      <div class="max-w-md">
        <h2 class="font-outfit text-xl font-bold">Project and license</h2>
        <p class="text-text-brand-secondary mt-2 text-sm leading-relaxed">
          Nogon is free software built with Go and Bleve, licensed under AGPLv3 or any later
          version. Source code and issue tracking are linked here.
        </p>
      </div>
      <div class="md:max-w-xs">
        <nav class="flex flex-wrap gap-x-5 gap-y-3" aria-label="Project and community">
          {#each communityLinks as link (link.href)}
            <a class="text-link" href={link.href} target="_blank" rel="noopener noreferrer"
              >{link.label}<ArrowUpRight class="size-3.5" aria-hidden="true" /></a
            >
          {/each}
        </nav>
        <p class="text-text-brand-secondary mt-4 text-xs">IRC: #hister on IRCNet.</p>
      </div>
    </footer>
  </div>
</div>

<style>
  .hero {
    border: 2px solid var(--brutal-border);
    background: var(--brutal-bg);
    box-shadow: 6px 6px 0 color-mix(in srgb, var(--nogon-indigo) 60%, var(--brutal-shadow));
  }

  .eyebrow {
    color: color-mix(
      in srgb,
      var(--eyebrow-color, var(--text-primary-brand)) 60%,
      var(--text-primary-brand)
    );
    font-family: var(--font-space);
    font-size: 0.7rem;
    font-weight: 700;
    letter-spacing: 0.12em;
    text-transform: uppercase;
  }

  .workflow {
    border: 1px solid var(--border-brand);
    background: var(--card-surface);
  }

  .section-title {
    font-family: var(--font-outfit);
    font-size: clamp(1.6rem, 3vw, 2rem);
    font-weight: 700;
    line-height: 1.2;
  }

  .section-intro {
    margin-top: 0.75rem;
    max-width: 40rem;
    color: var(--text-secondary-brand);
    font-size: 0.875rem;
    line-height: 1.7;
  }

  .onboarding-card {
    display: flex;
    flex-direction: column;
    min-width: 0;
    padding: 1.5rem;
    border: 1px solid var(--border-brand);
    border-top: 4px solid var(--step-color);
    background: var(--card-surface);
  }

  .step-number {
    margin-bottom: 1.25rem;
    font-family: var(--font-fira);
    font-size: 1.5rem;
    line-height: 1;
    color: color-mix(in srgb, var(--step-color) 70%, var(--text-primary-brand));
  }

  .card-title {
    font-family: var(--font-outfit);
    font-size: 1.25rem;
    font-weight: 700;
    line-height: 1.3;
  }

  .card-copy {
    margin-top: 0.75rem;
    color: var(--text-secondary-brand);
    font-size: 0.875rem;
    line-height: 1.7;
  }

  .text-link {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    font-size: 0.8rem;
    font-weight: 600;
    color: var(--text-primary-brand);
    text-decoration: underline;
    text-decoration-color: var(--border-brand);
    text-underline-offset: 4px;
  }

  .text-link:hover {
    text-decoration-color: var(--nogon-indigo);
  }

  .search-example {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding: 1rem 0.75rem;
    text-decoration: none;
  }

  .search-example:hover {
    background: var(--card-surface);
  }

  .guide-link {
    padding: 1.25rem;
    border: 1px solid var(--border-brand);
    color: var(--text-primary-brand);
    background: var(--card-surface);
    text-decoration: none;
    transition: border-color 150ms;
  }

  .guide-link:hover {
    border-color: var(--nogon-teal);
  }

  @media (prefers-reduced-motion: reduce) {
    .guide-link {
      transition: none;
    }
  }
</style>
