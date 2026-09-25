<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script lang="ts">
  import { onMount } from 'svelte';
  import { Button } from '@hister/components/ui/button';
  import { ArrowLeft } from '@lucide/svelte';
  import PreviewPanel from '$lib/components/PreviewPanel.svelte';
  import { replacePreviewHistory } from '$lib/preview';
  import { base } from '$app/paths';

  let docUrl = $state('');
  let documentId = $state('');
  let docTitle = $state('');
  let panelVersionId = $state<number | null>(null);

  function readParams() {
    const params = new URLSearchParams(window.location.search);
    docUrl = params.get('id') || '';
    documentId = params.get('document_id') || '';
    docTitle = params.get('title') || '';
    const v = params.get('version');
    panelVersionId = v ? parseInt(v, 10) || null : null;
  }

  onMount(() => {
    readParams();
  });
</script>

<svelte:window onpopstate={readParams} />

<svelte:head>
  <title>{docTitle ? `${docTitle} - Hister Preview` : 'Hister Preview'}</title>
</svelte:head>

<div class="flex min-h-0 flex-1 flex-col overflow-hidden">
  {#if docUrl}
    <PreviewPanel
      url={docUrl}
      {documentId}
      hintTitle={docTitle}
      fullscreen={true}
      initialViewingVersionId={panelVersionId}
      onviewingversionchange={(id) => {
        panelVersionId = id;
        replacePreviewHistory(docUrl, docTitle, id, documentId);
      }}
      onnavigate={(u, title, selectedDocumentId) => {
        docUrl = u;
        docTitle = title;
        documentId = selectedDocumentId;
      }}
      onclose={() => {
        try {
          const ref = document.referrer;
          if (ref && new URL(ref).origin === window.location.origin) {
            window.history.back();
            return;
          }
        } catch {
          // ignore referrer parse errors
        }
        window.location.href = base + '/';
      }}
    />
  {:else}
    <div class="flex flex-1 flex-col items-center justify-center gap-4">
      <p class="font-inter text-text-brand-secondary">No document URL specified.</p>
      <Button
        variant="outline"
        href={base + '/'}
        class="font-inter gap-2 rounded-none border-[2px]"
      >
        <ArrowLeft class="size-4" />
        Back to search
      </Button>
    </div>
  {/if}
</div>
