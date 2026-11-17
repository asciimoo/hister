<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script lang="ts">
  import { Button } from '@nogon/components/ui/button';
  import { ExternalLink, RotateCcw } from '@lucide/svelte';

  let {
    message,
    onretry,
    originalUrl = '',
  }: {
    message: string;
    onretry: () => void;
    originalUrl?: string;
  } = $props();

  // Only web documents have an original page the browser can open.
  const originalHref = $derived.by(() => {
    try {
      const parsed = new URL(originalUrl);
      return parsed.protocol === 'http:' || parsed.protocol === 'https:' ? parsed.href : '';
    } catch {
      return '';
    }
  });
</script>

<div class="font-inter flex flex-col items-start gap-3 p-4 text-sm">
  <p role="alert" class="text-text-brand-secondary">{message}</p>
  <div class="flex flex-wrap gap-2">
    <Button variant="outline" size="sm" onclick={onretry}>
      <RotateCcw class="size-3.5" />
      Retry
    </Button>
    {#if originalHref}
      <Button
        variant="outline"
        size="sm"
        href={originalHref}
        target="_blank"
        rel="noopener noreferrer"
      >
        <ExternalLink class="size-3.5" />
        Open original
      </Button>
    {/if}
  </div>
</div>
