<script lang="ts">
  import { onMount } from 'svelte';
  import { Button } from '@hister/components/ui/button';
  import { Input } from '@hister/components/ui/input';
  import { Label } from '@hister/components/ui/label';
  import { fetchAPI } from '../modules/network';

  let {
    serverURL,
    tabURL,
    customHeaders,
  }: {
    serverURL: string;
    tabURL: string;
    customHeaders: { name: string; value: string }[];
  } = $props();

  type Crawl = {
    id: string;
    url: string;
    state: 'running' | 'finished' | 'stopped';
    max_pages: number;
    indexed: number;
    skipped: number;
    error?: string;
  };

  let expanded = $state(false);
  let maxPages = $state(100);
  let crawl = $state<Crawl | null>(null);
  let error = $state('');
  let busy = $state(false);
  let supported = $state(true);
  let disposed = false;
  let revision = 0;
  const apiURL = $derived(serverURL.replace(/\/$/, '') + '/api/crawl');
  const siteURL = $derived.by(() => {
    try {
      const u = new URL(tabURL);
      return ['http:', 'https:'].includes(u.protocol) ? u.origin + '/' : '';
    } catch {
      return '';
    }
  });

  async function status() {
    const requestRevision = revision;
    try {
      const res = await fetchAPI(apiURL, { customHeaders });
      if (disposed || requestRevision !== revision) return;
      if (
        res.status === 404 ||
        (res.ok && !res.headers.get('content-type')?.includes('application/json'))
      ) {
        supported = false;
        return;
      }
      if (!res.ok) throw new Error(await res.text());
      const result = await res.json();
      if (disposed || requestRevision !== revision) return;
      crawl = result;
      if (crawl?.state === 'running') expanded = true;
      error = '';
    } catch (e) {
      if (!disposed && requestRevision === revision) error = String(e);
    }
  }

  onMount(() => {
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      if (!busy) await status();
      if (!disposed) timer = setTimeout(poll, 2000);
    }
    void poll();
    return () => {
      disposed = true;
      clearTimeout(timer);
    };
  });

  async function submit(stop: boolean) {
    busy = true;
    revision++;
    error = '';
    try {
      const res = await fetchAPI(
        stop ? `${apiURL}/stop?id=${encodeURIComponent(crawl!.id)}` : apiURL,
        {
          method: 'POST',
          body: stop ? {} : { url: siteURL, max_pages: maxPages },
          customHeaders,
        },
      );
      if (!res.ok) throw new Error(await res.text());
      await status();
    } catch (e) {
      if (!disposed) error = String(e);
    } finally {
      if (!disposed) busy = false;
    }
  }
</script>

<div class="mt-3">
  <Button
    variant="outline"
    class="border-brutal-border font-outfit hover:border-hister-indigo h-9 w-full border-[3px] text-sm font-bold tracking-wide transition-all hover:shadow-[3px_3px_0_var(--brutal-shadow)]"
    onclick={() => (expanded = !expanded)}
    aria-expanded={expanded}
  >
    Crawl this site
  </Button>
  {#if expanded}
    <div class="mt-3 space-y-3 text-sm">
      {#if !supported}
        <p>Update your Hister server to a version with site crawling.</p>
      {:else}
        <p>
          Follow public page links from <strong class="break-all"
            >{siteURL || 'an HTTP(S) site'}</strong
          > on this hostname. Login cookies are not used. The crawl continues after this popup closes.
        </p>
        {#if crawl?.state === 'running'}
          <p class="break-all">Crawling {crawl.url}</p>
          <Button variant="outline" disabled={busy} onclick={() => submit(true)}>Stop crawl</Button>
        {:else}
          <Label for="crawl-limit">Page limit (1–1000)</Label>
          <Input id="crawl-limit" type="number" min={1} max={1000} step={1} bind:value={maxPages} />
          <Button
            disabled={busy ||
              !siteURL ||
              !Number.isInteger(maxPages) ||
              maxPages < 1 ||
              maxPages > 1000}
            onclick={() => submit(false)}
          >
            Start crawl
          </Button>
        {/if}
        {#if crawl}
          <p aria-live="polite">
            {crawl.state}: {crawl.indexed} indexed, {crawl.skipped} already indexed or skipped. Limit:
            {crawl.max_pages} pages.
          </p>
          {#if crawl.state !== 'running'}<p class="break-all">{crawl.url}</p>{/if}
          {#if crawl.error}<p class="break-all" role="status">
              Some pages could not be indexed: {crawl.error}
            </p>{/if}
        {/if}
        {#if error}<p class="break-all" role="alert">{error}</p>{/if}
      {/if}
    </div>
  {/if}
</div>
