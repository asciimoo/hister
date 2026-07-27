<script lang="ts">
  import { onMount } from 'svelte';
  import { fetchConfig } from '$lib/api';
  import { Kbd } from '@hister/components/ui/kbd';
  import { PageHeader } from '@hister/components';

  let canWrite = $state(true);

  onMount(async () => {
    const cfg = await fetchConfig();
    canWrite = cfg.canWrite;
  });
</script>

<svelte:head>
  <title>Hister - Help</title>
</svelte:head>

<div class="flex-1 overflow-y-auto px-4 py-6 md:px-12 md:py-10">
  <PageHeader color="hister-indigo" class="mx-auto mb-8 max-w-3xl">Help</PageHeader>
  <div
    class="prose prose-neutral dark:prose-invert prose-headings:font-outfit prose-headings:font-extrabold prose-h1:text-text-brand prose-h2:text-text-brand prose-h3:text-text-brand prose-p:font-inter prose-p:text-text-brand-secondary prose-li:font-inter prose-li:text-text-brand-secondary prose-a:text-hister-indigo prose-a:no-underline hover:prose-a:underline prose-strong:text-text-brand prose-hr:border-border-brand-muted mx-auto max-w-3xl"
  >
    <h2>Search Shortcuts</h2>
    <p>
      Press <Kbd>enter</Kbd> to open the first result. Alternatively press <Kbd>alt+enter</Kbd> to open
      in new tab.
    </p>
    <p>
      Navigate in results with <Kbd>alt+j</Kbd> and <Kbd>alt+k</Kbd>. <Kbd>Enter</Kbd>/<Kbd
        >alt+enter</Kbd
      > opens the selected result.
    </p>
    <p>Press <Kbd>alt+o</Kbd> to open the search query in the configured search engine.</p>

    <h2>Search Syntax</h2>
    <p>Use <Kbd>quotes</Kbd> to match phrases.</p>
    <p>Use <Kbd>*</Kbd> for wildcard matches.</p>
    <p>Prefix words or phrases with <Kbd>-</Kbd> to exclude matching documents.</p>
    <p>Use <code>url:</code> prefix to search only in the URL field.</p>
    <p>
      Use <code>visits:</code> to filter by visit count, such as <code>visits:1</code>,
      <code>visits:2..4</code>, or <code>visits:10..</code>.
    </p>
    <p>
      Use <code>added:</code> and <code>updated:</code> to filter by relative time or an absolute
      <code>YYYY-MM-DD</code> date, such as <code>updated:&gt;90d</code> or
      <code>added:&gt;=2026-04-01</code>.
    </p>

    <h3>Examples</h3>
    <p>
      <code>"free software" url:*wikipedia.org*</code>: Search for the phrase "free software" only
      in URLs containing wikipedia.org.
    </p>
    <p>
      <code>golang template -url:*stackoverflow*</code>: Search sites containing both "golang" and
      "template" but the website's URL should not contain "stackoverflow".
    </p>
    <p>
      <code>project docs visits:5..9</code>: Search matching pages that have been visited between 5
      and 9 times.
    </p>
    <p>
      <code>updated:&gt;90d</code>: Search pages that have not been updated in more than 90 days.
    </p>
    <p>
      <code>updated:&gt;=2026-04-01 updated:&lt;2026-05-01</code>: Search pages updated during April
      2026. Absolute dates represent midnight UTC.
    </p>

    <h2>Search Aliases</h2>
    {#if canWrite}
      <p>
        Queries can become long and complex quickly. Aliases can be defined in the <a href="/rules"
          >rules</a
        > page to shorten common query parts.
      </p>
    {/if}

    <h3>Examples</h3>
    <p>
      <code>go</code> alias for <code>(go|golang)</code> matches either "go" or "golang" if you type "go".
    </p>
    <p>
      <code>!so</code> alias for <code>url:*stackoverflow.com*</code> matches only sites where the URL
      contains "stackoverflow.com".
    </p>

    <hr />

    <h2>Documentation</h2>
    <p>
      Full project documentation is available at <a
        href="https://hister.org/docs"
        target="_blank"
        rel="noopener noreferrer">hister.org/docs</a
      >.
    </p>
  </div>
</div>
