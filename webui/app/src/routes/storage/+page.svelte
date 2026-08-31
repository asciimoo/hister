<script lang="ts">
  import { onMount } from 'svelte';
  import { PageHeader } from '@hister/components';
  import { fetchDomainStats, type DomainStat } from '$lib/api';

  let domains = $state<DomainStat[]>([]);
  let loading = $state(true);
  let error = $state<string | null>(null);

  onMount(async () => {
    try {
      domains = await fetchDomainStats();
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load storage statistics';
    } finally {
      loading = false;
    }
  });

  // One decimal place from kilobytes upward. Rounding to whole units makes a row like
  // "4 KB + 4 KB = 2 MB" look wrong when it is not, and a storage table whose columns appear not
  // to add up is a storage table nobody believes.
  function formatBytes(n: number): string {
    if (n < 1024) return `${n} B`;
    let value = n;
    for (const unit of ['KB', 'MB', 'GB', 'TB']) {
      value /= 1024;
      if (value < 1024) return `${value.toFixed(1)} ${unit}`;
    }
    return `${(value / 1024).toFixed(1)} PB`;
  }

  const totals = $derived(
    domains.reduce(
      (acc, d) => ({
        pages: acc.pages + d.pages,
        text: acc.text + d.text_bytes,
        html: acc.html + d.html_bytes,
        favicon: acc.favicon + d.favicon_bytes,
        total: acc.total + d.total_bytes,
      }),
      { pages: 0, text: 0, html: 0, favicon: 0, total: 0 },
    ),
  );

  const anyShared = $derived(domains.some((d) => d.shared_bytes > 0));

  // Share of the reclaimable total, which is the question a percentage actually answers: how much
  // of the index is this domain? A bar was tried first and abandoned — the distribution is severely
  // skewed, so a linear bar left 44% of rows under a single pixel, and a square root scale fixed
  // the visibility by making the width no longer proportional. A number has neither problem.
  function share(bytes: number): string {
    if (totals.total <= 0) return '';
    const pct = (bytes / totals.total) * 100;
    if (pct > 0 && pct < 0.1) return '<0.1%';
    return `${pct.toFixed(1)}%`;
  }
</script>

<svelte:head>
  <title>Hister - Storage</title>
</svelte:head>

<div class="flex-1 overflow-y-auto px-4 py-6 md:px-12 md:py-10">
  <PageHeader color="hister-green" class="mx-auto mb-8 max-w-5xl">Storage by domain</PageHeader>

  <div class="mx-auto max-w-5xl">
    {#if error}
      <p class="text-hister-red mb-6">{error}</p>
    {:else if loading}
      <p class="text-muted-foreground mb-6">Measuring…</p>
    {:else if domains.length === 0}
      <p class="text-muted-foreground mb-6">Nothing indexed yet.</p>
    {:else}
      <p class="text-muted-foreground mb-6 text-sm">
        {domains.length} domains, {totals.pages.toLocaleString()} pages, {formatBytes(totals.total)} in
        total. Sizes are what deleting a domain would release.
      </p>

      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead>
            <tr class="text-muted-foreground border-b text-left">
              <th class="py-2 pr-4 font-medium">Domain</th>
              <th class="py-2 pr-4 text-right font-medium">Pages</th>
              <th class="py-2 pr-4 text-right font-medium">Text</th>
              <th class="py-2 pr-4 text-right font-medium">HTML</th>
              <th class="py-2 pr-4 text-right font-medium">Favicon</th>
              {#if anyShared}
                <th class="py-2 pr-4 text-right font-medium">Shared</th>
              {/if}
              <th class="py-2 pr-4 text-right font-medium">Total</th>
              <!-- "% of total", not "Share": the Shared column beside it means something else
                   entirely, and two adjacent headers differing by one letter is a trap. -->
              <th class="py-2 text-right font-medium">% of total</th>
            </tr>
          </thead>
          <tbody>
            {#each domains as d (d.domain)}
              <tr class="hover:bg-muted/40 border-b">
                <td class="py-2 pr-4 font-mono break-all">{d.domain}</td>
                <td class="py-2 pr-4 text-right tabular-nums">{d.pages.toLocaleString()}</td>
                <td class="py-2 pr-4 text-right tabular-nums">{formatBytes(d.text_bytes)}</td>
                <td class="py-2 pr-4 text-right tabular-nums">{formatBytes(d.html_bytes)}</td>
                <td class="py-2 pr-4 text-right tabular-nums">{formatBytes(d.favicon_bytes)}</td>
                {#if anyShared}
                  <td class="text-muted-foreground py-2 pr-4 text-right tabular-nums">
                    {d.shared_bytes > 0 ? formatBytes(d.shared_bytes) : ''}
                  </td>
                {/if}
                <td class="py-2 pr-4 text-right font-semibold tabular-nums"
                  >{formatBytes(d.total_bytes)}</td
                >
                <td class="text-muted-foreground py-2 text-right tabular-nums"
                  >{share(d.total_bytes)}</td
                >
              </tr>
            {/each}
          </tbody>
          <tfoot>
            <tr class="font-semibold">
              <td class="py-2 pr-4">{domains.length} domains</td>
              <td class="py-2 pr-4 text-right tabular-nums">{totals.pages.toLocaleString()}</td>
              <td class="py-2 pr-4 text-right tabular-nums">{formatBytes(totals.text)}</td>
              <td class="py-2 pr-4 text-right tabular-nums">{formatBytes(totals.html)}</td>
              <td class="py-2 pr-4 text-right tabular-nums">{formatBytes(totals.favicon)}</td>
              {#if anyShared}
                <!-- Deliberately blank: a shared file is referenced by several domains, so
                     summing the column would count the same bytes more than once. -->
                <td class="py-2 pr-4"></td>
              {/if}
              <td class="py-2 pr-4 text-right tabular-nums">{formatBytes(totals.total)}</td>
              <td class="py-2 text-right tabular-nums">100.0%</td>
            </tr>
          </tfoot>
        </table>
      </div>

      {#if anyShared}
        <p class="text-muted-foreground mt-6 text-xs">
          <strong>Shared</strong> is data a domain references but does not own alone — identical content
          is stored once. Deleting one of the domains referencing it releases nothing, so it is excluded
          from the total.
        </p>
      {/if}

      <p class="text-muted-foreground mt-2 text-xs">
        Text is the indexed content, and is an estimate of a domain's share of the search index
        rather than a measurement of it. HTML and favicon sizes are exact.
      </p>
    {/if}
  </div>
</div>
