<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script lang="ts">
  import { issueLink, type IssueComment, type IssuePreviewData } from '#lib/issue-preview.js';

  let { data }: { data: IssuePreviewData } = $props();

  function formatDate(value: string): string {
    // Preserve display dates when the saved page has no precise timestamp.
    if (!/^\d{4}-\d{2}-\d{2}T/.test(value)) return value;
    const date = new Date(value);
    return Number.isNaN(date.getTime())
      ? value
      : date.toLocaleString(undefined, {
          year: 'numeric',
          month: 'short',
          day: 'numeric',
          hour: '2-digit',
          minute: '2-digit',
        });
  }
</script>

{#snippet attribution(comment: IssueComment)}
  {@const authorURL = issueLink(comment.authorURL)}
  {@const permalink = issueLink(comment.url)}
  <div
    class="not-prose text-text-brand-muted flex flex-wrap items-baseline gap-x-2 gap-y-1 text-xs"
  >
    {#if authorURL}
      <a
        class="text-nogon-teal font-semibold hover:underline"
        href={authorURL}
        target="_blank"
        rel="noopener noreferrer">{comment.author || 'Unknown author'}</a
      >
    {:else}
      <span class="font-semibold">{comment.author || 'Unknown author'}</span>
    {/if}
    {#if comment.createdAt}
      {#if permalink}
        <a class="hover:underline" href={permalink} target="_blank" rel="noopener noreferrer"
          >{formatDate(comment.createdAt)}</a
        >
      {:else}
        <span>{formatDate(comment.createdAt)}</span>
      {/if}
    {:else if permalink}
      <a class="hover:underline" href={permalink} target="_blank" rel="noopener noreferrer"
        >View original</a
      >
    {/if}
  </div>
{/snippet}

<article
  aria-label="Issue preview"
  class="issue-preview min-w-0 space-y-5 [overflow-wrap:anywhere]"
>
  <header class="not-prose border-border-brand-muted space-y-3 border-b pb-4">
    <div class="text-text-brand-muted flex flex-wrap items-center gap-2 text-xs">
      {#if data.repository}<span>{data.repository}</span>{/if}
      {#if data.number}<span>#{data.number}</span>{/if}
      {#if data.state}
        <span
          class="border-border-brand-muted bg-card-surface-muted text-text-brand rounded-full border px-2 py-0.5 font-semibold"
          >{data.state}</span
        >
      {/if}
    </div>
    <h2 class="font-outfit text-text-brand text-xl leading-snug font-bold">
      {data.title || 'Issue'}
    </h2>
    {#if data.labels?.length}
      <ul aria-label="Issue labels" class="flex flex-wrap gap-1.5">
        {#each data.labels as label}
          <li
            class="border-border-brand-muted bg-card-surface-muted rounded border px-2 py-0.5 text-xs"
          >
            {label}
          </li>
        {/each}
      </ul>
    {/if}
  </header>

  <section aria-label="Description" class="min-w-0">
    {@render attribution(data.body)}
    {#if data.body.bodyHTML}
      <div class="issue-body mt-3">{@html data.body.bodyHTML}</div>
    {:else}
      <p class="text-text-brand-muted italic">No description provided.</p>
    {/if}
  </section>

  <section aria-label="Comments" class="border-border-brand-muted min-w-0 border-t pt-4">
    <h3 class="not-prose font-outfit text-text-brand mb-2 text-base font-bold">
      Comments ({data.comments.length})
    </h3>
    <p class="not-prose text-text-brand-muted mb-4 text-xs">
      Comments shown are from the saved page.
    </p>
    {#each data.comments as comment}
      <article
        aria-label={`Comment by ${comment.author || 'unknown author'}`}
        class="border-border-brand-muted mt-3 min-w-0 rounded border p-3"
      >
        {@render attribution(comment)}
        <div class="issue-body mt-3">{@html comment.bodyHTML}</div>
      </article>
    {:else}
      <p class="text-text-brand-muted italic">No comments in the saved page.</p>
    {/each}
  </section>
</article>

<style>
  .issue-body {
    display: flow-root;
  }
  .issue-body :global(pre) {
    max-width: 100%;
    overflow-x: auto;
  }
  .issue-body :global(img) {
    max-width: 100%;
    height: auto;
  }
  .issue-body :global(table) {
    display: block;
    max-width: 100%;
    overflow-x: auto;
  }
</style>
