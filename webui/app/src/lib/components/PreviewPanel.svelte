<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script lang="ts">
  import VideoPreview from './VideoPreview.svelte';
  import IssuePreview from './IssuePreview.svelte';
  import CodePreview, { parseCodePreview, type CodePreviewData } from './CodePreview.svelte';
  import { parseIssuePreview, type IssuePreviewData } from '#lib/issue-preview.js';
  import PreviewLoadError from './PreviewLoadError.svelte';
  import { apiFetch } from '#lib/api.js';
  import { resolve } from '$app/paths';
  import {
    buildPreviewUrl,
    getStoredPreviewDetailsOpen,
    setStoredPreviewDetailsOpen,
  } from '#lib/preview.svelte.js';
  import { formatTimestamp, formatMetaDate } from '#lib/search.js';
  import type {
    DocumentPreviewResponse,
    DocumentVersion,
    EmbeddedVideo,
    PreviewDocumentDetails,
    PreviewMetadata,
  } from '#lib/types.js';
  import { ScrollArea } from '@hister/components/ui/scroll-area';
  import { Button } from '@hister/components/ui/button';
  import * as DropdownMenu from '@hister/components/ui/dropdown-menu';
  import * as Tooltip from '@hister/components/ui/tooltip';
  import {
    Eye,
    X,
    Maximize2,
    Minimize2,
    History,
    MoreVertical,
    ExternalLink,
    Download,
    Info,
    Video,
  } from '@lucide/svelte';
  import { onMount, untrack } from 'svelte';

  interface Props {
    url: string;
    documentId?: string;
    hintTitle?: string;
    searchScore?: number;
    onclose: () => void;
    fullscreen?: boolean;
    onfullscreentoggle?: () => void;
    connected?: boolean;
    initialViewingVersionId?: number | null;
    onviewingversionchange?: (id: number | null) => void;
  }

  let {
    url,
    documentId = '',
    hintTitle = '',
    searchScore,
    onclose,
    fullscreen = false,
    onfullscreentoggle,
    connected = false,
    initialViewingVersionId = null,
    onviewingversionchange,
  }: Props = $props();

  let title = $state('');
  let content = $state('');
  let template = $state('');
  let templateData = $state<any>(null);
  let issueData = $state<IssuePreviewData | null>(null);
  let codeData = $state<CodePreviewData | null>(null);
  let meta = $state<PreviewMetadata | null>(null);
  let documentDetails = $state<PreviewDocumentDetails | null>(null);
  let added = $state<number | null>(null);
  let updated = $state<number | null>(null);
  let loading = $state(false);
  let previewError = $state('');
  let versionCount = $state(0);
  let versions = $state<DocumentVersion[]>([]);
  let showVersions = $state(false);
  let versionsLoading = $state(false);
  let versionsLoaded = $state(false);
  let versionsError = $state('');
  let viewingVersion = $state<DocumentVersion | null>(null);
  let extractorName = $state('');
  let availableExtractors = $state<{ name: string; description: string }[]>([]);
  let extractorsLoaded = $state(false);
  let extractorsLoading = $state(false);

  let showEmbeddedVideos = $state(false);
  let showDocumentDetails = $state(false);
  const previewMetadataEntries = $derived(
    [
      { label: 'Author', value: meta?.author },
      { label: 'Published', value: formatMetaDate(meta?.published) },
      { label: 'Content type', value: meta?.type },
      { label: 'Description', value: meta?.description },
    ].filter((entry) => entry.value),
  );
  const hasDocumentDetails = $derived(
    !!documentDetails ||
      added !== null ||
      updated !== null ||
      searchScore !== undefined ||
      previewMetadataEntries.length > 0 ||
      versionCount > 0 ||
      !!meta?.videos?.length,
  );

  type DetailEntry = { field: string; value: unknown };

  let documentDetailEntries = $derived.by((): DetailEntry[] => {
    if (!documentDetails) return [];
    return [
      { field: 'type', value: documentDetails.type },
      { field: 'language', value: documentDetails.language },
      { field: 'label', value: documentDetails.label },
      { field: 'visits', value: documentDetails.visits },
      { field: 'user_id', value: documentDetails.user_id },
    ];
  });

  let documentMetadataEntries = $derived.by((): DetailEntry[] =>
    Object.entries(documentDetails?.metadata ?? {})
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([key, value]) => ({ field: `metadata.${key}`, value })),
  );

  onMount(() => {
    showDocumentDetails = getStoredPreviewDetailsOpen();
  });

  function toggleDocumentDetails() {
    showDocumentDetails = !showDocumentDetails;
    setStoredPreviewDetailsOpen(showDocumentDetails);
  }

  function formatTechnicalValue(value: unknown): string {
    if (typeof value === 'string') return value === '' ? '""' : value;
    if (value === undefined) return 'Not set';
    const encoded = JSON.stringify(value, null, 2);
    return encoded ?? String(value);
  }

  function formatDocumentDetailValue(entry: DetailEntry): string {
    if (entry.value === '' || entry.value === null || entry.value === undefined) return 'Not set';
    return formatTechnicalValue(entry.value);
  }

  function parseTemplateData(c: string): any | null {
    try {
      return JSON.parse(c);
    } catch {
      return null;
    }
  }

  type DiffLine = { type: 'header' | 'add' | 'remove' | 'context'; text: string };

  function parseDiff(patch: string): DiffLine[] {
    return patch
      .split('\n')
      .filter((l) => l !== '')
      .map((l): DiffLine => {
        if (l.startsWith('@@')) return { type: 'header', text: l };
        if (l.startsWith('+')) return { type: 'add', text: l };
        if (l.startsWith('-')) return { type: 'remove', text: l };
        return { type: 'context', text: l };
      });
  }

  function versionTimestamp(iso: string): number {
    return new Date(iso).getTime() / 1000;
  }

  // Tracks whether this component instance has already performed its first load.
  // Plain variable (not $state) so it persists across effect runs without triggering reactivity.
  let _mountedWithDocument = '';
  let contentRequestId = 0;
  let versionsRequestId = 0;
  let retryPreview: () => void = () => {};

  function documentRequestUrl(
    path: string,
    u: string,
    selectedDocumentId: string,
    extra: Record<string, string> = {},
  ): string {
    const params = new URLSearchParams({ url: u, ...extra });
    if (selectedDocumentId) params.set('document_id', selectedDocumentId);
    return `${path}?${params.toString()}`;
  }

  // Reset all state when the document URL changes, then load with no explicit extractor.
  // On the very first run (component mount), restore initialViewingVersion if one was supplied
  // so that toggling fullscreen preserves the archived-version view.
  $effect(() => {
    const u = url;
    const selectedDocumentId = documentId;
    const hint = hintTitle;
    if (u) {
      const isFirstLoad = _mountedWithDocument === '';
      _mountedWithDocument = selectedDocumentId || u;
      extractorName = '';
      availableExtractors = [];
      extractorsLoaded = false;
      // untrack so that reading the prop here does not make the effect re-run on prop change.
      const versionId = isFirstLoad ? untrack(() => initialViewingVersionId) : null;
      loadContent(u, hint, '', versionId, selectedDocumentId);
    }
  });

  // Reload when the user picks a different extractor.
  $effect(() => {
    const name = extractorName;
    const selectedDocumentId = documentId;
    if (url && name) {
      loadContent(url, hintTitle, name, null, selectedDocumentId);
    }
  });

  async function loadContent(
    u: string,
    hint: string,
    extractor: string = '',
    versionId: number | null = null,
    selectedDocumentId: string = documentId,
  ) {
    const requestId = ++contentRequestId;
    retryPreview = () => loadContent(u, hint, extractor, versionId, selectedDocumentId);
    loading = true;
    previewError = '';
    content = '';
    template = '';
    templateData = null;
    issueData = null;
    codeData = null;
    documentDetails = null;
    showEmbeddedVideos = false;
    showVersions = false;
    // Discard version history requests belonging to the previous preview.
    versionsRequestId++;
    versionsLoading = false;
    versionsLoaded = false;
    versionsError = '';
    versions = [];
    viewingVersion = null;
    if (versionId === null) {
      meta = null;
      added = null;
      updated = null;
      title = hint;
      versionCount = 0;
    }
    try {
      const extra: Record<string, string> = {};
      if (extractor) extra.extractor = extractor;
      if (versionId != null) extra.version = String(versionId);
      const resp = await apiFetch(documentRequestUrl('/preview', u, selectedDocumentId, extra));
      if (!resp.ok) throw new Error('Preview request failed');
      const data = (await resp.json()) as DocumentPreviewResponse;
      if (requestId !== contentRequestId) return;
      template = data.template || '';
      templateData = template === 'video' ? parseTemplateData(data.content) : null;
      issueData = template === 'issue' ? parseIssuePreview(data.content) : null;
      codeData = template === 'code' ? parseCodePreview(data.content) : null;
      content =
        template === 'video' || template === 'issue' || template === 'code'
          ? ''
          : data.content || '<p>No content available</p>';
      // Always update metadata (server always returns current doc's metadata regardless of version).
      title = data.title || hint;
      added = data.added ?? null;
      updated = data.updated ?? null;
      meta = data.meta ?? null;
      documentDetails = data.details;
      versionCount = data.version_count ?? 0;
      if (data.version_id && data.version_created_at) {
        viewingVersion = {
          id: data.version_id,
          created_at: data.version_created_at,
          html_diff: '',
          text_diff: '',
        };
      }
      onviewingversionchange?.(viewingVersion?.id ?? null);
    } catch {
      if (requestId !== contentRequestId) return;
      previewError = 'Could not load this preview. Please try again.';
    } finally {
      if (requestId === contentRequestId) loading = false;
    }
  }

  async function loadExtractors(u: string) {
    if (extractorsLoaded || extractorsLoading) return;
    extractorsLoading = true;
    try {
      const resp = await apiFetch(documentRequestUrl('/extractors', u, documentId));
      if (resp.ok) {
        const data: { name: string; description: string }[] = await resp.json();
        availableExtractors = data ?? [];
      }
    } catch {
      // silently ignore
    } finally {
      extractorsLoading = false;
      extractorsLoaded = true;
    }
  }

  async function loadVersions() {
    if (versionsLoading) return;
    const requestId = ++versionsRequestId;
    versionsLoading = true;
    versionsError = '';
    try {
      const resp = await apiFetch(documentRequestUrl('/versions', url, documentId));
      if (!resp.ok) throw new Error('Version history request failed');
      const data = (await resp.json()) ?? [];
      if (requestId !== versionsRequestId) return;
      versions = data;
      versionsLoaded = true;
    } catch {
      if (requestId !== versionsRequestId) return;
      versionsError = 'Could not load previous versions. Please try again.';
    } finally {
      if (requestId === versionsRequestId) versionsLoading = false;
    }
  }

  function toggleVersions() {
    showVersions = !showVersions;
    if (showVersions && !versionsLoaded) void loadVersions();
  }
</script>

<div
  class="preview-panel border-border-brand bg-card-surface flex flex-1 flex-col overflow-hidden {fullscreen
    ? ''
    : connected
      ? 'preview-panel-connected shrink-0 border-l-0'
      : 'shrink-0 border-l-[3px]'}"
>
  {#if loading || previewError}
    <div
      class="border-border-brand-muted flex shrink-0 items-center justify-end gap-1 border-b-[2px] px-2 py-1"
    >
      <Tooltip.Provider delayDuration={0}>
        {#if onfullscreentoggle}
          <Tooltip.Root>
            <Tooltip.Trigger onclick={onfullscreentoggle}>
              {#snippet child({ props })}
                <Button
                  {...props}
                  variant="ghost"
                  size="icon-sm"
                  class="text-text-brand-muted hover:text-text-brand"
                  aria-label={fullscreen ? 'Exit fullscreen' : 'Enter fullscreen'}
                >
                  {#if fullscreen}
                    <Minimize2 class="size-4" />
                  {:else}
                    <Maximize2 class="size-4" />
                  {/if}
                </Button>
              {/snippet}
            </Tooltip.Trigger>
            <Tooltip.Content role="tooltip" side="bottom" sideOffset={6}
              >{fullscreen ? 'Exit fullscreen' : 'Enter fullscreen'}</Tooltip.Content
            >
          </Tooltip.Root>
        {/if}
        <Tooltip.Root>
          <Tooltip.Trigger onclick={onclose}>
            {#snippet child({ props })}
              <Button
                {...props}
                variant="ghost"
                size="icon-sm"
                class="text-text-brand-muted hover:text-text-brand"
                aria-label="Close preview"
              >
                <X class="size-4" />
              </Button>
            {/snippet}
          </Tooltip.Trigger>
          <Tooltip.Content role="tooltip" side="bottom" sideOffset={6}
            >Close preview</Tooltip.Content
          >
        </Tooltip.Root>
      </Tooltip.Provider>
    </div>
    <div class="flex flex-1 items-center justify-center">
      {#if previewError}
        <PreviewLoadError message={previewError} onretry={() => retryPreview()} originalUrl={url} />
      {:else}
        <span role="status" class="font-inter text-text-brand-muted text-sm">Loading…</span>
      {/if}
    </div>
  {:else if content || templateData || issueData || codeData}
    <div
      class="preview-header border-border-brand-muted flex shrink-0 flex-col gap-0.5 border-b-[2px] px-4 py-2.5"
    >
      <div class="flow-root max-h-[6.875em] overflow-hidden text-lg leading-snug md:text-3xl">
        <div class="float-right mt-1 ml-2 flex items-center gap-1 text-sm leading-normal">
          <Tooltip.Provider delayDuration={0}>
            {#if url.startsWith('file://') && documentId}
              <Tooltip.Root>
                <Tooltip.Trigger>
                  {#snippet child({ props })}
                    <Button
                      {...props}
                      variant="ghost"
                      size="icon-sm"
                      class="text-text-brand-muted hover:text-text-brand"
                      href={`${resolve('/')}api/file?id=${encodeURIComponent(documentId)}`}
                      data-sveltekit-reload
                      aria-label="Download file"
                    >
                      <Download class="size-4" />
                    </Button>
                  {/snippet}
                </Tooltip.Trigger>
                <Tooltip.Content role="tooltip" side="bottom" sideOffset={6}
                  >Download file</Tooltip.Content
                >
              </Tooltip.Root>
            {/if}
            <DropdownMenu.Root
              onOpenChange={(open) => {
                if (open) loadExtractors(url);
              }}
            >
              <Tooltip.Root>
                <Tooltip.Trigger>
                  {#snippet child({ props: tooltipProps })}
                    <DropdownMenu.Trigger {...tooltipProps}>
                      {#snippet child({ props })}
                        <Button
                          {...props}
                          variant="ghost"
                          size="icon-sm"
                          class="text-text-brand-muted hover:text-text-brand shrink-0 cursor-pointer"
                          aria-label="Change extractor"
                        >
                          <MoreVertical class="size-4" />
                        </Button>
                      {/snippet}
                    </DropdownMenu.Trigger>
                  {/snippet}
                </Tooltip.Trigger>
                <Tooltip.Content role="tooltip" side="bottom" sideOffset={6}
                  >Change extractor</Tooltip.Content
                >
              </Tooltip.Root>
              <DropdownMenu.Content
                class="border-brutal-border bg-card-surface w-44 rounded-none border-[3px] p-3 shadow-[4px_4px_0_var(--brutal-shadow)]"
              >
                <div class="space-y-2">
                  <p
                    class="font-outfit text-text-brand-muted mb-1 text-xs font-bold tracking-widest uppercase"
                  >
                    Extractor
                  </p>
                  {#if extractorsLoading}
                    <p class="font-inter text-text-brand-muted text-xs">Loading…</p>
                  {:else if availableExtractors.length}
                    <DropdownMenu.RadioGroup
                      value={extractorName || availableExtractors[0].name}
                      onValueChange={(v) => {
                        extractorName = v;
                      }}
                    >
                      {#each availableExtractors as ext, i (ext.name)}
                        <DropdownMenu.RadioItem value={ext.name}>{ext.name}</DropdownMenu.RadioItem>
                      {/each}
                    </DropdownMenu.RadioGroup>
                  {:else}
                    <p class="font-inter text-text-brand-muted text-xs">No extractors available</p>
                  {/if}
                </div>
              </DropdownMenu.Content>
            </DropdownMenu.Root>
            <Tooltip.Root>
              <Tooltip.Trigger>
                {#snippet child({ props })}
                  <Button
                    {...props}
                    variant="ghost"
                    size="icon-sm"
                    class="text-text-brand-muted hover:text-text-brand"
                    href={buildPreviewUrl(url, title || hintTitle, viewingVersion?.id, documentId)}
                    target="_blank"
                    rel="noopener noreferrer"
                    aria-label="Open direct preview"
                  >
                    <ExternalLink class="size-4" />
                  </Button>
                {/snippet}
              </Tooltip.Trigger>
              <Tooltip.Content role="tooltip" side="bottom" sideOffset={6}
                >Open direct preview</Tooltip.Content
              >
            </Tooltip.Root>
            {#if hasDocumentDetails}
              <Tooltip.Root>
                <Tooltip.Trigger onclick={toggleDocumentDetails}>
                  {#snippet child({ props })}
                    <Button
                      {...props}
                      variant="ghost"
                      size="icon-sm"
                      class={showDocumentDetails
                        ? 'text-hister-teal hover:text-hister-teal'
                        : 'text-text-brand-muted hover:text-text-brand'}
                      aria-label={showDocumentDetails
                        ? 'Hide document details'
                        : 'Show document details'}
                      aria-expanded={showDocumentDetails}
                      aria-controls="preview-document-details"
                    >
                      <Info class="size-4" />
                    </Button>
                  {/snippet}
                </Tooltip.Trigger>
                <Tooltip.Content role="tooltip" side="bottom" sideOffset={6}
                  >{showDocumentDetails
                    ? 'Hide document details'
                    : 'Show document details'}</Tooltip.Content
                >
              </Tooltip.Root>
            {/if}
            {#if onfullscreentoggle}
              <Tooltip.Root>
                <Tooltip.Trigger onclick={onfullscreentoggle}>
                  {#snippet child({ props })}
                    <Button
                      {...props}
                      variant="ghost"
                      size="icon-sm"
                      class="hover:text-text-brand"
                      aria-label={fullscreen ? 'Exit fullscreen' : 'Enter fullscreen'}
                    >
                      {#if fullscreen}
                        <Minimize2 class="size-4" />
                      {:else}
                        <Maximize2 class="size-4" />
                      {/if}
                    </Button>
                  {/snippet}
                </Tooltip.Trigger>
                <Tooltip.Content role="tooltip" side="bottom" sideOffset={6}
                  >{fullscreen ? 'Exit fullscreen' : 'Enter fullscreen'}</Tooltip.Content
                >
              </Tooltip.Root>
            {/if}
            <Tooltip.Root>
              <Tooltip.Trigger onclick={onclose}>
                {#snippet child({ props })}
                  <Button
                    {...props}
                    variant="ghost"
                    size="icon-sm"
                    class="hover:text-text-brand"
                    aria-label="Close preview"
                  >
                    <X class="size-4" />
                  </Button>
                {/snippet}
              </Tooltip.Trigger>
              <Tooltip.Content role="tooltip" side="bottom" sideOffset={6}
                >Close preview</Tooltip.Content
              >
            </Tooltip.Root>
          </Tooltip.Provider>
        </div>
        <h2 class="font-outfit text-text-brand font-bold">
          {#if url.startsWith('remote-file://')}
            {title}
          {:else}
            <a href={url} target="_blank" rel="noopener noreferrer" class="hover:underline"
              >{title}</a
            >
          {/if}
        </h2>
      </div>
      {#if showDocumentDetails && hasDocumentDetails}
        <div
          id="preview-document-details"
          class="border-border-brand-muted mt-2 max-h-[min(20rem,40vh)] overflow-y-auto overscroll-contain border-t pt-2"
        >
          {#if versionCount > 0 || meta?.videos?.length}
            <div class="mb-3 flex flex-wrap items-center gap-x-4 gap-y-2">
              {#if versionCount > 0}
                <button
                  onclick={toggleVersions}
                  aria-expanded={showVersions}
                  class="font-inter text-hister-teal inline-flex cursor-pointer items-center gap-1 text-xs hover:underline"
                >
                  <History class="size-3" />
                  {versionCount}
                  {versionCount === 1 ? 'previous version' : 'previous versions'}
                </button>
              {/if}
              {#if meta?.videos?.length}
                <button
                  onclick={() => (showEmbeddedVideos = !showEmbeddedVideos)}
                  aria-expanded={showEmbeddedVideos}
                  class="font-inter inline-flex cursor-pointer items-center gap-1.5 text-xs {showEmbeddedVideos
                    ? 'text-hister-teal'
                    : 'text-text-brand-muted hover:text-text-brand'}"
                >
                  <Video class="size-3.5 shrink-0" />
                  {showEmbeddedVideos ? 'Hide' : 'Show'} embedded
                  {meta.videos.length === 1 ? 'video' : 'videos'}
                </button>
              {/if}
            </div>
          {/if}
          <p
            class="font-outfit text-text-brand-muted mb-1 text-xs font-bold tracking-widest uppercase"
          >
            Current document properties
          </p>
          <dl class="font-inter text-xs">
            {#each previewMetadataEntries as entry (entry.label)}
              <div
                class="border-border-brand-muted grid gap-1 border-b py-1.5 last:border-b-0 sm:grid-cols-[minmax(8rem,12rem)_minmax(0,1fr)] sm:gap-3"
              >
                <dt class="text-text-brand-muted">{entry.label}</dt>
                <dd class="text-text-brand-secondary min-w-0 wrap-anywhere whitespace-pre-wrap">
                  {entry.value}
                </dd>
              </div>
            {/each}
            {#each [{ label: 'Added', timestamp: added }, { label: 'Updated', timestamp: updated }] as { label, timestamp }}
              {#if timestamp !== null}
                <div
                  class="border-border-brand-muted grid gap-1 border-b py-1.5 last:border-b-0 sm:grid-cols-[minmax(8rem,12rem)_minmax(0,1fr)] sm:gap-3"
                >
                  <dt class="text-text-brand-muted">{label}</dt>
                  <dd class="text-text-brand-secondary">
                    <time datetime={new Date(timestamp * 1000).toISOString()}>
                      {formatTimestamp(timestamp)}
                    </time>
                  </dd>
                </div>
              {/if}
            {/each}
            {#if searchScore !== undefined}
              <div
                class="border-border-brand-muted grid gap-1 border-b py-1.5 last:border-b-0 sm:grid-cols-[minmax(8rem,12rem)_minmax(0,1fr)] sm:gap-3"
              >
                <dt class="text-text-brand-muted">Search relevance score</dt>
                <dd class="text-text-brand-secondary">{searchScore.toFixed(2)}</dd>
              </div>
            {/if}
            {#each documentDetailEntries as entry (entry.field)}
              <div
                class="border-border-brand-muted grid gap-1 border-b py-1.5 last:border-b-0 sm:grid-cols-[minmax(8rem,12rem)_minmax(0,1fr)] sm:gap-3"
              >
                <dt><code class="text-hister-teal break-all">{entry.field}</code></dt>
                <dd class="text-text-brand-secondary min-w-0 break-all whitespace-pre-wrap">
                  {formatDocumentDetailValue(entry)}
                </dd>
              </div>
            {/each}
          </dl>
          {#if documentDetails}
            <p
              class="font-outfit text-text-brand-muted mt-3 mb-1 text-xs font-bold tracking-widest uppercase"
            >
              Metadata
            </p>
            {#if documentMetadataEntries.length}
              <dl class="font-inter text-xs">
                {#each documentMetadataEntries as entry (entry.field)}
                  <div
                    class="border-border-brand-muted grid gap-1 border-b py-1.5 last:border-b-0 sm:grid-cols-[minmax(8rem,12rem)_minmax(0,1fr)] sm:gap-3"
                  >
                    <dt><code class="text-hister-teal break-all">{entry.field}</code></dt>
                    <dd class="text-text-brand-secondary min-w-0 break-all whitespace-pre-wrap">
                      {formatTechnicalValue(entry.value)}
                    </dd>
                  </div>
                {/each}
              </dl>
            {:else}
              <p class="font-inter text-text-brand-muted text-xs">No metadata stored.</p>
            {/if}
          {/if}
        </div>
      {/if}
    </div>
    {#if viewingVersion}
      <div
        class="border-border-brand-muted bg-card-surface-muted flex shrink-0 items-center gap-1.5 border-b-[2px] px-4 py-2"
      >
        <History class="text-hister-teal size-3.5 shrink-0" />
        <span class="font-inter text-text-brand-muted text-xs">
          Viewing archived version from {formatTimestamp(
            versionTimestamp(viewingVersion.created_at),
          )}
        </span>
        <span class="text-text-brand-muted text-xs">·</span>
        <button
          onclick={() => loadContent(url, hintTitle, extractorName)}
          class="font-inter text-hister-teal cursor-pointer text-xs hover:underline"
        >
          Show current
        </button>
      </div>
    {/if}
    <ScrollArea class="min-h-0 flex-1 pr-2.5">
      {#if showVersions && versionsLoading}
        <p role="status" class="font-inter text-text-brand-muted p-4 text-sm">
          Loading previous versions…
        </p>
      {:else if showVersions && versionsError}
        <PreviewLoadError message={versionsError} onretry={loadVersions} />
      {:else if showVersions}
        <div class="flex flex-col divide-y divide-[var(--border-brand-muted)] p-4">
          {#each versions as v}
            <div class="py-4 first:pt-0 last:pb-0">
              <div class="mb-2 flex items-center justify-between gap-2">
                <p class="font-inter text-xs font-semibold tracking-wide uppercase">
                  {formatTimestamp(versionTimestamp(v.created_at))}
                </p>
                <button
                  onclick={() => loadContent(url, hintTitle, extractorName, v.id)}
                  class="font-inter text-hister-teal shrink-0 cursor-pointer text-xs hover:underline"
                >
                  show this version
                </button>
              </div>
              {#if v.text_diff || v.html_diff}
                <details class="group">
                  <summary
                    class="font-inter text-text-brand-muted hover:text-text-brand flex cursor-pointer list-none items-center gap-1 text-xs select-none"
                  >
                    <span class="inline-block transition-transform group-open:rotate-90">▶</span>
                    <span>show diff</span>
                  </summary>
                  <div class="mt-2 overflow-x-auto rounded font-mono text-xs leading-relaxed">
                    {#each parseDiff(v.text_diff || v.html_diff) as line}
                      <div
                        class="px-2 py-px break-all whitespace-pre-wrap {line.type === 'add'
                          ? 'bg-black text-green-300'
                          : line.type === 'remove'
                            ? 'bg-black text-red-300'
                            : line.type === 'header'
                              ? 'text-text-brand'
                              : 'text-text-brand-secondary'}"
                      >
                        {line.text}
                      </div>
                    {/each}
                  </div>
                </details>
              {:else}
                <p class="font-inter text-xs italic">No diff recorded.</p>
              {/if}
            </div>
          {:else}
            <p class="font-inter text-text-brand-muted text-sm">No previous versions available.</p>
          {/each}
        </div>
      {:else}
        <div
          class="preview-content font-inter text-text-brand-secondary prose dark:prose-invert prose-a:text-hister-teal w-full p-4 text-sm {template ===
          'code'
            ? 'max-w-none'
            : 'max-w-[60em]'}"
        >
          {#if meta?.videos?.length && showEmbeddedVideos}
            <div class="not-prose mb-6 space-y-4">
              {#each meta.videos as video (video.url)}
                {@const v = video as EmbeddedVideo}
                {#if v.type === 'iframe'}
                  <div class="relative aspect-video w-full overflow-hidden">
                    <iframe
                      src={v.url}
                      class="absolute inset-0 h-full w-full"
                      title="Embedded video"
                      allowfullscreen
                      allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share"
                      referrerpolicy="strict-origin-when-cross-origin"
                    ></iframe>
                  </div>
                {:else if v.type === 'video'}
                  <video controls class="w-full">
                    {#if v.mime}
                      <source src={v.url} type={v.mime} />
                    {:else}
                      <source src={v.url} />
                    {/if}
                  </video>
                {:else if v.type === 'embed'}
                  <div class="relative aspect-video w-full overflow-hidden">
                    <embed
                      src={v.url}
                      type={v.mime || 'video/mp4'}
                      class="absolute inset-0 h-full w-full"
                    />
                  </div>
                {:else if v.type === 'object'}
                  <div class="relative aspect-video w-full overflow-hidden">
                    <object
                      data={v.url}
                      type={v.mime || 'video/mp4'}
                      class="absolute inset-0 h-full w-full"
                      title="Embedded video"
                    >
                      <p class="font-inter text-text-brand-muted p-2 text-xs">
                        Video playback not supported.
                      </p>
                    </object>
                  </div>
                {/if}
              {/each}
            </div>
          {/if}
          {#if template === 'video' && templateData}
            <VideoPreview data={templateData} />
          {:else if template === 'issue' && issueData}
            <IssuePreview data={issueData} />
          {:else if template === 'code' && codeData}
            <CodePreview data={codeData} documentKey={documentId || url} />
          {:else}
            {@html content}
          {/if}
          {#if meta?.jsonld}
            <details class="not-prose border-border-brand-muted mt-6 border-t pt-3">
              <summary
                class="font-inter text-text-brand-muted cursor-pointer text-xs tracking-wide uppercase"
              >
                Extracted JSON-LD ({meta.jsonld.length})
              </summary>
              <pre
                class="bg-card-surface-muted text-text-brand-secondary mt-2 overflow-x-auto rounded p-2 text-[11px] leading-snug">{JSON.stringify(
                  meta.jsonld,
                  null,
                  2,
                )}</pre>
            </details>
          {/if}
        </div>
      {/if}
    </ScrollArea>
  {:else}
    <div
      class="border-border-brand-muted flex shrink-0 items-center justify-end gap-1 border-b-[2px] px-2 py-1"
    >
      {#if onfullscreentoggle}
        <Button
          variant="ghost"
          size="icon-sm"
          class="text-text-brand-muted hover:text-text-brand"
          onclick={onfullscreentoggle}
          title={fullscreen ? 'Exit fullscreen' : 'Enter fullscreen'}
        >
          {#if fullscreen}
            <Minimize2 class="size-4" />
          {:else}
            <Maximize2 class="size-4" />
          {/if}
        </Button>
      {/if}
      <Button
        variant="ghost"
        size="icon-sm"
        class="text-text-brand-muted hover:text-text-brand"
        onclick={onclose}
        aria-label="Close preview"
      >
        <X class="size-4" />
      </Button>
    </div>
    <div class="flex flex-1 flex-col items-center justify-center gap-2 opacity-40">
      <Eye class="size-6" />
      <p class="font-inter text-text-brand-muted text-sm">Focus a result to read it</p>
    </div>
  {/if}
</div>

<style>
  .preview-panel-connected .preview-header {
    background: var(--card-surface);
  }

  .preview-content :global(.pdf-preview) {
    display: grid;
    gap: 1.5rem;
    min-width: 0;
  }

  .preview-content :global(.pdf-page) {
    min-width: 0;
    padding: clamp(1rem, 3vw, 2.5rem);
    border: 1px solid var(--border-muted-brand);
    border-radius: 0.25rem;
    background: var(--card-surface);
    color: var(--text-primary-brand);
    font-family: Georgia, 'Times New Roman', serif;
    font-size: 1rem;
    line-height: 1.75;
    overflow-wrap: anywhere;
  }

  .preview-content :global(.pdf-page-number) {
    margin: 0 0 1.5rem;
    color: var(--text-muted-brand);
    font-family: var(--font-inter);
    font-size: 0.75rem;
    font-weight: 500;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .preview-content :global(.pdf-page p) {
    margin: 0 0 1em;
    white-space: pre-wrap;
    tab-size: 4;
  }

  .preview-content :global(.pdf-page p:last-child) {
    margin-bottom: 0;
  }
</style>
