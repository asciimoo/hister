<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script lang="ts">
  import VideoPreview from './VideoPreview.svelte';
  import { apiFetch } from '$lib/api';
  import {
    buildPreviewUrl,
    getStoredPreviewDetailsOpen,
    setStoredPreviewDetailsOpen,
  } from '$lib/preview';
  import { formatTimestamp, formatMetaDate } from '$lib/search';
  import type {
    DocumentPreviewResponse,
    DocumentVersion,
    EmbeddedVideo,
    PreviewDocumentDetails,
    PreviewMetadata,
  } from '$lib/types';
  import { ScrollArea } from '@hister/components/ui/scroll-area';
  import { Button } from '@hister/components/ui/button';
  import * as DropdownMenu from '@hister/components/ui/dropdown-menu';
  import {
    Eye,
    X,
    Maximize2,
    Minimize2,
    History,
    MoreVertical,
    ExternalLink,
    Info,
    Video,
  } from '@lucide/svelte';
  import { onMount, untrack } from 'svelte';

  interface Props {
    url: string;
    documentId?: string;
    hintTitle?: string;
    onclose: () => void;
    fullscreen?: boolean;
    onfullscreentoggle?: () => void;
    connected?: boolean;
    initialViewingVersionId?: number | null;
    onviewingversionchange?: (id: number | null) => void;
    onnavigate?: (url: string, title: string) => void;
  }

  let {
    url,
    documentId = '',
    hintTitle = '',
    onclose,
    fullscreen = false,
    onfullscreentoggle,
    connected = false,
    initialViewingVersionId = null,
    onviewingversionchange,
    onnavigate,
  }: Props = $props();

  // the document currently displayed
  let activeUrl = $state('');
  let activeDocumentId = $state('');
  let activeHintTitle = $state('');

  let title = $state('');
  let content = $state('');
  let template = $state('');
  let templateData = $state<any>(null);
  let meta = $state<PreviewMetadata | null>(null);
  let documentDetails = $state<PreviewDocumentDetails | null>(null);
  let added = $state<number | null>(null);
  let updated = $state<number | null>(null);
  let loading = $state(false);
  let versionCount = $state(0);
  let versions = $state<DocumentVersion[]>([]);
  let showVersions = $state(false);
  let viewingVersion = $state<DocumentVersion | null>(null);
  let extractorName = $state('');
  let availableExtractors = $state<{ name: string; description: string }[]>([]);
  let extractorsLoaded = $state(false);
  let extractorsLoading = $state(false);

  let showEmbeddedVideos = $state(false);
  let showDocumentDetails = $state(false);

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

  function resetExtractors() {
    extractorName = '';
    availableExtractors = [];
    extractorsLoaded = false;
  }

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
      // The parent echoing back a document this panel navigated to by itself: it is already
      // shown, so only adopt the (possibly more precise) identity without reloading it.
      const alreadyShown = untrack(
        () => u === activeUrl && (!activeDocumentId || selectedDocumentId === activeDocumentId),
      );
      if (alreadyShown) {
        activeDocumentId = selectedDocumentId;
        activeHintTitle = hint;
        return;
      }
      const isFirstLoad = _mountedWithDocument === '';
      _mountedWithDocument = selectedDocumentId || u;
      activeUrl = u;
      activeDocumentId = selectedDocumentId;
      activeHintTitle = hint;
      resetExtractors();
      // untrack so that reading the prop here does not make the effect re-run on prop change.
      const versionId = isFirstLoad ? untrack(() => initialViewingVersionId) : null;
      loadContent(u, hint, '', versionId, selectedDocumentId);
    }
  });

  // Reload when the user picks a different extractor.
  $effect(() => {
    const name = extractorName;
    const selectedDocumentId = activeDocumentId;
    if (activeUrl && name) {
      loadContent(activeUrl, activeHintTitle, name, null, selectedDocumentId);
    }
  });

  async function loadContent(
    u: string,
    hint: string,
    extractor: string = '',
    versionId: number | null = null,
    selectedDocumentId: string = activeDocumentId,
  ) {
    loading = true;
    content = '';
    template = '';
    templateData = null;
    documentDetails = null;
    showEmbeddedVideos = false;
    showVersions = false;
    viewingVersion = null;
    if (versionId === null) {
      meta = null;
      added = null;
      updated = null;
      title = hint;
      versions = [];
      versionCount = 0;
    }
    try {
      const extra: Record<string, string> = {};
      if (extractor) extra.extractor = extractor;
      if (versionId != null) extra.version = String(versionId);
      const resp = await apiFetch(documentRequestUrl('/preview', u, selectedDocumentId, extra));
      if (!resp.ok) {
        content = `<p class="text-hister-rose">Failed to load readable content. Status: ${resp.status}</p>`;
      } else {
        const data = (await resp.json()) as DocumentPreviewResponse;
        template = data.template || '';
        templateData = template === 'video' ? parseTemplateData(data.content) : null;
        content = template === 'video' ? '' : data.content || '<p>No content available</p>';
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
      }
    } catch (err) {
      content = `<p class="text-hister-rose">Failed to load: ${err}</p>`;
    } finally {
      loading = false;
    }
  }

  async function loadExtractors(u: string) {
    if (extractorsLoaded || extractorsLoading) return;
    extractorsLoading = true;
    try {
      const resp = await apiFetch(documentRequestUrl('/extractors', u, activeDocumentId));
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

  // Shows another document in this panel, as if it had been focused from the results list.
  function openDocument(u: string, hint: string, selectedDocumentId: string = '') {
    activeUrl = u;
    activeDocumentId = selectedDocumentId;
    activeHintTitle = hint;
    resetExtractors();
    loadContent(u, hint, '', null, selectedDocumentId);
    onnavigate?.(u, hint);
  }

  async function isArchived(u: string): Promise<boolean> {
    try {
      // HEAD skips the document body server side: we only care about the status.
      const resp = await apiFetch(documentRequestUrl('/document', u, ''), {
        method: 'HEAD',
        redirectOnForbidden: false,
      });
      return resp.ok;
    } catch {
      return false;
    }
  }

  function resolveContentHref(anchor: HTMLAnchorElement): URL | null {
    const href = anchor.getAttribute('href') ?? '';
    if (!href || href.startsWith('#')) return null;
    try {
      // Relative links belong to the archived page, not to the app serving the panel.
      return new URL(href, /^https?:\/\//i.test(activeUrl) ? activeUrl : anchor.href);
    } catch {
      return null;
    }
  }

  function followLink(anchor: HTMLAnchorElement, u: string) {
    if (anchor.target && anchor.target !== '_self') {
      window.open(u, anchor.target, 'noopener,noreferrer');
    } else {
      window.location.href = u;
    }
  }

  // Links inside extracted content point at the live web. When the target is archived too we
  // stay in the panel and show the stored copy; otherwise the click behaves like a plain anchor.
  async function handleContentClick(event: MouseEvent) {
    if (event.defaultprevented || event.button !== 0) return;
    if (event.metakey || event.ctrlkey || event.shiftkey || event.altkey) return;
    const anchor = (event.target as Element | null)?.closest('a');
    if (!anchor) return;
    const resolved = resolveContentHref(anchor);
    // In-page anchors and non-web schemes (mailto:, …) keep their native behavior.
    if (!resolved || (resolved.protocol !== 'http:' && resolved.protocol !== 'https:')) return;
    // The lookup is asynchronous, so the navigation has to be cancelled up front and
    // replayed below when the target turns out not to be archived.
    event.preventDefault();
    resolved.hash = '';

    const target = resolved.href;
    const hint = anchor.textContent?.trim() || '';

    if (await isArchived(target)) {
      openDocument(target, hint);
    } else {
      followLink(anchor, target);
    }
  }

  async function toggleVersions(u: string) {
    if (showVersions) {
      showVersions = false;
      return;
    }
    if (versions.length === 0) {
      try {
        const resp = await apiFetch(documentRequestUrl('/versions', u, activeDocumentId));
        if (resp.ok) {
          versions = (await resp.json()) ?? [];
        }
      } catch {
        // silently ignore
      }
    }
    showVersions = true;
  }
</script>

<div
  class="preview-panel border-border-brand bg-card-surface flex flex-1 flex-col overflow-hidden {fullscreen
    ? ''
    : connected
      ? 'preview-panel-connected shrink-0 border-l-0'
      : 'shrink-0 border-l-[3px]'}"
>
  {#if loading}
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
      >
        <X class="size-4" />
      </Button>
    </div>
    <div class="flex flex-1 items-center justify-center">
      <span class="font-inter text-text-brand-muted text-sm">Loading…</span>
    </div>
  {:else if content || templateData}
    <div
      class="preview-header border-border-brand-muted flex shrink-0 flex-col gap-0.5 border-b-[2px] px-4 py-2.5"
    >
      <div class="flow-root max-h-[6.875em] overflow-hidden text-lg leading-snug md:text-3xl">
        <div class="float-right mt-1 ml-2 flex items-center gap-1 text-sm leading-normal">
          <DropdownMenu.Root
            onOpenChange={(open) => {
              if (open) loadExtractors(activeUrl);
            }}
          >
            <DropdownMenu.Trigger>
              {#snippet child({ props })}
                <Button
                  {...props}
                  variant="ghost"
                  size="icon-sm"
                  class="text-text-brand-muted hover:text-text-brand shrink-0 cursor-pointer"
                  title="Change extractor"
                >
                  <MoreVertical class="size-4" />
                </Button>
              {/snippet}
            </DropdownMenu.Trigger>
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
          <Button
            variant="ghost"
            size="icon-sm"
            class="text-text-brand-muted hover:text-text-brand"
            href={buildPreviewUrl(
              activeUrl,
              title || activeHintTitle,
              viewingVersion?.id,
              activeDocumentId,
            )}
            target="_blank"
            rel="noopener noreferrer"
            title="Open direct preview"
            aria-label="Open direct preview"
          >
            <ExternalLink class="size-4" />
          </Button>
          {#if documentDetails}
            <Button
              variant="ghost"
              size="icon-sm"
              class={showDocumentDetails
                ? 'text-hister-teal hover:text-hister-teal'
                : 'text-text-brand-muted hover:text-text-brand'}
              onclick={toggleDocumentDetails}
              title={showDocumentDetails ? 'Hide document details' : 'Show document details'}
              aria-label={showDocumentDetails ? 'Hide document details' : 'Show document details'}
              aria-expanded={showDocumentDetails}
              aria-controls="preview-document-details"
            >
              <Info class="size-4" />
            </Button>
          {/if}
          {#if onfullscreentoggle}
            <Button
              variant="ghost"
              size="icon-sm"
              class="hover:text-text-brand"
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
          <Button variant="ghost" size="icon-sm" class="hover:text-text-brand" onclick={onclose}>
            <X class="size-4" />
          </Button>
        </div>
        <h2 class="font-outfit text-text-brand font-bold">
          {#if activeUrl.startsWith('remote-file://')}
            {title}
          {:else}
            <a href={activeUrl} target="_blank" rel="noopener noreferrer" class="hover:underline"
              >{title}</a
            >
          {/if}
        </h2>
      </div>
      {#if meta?.author || meta?.published || meta?.type}
        <span class="font-inter text-text-brand-muted text-xs">
          {#if meta?.author}<span>{meta.author}</span>{/if}
          {#if meta?.author && meta?.published}<span class="mx-1">·</span>{/if}
          {#if meta?.published}<span>{formatMetaDate(meta.published)}</span>{/if}
          {#if (meta?.author || meta?.published) && meta?.type}<span class="mx-1">·</span>{/if}
          {#if meta?.type}<span class="uppercase">{meta.type}</span>{/if}
        </span>
      {/if}
      {#if added}
        <span
          class="font-inter inline-flex flex-wrap items-center gap-1.5 text-xs"
          title={formatTimestamp(added)}
        >
          <span>added {formatTimestamp(added)}</span>
          {#if updated && updated !== added}
            <span class="text-text-brand-muted">·</span>
            <span title={formatTimestamp(updated)}>updated {formatTimestamp(updated)}</span>
          {/if}
          {#if versionCount > 0}
            <span class="text-text-brand-muted">·</span>
            <button
              onclick={() => toggleVersions(activeUrl)}
              class="font-inter text-hister-teal inline-flex cursor-pointer items-center gap-1 text-xs hover:underline"
            >
              <History class="size-3" />
              {versionCount}
              {versionCount === 1 ? 'previous version' : 'previous versions'}
            </button>
          {/if}
        </span>
      {/if}
      {#if meta?.description}
        <p class="font-inter text-text-brand-secondary mt-1 line-clamp-3 max-w-[60em] text-sm">
          {meta.description}
        </p>
      {/if}
      {#if meta?.videos?.length}
        <button
          onclick={() => (showEmbeddedVideos = !showEmbeddedVideos)}
          class="font-inter mt-1 inline-flex cursor-pointer items-center gap-1.5 text-xs {showEmbeddedVideos
            ? 'text-hister-teal'
            : 'text-text-brand-muted hover:text-text-brand'}"
        >
          <Video class="size-3.5 shrink-0" />
          {showEmbeddedVideos ? 'Hide' : 'Show'} embedded
          {meta.videos.length === 1 ? 'video' : 'videos'}
        </button>
      {/if}
      {#if showDocumentDetails && documentDetails}
        <div
          id="preview-document-details"
          class="border-border-brand-muted mt-2 max-h-[min(20rem,40vh)] overflow-y-auto overscroll-contain border-t pt-2"
        >
          <p
            class="font-outfit text-text-brand-muted mb-1 text-xs font-bold tracking-widest uppercase"
          >
            Current document properties
          </p>
          <dl class="font-inter text-xs">
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
          onclick={() => loadContent(activeUrl, activeHintTitle, extractorName)}
          class="font-inter text-hister-teal cursor-pointer text-xs hover:underline"
        >
          Show current
        </button>
      </div>
    {/if}
    <ScrollArea class="min-h-0 flex-1">
      {#if showVersions}
        <div class="flex flex-col divide-y divide-[var(--border-brand-muted)] p-4">
          {#each versions as v}
            <div class="py-4 first:pt-0 last:pb-0">
              <div class="mb-2 flex items-center justify-between gap-2">
                <p class="font-inter text-xs font-semibold tracking-wide uppercase">
                  {formatTimestamp(versionTimestamp(v.created_at))}
                </p>
                <button
                  onclick={() => loadContent(activeUrl, activeHintTitle, extractorName, v.id)}
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
          {/each}
        </div>
      {:else}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div
          class="preview-content font-inter text-text-brand-secondary prose dark:prose-invert prose-a:text-hister-teal w-full max-w-[60em] p-4 text-sm"
          onclick={handleContentClick}
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
</style>
