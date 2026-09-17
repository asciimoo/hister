<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script lang="ts">
  import { LoaderCircle } from '@lucide/svelte';
</script>

<section
  class="flex min-h-[40vh] flex-col items-center justify-center gap-3 px-6 py-12 text-center"
  role="status"
  aria-live="polite"
>
  <LoaderCircle class="text-hister-indigo size-9 motion-safe:animate-spin" aria-hidden="true" />
  <h2 class="font-outfit text-text-brand text-xl font-semibold">Searching…</h2>
  <p class="font-inter text-text-brand-muted text-sm">Results will appear here.</p>
</section>
