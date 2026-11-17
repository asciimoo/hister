<script lang="ts">
  import { onMount } from 'svelte';
  import { resolve } from '$app/paths';
  import { fetchConfig, type AppConfig } from '#lib/api.js';
  import { hotkeyDescriptions } from '#lib/hotkeys.js';
  import { Button } from '@nogon/components/ui/button';
  import { Kbd } from '@nogon/components/ui/kbd';
  import { ArrowRight, ArrowUpRight, Keyboard, Search } from '@lucide/svelte';

  interface QueryExample {
    query: string;
    description: string;
  }

  let config = $state<AppConfig | null>(null);
  let configError = $state(false);
  const shortcuts = $derived(
    Object.entries(config?.hotkeys ?? {}).filter(
      ([, action]) =>
        (config?.canWrite || action !== 'delete_result') &&
        (!config?.disablePreviews || action !== 'view_result_popup'),
    ),
  );

  onMount(() => {
    fetchConfig()
      .then((value) => (config = value))
      .catch(() => (configError = true));
  });

  const sections = [
    { id: 'syntax', title: 'Search syntax' },
    { id: 'shortcuts', title: 'Keyboard shortcuts' },
    { id: 'fields', title: 'Field filters' },
    { id: 'dates', title: 'Dates and sorting' },
    { id: 'aliases', title: 'Search aliases' },
    { id: 'troubleshooting', title: 'Troubleshooting' },
  ];

  const basicExamples: QueryExample[] = [
    { query: 'golang template', description: 'Require both terms.' },
    { query: '"free software"', description: 'Match an exact phrase using double quotes.' },
    { query: 'secur*', description: 'Use * to match words such as "secure" or "security".' },
    { query: 'golang -javascript', description: 'Exclude documents containing "javascript".' },
    {
      query: '(go|golang) template',
      description: 'Match either "go" or "golang", and require "template".',
    },
    {
      query: 'title:("privacy policy" OR security)',
      description:
        'Match a phrase or a word in the title. Spaces around alternatives are optional.',
    },
    {
      query: '(privacy OR security) NOT (archived OR deprecated)',
      description: 'Require either topic and exclude either unwanted term.',
    },
    {
      query: 'title:(privacy AND (policy OR protection))',
      description: 'Nest groups and apply the title field to every term.',
    },
    { query: '*', description: 'Show all documents available to you.' },
  ];

  const fieldExamples: QueryExample[] = [
    { query: 'domain:github.com', description: 'Limit results to a domain.' },
    {
      query: 'site:example.com',
      description: 'Include a domain and its subdomains, ignoring letter case.',
    },
    { query: 'title:"getting started"', description: 'Match a phrase in document titles.' },
    { query: 'text:encryption', description: 'Search only document content.' },
    { query: 'url:*/docs/*', description: 'Match a pattern in page addresses.' },
    {
      query: 'type:file budget',
      description: 'Search both local files and imported file snapshots.',
    },
    {
      query: 'language:en',
      description: 'Filter by English, including regional tags such as en-US.',
    },
    {
      query: 'language:en-US',
      description: 'Filter by a regional language tag, ignoring letter case.',
    },
    { query: 'language:nb', description: 'Find Norwegian Bokmal, including historical no labels.' },
    { query: 'label:research', description: 'Find documents with a matching label.' },
    { query: 'has:label', description: 'Find documents with a nonempty label.' },
    {
      query: '-has:metadata.author',
      description: 'Find documents without an author metadata value.',
    },
    {
      query: 'visits:5..9',
      description: 'Match visit counts from 5 through 9. Use visits:10.. for 10 or more.',
    },
  ];

  const dateExamples: QueryExample[] = [
    { query: 'added:<7d', description: 'Documents first indexed within the last 7 days.' },
    { query: 'updated:>90d', description: 'Documents not updated for more than 90 days.' },
    {
      query: 'updated:>=2026-04-01 updated:<2026-05-01',
      description: 'Documents updated during April 2026.',
    },
  ];

  const sortExamples: QueryExample[] = [
    { query: 'golang sort:date', description: 'Most recently updated documents first.' },
    { query: 'golang sort:-date', description: 'Least recently updated documents first.' },
    { query: 'golang sort:visits', description: 'Most visited documents first.' },
    { query: 'golang sort:domain', description: 'Order by domain from A to Z.' },
  ];

  const urlRegexp = 'url_re:^https://example\\.com/private/';
</script>

<svelte:head>
  <title>Nogon · Help</title>
  <meta
    name="description"
    content="Search syntax, field filters, date ranges, keyboard shortcuts, and troubleshooting for the Nogon web interface."
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

{#snippet queryExamples(examples: QueryExample[], columns = false)}
  <ul class="example-list {columns ? 'md:grid md:grid-cols-2 md:gap-x-8' : ''}">
    {#each examples as example (example.query)}
      <li>
        <a
          class="example-link group"
          href={`${resolve('/')}?q=${encodeURIComponent(example.query)}`}
        >
          <span class="min-w-0">
            <code>{example.query}</code>
            <span class="text-text-brand-secondary mt-1.5 block text-sm leading-relaxed"
              >{example.description}</span
            >
          </span>
          <ArrowRight
            class="text-text-brand-muted group-hover:text-text-brand mt-1 size-4 shrink-0"
            aria-hidden="true"
          />
        </a>
      </li>
    {/each}
  </ul>
{/snippet}

<div class="help-page flex-1 overflow-y-auto px-4 py-6 md:px-10 md:py-12">
  <div class="mx-auto max-w-6xl space-y-10 md:space-y-14">
    <section
      class="page-intro grid gap-8 p-6 md:p-10 lg:grid-cols-[1.4fr_1fr] lg:gap-12"
      aria-labelledby="help-title"
    >
      <div>
        <h1
          id="help-title"
          class="font-outfit text-4xl leading-tight font-bold tracking-tight sm:text-5xl"
        >
          Help
        </h1>
        <p class="text-text-brand-secondary mt-4 max-w-xl text-base leading-relaxed">
          Search syntax, keyboard shortcuts, and common tasks in the web interface. Searches run
          against documents indexed by this Nogon server.
        </p>
        <div class="mt-6 flex flex-wrap items-center gap-4">
          <Button
            href={resolve('/')}
            class="font-space shadow-brutal-sm gap-3 px-5 font-bold hover:no-underline"
          >
            <Search class="size-4" aria-hidden="true" />Search
          </Button>
          {@render docLink('query-language', 'Query language guide')}
        </div>
        <p class="text-text-brand-secondary mt-5 text-sm">
          For setup and indexing instructions, see <a class="text-link" href={resolve('/about')}
            >About Nogon</a
          >.
        </p>
      </div>
      <nav
        class="border-border-brand bg-card-surface self-center border p-5"
        aria-label="On this page"
      >
        <p class="eyebrow mb-3">On this page</p>
        <ul class="grid gap-x-5 gap-y-1 sm:grid-cols-2">
          {#each sections as section (section.id)}
            <li>
              <a class="section-link" href={`#${section.id}`}
                >{section.title}<ArrowRight class="size-3.5 shrink-0" aria-hidden="true" /></a
              >
            </li>
          {/each}
        </ul>
      </nav>
    </section>

    <div class="grid items-start gap-8 lg:grid-cols-[1.3fr_1fr] lg:gap-12">
      <section id="syntax" aria-labelledby="syntax-title">
        <h2 id="syntax-title" class="section-title">Search syntax</h2>
        <p class="section-intro">
          Separate terms with spaces to require all of them. Select any query example on this page
          to run it against your index.
        </p>
        <p class="section-intro">
          Use uppercase <code>AND</code>, <code>OR</code>, and <code>NOT</code> to combine
          conditions. Negation is evaluated first, then AND, then OR. Parentheses change that order.
          Quote operator words to search for them as text. Put <code>sort:</code> outside groups.
        </p>
        <p class="section-intro">
          Incomplete queries show an error with a character position, counted from one. Close quotes
          and parentheses, give each field a value, and add a term after each operator. For example,
          complete <code>privacy OR</code> as <code>privacy OR security</code>. Quote literal
          operators, such as <code>"OR"</code>. If an alias expands your query, the position refers
          to the expanded query.
        </p>
        {@render queryExamples(basicExamples)}
        <div class="mt-4">
          {@render docLink('query-language#combining-query-types', 'Combining queries')}
        </div>
      </section>

      <section
        id="shortcuts"
        class="border-border-brand bg-card-surface border p-5 md:p-6"
        aria-labelledby="shortcuts-title"
      >
        <h2 id="shortcuts-title" class="font-outfit flex items-center gap-3 text-2xl font-bold">
          <Keyboard class="text-nogon-indigo size-6 shrink-0" aria-hidden="true" />Keyboard
          shortcuts
        </h2>
        <p class="section-intro">
          These bindings apply on the search page and reflect this server's configuration.
        </p>
        {#if config}
          {#if shortcuts.length > 0}
            <dl class="mt-4">
              {#each shortcuts as [key, action] (key)}
                <div
                  class="border-border-brand-muted flex items-center justify-between gap-4 border-b py-2.5 last:border-b-0"
                >
                  <dt class="text-text-brand-secondary text-sm">
                    {hotkeyDescriptions[action] ?? action}
                  </dt>
                  <dd class="shrink-0">
                    <Kbd class="font-fira text-text-brand m-0 h-auto border px-2 py-1 text-xs"
                      >{key}</Kbd
                    >
                  </dd>
                </div>
              {/each}
            </dl>
          {:else}
            <p class="text-text-brand-secondary mt-4 text-sm">
              No keyboard shortcuts are configured.
            </p>
          {/if}
        {:else}
          <p class="text-text-brand-secondary mt-4 text-sm" role="status">
            {configError
              ? 'Could not load shortcuts. Open the search page menu to view its keyboard shortcuts.'
              : 'Loading configured shortcuts...'}
          </p>
        {/if}
        <p class="text-text-brand-secondary mt-4 text-xs leading-relaxed">
          Open result uses the selected result, or the first result if none is selected. Open in
          search engine sends your query to the configured external search engine.
        </p>
        <div class="mt-4">
          {@render docLink('configuration#hotkeysweb-section', 'Configure shortcuts')}
        </div>
      </section>
    </div>

    <section id="fields" aria-labelledby="fields-title">
      <h2 id="fields-title" class="section-title">Field filters</h2>
      <p class="section-intro">
        Use <code>field:value</code> to limit a search to a specific field. Combine filters with keywords
        or with other filters.
      </p>
      {@render queryExamples(fieldExamples, true)}
      <p class="text-text-brand-secondary mt-4 text-sm leading-relaxed">
        <code>site:example.com</code> includes <code>docs.example.com</code> but excludes
        <code>notexample.com</code>. Use a hostname without a scheme, port, path, or wildcard.
        <code>has:</code> accepts document fields and <code>metadata.KEY</code> paths. Missing, null,
        empty, or whitespace only values count as absent; zero and false count as present. Arrays and
        nested metadata objects need at least one nonempty value.
      </p>
      <div class="mt-5 grid gap-5 md:grid-cols-2 md:gap-8">
        <div class="reference-note">
          <h3 class="font-outfit text-lg font-bold">Document types and exclusions</h3>
          <p class="text-text-brand-secondary mt-2 text-sm leading-relaxed">
            Use <code>type:web</code> for web pages, <code>type:local</code> for watched files, and
            <code>type:remote</code> for imported file snapshots. <code>type:file</code> includes both
            file types.
          </p>
          <p class="text-text-brand-secondary mt-3 text-sm leading-relaxed">
            A leading minus sign excludes a filter. For example, <a
              class="query-link"
              href={`${resolve('/')}?q=${encodeURIComponent('golang -domain:stackoverflow.com')}`}
              ><code>golang -domain:stackoverflow.com</code></a
            > excludes results from that domain.
          </p>
        </div>
        <div class="reference-note">
          <h3 class="font-outfit text-lg font-bold">URL regular expressions</h3>
          <p class="text-text-brand-secondary mt-2 text-sm leading-relaxed">
            <code>url_re:</code> matches normalized URLs with a Go regular expression. Use quotes if
            the expression contains spaces. Anchor with <code>^</code> to match the start of a URL.
          </p>
          <a
            class="query-link mt-3 block"
            href={`${resolve('/')}?q=${encodeURIComponent(urlRegexp)}`}><code>{urlRegexp}</code></a
          >
          <div class="mt-3">
            {@render docLink('query-language#url-regular-expressions', 'Regular expression syntax')}
          </div>
        </div>
      </div>
    </section>

    <section id="dates" aria-labelledby="dates-title">
      <h2 id="dates-title" class="section-title">Dates and sorting</h2>
      <div class="mt-5 grid gap-8 lg:grid-cols-2 lg:gap-12">
        <div>
          <h3 class="font-outfit text-xl font-bold">Filter by date</h3>
          <p class="section-intro">
            <code>added:</code> is the first indexing time. <code>updated:</code> is the last update
            time. Relative values compare elapsed age: <code>&lt;7d</code> means newer than 7 days,
            and <code>&gt;90d</code> means older than 90 days.
          </p>
          {@render queryExamples(dateExamples)}
          <p class="text-text-brand-secondary mt-4 text-sm leading-relaxed">
            Durations support seconds (<code>s</code>), minutes (<code>m</code>), hours (<code
              >h</code
            >), days (<code>d</code>), and weeks (<code>w</code>). Absolute dates use
            <code>YYYY-MM-DD</code> at midnight UTC. Use the following day as an exclusive upper bound
            to include an entire day.
          </p>
        </div>
        <div>
          <h3 class="font-outfit text-xl font-bold">Set result order</h3>
          <p class="section-intro">
            <code>sort:relevance</code> is the default. Use <code>sort:</code> to change the order
            and prefix its value with <code>-</code> to reverse it.
          </p>
          {@render queryExamples(sortExamples)}
          <div class="mt-4">
            {@render docLink('query-language#sorting-results', 'Sorting reference')}
          </div>
        </div>
      </div>
    </section>

    <section
      id="aliases"
      class="alias-section grid gap-6 p-5 md:p-7 lg:grid-cols-[1fr_1.2fr] lg:gap-10"
      aria-labelledby="aliases-title"
    >
      <div>
        <h2 id="aliases-title" class="section-title">Search aliases</h2>
        <p class="section-intro">
          Aliases replace a keyword with a query expression before a search runs. Define an alias on
          the Rules page before using it.
        </p>
        <div class="mt-5 flex flex-wrap gap-x-5 gap-y-3">
          {#if config?.canWrite}
            <a class="text-link" href={resolve('/rules')}
              >Manage aliases<ArrowRight class="size-4" aria-hidden="true" /></a
            >
          {/if}
          {@render docLink('rules#aliases', 'Alias reference')}
        </div>
      </div>
      <div class="min-w-0">
        <h3 class="eyebrow mb-3">Example definitions</h3>
        <dl class="space-y-3">
          <div class="border-border-brand flex flex-wrap items-center gap-3 border-b pb-3">
            <dt><code>go</code></dt>
            <dd class="flex min-w-0 items-center gap-3">
              <ArrowRight
                class="text-text-brand-muted size-4 shrink-0"
                aria-label="expands to"
              /><code>(go|golang)</code>
            </dd>
          </div>
          <div class="border-border-brand flex flex-wrap items-center gap-3 border-b pb-3">
            <dt><code>!so</code></dt>
            <dd class="flex min-w-0 items-center gap-3">
              <ArrowRight
                class="text-text-brand-muted size-4 shrink-0"
                aria-label="expands to"
              /><code>domain:stackoverflow.com</code>
            </dd>
          </div>
        </dl>
        <p class="text-text-brand-secondary mt-4 text-sm leading-relaxed">
          With the second alias defined, <code>!so golang</code> expands to
          <code>domain:stackoverflow.com golang</code>.
        </p>
      </div>
    </section>

    <section id="troubleshooting" aria-labelledby="troubleshooting-title">
      <h2 id="troubleshooting-title" class="section-title">Troubleshooting</h2>
      <div class="mt-5 grid gap-4 md:grid-cols-3">
        <div class="reference-note">
          <h3 class="font-outfit text-lg font-bold">No results</h3>
          <p class="text-text-brand-secondary mt-2 text-sm leading-relaxed">
            Remove filters and try a single keyword. Search for <a
              class="query-link"
              href={`${resolve('/')}?q=*`}><code>*</code></a
            > to check which documents are available. Nogon searches its index, not the live web.
          </p>
          <div class="mt-4">
            {@render docLink('query-language#troubleshooting-queries', 'Search troubleshooting')}
          </div>
        </div>
        <div class="reference-note">
          <h3 class="font-outfit text-lg font-bold">A page is missing</h3>
          <p class="text-text-brand-secondary mt-2 text-sm leading-relaxed">
            Check that the server is running and the extension uses the correct URL and token. Check
            allow and skip rules. Browser history needs a separate import for pages you have not
            revisited.
          </p>
          <div class="mt-4">
            {@render docLink('browser-extension#troubleshooting', 'Extension troubleshooting')}
          </div>
        </div>
        <div class="reference-note">
          <h3 class="font-outfit text-lg font-bold">Unexpected matches</h3>
          <p class="text-text-brand-secondary mt-2 text-sm leading-relaxed">
            Check field names, closing quotes, and parentheses. Review aliases that may expand your
            query. Use double quotes for a phrase and a domain filter to narrow the source.
          </p>
          <div class="mt-4">{@render docLink('troubleshooting', 'Troubleshooting guide')}</div>
        </div>
      </div>
    </section>

    <footer
      class="border-border-brand flex flex-wrap items-center justify-between gap-4 border-t pt-6 pb-4"
    >
      <p class="text-text-brand-secondary text-sm">
        For setup, imports, and server configuration, see the documentation.
      </p>
      {@render docLink('', 'All documentation')}
    </footer>
  </div>
</div>

<style>
  .help-page section[id] {
    scroll-margin-top: 1.5rem;
  }

  .page-intro {
    border: 2px solid var(--brutal-border);
    background: var(--brutal-bg);
    box-shadow: 6px 6px 0 color-mix(in srgb, var(--nogon-indigo) 60%, var(--brutal-shadow));
  }

  .eyebrow {
    font-family: var(--font-space);
    font-size: 0.7rem;
    font-weight: 700;
    letter-spacing: 0.1em;
    text-transform: uppercase;
  }

  .section-title {
    font-family: var(--font-outfit);
    font-size: clamp(1.6rem, 3vw, 2rem);
    font-weight: 700;
    line-height: 1.2;
  }

  .section-intro {
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

  .section-link {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    padding: 0.5rem 0;
    font-size: 0.8rem;
    color: var(--text-primary-brand);
  }

  .example-list {
    margin-top: 1.25rem;
    border-top: 1px solid var(--border-brand);
  }

  .example-list li {
    border-bottom: 1px solid var(--border-muted-brand);
  }

  .example-link {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 1rem;
    height: 100%;
    padding: 0.9rem 0.65rem;
    color: var(--text-primary-brand);
    text-decoration: none;
  }

  .example-link:hover {
    background: var(--card-surface);
  }

  code {
    font-family: var(--font-fira);
    font-size: 0.8rem;
    color: var(--text-primary-brand);
    overflow-wrap: anywhere;
  }

  .example-link code {
    background: transparent;
    padding: 0;
  }

  .query-link {
    text-decoration: underline;
    text-decoration-color: var(--border-brand);
    text-underline-offset: 4px;
  }

  .reference-note {
    min-width: 0;
    padding: 1.25rem;
    border: 1px solid var(--border-brand);
    background: var(--card-surface);
  }

  .alias-section {
    border: 1px solid var(--border-brand);
    border-left: 4px solid var(--nogon-teal);
    background: var(--card-surface);
  }
</style>
