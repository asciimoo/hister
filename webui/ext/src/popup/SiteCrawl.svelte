<script lang="ts">
  import { Button } from '@hister/components/ui/button';
  import { Input } from '@hister/components/ui/input';
  import { Label } from '@hister/components/ui/label';
  import { buildCrawlCommand } from './crawl-command';

  let {
    serverURL,
    tabURL,
    hasCustomHeaders,
  }: {
    serverURL: string;
    tabURL: string;
    hasCustomHeaders: boolean;
  } = $props();

  let expanded = $state(false);
  let maxPages = $state(100);
  let delay = $state(1);
  let feedback = $state('');
  const result = $derived.by(() => {
    try {
      return { ...buildCrawlCommand(serverURL, tabURL, maxPages, delay), error: '' };
    } catch (error) {
      return { command: '', siteURL: '', error: (error as Error).message };
    }
  });

  $effect(() => {
    result.command;
    feedback = '';
  });

  async function copyCommand() {
    const command = result.command;
    if (!command) return;
    try {
      await navigator.clipboard.writeText(command);
      if (result.command === command) feedback = 'Command copied. Run it in your terminal.';
    } catch {
      if (result.command === command) {
        feedback = 'Could not copy. Select the command below and copy it manually.';
      }
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
      <p>
        Generate a command to run on your computer with Hister installed. Nothing starts until you
        run it in a terminal (bash or zsh).
      </p>
      <Label for="crawl-limit">Page limit (1–1000)</Label>
      <Input id="crawl-limit" type="number" min={1} max={1000} step={1} bind:value={maxPages} />
      <Label for="crawl-delay">Request delay in seconds (1–60)</Label>
      <Input id="crawl-delay" type="number" min={1} max={60} step={1} bind:value={delay} />
      {#if result.error}
        <p role="alert">{result.error}</p>
      {:else}
        <p>
          Start at <strong class="break-all">{result.siteURL}</strong> and follow links on this origin.
          Results go to your configured Hister server. Normal CLI crawler settings apply.
        </p>
        <Label for="crawl-command">Crawl command</Label>
        <textarea
          id="crawl-command"
          readonly
          rows={6}
          class="border-brutal-border w-full resize-y border-2 p-2 font-mono text-xs"
          value={result.command}
          onclick={(event) => event.currentTarget.select()}></textarea>
      {/if}
      <Button disabled={!result.command} onclick={copyCommand}>Copy crawl command</Button>
      {#if feedback}<p role="status">{feedback}</p>{/if}
      <p>
        If authentication is required, configure app.access_token in your local Hister
        configuration. Extension credentials, browser sessions, and the public documents setting are
        not copied. Keep the terminal open; use Ctrl+C to stop.
      </p>
      {#if hasCustomHeaders}
        <p>
          Your extension uses custom headers. Configure any required proxy access separately for the
          CLI. Its --header option sends headers to crawled websites, not to Hister.
        </p>
      {/if}
    </div>
  {/if}
</div>
